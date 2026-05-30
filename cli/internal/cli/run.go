package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
)

// newRunCommand wires `yacd run NAME [-- command...]`, the primary host-access
// verb. It establishes scoped port-forwards to the network's chain-API
// endpoints, injects the YACD_* environment, and execs the command (or the
// user's $SHELL when none is given) on the host with that environment, tearing
// the forwards down when the command exits. The command's exit status is
// propagated to the caller's shell so a test runner's failure code survives the
// wrapper, and the test runner itself stays YACD-agnostic — it only reads env
// vars. Use `--` before commands that take their own flags.
func newRunCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run NAME [-- command [args...]]",
		Short: "Run a command with the environment wired to a YACD network",
		Long: `Run a command on the host with the YACD_* environment wired to a network's
forwarded chain-API endpoints, or drop into your $SHELL when no command is given.

Put -- before any command that takes its own flags so they are passed through
to the command instead of being parsed by yacd.`,
		Example: `  # Run a test suite against the network (note the -- before the command)
  yacd run my-net -- go test ./e2e/...

  # Open a shell with the YACD_* environment set
  yacd run my-net`,
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
			command := runCommandLine(args[1:])

			kubeClient, err := commandContext.kubeClientFactory(kube.Config{
				Kubeconfig: runtimeConfig.Kubeconfig,
				Context:    runtimeConfig.KubeContext,
			})
			if err != nil {
				return err
			}

			connected, err := connectNetwork(cmd.Context(), kubeClient, namespace, name)
			if err != nil {
				return err
			}
			defer func() {
				_ = connected.Close()
			}()

			if _, err := fmt.Fprintf(commandContext.err, "Connected to %s/%s; running %s\n", namespace, name, strings.Join(command, " ")); err != nil {
				return fmt.Errorf("write run status: %w", err)
			}

			return runChild(cmd.Context(), commandContext, connected, command, namespace, name)
		},
	}

	return cmd
}

// runCommandLine returns the command to run: the provided args, or the user's
// $SHELL (falling back to /bin/sh) when no command was given, mirroring the
// drop-into-a-shell convenience.
func runCommandLine(args []string) []string {
	if len(args) > 0 {
		return args
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}

	return []string{shell}
}

// runChild execs the command on the host with the connected session's YACD_*
// environment, cancelling it if the forwards drop first. The child inherits the
// CLI's stdio and shares its process group, so an interactive Ctrl-C reaches it
// directly. A non-zero exit is propagated silently (the child already wrote its
// own output to the inherited streams); a dropped forward is reported instead
// of a bare exit code, because the child most likely failed for that reason. In
// the rare tie where the child succeeds at the same instant the forward drops,
// the drop is still reported — a conservative bias toward surfacing the lost
// connection.
func runChild(ctx context.Context, commandContext *commandContext, connected *connectedSession, command []string, namespace string, name string) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-connected.Done():
			cancel()
		case <-runCtx.Done():
		}
	}()

	child := exec.CommandContext(runCtx, command[0], command[1:]...)
	child.Env = append(os.Environ(), connected.env...)
	child.Stdin = commandContext.in
	child.Stdout = commandContext.out
	child.Stderr = commandContext.err

	runErr := child.Run()

	select {
	case <-connected.Done():
		return newExitError(1, fmt.Sprintf("lost connection to %s/%s while running: %v", namespace, name, connected.Err()))
	default:
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return newExitError(processExitCode(exitErr.ProcessState), "")
		}

		return fmt.Errorf("run %s: %w", command[0], runErr)
	}

	return nil
}

// processExitCode maps a finished process state to a shell-style exit code: the
// process's own code when it exited normally, or 128+signal when a signal
// killed it (so an interrupted child reports the conventional 130 for SIGINT).
func processExitCode(state *os.ProcessState) int {
	if state.Exited() {
		return state.ExitCode()
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}

	return 1
}
