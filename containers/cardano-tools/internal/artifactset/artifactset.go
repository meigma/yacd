package artifactset

import (
	"fmt"
	"sort"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/meigma/yacd/internal/ctrlkit/artifacts"
)

// NetworkIdentity is the subset of operator-facing configuration [Build] needs
// to render connection.json.
type NetworkIdentity struct {
	// Name is the owning CardanoNetwork resource name.
	Name string
	// Namespace is the owning CardanoNetwork namespace.
	Namespace string
	// Mode is the network mode (e.g. "local", "public").
	Mode string
	// Era is the Cardano era recorded in connection.json.
	Era string
	// NodeToNodeHost is the primary node-to-node Service hostname.
	NodeToNodeHost string
	// NodeToNodePort is the primary node-to-node TCP port; valid range is
	// 1-65535.
	NodeToNodePort int
	// NodeToNodeURL is the full URL clients use to reach the primary node,
	// typically tcp://<host>:<port>.
	NodeToNodeURL string
}

// Manifest is the subset of yacd-localnet-plan.json [Build] consumes, plus the
// raw bytes published verbatim under [networkartifacts.PlanManifestKey].
type Manifest struct {
	// NetworkMagic is the cardano network magic from inputs.networkMagic; must
	// be non-zero.
	NetworkMagic int64
	// Fingerprint is the cardano-testnet plan fingerprint from
	// fingerprint.value; must be non-empty.
	Fingerprint string
	// Raw is the verbatim manifest content; must be non-empty.
	Raw string
}

// Input bundles the values [Build] consumes.
type Input struct {
	// Network is the network identity surfaced in connection.json.
	Network NetworkIdentity
	// Manifest is the parsed localnet plan plus its raw bytes.
	Manifest Manifest
	// Artifacts maps declared source keys (see [Sources]) to file contents.
	// Optional sources may be omitted; keys not declared by [Sources] are
	// rejected.
	Artifacts map[string]string
}

// Annotations are the metadata applied to the network artifact ConfigMap
// alongside the assembled data.
type Annotations struct {
	// SchemaVersion is always [networkartifacts.SchemaVersion].
	SchemaVersion string
	// LocalnetFingerprint is the manifest fingerprint propagated from
	// [Manifest.Fingerprint].
	LocalnetFingerprint string
	// DataHash is the deterministic SHA-256 over the assembled data map,
	// computed by [artifacts.ComputeDataHash].
	DataHash string
}

// Set is the assembled artifact payload produced by [Build].
type Set struct {
	// Data holds the ConfigMap data keys to set, mapping each key to its
	// UTF-8 string content.
	Data map[string]string
	// KnownKeys is the full set of data keys the tool owns: every declared
	// source key plus [networkartifacts.PlanManifestKey] and
	// [networkartifacts.ConnectionKey], regardless of which optional sources
	// are present. Any [KnownKeys] entry absent from [Data] should be pruned
	// by the caller when applying the patch.
	KnownKeys []string
	// Annotations are the metadata annotations to apply alongside [Data].
	Annotations Annotations
}

// Build assembles a [Set] from input.
//
// Build validates input.Manifest and input.Network, walks the source registry
// from [Sources] to collect file contents from input.Artifacts, synthesizes
// connection.json, and computes the data hash via [artifacts.ComputeDataHash].
// Missing required sources, unknown artifact keys, an invalid manifest, or an
// invalid network identity each return an error.
func Build(input Input) (Set, error) {
	if err := validateManifest(input.Manifest); err != nil {
		return Set{}, err
	}
	if err := validateNetwork(input.Network); err != nil {
		return Set{}, err
	}

	sources := generatedSources
	declared := make(map[string]struct{}, len(sources)+2)
	for _, s := range sources {
		declared[s.Key] = struct{}{}
	}
	declared[networkartifacts.PlanManifestKey] = struct{}{}
	declared[networkartifacts.ConnectionKey] = struct{}{}
	for key := range input.Artifacts {
		if _, ok := declared[key]; !ok {
			return Set{}, fmt.Errorf("unknown artifact key %q (not declared by Sources())", key)
		}
	}

	data := make(map[string]string, len(sources)+2)
	fileKeys := map[string]string{
		"localnetPlan": networkartifacts.PlanManifestKey,
		"connection":   networkartifacts.ConnectionKey,
	}
	for _, s := range sources {
		content, present := input.Artifacts[s.Key]
		if !present {
			if s.Optional {
				continue
			}
			return Set{}, fmt.Errorf("missing required artifact %q", s.Key)
		}
		data[s.Key] = content
		if s.ConnectionKey != "" {
			fileKeys[s.ConnectionKey] = s.Key
		}
	}

	data[networkartifacts.PlanManifestKey] = input.Manifest.Raw

	connection, err := renderConnection(input, fileKeys)
	if err != nil {
		return Set{}, fmt.Errorf("render connection.json: %w", err)
	}
	data[networkartifacts.ConnectionKey] = connection

	knownKeys := make([]string, 0, len(sources)+2)
	for _, s := range sources {
		knownKeys = append(knownKeys, s.Key)
	}
	knownKeys = append(knownKeys, networkartifacts.PlanManifestKey, networkartifacts.ConnectionKey)
	sort.Strings(knownKeys)

	return Set{
		Data:      data,
		KnownKeys: knownKeys,
		Annotations: Annotations{
			SchemaVersion:       networkartifacts.SchemaVersion,
			LocalnetFingerprint: input.Manifest.Fingerprint,
			DataHash:            artifacts.ComputeDataHash(data),
		},
	}, nil
}

// validateManifest returns an error when m has a zero NetworkMagic or an empty
// Fingerprint or Raw.
func validateManifest(m Manifest) error {
	if m.NetworkMagic == 0 {
		return fmt.Errorf("manifest network magic is required")
	}
	if m.Fingerprint == "" {
		return fmt.Errorf("manifest fingerprint is required")
	}
	if m.Raw == "" {
		return fmt.Errorf("manifest raw content is required")
	}
	return nil
}

// validateNetwork returns an error when n is missing a required field or
// carries a TCP port outside 1-65535.
func validateNetwork(n NetworkIdentity) error {
	required := []struct {
		field string
		value string
	}{
		{"name", n.Name},
		{"namespace", n.Namespace},
		{"mode", n.Mode},
		{"era", n.Era},
		{"node-to-node host", n.NodeToNodeHost},
		{"node-to-node url", n.NodeToNodeURL},
	}
	for _, r := range required {
		if r.value == "" {
			return fmt.Errorf("network %s is required", r.field)
		}
	}
	if n.NodeToNodePort < 1 || n.NodeToNodePort > 65535 {
		return fmt.Errorf("network node-to-node port must be 1-65535, got %d", n.NodeToNodePort)
	}
	return nil
}
