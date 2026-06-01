package artifactset

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

func TestRenderPublicConnection(t *testing.T) {
	t.Parallel()

	raw, err := RenderPublicConnection(PublicConnection{
		Profile:              "preview",
		NetworkMagic:         2,
		RequiresNetworkMagic: true,
		Era:                  "conway",
		Files: map[string]string{
			"configuration":   networkartifacts.ConfigurationKey,
			"primaryTopology": networkartifacts.PrimaryTopologyKey,
		},
	})
	require.NoError(t, err)
	assert.True(t, len(raw) > 0 && raw[len(raw)-1] == '\n', "output ends with a trailing newline")

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))

	assert.Equal(t, networkartifacts.SchemaVersion, doc["schemaVersion"])

	network, ok := doc["network"].(map[string]any)
	require.True(t, ok, "network object is present")
	assert.Equal(t, "public", network["mode"])
	assert.Equal(t, "preview", network["profile"])
	assert.EqualValues(t, 2, network["networkMagic"])
	assert.Equal(t, true, network["requiresNetworkMagic"])
	assert.Equal(t, "conway", network["era"])

	// The public document carries no cluster-runtime identity: name, namespace,
	// and the node-to-node endpoint are filled in by the operator, not fetch.
	assert.NotContains(t, network, "name")
	assert.NotContains(t, network, "namespace")
	assert.NotContains(t, doc, "primaryNodeToNode")

	files, ok := doc["files"].(map[string]any)
	require.True(t, ok, "files map is present")
	assert.Equal(t, networkartifacts.ConfigurationKey, files["configuration"])
	assert.Equal(t, networkartifacts.PrimaryTopologyKey, files["primaryTopology"])
}

func TestRenderPublicConnectionOmitsEmptyEra(t *testing.T) {
	t.Parallel()

	raw, err := RenderPublicConnection(PublicConnection{
		Profile:      "mainnet",
		NetworkMagic: 764824073,
		Files:        map[string]string{"configuration": networkartifacts.ConfigurationKey},
	})
	require.NoError(t, err)

	var doc struct {
		Network map[string]any `json:"network"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))
	assert.NotContains(t, doc.Network, "era", "an empty era is omitted from the document")
	assert.Equal(t, false, doc.Network["requiresNetworkMagic"])
}

func TestRenderPublicConnectionRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	cases := map[string]PublicConnection{
		"missing profile": {
			NetworkMagic: 2,
			Files:        map[string]string{"configuration": networkartifacts.ConfigurationKey},
		},
		"missing network magic": {
			Profile: "preview",
			Files:   map[string]string{"configuration": networkartifacts.ConfigurationKey},
		},
		"empty files": {
			Profile:      "preview",
			NetworkMagic: 2,
		},
	}
	for name, conn := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := RenderPublicConnection(conn)
			require.Error(t, err)
		})
	}
}
