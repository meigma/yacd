package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	utilexec "k8s.io/client-go/util/exec"
)

const (
	// cardanoNodeContainerName is the primary node container the exec verb runs
	// commands in. It is pinned here as a deliberate, stable coupling to the
	// operator's fixed primary Pod layout rather than imported from
	// controller-internal vocabulary: the CLI discovers the Pod from the
	// operator's published node-to-node Service, but the container name is not
	// part of any published contract.
	cardanoNodeContainerName = "cardano-node"

	// cardanoNodeSocketPath is the in-pod cardano-node IPC socket path exec
	// exposes as CARDANO_NODE_SOCKET_PATH, so socket-bound tools such as
	// cardano-cli can reach the node. Pinned for the same reason as the
	// container name; it mirrors the controller's fixed socket mount.
	cardanoNodeSocketPath = "/ipc/node.socket"
)

// newExecCommand wires `yacd exec NAME -- command...`, the in-pod path for
// socket-bound tooling. cardano-cli talks to the node over a Unix domain socket
// rather than TCP, so a port-forward cannot expose it; exec runs the command
// inside the primary node Pod (kubectl-exec semantics) with
// CARDANO_NODE_SOCKET_PATH and the YACD_* variables set in the pod environment.
// The command's exit code is propagated to the caller.
func newExecCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec NAME -- command [args...]",
		Short: "Run a command inside the primary node Pod (for socket-bound tools)",
		Long: `Run a command inside the primary cardano-node Pod with kubectl-exec semantics,
for tools that reach the node over its local Unix socket (notably cardano-cli)
rather than over a forwarded TCP port. CARDANO_NODE_SOCKET_PATH and the YACD_*
variables are set in the pod environment, so cardano-cli finds the socket
automatically.

Put -- before the command so its flags are passed through to it. The command is
run directly, not through a shell, so $VAR references in arguments are NOT
expanded; wrap the command in a shell (sh -c '...') to interpolate the YACD_*
variables into arguments.

When stdin and stdout are a terminal, exec attaches an interactive TTY (raw
mode), so "yacd exec my-net -- sh" opens an interactive shell in the node Pod;
piped or non-terminal invocations (CI) stream without a TTY.`,
		Example: `  # cardano-cli reads CARDANO_NODE_SOCKET_PATH from the pod environment:
  yacd exec my-net -- cardano-cli query tip --testnet-magic 42

  # To interpolate YACD_* variables into arguments, run a shell explicitly:
  yacd exec my-net -- sh -c 'cardano-cli query tip --testnet-magic "$YACD_NETWORK_MAGIC"'

  # From a terminal, open an interactive shell in the node Pod:
  yacd exec my-net -- sh`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeConfig, err := loadRuntimeConfig(commandContext.viper)
			if err != nil {
				return err
			}
			name, namespace, err := resolveIdentity(args[0], runtimeConfig)
			if err != nil {
				return err
			}
			command := args[1:]
			if len(command) == 0 {
				return fmt.Errorf("a command is required: yacd exec NAME -- command [args...]")
			}

			kubeClient, err := commandContext.kubeClientFactory(kube.Config{
				Kubeconfig: runtimeConfig.Kubeconfig,
				Context:    runtimeConfig.KubeContext,
			})
			if err != nil {
				return err
			}

			network, err := kubeClient.GetCardanoNetwork(cmd.Context(), namespace, name)
			if err != nil {
				return err
			}
			if err := requireReady(network, namespace, name); err != nil {
				return err
			}
			podName, err := kubeClient.PrimaryPodName(cmd.Context(), namespace, name)
			if err != nil {
				return err
			}

			stdin, stdinFile, stdinIsTTY := execStdin(commandContext.in)
			// Drive an interactive TTY only when both stdin and stdout are real
			// terminals: requesting a remote PTY while stdout is redirected would
			// garble the captured output.
			interactive := stdinIsTTY && isTerminalWriter(commandContext.out)
			request := kube.ExecRequest{
				Namespace: namespace,
				PodName:   podName,
				Container: cardanoNodeContainerName,
				Command:   wrapExecCommand(podEnv(network, cardanoNodeSocketPath), command),
				Stdin:     stdin,
				Stdout:    commandContext.out,
				Stderr:    commandContext.err,
				TTY:       interactive,
			}
			if interactive {
				restore, sizeQueue, err := enterRawTerminal(cmd.Context(), stdinFile)
				if err != nil {
					return fmt.Errorf("prepare interactive terminal: %w", err)
				}
				defer restore()
				request.SizeQueue = sizeQueue
			}
			if err := kubeClient.Exec(cmd.Context(), request); err != nil {
				return execExitError(err)
			}

			return nil
		},
	}

	return cmd
}

