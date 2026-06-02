package exec_test

import (
	"context"
	"testing"

	"github.com/meigma/yacd/cli/internal/exec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOSRunnerCapturesStdout(t *testing.T) {
	// `go env GOOS` is available wherever the test runs and prints to stdout.
	stdout, _, err := exec.OS().Run(context.Background(), "go", "env", "GOOS")
	require.NoError(t, err)
	assert.NotEmpty(t, stdout)
}

func TestOSRunnerWrapsStderrOnFailure(t *testing.T) {
	// An unknown go subcommand exits non-zero and writes to stderr.
	_, stderr, err := exec.OS().Run(context.Background(), "go", "definitely-not-a-command")
	require.Error(t, err)
	assert.Contains(t, string(stderr), "unknown command")
	assert.Contains(t, err.Error(), "unknown command", "error must surface stderr")
}
