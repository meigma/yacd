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
metadata:
  name: devnet
  namespace: yacd-dev
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

	network, err := CardanoNetwork(environment, "override")
	require.NoError(t, err)
	assert.Equal(t, "yacd.meigma.io/v1alpha1", network.APIVersion)
	assert.Equal(t, "CardanoNetwork", network.Kind)
	assert.Equal(t, "devnet", network.Name)
	assert.Equal(t, "override", network.Namespace)
	assert.Equal(t, int64(42), network.Spec.Local.NetworkMagic)
}

func TestNamespacePrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		override   string
		configured string
		fallback   string
		want       string
	}{
		{name: "override", override: "flag", configured: "config", fallback: "kube", want: "flag"},
		{name: "configured", configured: "config", fallback: "kube", want: "config"},
		{name: "fallback", fallback: "kube", want: "kube"},
		{name: "default", want: "default"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Namespace(tc.override, tc.configured, tc.fallback))
		})
	}
}

func TestManifestRendersInspectableYAML(t *testing.T) {
	t.Parallel()

	environment, err := devconfig.Load(strings.NewReader(validConfig))
	require.NoError(t, err)
	network, err := CardanoNetwork(environment, "")
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
