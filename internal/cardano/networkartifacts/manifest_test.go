package networkartifacts_test

import (
	"encoding/json"
	"testing"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildManifestDigestsFiles(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		networkartifacts.ConfigurationKey: []byte("config-bytes"),
		networkartifacts.ConnectionKey:    []byte(`{"connection":true}`),
		// A manifest must never list itself, even if a stale copy is present.
		networkartifacts.ManifestKey: []byte("ignored"),
	}

	manifest := networkartifacts.BuildManifest(files)

	assert.Equal(t, networkartifacts.SchemaVersion, manifest.SchemaVersion)
	assert.NotContains(t, manifest.Files, networkartifacts.ManifestKey, "manifest must not list itself")
	assert.Equal(t, networkartifacts.FileDigest([]byte("config-bytes")), manifest.Files[networkartifacts.ConfigurationKey])
	assert.Len(t, manifest.Files, 2)
	for _, digest := range manifest.Files {
		assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, digest)
	}
}

func TestManifestVerify(t *testing.T) {
	t.Parallel()

	manifest := networkartifacts.BuildManifest(map[string][]byte{
		networkartifacts.ConfigurationKey: []byte("config-bytes"),
	})

	require.NoError(t, manifest.Verify(networkartifacts.ConfigurationKey, []byte("config-bytes")))

	err := manifest.Verify(networkartifacts.ConfigurationKey, []byte("tampered"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest mismatch")

	err = manifest.Verify(networkartifacts.ByronGenesisKey, []byte("anything"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not listed")
}

func TestManifestJSONIsDeterministicAndRoundTrips(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		networkartifacts.ConfigurationKey: []byte("c"),
		networkartifacts.ByronGenesisKey:  []byte("b"),
		networkartifacts.ConnectionKey:    []byte("x"),
	}
	manifest := networkartifacts.BuildManifest(files)

	first, err := manifest.JSON()
	require.NoError(t, err)
	second, err := manifest.JSON()
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second), "manifest JSON must be deterministic")

	var decoded networkartifacts.Manifest
	require.NoError(t, json.Unmarshal(first, &decoded))
	assert.Equal(t, manifest, decoded)

	assert.Equal(t,
		[]string{networkartifacts.ByronGenesisKey, networkartifacts.ConfigurationKey, networkartifacts.ConnectionKey},
		decoded.SortedFileNames())
}

// TestManifestKeyIsServable guards the serve-sidecar prerequisite: the manifest
// must be in the artifact key allowlist (optional keys) so the serve sidecar
// exposes GET /manifest.json by construction.
func TestManifestKeyIsServable(t *testing.T) {
	t.Parallel()

	assert.Contains(t, networkartifacts.OptionalKeys(), networkartifacts.ManifestKey)
	assert.NotContains(t, networkartifacts.RequiredKeys(), networkartifacts.ManifestKey,
		"manifest is discovery metadata, not a required mounted artifact")
}