// wrapExecCommand builds the in-pod argv that sets the YACD_* / socket
// environment before the user's command. It prepends the env binary with each
// KEY=VALUE as a separate argument, so the values are passed as a deterministic
// argv array and never through a shell — PodExecOptions.Command is executed
// directly (no shell) and has no environment field, so a shell wrapper would be
// a pointless quoting/injection hazard.
func wrapExecCommand(env []string, command []string) []string {
	wrapped := make([]string, 0, len(env)+len(command)+1)
	wrapped = append(wrapped, "env")
	wrapped = append(wrapped, env...)
	wrapped = append(wrapped, command...)

	return wrapped
}

// execStdin attaches the caller's stdin and, when stdin is a real terminal,
// surfaces the underlying *os.File so the caller can enter raw mode and read the
// terminal size. isTTY is true only when a terminal file was found; scripted or
// piped invocations (CI) stream without one.
func execStdin(in io.Reader) (stdin io.Reader, file *os.File, isTTY bool) {
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return in, f, true
	}

	return in, nil, false
}

// isTerminalWriter reports whether w is backed by a terminal file descriptor.
func isTerminalWriter(w io.Writer) bool {
	file, ok := w.(*os.File)

	return ok && term.IsTerminal(int(file.Fd()))
}

// enterRawTerminal puts the terminal into raw mode and returns a restore
// function plus a TerminalSizeQueue seeded with the current size and refreshed
// on SIGWINCH. restore is idempotent and safe to defer: it stops SIGWINCH
// delivery, ends the size queue (so the adapter's resize stream terminates),
// and restores the cooked terminal state — on normal return, on the exec error
// path, and during a panic unwind. SIGWINCH is unix-only; releases target
// darwin/linux, so a Windows build would need this wiring behind a build tag.
func enterRawTerminal(ctx context.Context, file *os.File) (func(), kube.TerminalSizeQueue, error) {
	fd := int(file.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, nil, err
	}

	queue := newTerminalSizeQueue(fd)
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)

	go func() {
		for {
			select {
			case <-winch:
				queue.refresh()
			case <-queue.done:
				return
			case <-ctx.Done():
				queue.close()

				return
			}
		}
	}()

	var once sync.Once
	restore := func() {
		once.Do(func() {
			signal.Stop(winch)
			queue.close()
			_ = term.Restore(fd, oldState)
		})
	}

	return restore, queue, nil
}

// terminalSizeQueue implements kube.TerminalSizeQueue from the host terminal. It
// seeds the current size and coalesces SIGWINCH-driven updates through a
// capacity-one channel so a burst never blocks the signal goroutine; closing it
// makes Next return ok=false so the adapter ends the resize stream.
type terminalSizeQueue struct {
	fd        int
	sizes     chan kube.TerminalSize
	done      chan struct{}
	closeOnce sync.Once
}

func newTerminalSizeQueue(fd int) *terminalSizeQueue {
	queue := &terminalSizeQueue{
		fd:    fd,
		sizes: make(chan kube.TerminalSize, 1),
		done:  make(chan struct{}),
	}
	queue.refresh() // seed the initial size so the remote PTY starts correctly sized

	return queue
}

// Next returns the next terminal size, or ok=false once the queue is closed.
func (q *terminalSizeQueue) Next() (kube.TerminalSize, bool) {
	select {
	case size := <-q.sizes:
		return size, true
	case <-q.done:
		return kube.TerminalSize{}, false
	}
}

// refresh reads the current terminal size and enqueues it, dropping any stale
// pending size so the newest one wins. It never blocks: there is a single
// producer (the seed call, then the SIGWINCH goroutine), so the
// drain-then-send keeps the capacity-one channel holding the latest size.
func (q *terminalSizeQueue) refresh() {
	width, height, err := term.GetSize(q.fd)
	if err != nil {
		return
	}
	size := kube.TerminalSize{Width: clampDimension(width), Height: clampDimension(height)}
	select {
	case q.sizes <- size:
	default:
		select {
		case <-q.sizes:
		default:
		}
		select {
		case q.sizes <- size:
		default:
		}
	}
}

func (q *terminalSizeQueue) close() {
	q.closeOnce.Do(func() { close(q.done) })
}

// clampDimension narrows a terminal dimension to the uint16 the remote PTY
// protocol uses, guarding the conversion against absurd values.
func clampDimension(value int) uint16 {
	if value < 0 {
		return 0
	}
	if value > 65535 {
		return 65535
	}

	return uint16(value)
}

// execExitError maps a remote exec failure to an exitError carrying the remote
// process's exit code (silent: the command already streamed its own output to
// the inherited streams). Non-exit failures (for example a transport error) are
// returned unchanged.
func execExitError(err error) error {
	var exitErr utilexec.ExitError
	if errors.As(err, &exitErr) && exitErr.Exited() {
		return newExitError(exitErr.ExitStatus(), "")
	}

	return err
}
