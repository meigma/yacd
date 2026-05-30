package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseViper returns a Viper instance pre-populated with the minimum valid
// report inputs. Tests override individual keys to exercise derivation and
// validation branches.
func baseViper(t *testing.T) *viper.Viper {
	t.Helper()
	vp := viper.New()
	vp.Set("artifact-configmap-name", "demo-network-artifacts")
	vp.Set("artifact-configmap-namespace", "demo")
	vp.Set("artifact-dir", "/state/env")
	vp.Set("cardano-network-name", "demo")
	vp.Set("cardano-network-mode", "local")
	vp.Set("cardano-network-era", "conway")
	vp.Set("cardano-node-to-node-host", "demo-node.demo.svc.cluster.local")
	vp.Set("cardano-node-to-node-port", 3001)
	vp.Set("kubernetes-api-url", "https://api.internal/")
	return vp
}

func TestLoadReportDerivations(t *testing.T) {
	cfg, err := LoadReport(baseViper(t))
	require.NoError(t, err)

	assert.Equal(t, "/state/env/yacd-localnet-plan.json", cfg.PlanManifestFile, "manifest path derives from artifact dir")
	assert.Equal(t, "tcp://demo-node.demo.svc.cluster.local:3001", cfg.CardanoNodeToNodeURL, "url synthesizes from host:port")
	assert.Equal(t, "demo", cfg.CardanoNetworkNamespace, "network namespace defaults to artifact namespace")
	assert.Equal(t, "https://api.internal", cfg.KubernetesAPIURL, "trailing slash trimmed")
	assert.Equal(t, DefaultServiceTokenPath, cfg.ArtifactTokenPath, "token path defaults")
}

func TestLoadReportResolvesNamespaceFromProjectedFile(t *testing.T) {
	nsPath := filepath.Join(t.TempDir(), "namespace")
	require.NoError(t, os.WriteFile(nsPath, []byte("from-file\n"), 0o600))

	vp := baseViper(t)
	vp.Set("artifact-configmap-namespace", "")
	vp.Set("artifact-namespace-path", nsPath)

	cfg, err := LoadReport(vp)
	require.NoError(t, err)
	assert.Equal(t, "from-file", cfg.ArtifactConfigMapNamespace)
	assert.Equal(t, "from-file", cfg.CardanoNetworkNamespace, "network namespace follows the resolved artifact namespace")
}

func TestLoadReportDerivesAPIURLFromPodEnv(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT_HTTPS", "6443")

	vp := baseViper(t)
	vp.Set("kubernetes-api-url", "")

	cfg, err := LoadReport(vp)
	require.NoError(t, err)
	assert.Equal(t, "https://10.0.0.1:6443", cfg.KubernetesAPIURL)
}

func TestLoadReportValidation(t *testing.T) {
	tests := []struct {
		name  string
		mut   func(vp *viper.Viper)
		field string
	}{
		{"missing configmap name", func(vp *viper.Viper) { vp.Set("artifact-configmap-name", "") }, "--artifact-configmap-name"},
		{"missing artifact dir", func(vp *viper.Viper) { vp.Set("artifact-dir", "") }, "--artifact-dir"},
		{"port out of range", func(vp *viper.Viper) { vp.Set("cardano-node-to-node-port", 70000) }, "--cardano-node-to-node-port"},
		{"missing era", func(vp *viper.Viper) { vp.Set("cardano-network-era", "") }, "--cardano-network-era"},
		{"non-https api url", func(vp *viper.Viper) { vp.Set("kubernetes-api-url", "http://api.internal") }, "--kubernetes-api-url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vp := baseViper(t)
			tt.mut(vp)
			_, err := LoadReport(vp)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.field)
		})
	}
}
