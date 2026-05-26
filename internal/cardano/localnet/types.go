package localnet

import "time"

// Spec describes the supported cardano-testnet create-env inputs for a local
// Cardano development network.
type Spec struct {
	// NetworkMagic is the Cardano testnet magic passed to cardano-testnet.
	NetworkMagic int64

	// PoolCount is the number of generated stake pool nodes.
	PoolCount int

	// Timing controls the generated network's slot and epoch settings.
	Timing Timing

	// Paths identifies the container filesystem locations used by the plan.
	Paths Paths

	// Tool identifies the cardano-testnet binary and optional release version.
	Tool Tool
}

// Timing controls local testnet slot and epoch settings.
type Timing struct {
	// SlotLength is the generated local network slot duration.
	SlotLength time.Duration

	// EpochLength is the number of slots in each epoch.
	EpochLength int
}

// Paths identifies the container filesystem locations used by the local
// testnet plan.
type Paths struct {
	// StateDir is the root directory mounted for durable node state.
	StateDir string

	// EnvDir is the directory populated by cardano-testnet create-env.
	EnvDir string
}

// Tool identifies the cardano-testnet executable used by the plan.
type Tool struct {
	// Binary is the cardano-testnet command name or absolute path used to
	// invoke create-env.
	Binary string

	// Version optionally records the cardano-testnet release used to generate
	// the local network environment.
	Version string
}

// Plan is the normalized local testnet plan consumed by later Kubernetes
// workload builders.
type Plan struct {
	// Spec is the normalized local testnet input specification.
	Spec Spec

	// CreateEnv is the cardano-testnet create-env invocation.
	CreateEnv Invocation

	// Layout describes the expected generated environment paths.
	Layout Layout

	// Fingerprint identifies the create-env inputs that produced this plan.
	Fingerprint Fingerprint

	// Manifest is the JSON-serializable value later init-container code can
	// write for idempotency checks.
	Manifest Manifest
}

// Invocation describes a command invocation without executing it.
type Invocation struct {
	// Command is the executable name or path.
	Command string

	// Args are the arguments passed to Command.
	Args []string
}

// Layout describes stable paths used by the generated local testnet
// environment.
type Layout struct {
	// StateDir is the root directory mounted for durable node state.
	StateDir string

	// EnvDir is the directory populated by cardano-testnet create-env.
	EnvDir string

	// ConfigFile is the generated cardano-node configuration file.
	ConfigFile string

	// ManifestFile is the path reserved for the YACD localnet plan manifest.
	ManifestFile string
}

// JSON tags on Fingerprint, Manifest, and ManifestInputs are the on-disk and
// fingerprint wire contract. Adding a field is safe; renaming or removing a
// tag rolls every persisted fingerprint and breaks init-container readback of
// existing local environments. Do not change them.

// Fingerprint identifies a normalized local testnet plan.
type Fingerprint struct {
	// Algorithm is the digest algorithm used to compute Value.
	Algorithm string `json:"algorithm"`

	// Value is the hex-encoded digest.
	Value string `json:"value"`
}

// Manifest is the JSON-serializable plan marker later init-container code can
// write next to cardano-testnet output.
type Manifest struct {
	// SchemaVersion identifies the manifest wire format.
	SchemaVersion string `json:"schemaVersion"`

	// Inputs are the fingerprint inputs covered by Fingerprint.
	Inputs ManifestInputs `json:"inputs"`

	// Fingerprint identifies the normalized create-env inputs.
	Fingerprint Fingerprint `json:"fingerprint"`
}

// ManifestInputs are the normalized create-env inputs covered by a plan
// fingerprint.
type ManifestInputs struct {
	// NetworkMagic is the Cardano testnet magic passed to cardano-testnet.
	NetworkMagic int64 `json:"networkMagic"`

	// PoolCount is the number of generated stake pool nodes.
	PoolCount int `json:"poolCount"`

	// EpochLength is the number of slots in each epoch.
	EpochLength int `json:"epochLength"`

	// SlotLength is the rendered create-env slot length value in seconds,
	// e.g. "0.1".
	SlotLength string `json:"slotLength"`

	// EnvDir is the create-env output directory.
	EnvDir string `json:"envDir"`

	// ToolVersion optionally records the cardano-testnet release used to
	// generate the environment.
	ToolVersion string `json:"toolVersion,omitempty"`
}
