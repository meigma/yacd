package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/meigma/yacd/internal/cardano/publicpins"
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

// previewProfile is the profile under test in this file's fetch cases.
const previewProfile = "preview"

// profileSourceURL returns the preview download URL for a source file, sourced
// from the shared publicpins registry so the test and production agree on URLs.
func profileSourceURL(t *testing.T, sourceName string) string {
	t.Helper()
	p, ok := publicpins.Lookup(previewProfile)
	require.Truef(t, ok, "profile %q must be known", previewProfile)
	for _, file := range p.Files {
		if file.SourceName == sourceName {
			return file.URL(previewProfile)
		}
	}
	require.FailNowf(t, "missing source file", "profile %q has no source file %q", previewProfile, sourceName)
	return ""
}

// pinnedDigest returns the pinned sha256 for a preview source file, asserting
// the file is pinned. It lets the happy path verify against the real pin.
func pinnedDigest(t *testing.T, sourceName string) string {
	t.Helper()
	p, ok := publicpins.Lookup(previewProfile)
	require.Truef(t, ok, "profile %q must be known", previewProfile)
	for _, file := range p.Files {
		if file.SourceName == sourceName {
			require.Truef(t, file.Pinned, "%s/%s is expected to be pinned", previewProfile, sourceName)
			return file.SHA256
		}
	}
	require.FailNowf(t, "missing source file", "profile %q has no source file %q", previewProfile, sourceName)
	return ""
}

// previewBodies returns canned responses for the preview profile whose
// config.json matches the pinned digest. It includes every required file
// (config, the four genesis files, topology, and checkpoints) and omits the
// optional peer-snapshot.
func previewBodies(t *testing.T, config []byte) map[string][]byte {
	t.Helper()
	require.Equal(t, pinnedDigest(t, "config.json"), digest(config), "test config must match the pinned preview digest")
	topology := embeddedProfileFile(t, "preview", "topology.json")
	require.Equal(t, pinnedDigest(t, "topology.json"), digest(topology), "test topology must match the pinned preview digest")
	return map[string][]byte{
		profileSourceURL(t, "config.json"):          config,
		profileSourceURL(t, "byron-genesis.json"):   []byte(`{"byron":true}`),
		profileSourceURL(t, "shelley-genesis.json"): []byte(`{"shelley":true}`),
		profileSourceURL(t, "alonzo-genesis.json"):  []byte(`{"alonzo":true}`),
		profileSourceURL(t, "conway-genesis.json"):  []byte(`{"conway":true}`),
		profileSourceURL(t, "topology.json"):        topology,
		profileSourceURL(t, "checkpoints.json"):     []byte(`[]`),
	}
}

// embeddedProfileFile loads a file from the checked-in public profile assets so
// tests can verify against the real pinned digests.
func embeddedProfileFile(t *testing.T, profile, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "internal", "cardano", "publicnet", "profiles", profile, name))
	require.NoError(t, err)
	return raw
}

// pinnedPreviewConfig loads the embedded preview config bytes whose digest the
// pin table records, so the happy path verifies against the real pinned value.
func pinnedPreviewConfig(t *testing.T) []byte {
	return embeddedProfileFile(t, "preview", "config.json")
}

func TestRunWritesVerifiedArtifacts(t *testing.T) {
	t.Parallel()

	config := pinnedPreviewConfig(t)
	dir := t.TempDir()
	err := Run(t.Context(), Options{Profile: "preview", OutputDir: dir},
		fakeDoer{bodies: previewBodies(t, config)}, io.Discard)
	require.NoError(t, err)

	// The book's config.json is written under the YACD artifact key
	// configuration.yaml (and source config.json is NOT left behind).
	got, err := os.ReadFile(filepath.Join(dir, "configuration.yaml"))
	require.NoError(t, err)
	assert.Equal(t, config, got, "config bytes are written verbatim under configuration.yaml after digest verification")
	assert.NoFileExists(t, filepath.Join(dir, "config.json"))
	assert.FileExists(t, filepath.Join(dir, "byron-genesis.json"))
	assert.FileExists(t, filepath.Join(dir, "primary-topology.json"), "topology.json is written under its artifact key")
}

