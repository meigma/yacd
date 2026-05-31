package publicnet

import (
	"encoding/json"
	"path"
	"testing"

	"github.com/meigma/yacd/internal/cardano/publicpins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublicPinsStaticIdentityMatchesEmbedded verifies that the static
// NetworkMagic / RequiresNetworkMagic facts recorded in publicpins match what
// the still-embedded shelley-genesis and node config actually say. These facts
// are recorded statically in publicpins because, once the embed is removed, the
// operator builds the profile plan without the genesis/config bytes and can no
// longer parse them. While the embedded copies still exist they are the ground
// truth — this test fails the build on any transcription error before the embed
// is shrunk away.
func TestPublicPinsStaticIdentityMatchesEmbedded(t *testing.T) {
	for _, name := range publicpins.Known() {
		profile, ok := publicpins.Lookup(name)
		require.Truef(t, ok, "profile %q must be known", name)

		shelleyRaw, err := profileAssets.ReadFile(path.Join("profiles", name, "shelley-genesis.json"))
		require.NoErrorf(t, err, "%s: read embedded shelley-genesis", name)
		var shelley struct {
			NetworkMagic int64 `json:"networkMagic"`
		}
		require.NoErrorf(t, json.Unmarshal(shelleyRaw, &shelley), "%s: parse embedded shelley-genesis", name)
		assert.Equalf(t, shelley.NetworkMagic, profile.NetworkMagic,
			"%s: publicpins NetworkMagic does not match embedded shelley-genesis", name)

		configRaw, err := profileAssets.ReadFile(path.Join("profiles", name, "config.json"))
		require.NoErrorf(t, err, "%s: read embedded config.json", name)
		var config struct {
			RequiresNetworkMagic string `json:"RequiresNetworkMagic"`
		}
		require.NoErrorf(t, json.Unmarshal(configRaw, &config), "%s: parse embedded config.json", name)

		var wantRequires bool
		switch config.RequiresNetworkMagic {
		case "RequiresMagic":
			wantRequires = true
		case "RequiresNoMagic":
			wantRequires = false
		default:
			t.Fatalf("%s: unexpected RequiresNetworkMagic %q in embedded config", name, config.RequiresNetworkMagic)
		}
		assert.Equalf(t, wantRequires, profile.RequiresNetworkMagic,
			"%s: publicpins RequiresNetworkMagic does not match embedded config.json", name)
	}
}
