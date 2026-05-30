package cli

import (
	"errors"
	"fmt"
)

// exitError carries a process exit code the CLI must return verbatim. The run
// and exec commands produce it to propagate a child or in-pod process's exit
// status to the caller's shell, so a test runner's failure code survives the
// `yacd run`/`yacd exec` wrapper. An empty msg marks a silent failure — the
// process already wrote its own output to the inherited terminal, so the
// top-level handler returns the code without printing anything; a non-empty msg
// (for example a lost port-forward) is surfaced before exiting.
type exitError struct {
	code int
	msg  string
}

// newExitError builds an exitError. Pass an empty msg for a silent
// process-exit code and a message for CLI-originated failures that still carry
// a specific exit code.
func newExitError(code int, msg string) *exitError {
	return &exitError{code: code, msg: msg}
}

func (e *exitError) Error() string {
	if e.msg != "" {
		return e.msg
	}

	return fmt.Sprintf("exit status %d", e.code)
}

// ResolveExit maps a command error to the process exit code and whether the
// top-level handler should print it to stderr. It centralises the exit-code
// policy so cmd/yacd/main.go stays a thin shim: a nil error exits 0 silently;
// an exitError carries its own code and decides whether it is printed; any
// other error exits 1 and is printed.
func ResolveExit(err error) (code int, printErr bool) {
	if err == nil {
		return 0, false
	}

	var exit *exitError
	if errors.As(err, &exit) {
		return exit.code, exit.msg != ""
	}

	return 1, true
}
