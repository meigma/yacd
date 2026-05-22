package render

import (
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/devconfig"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const defaultNamespace = "default"

// Namespace resolves the namespace precedence for a rendered environment.
func Namespace(override string, configured string, fallback string) string {
	for _, candidate := range []string{override, configured, fallback, defaultNamespace} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}

	return defaultNamespace
}

// CardanoNetwork renders the single CardanoNetwork managed by a developer config.
func CardanoNetwork(environment *devconfig.Environment, namespace string) (*yacdv1alpha1.CardanoNetwork, error) {
	if environment == nil {
		return nil, fmt.Errorf("developer config is required")
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

// Manifest renders a CardanoNetwork as YAML suitable for kubectl inspection or apply.
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
