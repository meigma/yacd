package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// setValues is a small alias for a Viper key/value bag used by tests
// to build inputs to [Load] without touching real flags or env vars.
type setValues map[string]any

// newViper returns a fresh Viper instance pre-populated with the given
// values via [viper.Viper.Set]. Set values take precedence over flag
// and env-var resolution, which lets tests fix configuration directly.
func newViper(values setValues) *viper.Viper {
	vp := viper.New()
	for key, value := range values {
		vp.Set(key, value)
	}
	return vp
}

// validValues returns a setValues bag containing a minimal, valid
// configuration that [Load] accepts without further mutation. Each test
// case starts from this baseline and mutates only the fields under test.
func validValues(t *testing.T) setValues {
	t.Helper()
	return setValues{
		"artifact-configmap-name":      "demo-artifacts",
		"artifact-configmap-namespace": "dev",
		"localnet-env-dir":             "/state/env",
		"localnet-plan-manifest-file":  "/state/env/yacd-localnet-plan.json",
		"cardano-network-name":         "demo",
		"cardano-network-namespace":    "dev",
		"cardano-network-mode":         "local",
		"cardano-network-era":          "conway",
		"cardano-node-to-node-host":    "demo-node.dev.svc.cluster.local",
		"cardano-node-to-node-port":    3001,
		"kubernetes-api-url":           "https://kubernetes.default.svc",
	}
}

func TestLoad_Success(t *testing.T) {
	cfg, err := Load(newViper(validValues(t)))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ArtifactTokenPath != DefaultServiceTokenPath {
		t.Errorf("ArtifactTokenPath = %q, want default %q", cfg.ArtifactTokenPath, DefaultServiceTokenPath)
	}
	if cfg.CardanoNodeToNodeURL != "tcp://demo-node.dev.svc.cluster.local:3001" {
		t.Errorf("CardanoNodeToNodeURL = %q", cfg.CardanoNodeToNodeURL)
	}
	if cfg.KubernetesAPIURL != "https://kubernetes.default.svc" {
		t.Errorf("KubernetesAPIURL = %q", cfg.KubernetesAPIURL)
	}
}

func TestLoad_PreservesExplicitNodeToNodeURL(t *testing.T) {
	values := validValues(t)
	values["cardano-node-to-node-url"] = "tcp://override:9999"
	cfg, err := Load(newViper(values))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CardanoNodeToNodeURL != "tcp://override:9999" {
		t.Errorf("CardanoNodeToNodeURL = %q, want override", cfg.CardanoNodeToNodeURL)
	}
}

func TestLoad_NetworkNamespaceDefaultsToConfigMapNamespace(t *testing.T) {
	values := validValues(t)
	delete(values, "cardano-network-namespace")
	cfg, err := Load(newViper(values))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CardanoNetworkNamespace != "dev" {
		t.Errorf("CardanoNetworkNamespace = %q, want dev", cfg.CardanoNetworkNamespace)
	}
}

func TestLoad_NamespaceFromProjectedFile(t *testing.T) {
	nsPath := filepath.Join(t.TempDir(), "namespace")
	if err := os.WriteFile(nsPath, []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("write namespace file: %v", err)
	}

	values := validValues(t)
	delete(values, "artifact-configmap-namespace")
	values["artifact-namespace-path"] = nsPath
	cfg, err := Load(newViper(values))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ArtifactConfigMapNamespace != "from-file" {
		t.Errorf("ArtifactConfigMapNamespace = %q", cfg.ArtifactConfigMapNamespace)
	}
}

