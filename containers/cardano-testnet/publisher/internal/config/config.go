// Package config builds a validated publisher runtime configuration
// from a Viper instance that has already been bound to the publish
// subcommand's flags (and therefore transparently reads the matching
// YACD_* environment variables).
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

const (
	DefaultServiceTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	DefaultServiceCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	DefaultNamespacePath    = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

// Config is the validated publisher runtime configuration.
type Config struct {
	ArtifactConfigMapName      string
	ArtifactConfigMapNamespace string
	ArtifactTokenPath          string
	ArtifactCAPath             string
	ArtifactNamespacePath      string
	KubernetesAPIURL           string
	LocalnetEnvDir             string
	LocalnetPlanManifestFile   string
	CardanoNetworkName         string
	CardanoNetworkNamespace    string
	CardanoNetworkMode         string
	CardanoNetworkEra          string
	CardanoNodeToNodeHost      string
	CardanoNodeToNodePort      int
	CardanoNodeToNodeURL       string
}

// Load reads, derives, and validates the publisher configuration.
//
// Derivation rules applied before validation:
//   - Empty path fields fall back to their projected-ServiceAccount defaults.
//   - An empty ArtifactConfigMapNamespace is read from the projected
//     namespace file at ArtifactNamespacePath.
//   - An empty CardanoNetworkNamespace defaults to ArtifactConfigMapNamespace.
//   - An empty KubernetesAPIURL is derived from the standard pod-injected
//     KUBERNETES_SERVICE_HOST / _PORT[_HTTPS] environment variables.
//   - An empty CardanoNodeToNodeURL is synthesized as "tcp://<host>:<port>"
//     when host and port are both supplied.
func Load(vp *viper.Viper) (Config, error) {
	cfg := Config{
		ArtifactConfigMapName:      strings.TrimSpace(vp.GetString("artifact-configmap-name")),
		ArtifactConfigMapNamespace: strings.TrimSpace(vp.GetString("artifact-configmap-namespace")),
		ArtifactTokenPath:          strings.TrimSpace(vp.GetString("artifact-token-path")),
		ArtifactCAPath:             strings.TrimSpace(vp.GetString("artifact-ca-path")),
		ArtifactNamespacePath:      strings.TrimSpace(vp.GetString("artifact-namespace-path")),
		KubernetesAPIURL:           strings.TrimRight(strings.TrimSpace(vp.GetString("kubernetes-api-url")), "/"),
		LocalnetEnvDir:             strings.TrimSpace(vp.GetString("localnet-env-dir")),
		LocalnetPlanManifestFile:   strings.TrimSpace(vp.GetString("localnet-plan-manifest-file")),
		CardanoNetworkName:         strings.TrimSpace(vp.GetString("cardano-network-name")),
		CardanoNetworkNamespace:    strings.TrimSpace(vp.GetString("cardano-network-namespace")),
		CardanoNetworkMode:         strings.TrimSpace(vp.GetString("cardano-network-mode")),
		CardanoNetworkEra:          strings.TrimSpace(vp.GetString("cardano-network-era")),
		CardanoNodeToNodeHost:      strings.TrimSpace(vp.GetString("cardano-node-to-node-host")),
		CardanoNodeToNodePort:      vp.GetInt("cardano-node-to-node-port"),
		CardanoNodeToNodeURL:       strings.TrimSpace(vp.GetString("cardano-node-to-node-url")),
	}

	if cfg.ArtifactTokenPath == "" {
		cfg.ArtifactTokenPath = DefaultServiceTokenPath
	}
	if cfg.ArtifactCAPath == "" {
		cfg.ArtifactCAPath = DefaultServiceCAPath
	}
	if cfg.ArtifactNamespacePath == "" {
		cfg.ArtifactNamespacePath = DefaultNamespacePath
	}

	if cfg.ArtifactConfigMapNamespace == "" {
		ns, err := readTrimmedFile(cfg.ArtifactNamespacePath)
		if err != nil {
			return Config{}, fmt.Errorf("resolve artifact ConfigMap namespace from %s: %w", cfg.ArtifactNamespacePath, err)
		}
		cfg.ArtifactConfigMapNamespace = ns
	}

	if cfg.CardanoNetworkNamespace == "" {
		cfg.CardanoNetworkNamespace = cfg.ArtifactConfigMapNamespace
	}

	if cfg.KubernetesAPIURL == "" {
		cfg.KubernetesAPIURL = kubernetesAPIURLFromPodEnv()
	}

	if cfg.CardanoNodeToNodeURL == "" && cfg.CardanoNodeToNodeHost != "" && cfg.CardanoNodeToNodePort != 0 {
		cfg.CardanoNodeToNodeURL = fmt.Sprintf("tcp://%s:%d", cfg.CardanoNodeToNodeHost, cfg.CardanoNodeToNodePort)
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	required := []struct {
		flag  string
		value string
	}{
		{"--artifact-configmap-name", c.ArtifactConfigMapName},
		{"--localnet-env-dir", c.LocalnetEnvDir},
		{"--localnet-plan-manifest-file", c.LocalnetPlanManifestFile},
		{"--cardano-network-name", c.CardanoNetworkName},
		{"--cardano-network-mode", c.CardanoNetworkMode},
		{"--cardano-network-era", c.CardanoNetworkEra},
		{"--cardano-node-to-node-host", c.CardanoNodeToNodeHost},
	}
	for _, r := range required {
		if r.value == "" {
			return fmt.Errorf("%s is required", r.flag)
		}
	}

	if c.ArtifactConfigMapNamespace == "" {
		return fmt.Errorf("--artifact-configmap-namespace or a readable --artifact-namespace-path is required")
	}
	if c.CardanoNodeToNodePort < 1 || c.CardanoNodeToNodePort > 65535 {
		return fmt.Errorf("--cardano-node-to-node-port must be a TCP port between 1 and 65535")
	}
	if c.CardanoNodeToNodeURL == "" {
		return fmt.Errorf("--cardano-node-to-node-url is required")
	}
	if c.KubernetesAPIURL == "" {
		return fmt.Errorf("--kubernetes-api-url or pod-injected KUBERNETES_SERVICE_HOST/PORT is required")
	}
	return nil
}

func kubernetesAPIURLFromPodEnv() string {
	host := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST"))
	if host == "" {
		return ""
	}
	port := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_PORT_HTTPS"))
	if port == "" {
		port = strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_PORT"))
	}
	if port == "" {
		port = "443"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return "https://" + host + ":" + port
}

func readTrimmedFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}
