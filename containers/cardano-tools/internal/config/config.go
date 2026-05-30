package config

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/spf13/viper"
)

// Default mount paths for the projected ServiceAccount the report verb uses
// when running as a Pod init container. These are the standard Kubernetes
// locations and apply when the corresponding flag is left empty.
const (
	// DefaultServiceTokenPath is the standard mount path for a Pod's projected
	// ServiceAccount bearer token.
	DefaultServiceTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	// DefaultServiceCAPath is the standard mount path for the Kubernetes API
	// CA bundle inside a Pod.
	DefaultServiceCAPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	// DefaultNamespacePath is the standard mount path for the file containing
	// the Pod's own namespace.
	DefaultNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

// planManifestFilename is the localnet plan manifest file the report verb
// reads from the artifact directory.
const planManifestFilename = "yacd-localnet-plan.json"

// ReportConfig is the validated report runtime configuration produced by
// [LoadReport]. All fields are populated after derivation and validation, so
// consumers can trust the values without further normalization.
type ReportConfig struct {
	// ArtifactConfigMapName is the name of the network artifact ConfigMap to
	// patch.
	ArtifactConfigMapName string
	// ArtifactConfigMapNamespace is the namespace of the target ConfigMap.
	// When empty in input, it is read from the projected namespace file at
	// ArtifactNamespacePath.
	ArtifactConfigMapNamespace string
	// ArtifactTokenPath is the filesystem path to the projected ServiceAccount
	// bearer token used to authenticate the PATCH.
	ArtifactTokenPath string
	// ArtifactCAPath is the filesystem path to the projected Kubernetes API CA
	// bundle used for TLS verification.
	ArtifactCAPath string
	// ArtifactNamespacePath is the filesystem path to the file holding the
	// Pod's namespace, the fallback source for ArtifactConfigMapNamespace.
	ArtifactNamespacePath string
	// KubernetesAPIURL is the base URL of the Kubernetes API server, with any
	// trailing slash trimmed. When empty in input, it is derived from the
	// pod-injected KUBERNETES_SERVICE_HOST / _PORT[_HTTPS] env vars.
	KubernetesAPIURL string
	// ArtifactDir is the directory holding the artifact files to publish.
	ArtifactDir string
	// PlanManifestFile is the absolute path of the localnet plan manifest.
	// When empty in input, it is derived as ArtifactDir/yacd-localnet-plan.json.
	PlanManifestFile string
	// CardanoNetworkName is the name of the owning CardanoNetwork resource.
	CardanoNetworkName string
	// CardanoNetworkNamespace is the namespace of the owning CardanoNetwork
	// resource. When empty in input, it defaults to ArtifactConfigMapNamespace.
	CardanoNetworkNamespace string
	// CardanoNetworkMode is the network mode (e.g. "local", "public") recorded
	// in the published connection metadata.
	CardanoNetworkMode string
	// CardanoNetworkEra is the Cardano era recorded in the published
	// connection metadata.
	CardanoNetworkEra string
	// CardanoNodeToNodeHost is the hostname clients use to reach the primary
	// node's node-to-node Service.
	CardanoNodeToNodeHost string
	// CardanoNodeToNodePort is the TCP port for the node-to-node Service.
	// Valid range is 1-65535.
	CardanoNodeToNodePort int
	// CardanoNodeToNodeURL is the full URL for the node-to-node Service. When
	// empty in input and both host and port are set, it is synthesized as
	// "tcp://<host>:<port>".
	CardanoNodeToNodeURL string
	// DryRun reports whether report should render the merge patch instead of
	// applying it.
	DryRun bool
}

// LoadReport reads, derives, and validates the report configuration from vp.
// The returned ReportConfig is safe to use; on error the zero value is
// returned with a message naming the offending flag.
//
// Derivation rules applied before validation:
//   - Empty path fields fall back to their projected-ServiceAccount defaults.
//   - An empty ArtifactConfigMapNamespace is read from the projected namespace
//     file at ArtifactNamespacePath.
//   - An empty PlanManifestFile is derived as ArtifactDir/yacd-localnet-plan.json.
//   - An empty CardanoNetworkNamespace defaults to ArtifactConfigMapNamespace.
//   - An empty KubernetesAPIURL is derived from the pod-injected
//     KUBERNETES_SERVICE_HOST / _PORT[_HTTPS] env vars.
//   - An empty CardanoNodeToNodeURL is synthesized as "tcp://<host>:<port>"
//     when host and port are both supplied.
func LoadReport(vp *viper.Viper) (ReportConfig, error) {
	cfg := ReportConfig{
		ArtifactConfigMapName:      strings.TrimSpace(vp.GetString("artifact-configmap-name")),
		ArtifactConfigMapNamespace: strings.TrimSpace(vp.GetString("artifact-configmap-namespace")),
		ArtifactTokenPath:          strings.TrimSpace(vp.GetString("artifact-token-path")),
		ArtifactCAPath:             strings.TrimSpace(vp.GetString("artifact-ca-path")),
		ArtifactNamespacePath:      strings.TrimSpace(vp.GetString("artifact-namespace-path")),
		KubernetesAPIURL:           strings.TrimRight(strings.TrimSpace(vp.GetString("kubernetes-api-url")), "/"),
		ArtifactDir:                strings.TrimSpace(vp.GetString("artifact-dir")),
		PlanManifestFile:           strings.TrimSpace(vp.GetString("plan-manifest-file")),
		CardanoNetworkName:         strings.TrimSpace(vp.GetString("cardano-network-name")),
		CardanoNetworkNamespace:    strings.TrimSpace(vp.GetString("cardano-network-namespace")),
		CardanoNetworkMode:         strings.TrimSpace(vp.GetString("cardano-network-mode")),
		CardanoNetworkEra:          strings.TrimSpace(vp.GetString("cardano-network-era")),
		CardanoNodeToNodeHost:      strings.TrimSpace(vp.GetString("cardano-node-to-node-host")),
		CardanoNodeToNodePort:      vp.GetInt("cardano-node-to-node-port"),
		CardanoNodeToNodeURL:       strings.TrimSpace(vp.GetString("cardano-node-to-node-url")),
		DryRun:                     vp.GetBool("dry-run"),
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

	if cfg.PlanManifestFile == "" && cfg.ArtifactDir != "" {
		cfg.PlanManifestFile = path.Join(cfg.ArtifactDir, planManifestFilename)
	}

	if cfg.ArtifactConfigMapNamespace == "" {
		ns, err := readTrimmedFile(cfg.ArtifactNamespacePath)
		if err != nil {
			return ReportConfig{}, fmt.Errorf("resolve artifact ConfigMap namespace from %s: %w", cfg.ArtifactNamespacePath, err)
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
		return ReportConfig{}, err
	}
	return cfg, nil
}

// validate returns an error when c is missing a required field or carries an
// out-of-range port. Messages reference the user-facing flag name.
func (c ReportConfig) validate() error {
	required := []struct {
		flag  string
		value string
	}{
		{"--artifact-configmap-name", c.ArtifactConfigMapName},
		{"--artifact-dir", c.ArtifactDir},
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
	// report sends the projected ServiceAccount bearer token to this URL, so it
	// must be HTTPS; refuse a cleartext scheme that would leak the token. The
	// pod-injected default is always https.
	if !strings.HasPrefix(c.KubernetesAPIURL, "https://") {
		return fmt.Errorf("--kubernetes-api-url must be an https:// URL, got %q", c.KubernetesAPIURL)
	}
	return nil
}

// kubernetesAPIURLFromPodEnv derives an in-cluster Kubernetes API base URL from
// the standard pod-injected service environment variables. It prefers
// KUBERNETES_SERVICE_PORT_HTTPS, falls back to KUBERNETES_SERVICE_PORT and then
// 443, and wraps IPv6 hosts in brackets. An empty string is returned when the
// host is unset, leaving validation to surface the missing-source error.
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

// readTrimmedFile reads the file at p and returns its contents with leading and
// trailing whitespace removed. Errors from [os.ReadFile] are returned unwrapped
// so callers can attach their own context.
func readTrimmedFile(p string) (string, error) {
	raw, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}
