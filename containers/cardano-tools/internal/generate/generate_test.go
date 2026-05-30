package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/meigma/yacd/internal/cardano/localnet"
)

// planForDir builds a localnet plan rooted at dir so the test can manipulate
// its env directory.
func planForDir(t *testing.T, dir string) localnet.Plan {
	t.Helper()
	spec := localnet.DefaultSpec()
	spec.Paths.StateDir = dir
	spec.Paths.EnvDir = filepath.Join(dir, "env")
	plan, err := localnet.BuildPlan(spec)
	require.NoError(t, err)
	return plan
}

// writeFile creates parent dirs and writes content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestInspectEnv(t *testing.T) {
	t.Parallel()

	t.Run("absent when env dir does not exist", func(t *testing.T) {
		t.Parallel()
		plan := planForDir(t, t.TempDir())
		state, err := inspectEnv(plan)
		require.NoError(t, err)
		require.Equal(t, envAbsent, state)
	})

	t.Run("absent when env dir is empty", func(t *testing.T) {
		t.Parallel()
		plan := planForDir(t, t.TempDir())
		require.NoError(t, os.MkdirAll(plan.Layout.EnvDir, 0o755))
		state, err := inspectEnv(plan)
		require.NoError(t, err)
		require.Equal(t, envAbsent, state)
	})

	t.Run("matches when manifest and config match the plan", func(t *testing.T) {
		t.Parallel()
		plan := planForDir(t, t.TempDir())
		want, err := marshalManifest(plan)
		require.NoError(t, err)
		writeFile(t, plan.Layout.ManifestFile, string(want))
		writeFile(t, plan.Layout.ConfigFile, "ConwayGenesisFile: conway-genesis.json\n")

		state, err := inspectEnv(plan)
		require.NoError(t, err)
		require.Equal(t, envMatches, state)
	})

	t.Run("conflicts when manifest differs", func(t *testing.T) {
		t.Parallel()
		plan := planForDir(t, t.TempDir())
		writeFile(t, plan.Layout.ManifestFile, `{"schemaVersion":"different"}`)
		writeFile(t, plan.Layout.ConfigFile, "x")

		state, err := inspectEnv(plan)
		require.NoError(t, err)
		require.Equal(t, envConflicts, state)
	})

	t.Run("conflicts when env populated without a manifest", func(t *testing.T) {
		t.Parallel()
		plan := planForDir(t, t.TempDir())
		writeFile(t, filepath.Join(plan.Layout.EnvDir, "byron-genesis.json"), "{}")

		state, err := inspectEnv(plan)
		require.NoError(t, err)
		require.Equal(t, envConflicts, state)
	})

	t.Run("conflicts when manifest present but config missing", func(t *testing.T) {
		t.Parallel()
		plan := planForDir(t, t.TempDir())
		want, err := marshalManifest(plan)
		require.NoError(t, err)
		writeFile(t, plan.Layout.ManifestFile, string(want))

		state, err := inspectEnv(plan)
		require.NoError(t, err)
		require.Equal(t, envConflicts, state)
	})
}

// TestRunRefusesConflictingEnv proves the conflict path returns an error
// without invoking cardano-testnet (no binary is available in the test env).
func TestRunRefusesConflictingEnv(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plan := planForDir(t, dir)
	writeFile(t, filepath.Join(plan.Layout.EnvDir, "byron-genesis.json"), "{}")

	spec := localnet.DefaultSpec()
	spec.Paths.StateDir = dir
	spec.Paths.EnvDir = plan.Layout.EnvDir
	err := Run(t.Context(), Options{Spec: spec}, os.Stderr)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to overwrite")
}
