package render

import (
	"strings"
	"testing"

	"github.com/meigma/yacd/cli/internal/devconfig"
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
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}

	network, err := CardanoNetwork(environment, "override")
	if err != nil {
		t.Fatalf("CardanoNetwork returned an error: %v", err)
	}

	if got, want := network.APIVersion, "yacd.meigma.io/v1alpha1"; got != want {
		t.Fatalf("apiVersion = %q, want %q", got, want)
	}
	if got, want := network.Kind, "CardanoNetwork"; got != want {
		t.Fatalf("kind = %q, want %q", got, want)
	}
	if got, want := network.Name, "devnet"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := network.Namespace, "override"; got != want {
		t.Fatalf("namespace = %q, want %q", got, want)
	}
	if got, want := network.Spec.Local.NetworkMagic, int64(42); got != want {
		t.Fatalf("network magic = %d, want %d", got, want)
	}
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Namespace(tt.override, tt.configured, tt.fallback); got != tt.want {
				t.Fatalf("Namespace() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestManifestRendersInspectableYAML(t *testing.T) {
	t.Parallel()

	environment, err := devconfig.Load(strings.NewReader(validConfig))
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	network, err := CardanoNetwork(environment, "")
	if err != nil {
		t.Fatalf("CardanoNetwork returned an error: %v", err)
	}

	manifest, err := Manifest(network)
	if err != nil {
		t.Fatalf("Manifest returned an error: %v", err)
	}

	output := string(manifest)
	for _, want := range []string{
		"apiVersion: yacd.meigma.io/v1alpha1",
		"kind: CardanoNetwork",
		"name: devnet",
		"namespace: yacd-dev",
		"networkMagic: 42",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("manifest missing %q:\n%s", want, output)
		}
	}
}
