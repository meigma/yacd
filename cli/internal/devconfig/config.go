package devconfig

import (
	"fmt"
	"io"
	"os"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"sigs.k8s.io/yaml"
)

const (
	APIVersion = "yacd.meigma.io/devconfig/v1alpha1"
	Kind       = "Environment"
)

// Environment is the local developer-facing YACD configuration.
type Environment struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   Metadata        `json:"metadata"`
	Spec       EnvironmentSpec `json:"spec"`
}

// Metadata identifies the generated Kubernetes environment.
type Metadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// EnvironmentSpec contains the cluster resources derived from the local config.
type EnvironmentSpec struct {
	Network yacdv1alpha1.CardanoNetworkSpec `json:"network"`
}

// LoadFile reads and validates a developer config file.
func LoadFile(path string) (*Environment, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open developer config %s: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	environment, err := Load(file)
	if err != nil {
		return nil, fmt.Errorf("load developer config %s: %w", path, err)
	}

	return environment, nil
}

// Load reads and validates a developer config document.
func Load(r io.Reader) (*Environment, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read developer config: %w", err)
	}

	var environment Environment
	if err := yaml.UnmarshalStrict(data, &environment); err != nil {
		return nil, fmt.Errorf("parse developer config: %w", err)
	}
	if err := environment.Validate(); err != nil {
		return nil, err
	}
	if err := validateExplicitFields(data, environment); err != nil {
		return nil, err
	}

	return &environment, nil
}

// Validate checks the local config envelope before rendering Kubernetes objects.
func (e Environment) Validate() error {
	if strings.TrimSpace(e.APIVersion) != APIVersion {
		return fmt.Errorf("apiVersion must be %q", APIVersion)
	}
	if strings.TrimSpace(e.Kind) != Kind {
		return fmt.Errorf("kind must be %q", Kind)
	}
	if strings.TrimSpace(e.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if e.Spec.Network.Mode != yacdv1alpha1.CardanoNetworkModeLocal {
		return fmt.Errorf("spec.network.mode must be %q for phase 4 developer configs", yacdv1alpha1.CardanoNetworkModeLocal)
	}
	if strings.TrimSpace(e.Spec.Network.Node.Version) == "" {
		return fmt.Errorf("spec.network.node.version is required")
	}
	if e.Spec.Network.Node.Port <= 0 {
		return fmt.Errorf("spec.network.node.port must be greater than 0")
	}
	if e.Spec.Network.Local == nil {
		return fmt.Errorf("spec.network.local is required")
	}

	return nil
}

func validateExplicitFields(data []byte, environment Environment) error {
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("parse developer config fields: %w", err)
	}

	requiredPaths := [][]string{
		{"spec", "network", "mode"},
		{"spec", "network", "node", "version"},
		{"spec", "network", "node", "port"},
		{"spec", "network", "local", "networkMagic"},
		{"spec", "network", "local", "era"},
		{"spec", "network", "local", "timing", "slotLength"},
		{"spec", "network", "local", "timing", "epochLength"},
		{"spec", "network", "local", "topology", "pools", "count"},
	}
	if environment.Spec.Network.Node.Storage != nil {
		requiredPaths = append(requiredPaths, []string{"spec", "network", "node", "storage", "size"})
	}
	if environment.Spec.Network.Local != nil && environment.Spec.Network.Local.Genesis != nil {
		requiredPaths = append(requiredPaths, []string{"spec", "network", "local", "genesis", "profile"})
	}
	if environment.Spec.Network.ChainAPI != nil && environment.Spec.Network.ChainAPI.Ogmios != nil {
		requiredPaths = append(requiredPaths,
			[]string{"spec", "network", "chainAPI", "ogmios", "enabled"},
			[]string{"spec", "network", "chainAPI", "ogmios", "image"},
			[]string{"spec", "network", "chainAPI", "ogmios", "port"},
		)
	}
	if environment.Spec.Network.ChainAPI != nil && environment.Spec.Network.ChainAPI.Kupo != nil {
		requiredPaths = append(requiredPaths,
			[]string{"spec", "network", "chainAPI", "kupo", "enabled"},
			[]string{"spec", "network", "chainAPI", "kupo", "image"},
			[]string{"spec", "network", "chainAPI", "kupo", "port"},
		)
	}

	for _, path := range requiredPaths {
		if !hasPath(document, path...) {
			return fmt.Errorf("%s must be set explicitly in developer config", strings.Join(path, "."))
		}
	}

	return nil
}

func hasPath(document map[string]any, path ...string) bool {
	var current any = document
	for _, segment := range path {
		fields, ok := current.(map[string]any)
		if !ok {
			return false
		}
		next, ok := fields[segment]
		if !ok || next == nil {
			return false
		}
		current = next
	}

	return true
}
