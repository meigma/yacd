package k3d_test

import (
	"context"
	"net"
	"os"
	"strconv"
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
	// Test-only host/node ports (distinct from a real devnet's 1337/1442) so the
	// live test exercises the real "--port HOST:NODEPORT@loadbalancer" path
	// against real k3d without clashing with a running devnet.
	mappings := []cluster.PortMapping{
		{HostPort: 18337, NodePort: 31337},
		{HostPort: 18442, NodePort: 31442},
	}
	spec := cluster.Spec{Name: name, K3sImage: cluster.K3sImage, Timeout: 3 * time.Minute, PortMappings: mappings}

	t.Cleanup(func() {
		_ = prov.DeleteCluster(context.Background(), name)
	})

	info, err := prov.EnsureCluster(ctx, spec)
	require.NoError(t, err)
	assert.Equal(t, "k3d-"+name, info.Context)
	assert.True(t, info.Running)

	// Real k3d accepted the --port mappings: the serverlb publishes each host
	// port (its proxy listener accepts TCP even before a NodePort backend
	// exists), proving the mapping syntax routes a host port to the cluster.
	for _, m := range mappings {
		addr := net.JoinHostPort("localhost", strconv.Itoa(int(m.HostPort)))
		conn, derr := net.DialTimeout("tcp", addr, 10*time.Second)
		require.NoErrorf(t, derr, "mapped host port %d should be published by the serverlb", m.HostPort)
		require.NoError(t, conn.Close())
	}

	// Idempotent: a second EnsureCluster is a no-op.
	_, err = prov.EnsureCluster(ctx, spec)
	require.NoError(t, err)

	status, err := prov.Status(ctx, name, "")
	require.NoError(t, err)
	assert.True(t, status.Exists && status.Running && status.Healthy, "cluster should be healthy")

	// Out-of-band delete, then EnsureCluster heals (recreates).
	require.NoError(t, prov.DeleteCluster(ctx, name))
	gone, err := prov.Status(ctx, name, "")
	require.NoError(t, err)
	assert.False(t, gone.Exists, "cluster should be gone after delete")

	// Deleting an absent cluster is tolerated.
	require.NoError(t, prov.DeleteCluster(ctx, name))

	_, err = prov.EnsureCluster(ctx, spec)
	require.NoError(t, err)
	healed, err := prov.Status(ctx, name, "")
	require.NoError(t, err)
	assert.True(t, healed.Exists && healed.Healthy, "cluster should heal")
}
