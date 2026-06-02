package ghrelease

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveLiveFetchesPinnedK3d actually downloads the pinned k3d release from
// GitHub, follows the CDN redirect, verifies the embedded digest, and runs the
// binary. It is the one path the mocked tests cannot cover, so it is opt-in:
// set YACD_TOOLBIN_LIVE=1 to run it. It is skipped in the default suite.
func TestResolveLiveFetchesPinnedK3d(t *testing.T) {
	if os.Getenv("YACD_TOOLBIN_LIVE") == "" {
		t.Skip("set YACD_TOOLBIN_LIVE=1 to run the live k3d download test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dir := t.TempDir()
	path, err := New(DefaultK3dPin, dir, DefaultHTTPClient()).Resolve(ctx)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.NotZero(t, info.Size(), "downloaded binary must be non-empty")

	out, err := exec.CommandContext(ctx, path, "version").CombinedOutput()
	require.NoError(t, err, "k3d version output: %s", out)
	assert.Contains(t, string(out), k3dVersion, "binary must report the pinned version")
}
