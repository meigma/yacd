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

	return nil
}
