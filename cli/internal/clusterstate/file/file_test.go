package file_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/clusterstate/file"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMissingReturnsNotFound(t *testing.T) {
	store := file.New(filepath.Join(t.TempDir(), "state"))

	_, ok, err := store.Load()
	require.NoError(t, err)
	assert.False(t, ok, "absent record is not an error")
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store := file.New(dir)

	record := clusterstate.Record{
		ClusterName:    "yacd",
		Context:        "k3d-yacd",
		PriorContext:   "docker-desktop",
		K3dVersion:     "v5.9.0",
		KubeconfigPath: "/home/dev/.kube/config",
	}
	require.NoError(t, store.Save(record))

	got, ok, err := store.Load()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, record, got)

	info, err := os.Stat(filepath.Join(dir, "cluster.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "record must be 0600")
	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "state dir must be 0700")
}

func TestClearIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	store := file.New(dir)
	require.NoError(t, store.Save(clusterstate.Record{ClusterName: "yacd"}))

	require.NoError(t, store.Clear())
	_, ok, err := store.Load()
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, store.Clear(), "clearing an absent record is a no-op")
}

func TestLoadCorruptRecordIsError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cluster.json"), []byte("{not json"), 0o600))

	_, _, err := file.New(dir).Load()
	require.Error(t, err)
}

func TestLockSerializes(t *testing.T) {
	store := file.New(filepath.Join(t.TempDir(), "state"))

	release, err := store.Lock(context.Background())
	require.NoError(t, err)

	// A second acquisition while held must not succeed before its deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err = store.Lock(ctx)
	require.Error(t, err, "lock is held; second acquisition must time out")

	require.NoError(t, release())

	// After release, the lock is acquirable again.
	release2, err := store.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, release2())
}
