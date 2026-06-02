package kube

import (
	"fmt"

	"k8s.io/client-go/tools/clientcmd"
)

// CurrentContext returns the current-context of the kubeconfig resolved by the
// standard loading rules (the KUBECONFIG env var or ~/.kube/config). It returns
// an empty string when no current-context is set.
//
// The lifecycle manager captures this before provisioning the managed cluster
// (which switches the kubectl context) so teardown can restore it.
func CurrentContext() (string, error) {
	pathOptions := clientcmd.NewDefaultPathOptions()
	config, err := pathOptions.GetStartingConfig()
	if err != nil {
		return "", fmt.Errorf("load kubeconfig: %w", err)
	}

	return config.CurrentContext, nil
}

// SetCurrentContext sets current-context in the kubeconfig at path, writing the
// change back to disk. An empty path targets the kubeconfig resolved by the
// standard loading rules. It mirrors `kubectl config use-context`.
func SetCurrentContext(path, context string) error {
	pathOptions := clientcmd.NewDefaultPathOptions()
	if path != "" {
		pathOptions.GlobalFile = path
		pathOptions.EnvVar = ""
	}

	config, err := pathOptions.GetStartingConfig()
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}

	config.CurrentContext = context
	if err := clientcmd.ModifyConfig(pathOptions, *config, true); err != nil {
		return fmt.Errorf("write kubeconfig context %q: %w", context, err)
	}

	return nil
}
