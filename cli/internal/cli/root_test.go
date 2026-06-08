package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testBuildInfo() BuildInfo {
	return BuildInfo{Version: "0.1.0", Commit: "abc1234", Date: "2026-05-22T10:00:00Z"}
}

func TestVersionSubcommandPrintsBuildMetadata(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{Out: &stdout, Err: &stderr, Build: testBuildInfo()})
	root.SetArgs([]string{"version"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, "yacd 0.1.0 (abc1234) built 2026-05-22T10:00:00Z\n", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestVersionFlagIsRemoved(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{Out: &stdout, Err: &stderr, Build: testBuildInfo()})
	root.SetArgs([]string{"--version"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err, "--version must no longer be a registered flag")
	assert.Contains(t, err.Error(), "unknown flag")
	assert.Empty(t, stdout.String(), "no version string prints for the removed flag")
}

func TestVersionSubcommandPrintsDataUnderQuiet(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{Out: &stdout, Err: &stderr, Build: testBuildInfo()})
	root.SetArgs([]string{"-q", "version"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, "yacd 0.1.0 (abc1234) built 2026-05-22T10:00:00Z\n", stdout.String(),
		"data still prints to stdout under --quiet")
}

func TestVerboseShorthandIsVerbosityNotVersion(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{Out: &stdout, Err: &stderr, Build: testBuildInfo()})
	root.SetArgs([]string{"-vvv", "version"})

	require.NoError(t, root.ExecuteContext(context.Background()),
		"-v is the verbosity count, not --version")
	assert.Contains(t, stdout.String(), "yacd 0.1.0")
}
