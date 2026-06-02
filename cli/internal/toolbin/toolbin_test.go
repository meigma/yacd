package toolbin_test

import (
	"path/filepath"
	"testing"

	"github.com/meigma/yacd/cli/internal/toolbin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultDirHonorsXDGDataHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/custom/data")

	dir, err := toolbin.DefaultDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/custom/data", "yacd", "bin"), dir)
}

func TestDefaultDirFallsBackToHomeLocalShare(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/dev")

	dir, err := toolbin.DefaultDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/home/dev", ".local", "share", "yacd", "bin"), dir)
}
