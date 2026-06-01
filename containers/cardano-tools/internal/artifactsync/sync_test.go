package artifactsync

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bundleFiles is a representative staged artifact set (no manifest.json — that
// is derived). The bytes are placeholders; sync verifies them against the
// served manifest, which is computed from exactly these bytes.
func bundleFiles() map[string][]byte {
	return map[string][]byte{
		"configuration.yaml":   []byte("NodeConfig: true\n"),
		"shelley-genesis.json": []byte(`{"shelley":true}`),
		"connection.json":      []byte(`{"schemaVersion":"v1"}`),
	}
}

// servedManifest renders the manifest cardano-tools serve would expose for the
// given files (manifest.json excluded from its own list).
func servedManifest(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	raw, err := networkartifacts.BuildManifest(files).JSON()
	require.NoError(t, err)
	return raw
}

// bundleServer serves manifest.json and each artifact file by name, 404 for
// anything else. The returned manifest bytes are what /manifest.json serves.
func bundleServer(t *testing.T, files map[string][]byte) (*httptest.Server, []byte) {
	t.Helper()
	manifestRaw := servedManifest(t, files)
	mux := http.NewServeMux()
	mux.HandleFunc("/"+networkartifacts.ManifestKey, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(manifestRaw)
	})
	for name, body := range files {
		mux.HandleFunc("/"+name, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, manifestRaw
}

// redirectRefusingClient mirrors the client the sync CLI builds: it refuses to
// follow redirects so a 3xx surfaces as a non-200 download failure.
func redirectRefusingClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func TestRunWritesAndVerifiesEveryFile(t *testing.T) {
	t.Parallel()

	files := bundleFiles()
	srv, manifestRaw := bundleServer(t, files)
	dir := t.TempDir()

	err := Run(t.Context(), Options{ServeURL: srv.URL, OutputDir: dir}, srv.Client(), io.Discard)
	require.NoError(t, err)

	for name, want := range files {
		got, readErr := os.ReadFile(filepath.Join(dir, name))
		require.NoErrorf(t, readErr, "expected %s written", name)
		assert.Equal(t, want, got, "%s written verbatim after verification", name)
	}

	// manifest.json is written verbatim (the served bytes), so the output dir
	// is self-describing and re-verifiable.
	gotManifest, err := os.ReadFile(filepath.Join(dir, networkartifacts.ManifestKey))
	require.NoError(t, err)
	assert.Equal(t, manifestRaw, gotManifest, "served manifest.json is written verbatim")
}

func TestRunFailsOnDigestMismatch(t *testing.T) {
	t.Parallel()

	files := bundleFiles()
	srv, _ := bundleServer(t, files)
	// Serve a tampered configuration.yaml whose digest no longer matches the
	// manifest computed over the original bytes.
	mux := http.NewServeMux()
	manifestRaw := servedManifest(t, files)
	mux.HandleFunc("/"+networkartifacts.ManifestKey, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(manifestRaw) })
	mux.HandleFunc("/configuration.yaml", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("tampered")) })
	mux.HandleFunc("/shelley-genesis.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(files["shelley-genesis.json"]) })
	mux.HandleFunc("/connection.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(files["connection.json"]) })
	tampered := httptest.NewServer(mux)
	t.Cleanup(tampered.Close)
	srv.Close()

	dir := t.TempDir()
	err := Run(t.Context(), Options{ServeURL: tampered.URL, OutputDir: dir}, tampered.Client(), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest mismatch")
	assert.NoFileExists(t, filepath.Join(dir, "configuration.yaml"), "no file is written when verification fails")
}

func TestRunFailsWhenManifestMissing(t *testing.T) {
	t.Parallel()

	// A server that 404s everything, including manifest.json.
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	err := Run(t.Context(), Options{ServeURL: srv.URL, OutputDir: t.TempDir()}, srv.Client(), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), networkartifacts.ManifestKey)
}

func TestRunFailsWhenListedFileMissing(t *testing.T) {
	t.Parallel()

	files := bundleFiles()
	manifestRaw := servedManifest(t, files)
	// Serve the manifest but omit one listed file so its GET 404s.
	mux := http.NewServeMux()
	mux.HandleFunc("/"+networkartifacts.ManifestKey, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(manifestRaw) })
	mux.HandleFunc("/configuration.yaml", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(files["configuration.yaml"]) })
	mux.HandleFunc("/connection.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(files["connection.json"]) })
	// shelley-genesis.json intentionally not registered → 404.
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := Run(t.Context(), Options{ServeURL: srv.URL, OutputDir: t.TempDir()}, srv.Client(), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shelley-genesis.json")
}

func TestRunRefusesRedirect(t *testing.T) {
	t.Parallel()

	// The manifest endpoint 302-redirects elsewhere; the redirect-refusing
	// client surfaces it as a non-200, so the run fails closed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "http://example.invalid/manifest.json", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	err := Run(t.Context(), Options{ServeURL: srv.URL, OutputDir: t.TempDir()}, redirectRefusingClient(), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status")
}

func TestRunRequiresServeURL(t *testing.T) {
	t.Parallel()

	err := Run(t.Context(), Options{ServeURL: "  ", OutputDir: t.TempDir()}, redirectRefusingClient(), io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "serve URL is required")
}

func TestRunDryRunWritesNothing(t *testing.T) {
	t.Parallel()

	files := bundleFiles()
	srv, _ := bundleServer(t, files)
	dir := t.TempDir()

	var out bytes.Buffer
	err := Run(t.Context(), Options{ServeURL: srv.URL, OutputDir: dir, DryRun: true}, srv.Client(), &out)
	require.NoError(t, err)

	assert.Contains(t, out.String(), "serve-url: "+srv.URL)
	assert.Contains(t, out.String(), "configuration.yaml")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "dry-run writes no files")
}
