package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONFlagIsRemoved(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{Out: &stdout, Err: &stderr, Build: testBuildInfo()})
	root.SetArgs([]string{"list", "--json"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err, "--json must no longer be a registered flag")
	assert.Contains(t, err.Error(), "unknown flag")
}

func TestLoadRuntimeConfigRejectsUnknownOutput(t *testing.T) {
	t.Parallel()

	vp := viper.New()
	vp.Set("output", "xml")

	_, err := loadRuntimeConfig(globalFlagsCommand(t), vp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported output format")
}

func TestLoadRuntimeConfigOutputDefaultsToText(t *testing.T) {
	t.Parallel()

	config, err := loadRuntimeConfig(globalFlagsCommand(t), viper.New())
	require.NoError(t, err)
	assert.Equal(t, formatText, config.OutputFormat)
}

// TestOutputEnvIsReadAndValidated proves YACD_OUTPUT flows through the env
// binding: a bogus value is rejected in PersistentPreRunE before the command
// runs, which can only happen if the env var is read for the output format.
func TestOutputEnvIsReadAndValidated(t *testing.T) {
	t.Setenv("YACD_OUTPUT", "bogus")

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{Out: &stdout, Err: &stderr, Build: testBuildInfo()})
	root.SetArgs([]string{"version"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported output format")
	assert.Empty(t, stdout.String(), "the command must not run when output is invalid")
}
