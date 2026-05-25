// Package builder assembles the publisher's patch payload from
// precompiled inputs. It performs no filesystem access and produces no
// Kubernetes types: callers supply already-read file contents in a
// typed [Input] and receive a domain [Patch] that a separate adapter
// converts into a ConfigMap merge patch.
//
// The package is intentionally pure. Same input produces the same
// output, no globals are mutated, and no I/O happens past the package
// boundary, which makes the contract trivially unit-testable.
package builder

import (
	"fmt"
	"sort"
)

// SchemaVersion identifies the wire format of the network artifact
// payload the publisher produces. It is recorded both on the published
// ConfigMap as an annotation and inside connection.json, and is part
// of the YACD controller's artifact verification contract.
const SchemaVersion = "yacd.meigma.io/cardano-network-artifacts/v1alpha1"

// Annotation keys applied to the network artifact ConfigMap. The YACD
// controller reads these to verify schema, freshness, and integrity of
// the published artifacts.
const (
	// AnnotationSchemaVersion holds the artifact payload schema version,
	// always equal to [SchemaVersion].
	AnnotationSchemaVersion = "yacd.meigma.io/artifact-schema-version"
	// AnnotationLocalnetFingerprint holds the localnet plan fingerprint
	// captured by cardano-testnet, propagating the manifest's
	// fingerprint.value verbatim.
	AnnotationLocalnetFingerprint = "yacd.meigma.io/localnet-fingerprint"
	// AnnotationDataHash holds the deterministic SHA-256 hash of the
	// ConfigMap data the publisher produced. The controller recomputes
	// this from live data to detect tampering or drift.
	AnnotationDataHash = "yacd.meigma.io/artifact-data-hash"
)

// Reserved ConfigMap data keys the builder always populates, in
// addition to keys declared by [Sources].
const (
	// PlanManifestKey is the data key holding the verbatim
	// yacd-localnet-plan.json bytes.
	PlanManifestKey = "yacd-localnet-plan.json"
	// ConnectionKey is the data key holding the synthesized connection
	// discovery document.
	ConnectionKey = "connection.json"
)

// NetworkIdentity is the subset of operator-facing configuration the
// builder needs to render connection.json. It is intentionally narrow
// so the builder stays decoupled from publisher configuration concerns
// (paths, ServiceAccount mounts, Kubernetes API URLs).
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
	// NodeToNodePort is the primary node-to-node TCP port; valid range
	// is 1-65535.
	NodeToNodePort int
	// NodeToNodeURL is the full URL clients use to reach the primary
	// node, typically tcp://<host>:<port>.
	NodeToNodeURL string
}

// Manifest is the subset of yacd-localnet-plan.json that the builder
// consumes, plus the raw bytes that get published verbatim in
// [PlanManifestKey].
type Manifest struct {
	// NetworkMagic is the cardano network magic from
	// inputs.networkMagic; must be non-zero.
	NetworkMagic int64
	// Fingerprint is the cardano-testnet plan fingerprint from
	// fingerprint.value; must be non-empty.
	Fingerprint string
	// Raw is the verbatim manifest content published under
	// [PlanManifestKey]; must be non-empty.
	Raw string
}

// Input bundles the precompiled inputs [Build] consumes. No filesystem
// access happens past this boundary; callers read files and parse the
// manifest, then hand the results to [Build].
type Input struct {
	// Network is the network identity surfaced in connection.json.
	Network NetworkIdentity
	// Manifest is the parsed localnet plan plus its raw bytes.
	Manifest Manifest
	// Artifacts is the map of declared source keys (see [Sources]) to
	// already-read file contents. Optional sources may be omitted;
	// keys not declared by [Sources] are rejected.
	Artifacts map[string]string
}

// Annotations are the metadata the publisher applies to the network
// artifact ConfigMap alongside the patch data.
type Annotations struct {
	// SchemaVersion is always [SchemaVersion].
	SchemaVersion string
	// LocalnetFingerprint is the manifest fingerprint propagated from
	// [Manifest.Fingerprint].
	LocalnetFingerprint string
	// DataHash is the deterministic SHA-256 over the published data
	// map, prefixed with "sha256:".
	DataHash string
}

