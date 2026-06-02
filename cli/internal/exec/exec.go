package exec

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes an external command, capturing its output streams.
type Runner interface {
	// Run executes name with args under ctx and returns the captured stdout and
	// stderr. A non-zero exit yields a non-nil error that wraps the underlying
	// *exec.ExitError (reachable via errors.As) and includes the trimmed stderr
	// for a human-readable message.
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// OS returns the default Runner backed by os/exec.
func OS() Runner {
	return osRunner{}
}

type osRunner struct{}

func (osRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("exec %s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), stderr.Bytes(), err
}
