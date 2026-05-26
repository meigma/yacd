package kube

import (
	"fmt"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// defaultNamespace is the fallback when kubeconfig does not select one.
const defaultNamespace = "default"

// fieldOwner identifies the CLI in server-side apply field-ownership records.
const fieldOwner = "yacd-cli"

// Config carries the inputs needed to construct a Kubernetes client adapter:
// an optional kubeconfig path and an optional context name. Empty values
// defer to the standard kubeconfig loading rules.
type Config struct {
	// Kubeconfig is the path to the kubeconfig file. Empty means use the
	// default loading rules (KUBECONFIG env or ~/.kube/config).
	Kubeconfig string

	// Context is the kubeconfig context to use. Empty means use the
	// kubeconfig's current-context.
	Context string
}

// DefaultNamespace resolves the namespace selected by the user's kubeconfig.
// It returns "default" when the kubeconfig does not specify one.
func DefaultNamespace(config Config) (string, error) {
	clientConfig := newClientConfig(config)

	namespace, _, err := clientConfig.Namespace()
	if err != nil {
		return "", fmt.Errorf("resolve Kubernetes namespace: %w", err)
	}
	if strings.TrimSpace(namespace) == "" {
		namespace = defaultNamespace
	}

	return namespace, nil
}

// restConfig resolves the rest.Config and default namespace from a Config.
// It is the single entry point used by NewClient to construct the underlying
// controller-runtime client.
func restConfig(config Config) (*rest.Config, string, error) {
	clientConfig := newClientConfig(config)

	namespace, err := DefaultNamespace(config)
	if err != nil {
		return nil, "", err
	}

	restCfg, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, "", fmt.Errorf("load Kubernetes config: %w", err)
	}

	return restCfg, namespace, nil
}

// newClientConfig wires the standard kubeconfig loading rules with the
// explicit path and context overrides from Config.
func newClientConfig(config Config) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if path := strings.TrimSpace(config.Kubeconfig); path != "" {
		loadingRules.ExplicitPath = path
	}

	overrides := &clientcmd.ConfigOverrides{
		CurrentContext: strings.TrimSpace(config.Context),
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
}
