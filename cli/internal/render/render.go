package render

import (
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/devconfig"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// CardanoNetwork renders the single CardanoNetwork described by a developer
// environment configuration under the supplied identity. Name and namespace
// are command-line concerns resolved by the caller (they are not read from the
// file); validation is re-run defensively so callers cannot bypass it by
// constructing the Environment by hand.
func CardanoNetwork(environment *devconfig.Environment, name string, namespace string) (*yacdv1alpha1.CardanoNetwork, error) {
	if environment == nil {
		return nil, fmt.Errorf("developer environment is required")
	}
	if err := environment.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	if strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	networkSpec := *environment.Spec.Network.DeepCopy()
	network := &yacdv1alpha1.CardanoNetwork{
		TypeMeta: metav1.TypeMeta{
			APIVersion: yacdv1alpha1.GroupVersion.String(),
			Kind:       "CardanoNetwork",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.TrimSpace(name),
			Namespace: strings.TrimSpace(namespace),
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