func TestRunFailsOnPinnedDigestMismatch(t *testing.T) {
	t.Parallel()

	bodies := previewBodies(t, pinnedPreviewConfig(t))
	bodies[profileSourceURL(t, "config.json")] = []byte("tampered config")

	dir := t.TempDir()
	err := Run(t.Context(), Options{Profile: "preview", OutputDir: dir},
		fakeDoer{bodies: bodies}, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pinned digest mismatch")
	assert.NoFileExists(t, filepath.Join(dir, "configuration.yaml"), "no file is written when verification fails")
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
			delete(bodies, profileSourceURL(t, missing))

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

// TestRunWritesConnectionAndManifest verifies that after the pinned files are
// downloaded, Run completes the served directory with connection.json and a
// manifest.json that covers every served file (itself excluded) and verifies.
func TestRunWritesConnectionAndManifest(t *testing.T) {
	t.Parallel()

	config := pinnedPreviewConfig(t)
	dir := t.TempDir()
	err := Run(t.Context(), Options{Profile: "preview", OutputDir: dir},
		fakeDoer{bodies: previewBodies(t, config)}, io.Discard)
	require.NoError(t, err)

	// connection.json is a public document: it records the profile and the
	// static network magic, omits cluster-runtime identity, and maps connection
	// keys to served filenames.
	connRaw, err := os.ReadFile(filepath.Join(dir, networkartifacts.ConnectionKey))
	require.NoError(t, err)
	var conn struct {
		SchemaVersion string `json:"schemaVersion"`
		Network       struct {
			Mode         string `json:"mode"`
			Profile      string `json:"profile"`
			NetworkMagic int64  `json:"networkMagic"`
		} `json:"network"`
		Files map[string]string `json:"files"`
	}
	require.NoError(t, json.Unmarshal(connRaw, &conn))
	assert.Equal(t, networkartifacts.SchemaVersion, conn.SchemaVersion)
	assert.Equal(t, "public", conn.Network.Mode)
	assert.Equal(t, "preview", conn.Network.Profile)
	assert.EqualValues(t, 2, conn.Network.NetworkMagic, "preview network magic from publicpins")
	assert.Equal(t, networkartifacts.ConfigurationKey, conn.Files["configuration"])
	assert.Equal(t, networkartifacts.PrimaryTopologyKey, conn.Files["primaryTopology"])
	// The skipped optional peer-snapshot is not referenced.
	assert.NotContains(t, conn.Files, "peerSnapshot")

	// manifest.json verifies every served file, including connection.json, and
	// excludes itself.
	manifestRaw, err := os.ReadFile(filepath.Join(dir, networkartifacts.ManifestKey))
	require.NoError(t, err)
	var manifest networkartifacts.Manifest
	require.NoError(t, json.Unmarshal(manifestRaw, &manifest))
	assert.Equal(t, networkartifacts.SchemaVersion, manifest.SchemaVersion)
	assert.NotContains(t, manifest.Files, networkartifacts.ManifestKey, "manifest never lists itself")

	for _, name := range manifest.SortedFileNames() {
		content, readErr := os.ReadFile(filepath.Join(dir, name))
		require.NoErrorf(t, readErr, "manifest names an unreadable file %s", name)
		assert.NoErrorf(t, manifest.Verify(name, content), "manifest digest must match %s", name)
	}
	// connection.json is one of the verified files.
	assert.Contains(t, manifest.Files, networkartifacts.ConnectionKey)
	assert.NoError(t, manifest.Verify(networkartifacts.ConnectionKey, connRaw))
}

// TestRunDryRunWritesNothing confirms dry-run prints the manifest and leaves the
// output directory empty: no artifacts, no connection.json, no manifest.json.
func TestRunDryRunWritesNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var out bytes.Buffer
	err := Run(t.Context(), Options{Profile: "preview", OutputDir: dir, DryRun: true},
		fakeDoer{}, &out)
	require.NoError(t, err)

	assert.Contains(t, out.String(), "profile: preview")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "dry-run writes no files")
}
