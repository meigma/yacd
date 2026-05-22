package devconfig

import (
	"strings"
	"testing"
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

func TestLoadReadsEnvironmentConfig(t *testing.T) {
	t.Parallel()

	environment, err := Load(strings.NewReader(validConfig))
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}

	if got, want := environment.Metadata.Name, "devnet"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := environment.Metadata.Namespace, "yacd-dev"; got != want {
		t.Fatalf("namespace = %q, want %q", got, want)
	}
	if got, want := environment.Spec.Network.Local.NetworkMagic, int64(42); got != want {
		t.Fatalf("network magic = %d, want %d", got, want)
	}
}

func TestLoadRejectsUnknownTopLevelFields(t *testing.T) {
	t.Parallel()

	_, err := Load(strings.NewReader(validConfig + "\nunknown: true\n"))
	if err == nil {
		t.Fatal("Load succeeded, want error")
	}
	if got := err.Error(); !strings.Contains(got, "unknown") {
		t.Fatalf("error = %q, want unknown field message", got)
	}
}

func TestLoadRejectsOmittedConcreteCRDDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "node version",
			config:  strings.Replace(validConfig, "      version: \"11.0.1\"\n", "", 1),
			wantErr: "spec.network.node.version",
		},
		{
			name:    "node port",
			config:  strings.Replace(validConfig, "      port: 3001\n", "", 1),
			wantErr: "spec.network.node.port",
		},
		{
			name:    "local network magic",
			config:  strings.Replace(validConfig, "      networkMagic: 42\n", "", 1),
			wantErr: "spec.network.local.networkMagic",
		},
		{
			name: "kupo image",
			config: validConfig + `    chainAPI:
      kupo:
        enabled: true
        port: 1442
`,
			wantErr: "spec.network.chainAPI.kupo.image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(tt.config))
			if err == nil {
				t.Fatal("Load succeeded, want error")
			}
			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Fatalf("error = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

func TestValidateRequiresEnvelope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "api version",
			config:  strings.Replace(validConfig, APIVersion, "example.com/v1", 1),
			wantErr: "apiVersion",
		},
		{
			name:    "kind",
			config:  strings.Replace(validConfig, Kind, "Other", 1),
			wantErr: "kind",
		},
		{
			name:    "name",
			config:  strings.Replace(validConfig, "name: devnet", "name: \"\"", 1),
			wantErr: "metadata.name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(tt.config))
			if err == nil {
				t.Fatal("Load succeeded, want error")
			}
			if got := err.Error(); !strings.Contains(got, tt.wantErr) {
				t.Fatalf("error = %q, want %q", got, tt.wantErr)
			}
		})
	}
}
