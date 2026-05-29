package render

import (
	"strings"
	"testing"

	"github.com/meigma/yacd/cli/internal/devconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validConfig = `
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: local
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 2Gi
    local:
      networkMagic: 42
      era: conway
      timing:
        slotLength: 100ms
        epochLength: 500
      topology:
        pools:
          count: 1
`

func TestCardanoNetworkRendersDeveloperConfig(t *testing.T) {
	t.Parallel()

	environment, err := devconfig.Load(strings.NewReader(validConfig))
	require.NoError(t, err)

	network, err := CardanoNetwork(environment, "devnet", "yacd-dev")
	require.NoError(t, err)
	assert.Equal(t, "yacd.meigma.io/v1alpha1", network.APIVersion)
	assert.Equal(t, "CardanoNetwork", network.Kind)
	assert.Equal(t, "devnet", network.Name)
	assert.Equal(t, "yacd-dev", network.Namespace)
	assert.Equal(t, int64(42), network.Spec.Local.NetworkMagic)
}

func TestCardanoNetworkTrimsIdentity(t *testing.T) {
	t.Parallel()

	environment, err := devconfig.Load(strings.NewReader(validConfig))
	require.NoError(t, err)

	network, err := CardanoNetwork(environment, "  devnet  ", "  yacd-dev  ")
	require.NoError(t, err)
	assert.Equal(t, "devnet", network.Name)
	assert.Equal(t, "yacd-dev", network.Namespace)
}

func TestCardanoNetworkRequiresIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		network   string
		namespace string
		wantErr   string
	}{
		{name: "empty name", network: "", namespace: "yacd-dev", wantErr: "name is required"},
		{name: "blank name", network: "   ", namespace: "yacd-dev", wantErr: "name is required"},
		{name: "empty namespace", network: "devnet", namespace: "", wantErr: "namespace is required"},
		{name: "blank namespace", network: "devnet", namespace: "  ", wantErr: "namespace is required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			environment, err := devconfig.Load(strings.NewReader(validConfig))
			require.NoError(t, err)

			_, err = CardanoNetwork(environment, tc.network, tc.namespace)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestCardanoNetworkRequiresEnvironment(t *testing.T) {
	t.Parallel()

	_, err := CardanoNetwork(nil, "devnet", "yacd-dev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "developer environment is required")
}

func TestManifestRendersInspectableYAML(t *testing.T) {
	t.Parallel()

	environment, err := devconfig.Load(strings.NewReader(validConfig))
	require.NoError(t, err)
	network, err := CardanoNetwork(environment, "devnet", "yacd-dev")
	require.NoError(t, err)

	manifest, err := Manifest(network)
	require.NoError(t, err)

	output := string(manifest)
	for _, want := range []string{
		"apiVersion: yacd.meigma.io/v1alpha1",
		"kind: CardanoNetwork",
		"name: devnet",
		"namespace: yacd-dev",
		"networkMagic: 42",
	} {
		assert.Contains(t, output, want)
	}
}