// Patch is the domain patch payload produced by [Build]. A separate
// Kubernetes adapter converts this into the merge patch sent to the
// API server. The builder itself is API-agnostic.
type Patch struct {
	// Data holds the ConfigMap data keys that should be set, mapping
	// each key to its UTF-8 string content.
	Data map[string]string
	// KnownKeys is the full set of data keys the publisher owns: every
	// declared source key plus [PlanManifestKey] and [ConnectionKey],
	// regardless of which optional sources are present. The K8s
	// adapter prunes any [KnownKeys] entry absent from [Data].
	KnownKeys []string
	// Annotations are the metadata annotations to apply alongside
	// [Data].
	Annotations Annotations
}

// Build assembles a [Patch] from input.
//
// Build walks the source registry from [Sources], collecting file
// contents from input.Artifacts. Required sources missing from
// Artifacts cause an error; optional sources may be absent. Build
// rejects keys in Artifacts that are not declared by [Sources] as a
// defense-in-depth check against the caller publishing unintended
// content. The manifest and network identity are validated before any
// data is assembled.
//
// The returned [Patch] is self-contained: [Patch.Data] holds present
// keys with their content, [Patch.KnownKeys] enumerates everything the
// publisher owns so adapters can prune absent keys, and
// [Patch.Annotations] carries the schema version, localnet
// fingerprint, and data hash.
func Build(input Input) (Patch, error) {
	if err := validateManifest(input.Manifest); err != nil {
		return Patch{}, err
	}
	if err := validateNetwork(input.Network); err != nil {
		return Patch{}, err
	}

	sources := generatedSources
	declared := make(map[string]struct{}, len(sources)+2)
	for _, s := range sources {
		declared[s.Key] = struct{}{}
	}
	declared[PlanManifestKey] = struct{}{}
	declared[ConnectionKey] = struct{}{}
	for key := range input.Artifacts {
		if _, ok := declared[key]; !ok {
			return Patch{}, fmt.Errorf("unknown artifact key %q (not declared by Sources())", key)
		}
	}

	data := make(map[string]string, len(sources)+2)
	fileKeys := map[string]string{
		"localnetPlan": PlanManifestKey,
		"connection":   ConnectionKey,
	}
	for _, s := range sources {
		content, present := input.Artifacts[s.Key]
		if !present {
			if s.Optional {
				continue
			}
			return Patch{}, fmt.Errorf("missing required artifact %q", s.Key)
		}
		data[s.Key] = content
		if s.ConnectionKey != "" {
			fileKeys[s.ConnectionKey] = s.Key
		}
	}

	data[PlanManifestKey] = input.Manifest.Raw

	connection, err := renderConnection(input, fileKeys)
	if err != nil {
		return Patch{}, fmt.Errorf("render connection.json: %w", err)
	}
	data[ConnectionKey] = connection

	knownKeys := make([]string, 0, len(sources)+2)
	for _, s := range sources {
		knownKeys = append(knownKeys, s.Key)
	}
	knownKeys = append(knownKeys, PlanManifestKey, ConnectionKey)
	sort.Strings(knownKeys)

	return Patch{
		Data:      data,
		KnownKeys: knownKeys,
		Annotations: Annotations{
			SchemaVersion:       SchemaVersion,
			LocalnetFingerprint: input.Manifest.Fingerprint,
			DataHash:            computeDataHash(data),
		},
	}, nil
}

// validateManifest returns an error when m is missing one of the
// fields Build relies on. All three fields are required because
// fingerprint and network magic appear in connection.json and the raw
// manifest is published verbatim.
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

// validateNetwork returns an error when n is missing a required field
// or carries an out-of-range TCP port. The same shape is enforced by
// the config package upstream, but the builder validates again so it
// stands alone as a contract.
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
