package publicnet

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"testing"

	"github.com/meigma/yacd/internal/cardano/publicpins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublicPinsMatchEmbeddedProfiles recomputes every pinned digest in the
// shared publicpins registry against the bytes still embedded in this package
// and asserts they match. It is the safety net for the F0 transport redesign:
// once the embed is removed (so the controller fetches these files at runtime),
// the pins become the only authentication of the downloaded bytes, so a
// transcription error in publicpins would silently weaken integrity. While the
// embedded copies still exist they are the ground truth — this test fails the
// build on any mismatch.
//
// It also asserts the source filename mapping (config.json -> configuration.yaml
// etc.) resolves to a real embedded asset, and that unpinned/optional files
// (peer-snapshot) carry no digest.
func TestPublicPinsMatchEmbeddedProfiles(t *testing.T) {
	// assetName maps a YACD artifact key back to the on-disk profile asset
	// filename so the test can read the embedded bytes the pin authenticates.
	assetName := map[string]string{}
	for _, f := range append(append([]profileFile{}, requiredProfileFiles...), append(optionalProfileFiles, mithrilProfileFiles...)...) {
		assetName[f.artifactKey] = f.assetPath
	}

	for _, name := range publicpins.Known() {
		profile, ok := publicpins.Lookup(name)
		require.True(t, ok, "profile %q must be known", name)

		for _, file := range profile.Files {
			asset, mapped := assetName[file.ArtifactKey]
			require.True(t, mapped, "%s: artifact key %q has no embedded asset mapping", name, file.ArtifactKey)

			raw, err := profileAssets.ReadFile(path.Join("profiles", name, asset))
			if !file.Pinned {
				// Unpinned files (peer-snapshot) must not carry a digest. The
				// embedded copy may or may not exist; we only assert the pin is
				// absent.
				assert.Empty(t, file.SHA256, "%s: unpinned file %q must not carry a digest", name, file.ArtifactKey)
				continue
			}
			require.NoError(t, err, "%s: read embedded asset %q", name, asset)

			sum := sha256.Sum256(raw)
			want := hex.EncodeToString(sum[:])
			assert.Equalf(t, want, file.SHA256,
				"%s: pinned digest for %q does not match embedded %s", name, file.ArtifactKey, asset)
		}
	}
}

// TestPublicPinsCoverCuratedProfiles asserts the shared registry and this
// package's curated definitions describe the same profiles and the same Mithril
// wiring, so the two cannot drift before publicnet is switched over to
// publicpins.
func TestPublicPinsCoverCuratedProfiles(t *testing.T) {
	for _, name := range publicpins.Known() {
		_, ok := curatedProfiles[name]
		assert.Truef(t, ok, "publicpins profile %q missing from curatedProfiles", name)
	}
	assert.Len(t, publicpins.Known(), len(curatedProfiles))

	mainnet, ok := publicpins.Lookup("mainnet")
	require.True(t, ok)
	require.NotNil(t, mainnet.Mithril, "mainnet must define mithril pins")
	assert.Equal(t, releaseMainnetMithrilAggregator, mainnet.Mithril.AggregatorEndpoint)
}
