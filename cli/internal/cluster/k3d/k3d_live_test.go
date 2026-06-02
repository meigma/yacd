package k3d_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/meigma/yacd/cli/internal/cluster/k3d"
	"github.com/meigma/yacd/cli/internal/exec"
	"github.com/meigma/yacd/cli/internal/toolbin/ghrelease"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureClusterLive provisions a REAL k3d cluster (Docker required) using the
// real pinned k3d binary, exercising create -> idempotent no-op -> out-of-band
// delete -> heal. It uses a test-only cluster name so it never clobbers a real
// devnet. Opt-in: set YACD_CLUSTER_LIVE=1; skipped in the default suite.
func TestEnsureClusterLive(t *testing.T) {
	if os.Getenv("YACD_CLUSTER_LIVE") == "" {
		t.Skip("set YACD_CLUSTER_LIVE=1 (with Docker running) to run the live k3d cluster test")
	}

	const name = "yacd-livetest"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	resolver := ghrelease.New(ghrelease.DefaultK3dPin, t.TempDir(), ghrelease.DefaultHTTPClient())
	prov := k3d.New(resolver, exec.OS())
	spec := cluster.Spec{Name: name, K3sImage: cluster.K3sImage, Timeout: 3 * time.Minute}

	t.Cleanup(func() {
		_ = prov.DeleteCluster(context.Background(), name)
	})

	info, err := prov.EnsureCluster(ctx, spec)
	require.NoError(t, err)
	assert.Equal(t, "k3d-"+name, info.Context)
	assert.True(t, info.Running)

	// Idempotent: a second EnsureCluster is a no-op.
	_, err = prov.EnsureCluster(ctx, spec)
	require.NoError(t, err)

	status, err := prov.Status(ctx, name)
	require.NoError(t, err)
	assert.True(t, status.Exists && status.Running && status.Healthy, "cluster should be healthy")

	// Out-of-band delete, then EnsureCluster heals (recreates).
	require.NoError(t, prov.DeleteCluster(ctx, name))
	gone, err := prov.Status(ctx, name)
	require.NoError(t, err)
	assert.False(t, gone.Exists, "cluster should be gone after delete")

	// Deleting an absent cluster is tolerated.
	require.NoError(t, prov.DeleteCluster(ctx, name))

	_, err = prov.EnsureCluster(ctx, spec)
	require.NoError(t, err)
	healed, err := prov.Status(ctx, name)
	require.NoError(t, err)
	assert.True(t, healed.Exists && healed.Healthy, "cluster should heal")
}