func TestLoad_NamespaceFileUnreadable(t *testing.T) {
	values := validValues(t)
	delete(values, "artifact-configmap-namespace")
	values["artifact-namespace-path"] = filepath.Join(t.TempDir(), "missing")
	_, err := Load(newViper(values))
	if err == nil {
		t.Fatal("Load() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "resolve artifact ConfigMap namespace") {
		t.Errorf("error = %v, want resolve-artifact-namespace prefix", err)
	}
}

func TestLoad_KubernetesAPIURLFromPodEnv(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT_HTTPS", "6443")

	values := validValues(t)
	delete(values, "kubernetes-api-url")
	cfg, err := Load(newViper(values))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.KubernetesAPIURL != "https://10.96.0.1:6443" {
		t.Errorf("KubernetesAPIURL = %q", cfg.KubernetesAPIURL)
	}
}

func TestLoad_KubernetesAPIURLPodEnvIPv6(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "fd00::1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	t.Setenv("KUBERNETES_SERVICE_PORT_HTTPS", "")

	values := validValues(t)
	delete(values, "kubernetes-api-url")
	cfg, err := Load(newViper(values))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.KubernetesAPIURL != "https://[fd00::1]:443" {
		t.Errorf("KubernetesAPIURL = %q", cfg.KubernetesAPIURL)
	}
}

func TestLoad_FromYACDEnvVarsThroughViper(t *testing.T) {
	t.Setenv("YACD_ARTIFACT_CONFIGMAP_NAME", "env-artifacts")
	t.Setenv("YACD_ARTIFACT_CONFIGMAP_NAMESPACE", "env-ns")
	t.Setenv("YACD_LOCALNET_ENV_DIR", "/env/state")
	t.Setenv("YACD_LOCALNET_PLAN_MANIFEST_FILE", "/env/state/yacd-localnet-plan.json")
	t.Setenv("YACD_CARDANO_NETWORK_NAME", "env-net")
	t.Setenv("YACD_CARDANO_NETWORK_MODE", "local")
	t.Setenv("YACD_CARDANO_NETWORK_ERA", "conway")
	t.Setenv("YACD_CARDANO_NODE_TO_NODE_HOST", "env-host")
	t.Setenv("YACD_CARDANO_NODE_TO_NODE_PORT", "4001")
	t.Setenv("YACD_KUBERNETES_API_URL", "https://api.example/")

	vp := viper.New()
	vp.SetEnvPrefix("YACD")
	vp.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	vp.AutomaticEnv()

	cfg, err := Load(vp)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ArtifactConfigMapName != "env-artifacts" {
		t.Errorf("ArtifactConfigMapName = %q", cfg.ArtifactConfigMapName)
	}
	if cfg.CardanoNodeToNodePort != 4001 {
		t.Errorf("CardanoNodeToNodePort = %d", cfg.CardanoNodeToNodePort)
	}
	if cfg.KubernetesAPIURL != "https://api.example" {
		t.Errorf("KubernetesAPIURL = %q (trailing slash should be trimmed)", cfg.KubernetesAPIURL)
	}
}

func TestLoad_ValidationFailures(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(setValues)
		wantMsg string
	}{
		{
			name:    "missing configmap name",
			mutate:  func(v setValues) { delete(v, "artifact-configmap-name") },
			wantMsg: "--artifact-configmap-name is required",
		},
		{
			name:    "missing localnet env dir",
			mutate:  func(v setValues) { delete(v, "localnet-env-dir") },
			wantMsg: "--localnet-env-dir is required",
		},
		{
			name:    "missing network name",
			mutate:  func(v setValues) { delete(v, "cardano-network-name") },
			wantMsg: "--cardano-network-name is required",
		},
		{
			name:    "port zero",
			mutate:  func(v setValues) { v["cardano-node-to-node-port"] = 0 },
			wantMsg: "port between 1 and 65535",
		},
		{
			name:    "port too high",
			mutate:  func(v setValues) { v["cardano-node-to-node-port"] = 70000 },
			wantMsg: "port between 1 and 65535",
		},
		{
			name: "missing kubernetes api url and pod env",
			mutate: func(v setValues) {
				delete(v, "kubernetes-api-url")
			},
			wantMsg: "--kubernetes-api-url",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "missing kubernetes api url and pod env" {
				t.Setenv("KUBERNETES_SERVICE_HOST", "")
				t.Setenv("KUBERNETES_SERVICE_PORT", "")
				t.Setenv("KUBERNETES_SERVICE_PORT_HTTPS", "")
			}
			values := validValues(t)
			tc.mutate(values)
			_, err := Load(newViper(values))
			if err == nil {
				t.Fatalf("Load() error = nil, want %q", tc.wantMsg)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("Load() error = %v, want substring %q", err, tc.wantMsg)
			}
		})
	}
}
