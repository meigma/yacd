package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDoer serves canned responses keyed by URL. A missing URL yields 404.
type fakeDoer struct {
	bodies map[string][]byte
}

func (f fakeDoer) Do(req *http.Request) (*http.Response, error) {
	body, ok := f.bodies[req.URL.String()]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}, nil
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// previewBodies returns canned responses for the preview profile whose
// config.json matches the pinned digest. It includes every required file
// (config, the four genesis files, topology, and checkpoints) and omits the
// optional peer-snapshot.
func previewBodies(t *testing.T, config []byte) map[string][]byte {
	t.Helper()
	require.Equal(t, previewConfigSHA256, digest(config), "test config must match the pinned preview digest")
	return map[string][]byte{
		bookBase + "preview/config.json":          config,
		bookBase + "preview/byron-genesis.json":   []byte(`{"byron":true}`),
		bookBase + "preview/shelley-genesis.json": []byte(`{"shelley":true}`),
		bookBase + "preview/alonzo-genesis.json":  []byte(`{"alonzo":true}`),
		bookBase + "preview/conway-genesis.json":  []byte(`{"conway":true}`),
		bookBase + "preview/topology.json":        []byte(`{"Producers":[]}`),
		bookBase + "preview/checkpoints.json":     []byte(`[]`),
	}
}

// pinnedPreviewConfig loads the embedded preview config bytes whose digest the
// pin table records, so the happy path verifies against the real pinned value.
func pinnedPreviewConfig(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "cardano", "publicnet", "profiles", "preview", "config.json"))
	require.NoError(t, err)
	return raw
}

func TestRunWritesVerifiedArtifacts(t *testing.T) {
	t.Parallel()

	config := pinnedPreviewConfig(t)
	dir := t.TempDir()
	err := Run(t.Context(), Options{Profile: "preview", OutputDir: dir},
		fakeDoer{bodies: previewBodies(t, config)}, io.Discard)
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(dir, "config.json"))
	require.NoError(t, err)
	assert.Equal(t, config, got, "config.json is written verbatim after digest verification")
	assert.FileExists(t, filepath.Join(dir, "byron-genesis.json"))
}

func TestRunFailsOnPinnedDigestMismatch(t *testing.T) {
	t.Parallel()

	bodies := previewBodies(t, pinnedPreviewConfig(t))
	bodies[bookBase+"preview/config.json"] = []byte("tampered config")

	dir := t.TempDir()
	err := Run(t.Context(), Options{Profile: "preview", OutputDir: dir},
		fakeDoer{bodies: bodies}, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pinned digest mismatch")
	assert.NoFileExists(t, filepath.Join(dir, "config.json"), "no file is written when verification fails")
}

func TestRunFailsWhenRequiredFileMissing(t *testing.T) {
	t.Parallel()

	// byron-genesis and checkpoints are both required for preview (preview
	// config.json references CheckpointsFileHash), so a missing download fails
	// the run rather than producing an incomplete artifact directory.
	for _, missing := range []string{"byron-genesis.json", "checkpoints.json"} {
		t.Run(missing, func(t *testing.T) {
			t.Parallel()
			bodies := previewBodies(t, pinnedPreviewConfig(t))
			delete(bodies, bookBase+"preview/"+missing)

			err := Run(t.Context(), Options{Profile: "preview", OutputDir: t.TempDir()},
				fakeDoer{bodies: bodies}, io.Discard)
			require.Error(t, err)
			assert.Contains(t, err.Error(), missing)
		})
	}
}

func TestRunToleratesMissingOptionalFile(t *testing.T) {
	t.Parallel()

	// peer-snapshot is the only optional preview file; it is absent from the
	// canned bodies, so its download 404s and the run still succeeds.
	dir := t.TempDir()
	err := Run(t.Context(), Options{Profile: "preview", OutputDir: dir},
		fakeDoer{bodies: previewBodies(t, pinnedPreviewConfig(t))}, io.Discard)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "peer-snapshot.json"))
	assert.FileExists(t, filepath.Join(dir, "checkpoints.json"), "required checkpoints is written")
}

func TestRunRejectsUnknownProfile(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), Options{Profile: "nope", OutputDir: t.TempDir()}, fakeDoer{}, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown profile")
}
