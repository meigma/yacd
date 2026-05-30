package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

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
variables into arguments.`,
		Example: `  # cardano-cli reads CARDANO_NODE_SOCKET_PATH from the pod environment:
  yacd exec my-net -- cardano-cli query tip --testnet-magic 42

  # To interpolate YACD_* variables into arguments, run a shell explicitly:
  yacd exec my-net -- sh -c 'cardano-cli query tip --testnet-magic "$YACD_NETWORK_MAGIC"'`,
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

			stdin, tty := execStdin(commandContext.in)
			request := kube.ExecRequest{
				Namespace: namespace,
				PodName:   podName,
				Container: cardanoNodeContainerName,
				Command:   wrapExecCommand(podEnv(network, cardanoNodeSocketPath), command),
				Stdin:     stdin,
				Stdout:    commandContext.out,
				Stderr:    commandContext.err,
				TTY:       tty,
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

// execStdin attaches the caller's stdin and requests a TTY only when stdin is a
// real terminal, so interactive sessions get a TTY while scripted or piped
// invocations (CI) stream without one.
func execStdin(in io.Reader) (io.Reader, bool) {
	if file, ok := in.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return in, true
	}

	return in, false
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
