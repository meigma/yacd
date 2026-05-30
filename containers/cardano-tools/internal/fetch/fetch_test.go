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
// config.json matches the pinned digest, leaving the optional files absent.
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

	bodies := previewBodies(t, pinnedPreviewConfig(t))
	delete(bodies, bookBase+"preview/byron-genesis.json")

	err := Run(t.Context(), Options{Profile: "preview", OutputDir: t.TempDir()},
		fakeDoer{bodies: bodies}, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "byron-genesis.json")
}

func TestRunToleratesMissingOptionalFile(t *testing.T) {
	t.Parallel()

	// The preview optional files (checkpoints, peer-snapshot) are absent from
	// the canned bodies, so their downloads 404; the run still succeeds.
	dir := t.TempDir()
	err := Run(t.Context(), Options{Profile: "preview", OutputDir: dir},
		fakeDoer{bodies: previewBodies(t, pinnedPreviewConfig(t))}, io.Discard)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "checkpoints.json"))
}

func TestRunRejectsUnknownProfile(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), Options{Profile: "nope", OutputDir: t.TempDir()}, fakeDoer{}, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown profile")
}
