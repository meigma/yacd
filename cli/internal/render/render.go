package render

import (
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/devconfig"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// defaultNamespace is the final fallback when no caller has supplied a namespace.
const defaultNamespace = "default"

// Namespace resolves the namespace for a rendered environment by walking the
// precedence override > configured > fallback > "default" and returning the
// first non-empty, trimmed value.
func Namespace(override string, configured string, fallback string) string {
	for _, candidate := range []string{override, configured, fallback, defaultNamespace} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}

	return defaultNamespace
}

// CardanoNetwork renders the single CardanoNetwork described by a developer
// environment configuration. The provided namespace is the resolved override
// (typically from Namespace); validation is re-run defensively so callers
// cannot bypass it by constructing the Environment by hand.
func CardanoNetwork(environment *devconfig.Environment, namespace string) (*yacdv1alpha1.CardanoNetwork, error) {
	if environment == nil {
		return nil, fmt.Errorf("developer environment is required")
	}
	if err := environment.Validate(); err != nil {
		return nil, err
	}

	networkSpec := *environment.Spec.Network.DeepCopy()
	network := &yacdv1alpha1.CardanoNetwork{
		TypeMeta: metav1.TypeMeta{
			APIVersion: yacdv1alpha1.GroupVersion.String(),
			Kind:       "CardanoNetwork",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.TrimSpace(environment.Metadata.Name),
			Namespace: Namespace(namespace, environment.Metadata.Namespace, ""),
		},
		Spec: networkSpec,
	}

	return network, nil
}

// Manifest renders a CardanoNetwork as YAML suitable for kubectl inspection or
// apply. The output is the same shape the CLI's --dry-run path emits.
func Manifest(network *yacdv1alpha1.CardanoNetwork) ([]byte, error) {
	if network == nil {
		return nil, fmt.Errorf("cardanonetwork is required")
	}

	manifest, err := yaml.Marshal(network)
	if err != nil {
		return nil, fmt.Errorf("marshal cardanonetwork manifest: %w", err)
	}

	return manifest, nil
}
