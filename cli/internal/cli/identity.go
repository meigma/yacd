package cli

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// resolveIdentity derives the CardanoNetwork name and namespace for a
// NAME-keyed command from the positional argument and the --namespace flag.
//
// Identity is a command-line concern, not a file concern: NAME becomes the
// network name, and the namespace defaults to NAME when --namespace is not
// given, so each environment lands in its own isolated namespace by default.
// Both must be valid DNS-1123 labels because the namespace and the operator's
// child resource names derive from them; invalid input is rejected with a
// clear error rather than silently mangled.
func resolveIdentity(name string, runtimeConfig RuntimeConfig) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", fmt.Errorf("NAME is required")
	}
	if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
		return "", "", fmt.Errorf("invalid NAME %q: %s", name, strings.Join(errs, "; "))
	}

	namespace := strings.TrimSpace(runtimeConfig.Namespace)
	if namespace == "" {
		namespace = name
	}
	if errs := validation.IsDNS1123Label(namespace); len(errs) > 0 {
		return "", "", fmt.Errorf("invalid namespace %q: %s", namespace, strings.Join(errs, "; "))
	}

	return name, namespace, nil
}
