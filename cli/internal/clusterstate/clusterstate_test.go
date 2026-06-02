package clusterstate_test

import (
	"path/filepath"
	"testing"

	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultDirHonorsXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/custom/state")

	dir, err := clusterstate.DefaultDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/custom/state", "yacd"), dir)
}

func TestDefaultDirFallsBackToHomeLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/dev")

	dir, err := clusterstate.DefaultDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/home/dev", ".local", "state", "yacd"), dir)
}
