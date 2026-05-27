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
	// APIVersion is the apiVersion key required on every developer
	// environment document.
	APIVersion = "yacd.meigma.io/devconfig/v1alpha1"

	// Kind is the kind key required on every developer environment document.
	Kind = "Environment"
)

// Environment is the local developer-facing YACD configuration document.
// It mirrors the shape of a Kubernetes object so authors recognise the
// metadata/spec layout, but the controllers consume the rendered
// CardanoNetwork rather than this envelope.
type Environment struct {
	// APIVersion must equal the package APIVersion constant.
	APIVersion string `json:"apiVersion"`

	// Kind must equal the package Kind constant.
	Kind string `json:"kind"`

	// Metadata identifies the rendered Kubernetes object.
	Metadata Metadata `json:"metadata"`

	// Spec carries the CardanoNetwork inputs.
	Spec EnvironmentSpec `json:"spec"`
}

// Metadata identifies the generated Kubernetes object.
type Metadata struct {
	// Name is the rendered CardanoNetwork's metadata.name; required.
	Name string `json:"name"`

	// Namespace is the rendered CardanoNetwork's metadata.namespace;
	// empty defers to the CLI's namespace precedence.
	Namespace string `json:"namespace,omitempty"`
}

// EnvironmentSpec wraps the network configuration. It is intentionally a thin
// envelope so future top-level fields (for example, multiple networks per
// environment) can be added without changing existing documents.
type EnvironmentSpec struct {
	// Network is the desired CardanoNetwork spec, decoded directly into the
	// API type so developer documents see the same fields the CRD exposes.
	Network yacdv1alpha1.CardanoNetworkSpec `json:"network"`
}

// LoadFile reads and validates a developer environment file at the given path.
// It is a thin wrapper around Load that adds file-open and file-name context
// to errors.
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

// Load reads, parses, and validates a developer environment document.
//
// Validation runs in two passes. The first pass (Validate) checks envelope
// integrity — apiVersion, kind, required fields on the decoded Go value.
// The second pass (validateExplicitFields) re-decodes the raw YAML into a
// generic map and checks that certain CRD-defaulted fields were actually
// written by the author rather than filled in by Go's zero value. Both are
// required because the decoder cannot distinguish "absent" from "zero" on
// the strongly-typed API value.
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

// Validate checks the document envelope before rendering Kubernetes objects.
// It accepts the supported developer network shapes and rejects future API
// shapes here so the rendering pipeline can assume a narrow input.
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
	if strings.TrimSpace(e.Spec.Network.Node.Version) == "" {
		return fmt.Errorf("spec.network.node.version is required")
	}
	if e.Spec.Network.Node.Port <= 0 {
		return fmt.Errorf("spec.network.node.port must be greater than 0")
	}

	switch e.Spec.Network.Mode {
	case yacdv1alpha1.CardanoNetworkModeLocal:
		if e.Spec.Network.Local == nil {
			return fmt.Errorf("spec.network.local is required")
		}
		if e.Spec.Network.Public != nil {
			return fmt.Errorf("spec.network.public is not supported with local mode")
		}
	case yacdv1alpha1.CardanoNetworkModePublic:
		if e.Spec.Network.Public == nil {
			return fmt.Errorf("spec.network.public is required")
		}
		if e.Spec.Network.Local != nil {
			return fmt.Errorf("spec.network.local is not supported with public mode")
		}
		if e.Spec.Network.Public.Profile != yacdv1alpha1.PublicNetworkProfilePreview {
			return fmt.Errorf("spec.network.public.profile must be %q for public developer configs", yacdv1alpha1.PublicNetworkProfilePreview)
		}
		if e.Spec.Network.Public.ConfigSource != nil {
			return fmt.Errorf("spec.network.public.configSource is not supported")
		}
	default:
		return fmt.Errorf("spec.network.mode must be %q or %q", yacdv1alpha1.CardanoNetworkModeLocal, yacdv1alpha1.CardanoNetworkModePublic)
	}

	return nil
}

// validateExplicitFields enforces that certain CRD-defaulted fields are
// present explicitly in the YAML source, not merely zero on the decoded
// Go value.
//
// The strict decoder catches unknown fields, and Validate catches missing
// required fields on the typed value, but neither can tell whether the
// author wrote "port: 0" or omitted the key entirely. For fields whose
// implied default would silently produce surprising runtime behaviour
// (for example, an unset node.port leaving the rendered Service with port 0),
// the document must spell the value out. This pass walks the raw map and
// fails fast when a required path is missing.
func validateExplicitFields(data []byte, environment Environment) error {
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("parse developer config fields: %w", err)
	}

	requiredPaths := [][]string{
		{"spec", "network", "mode"},
		{"spec", "network", "node", "version"},
		{"spec", "network", "node", "port"},
	}
	switch environment.Spec.Network.Mode {
	case yacdv1alpha1.CardanoNetworkModeLocal:
		requiredPaths = append(requiredPaths,
			[]string{"spec", "network", "local", "networkMagic"},
			[]string{"spec", "network", "local", "era"},
			[]string{"spec", "network", "local", "timing", "slotLength"},
			[]string{"spec", "network", "local", "timing", "epochLength"},
			[]string{"spec", "network", "local", "topology", "pools", "count"},
		)
	case yacdv1alpha1.CardanoNetworkModePublic:
		requiredPaths = append(requiredPaths,
			[]string{"spec", "network", "public", "profile"},
		)
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
	if environment.Spec.Network.ChainAPI != nil && environment.Spec.Network.ChainAPI.Faucet != nil {
		requiredPaths = append(requiredPaths,
			[]string{"spec", "network", "chainAPI", "faucet", "enabled"},
			[]string{"spec", "network", "chainAPI", "faucet", "port"},
			[]string{"spec", "network", "chainAPI", "faucet", "defaultSource"},
			[]string{"spec", "network", "chainAPI", "faucet", "minTopUpLovelace"},
			[]string{"spec", "network", "chainAPI", "faucet", "maxTopUpLovelace"},
		)
	}

	for _, path := range requiredPaths {
		if !hasPath(document, path...) {
			return fmt.Errorf("%s must be set explicitly in developer config", strings.Join(path, "."))
		}
	}

	return nil
}

// hasPath reports whether the given dotted path exists with a non-nil value
// in the decoded YAML map. Only object segments are traversed; an
// intermediate non-object value or a missing/nil leaf returns false.
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
