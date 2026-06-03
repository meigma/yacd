package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDevnetLifecycleLive exercises the full devnet happy path against real
// Docker: it provisions a k3d cluster, installs the operator, applies the
// default funded network, queries the chain tip through the in-pod exec path,
// proves a re-run is an idempotent no-op, and tears the cluster down.
//
// It is opt-in: set YACD_DEVNET_LIVE=1 (with Docker running) to run it. It is
// skipped in the default suite because it needs Docker, the network, and
// several minutes. It isolates its kubeconfig and cluster-state record under a
// temp dir so it does not touch the developer's real ~/.kube/config or XDG
// state; the k3d binary cache (XDG_DATA_HOME) is left alone so the pinned
// binary is reused. Pointing KUBECONFIG at a temp file also exercises the
// KUBECONFIG-aware path: k3d merges the managed context there and the cluster
// adapter must report that same file (not ~/.kube/config) for the operator
// install and apply to succeed.
func TestDevnetLifecycleLive(t *testing.T) {
	if os.Getenv("YACD_DEVNET_LIVE") == "" {
		t.Skip("set YACD_DEVNET_LIVE=1 (with Docker running) to run the live devnet lifecycle test")
	}

	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "kubeconfig"))
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Always attempt teardown, even if an assertion fails mid-way.
	t.Cleanup(func() {
		var out, errOut bytes.Buffer
		_ = runDevnetLive(context.Background(), &out, &errOut, "devnet", "down")
	})

	// Bring up the devnet.
	var upOut, upErr bytes.Buffer
	require.NoError(t, runDevnetLive(context.Background(), &upOut, &upErr, "devnet"),
		"devnet bring-up: %s", upErr.String())
	assert.Contains(t, upOut.String(), "devnet is ready.")
	assert.Contains(t, upOut.String(), "addr_test", "expected a funded wallet address in the output")

	// status reflects a present, healthy cluster.
	var statusOut, statusErr bytes.Buffer
	require.NoError(t, runDevnetLive(context.Background(), &statusOut, &statusErr, "devnet", "status"))
	assert.Contains(t, statusOut.String(), cluster.ManagedContext)

	// The in-pod exec path can query the chain tip on the default magic.
	var execOut, execErr bytes.Buffer
	require.NoError(t,
		runDevnetLive(context.Background(), &execOut, &execErr,
			"exec", devnetNetworkName, "--", "cardano-cli", "query", "tip", "--testnet-magic", "42"),
		"exec query tip: %s", execErr.String())

	// A re-run is an idempotent no-op that still reports ready.
	var rerunOut, rerunErr bytes.Buffer
	require.NoError(t, runDevnetLive(context.Background(), &rerunOut, &rerunErr, "devnet"),
		"devnet re-run: %s", rerunErr.String())
	assert.Contains(t, rerunOut.String(), "devnet is ready.")

	// Teardown removes the cluster.
	var downOut, downErr bytes.Buffer
	require.NoError(t, runDevnetLive(context.Background(), &downOut, &downErr, "devnet", "down"),
		"devnet down: %s", downErr.String())
	assert.Contains(t, downOut.String(), "devnet cluster removed.")
}

// runDevnetLive runs the real command tree (default production factories) with
// the given args, capturing stdout and stderr.
func runDevnetLive(ctx context.Context, out, errOut *bytes.Buffer, args ...string) error {
	root := NewRootCommand(Options{
		Out:   out,
		Err:   errOut,
		Viper: viper.New(),
	})
	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}
