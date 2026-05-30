package artifactset

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/meigma/yacd/internal/ctrlkit/artifacts"
)

// validInput returns a minimal, valid Build input covering every required
// source plus the optional dijkstra genesis.
func validInput() Input {
	return Input{
		Network: NetworkIdentity{
			Name:           "demo",
			Namespace:      "demo",
			Mode:           "local",
			Era:            "conway",
			NodeToNodeHost: "demo-node.demo.svc.cluster.local",
			NodeToNodePort: 3001,
			NodeToNodeURL:  "tcp://demo-node.demo.svc.cluster.local:3001",
		},
		Manifest: Manifest{
			NetworkMagic: 42,
			Fingerprint:  "sha256:abc",
			Raw:          `{"inputs":{"networkMagic":42},"fingerprint":{"value":"sha256:abc"}}`,
		},
		Artifacts: map[string]string{
			networkartifacts.ConfigurationKey:   "config",
			networkartifacts.ByronGenesisKey:    "byron",
			networkartifacts.ShelleyGenesisKey:  "shelley",
			networkartifacts.AlonzoGenesisKey:   "alonzo",
			networkartifacts.ConwayGenesisKey:   "conway",
			networkartifacts.PrimaryTopologyKey: "topology",
		},
	}
}

func TestBuildAssemblesRequiredKeysAndConnection(t *testing.T) {
	t.Parallel()

	set, err := Build(validInput())
	require.NoError(t, err)

	// Every required key plus the synthesized manifest and connection are set.
	for _, key := range []string{
		networkartifacts.ConfigurationKey,
		networkartifacts.ByronGenesisKey,
		networkartifacts.ShelleyGenesisKey,
		networkartifacts.AlonzoGenesisKey,
		networkartifacts.ConwayGenesisKey,
		networkartifacts.PrimaryTopologyKey,
		networkartifacts.PlanManifestKey,
		networkartifacts.ConnectionKey,
	} {
		assert.Contains(t, set.Data, key, "expected data key %s", key)
	}
	// The omitted optional dijkstra genesis is absent from data but still owned.
	assert.NotContains(t, set.Data, networkartifacts.DijkstraGenesisKey)
	assert.Contains(t, set.KnownKeys, networkartifacts.DijkstraGenesisKey)

	assert.Equal(t, networkartifacts.SchemaVersion, set.Annotations.SchemaVersion)
	assert.Equal(t, "sha256:abc", set.Annotations.LocalnetFingerprint)
	assert.Contains(t, set.Data[networkartifacts.ConnectionKey], `"schemaVersion": "`+networkartifacts.SchemaVersion+`"`)
}

// TestBuildDataHashMatchesControllerHasher is the cross-contract guard: the
// hash this tool stamps must be byte-identical to the one the controller
// recomputes from live ConfigMap data. Both must come from the same shared
// function, so a divergence here means the producer/verifier contract drifted.
func TestBuildDataHashMatchesControllerHasher(t *testing.T) {
	t.Parallel()

	set, err := Build(validInput())
	require.NoError(t, err)

	assert.Equal(t, artifacts.ComputeDataHash(set.Data), set.Annotations.DataHash)
	assert.True(t, artifacts.ValidDataHash(set.Annotations.DataHash))

	// The assembled set must also pass the controller's allowlist validation
	// under the shared contract.
	contract := artifacts.Contract{
		RequiredKeys: networkartifacts.RequiredKeys(),
		OptionalKeys: networkartifacts.OptionalKeys(),
	}
	configMap := &corev1.ConfigMap{Data: set.Data}
	require.NoError(t, artifacts.ValidateConfigMapData(configMap, contract, set.Annotations.DataHash))
}

func TestBuildRejectsMissingRequiredAndUnknownKeys(t *testing.T) {
	t.Parallel()

	t.Run("missing required artifact", func(t *testing.T) {
		t.Parallel()
		input := validInput()
		delete(input.Artifacts, networkartifacts.ConwayGenesisKey)
		_, err := Build(input)
		require.Error(t, err)
		assert.Contains(t, err.Error(), networkartifacts.ConwayGenesisKey)
	})

	t.Run("unknown artifact key", func(t *testing.T) {
		t.Parallel()
		input := validInput()
		input.Artifacts["surprise.json"] = "x"
		_, err := Build(input)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "surprise.json")
	})

	t.Run("invalid manifest", func(t *testing.T) {
		t.Parallel()
		input := validInput()
		input.Manifest.NetworkMagic = 0
		_, err := Build(input)
		require.Error(t, err)
	})

	t.Run("invalid network port", func(t *testing.T) {
		t.Parallel()
		input := validInput()
		input.Network.NodeToNodePort = 0
		_, err := Build(input)
		require.Error(t, err)
	})
}

// TestSourcesAreSafePaths is an invariant guard: every registered source must
// stay within the artifact directory and never reference key material.
func TestSourcesAreSafePaths(t *testing.T) {
	t.Parallel()

	for _, src := range Sources() {
		assert.NoErrorf(t, validateSourcePath(src.Key, src.RelativePath), "source %s", src.Key)
	}
}
