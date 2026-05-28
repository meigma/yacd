package publicnet

// Spec describes the supported public profile inputs.
type Spec struct {
	// Profile is the public network profile name.
	Profile string

	// Custom carries the profile files supplied by the controller when Profile
	// is "custom". Curated profiles must leave it nil.
	Custom *CustomBundle

	// Paths identifies the container filesystem locations used by the plan.
	Paths Paths
}

// CustomBundle carries caller-supplied public profile files. Files use the
// source bundle keys documented on the CardanoNetwork API, not the published
// network-artifact ConfigMap keys.
type CustomBundle struct {
	// Files maps custom profile source keys to UTF-8 file content.
	Files map[string]string
}

// Paths identifies the mounted public profile directory.
type Paths struct {
	// ProfileDir is the directory where profile files are mounted.
	ProfileDir string
}

// Plan is the normalized public profile plan consumed by Kubernetes workload
// builders.
type Plan struct {
	// Spec is the normalized public profile input specification.
	Spec Spec

	// Layout describes the expected profile file paths.
	Layout Layout

	// Profile is the resolved profile name.
	Profile string

	// NetworkMagic is the Cardano network magic from Shelley genesis.
	NetworkMagic int64

	// RequiresNetworkMagic reports whether node configuration requires magic.
	RequiresNetworkMagic bool

	// Fingerprint identifies the checked-in profile assets.
	Fingerprint Fingerprint

	// Manifest is the JSON-serializable profile marker published with artifacts.
	Manifest Manifest

	// Artifacts maps network-artifact ConfigMap keys to UTF-8 file content.
	Artifacts map[string]string
}

// Layout describes stable paths used by the mounted public profile.
type Layout struct {
	// ProfileDir is the directory where profile files are mounted.
	ProfileDir string

	// ConfigFile is the mounted cardano-node configuration file.
	ConfigFile string

	// TopologyFile is the mounted cardano-node topology file.
	TopologyFile string
}

// Fingerprint identifies a normalized public profile.
type Fingerprint struct {
	// Algorithm is the digest algorithm used to compute Value.
	Algorithm string `json:"algorithm"`

	// Value is the hex-encoded digest.
	Value string `json:"value"`
}

// Manifest is the JSON-serializable profile marker published with artifacts.
type Manifest struct {
	// SchemaVersion identifies the manifest wire format.
	SchemaVersion string `json:"schemaVersion"`

	// Profile is the resolved public network profile.
	Profile string `json:"profile"`

	// NetworkMagic is the Cardano network magic from Shelley genesis.
	NetworkMagic int64 `json:"networkMagic"`

	// RequiresNetworkMagic reports whether node configuration requires magic.
	RequiresNetworkMagic bool `json:"requiresNetworkMagic"`

	// Files maps logical names to artifact ConfigMap keys.
	Files map[string]string `json:"files"`

	// Source records the upstream profile bundle URL used for the checked-in assets.
	Source string `json:"source"`

	// CompatibleNodeRelease records the cardano-node release advertised by the
	// checked-in source page. Custom profiles leave this empty.
	CompatibleNodeRelease string `json:"compatibleNodeRelease,omitempty"`

	// Fingerprint identifies the checked-in profile assets.
	Fingerprint Fingerprint `json:"fingerprint"`
}

type profileFile struct {
	artifactKey   string
	assetPath     string
	connectionKey string
}

type fingerprintInput struct {
	SchemaVersion        string                 `json:"schemaVersion"`
	Profile              string                 `json:"profile"`
	NetworkMagic         int64                  `json:"networkMagic"`
	RequiresNetworkMagic bool                   `json:"requiresNetworkMagic"`
	Files                []fingerprintInputFile `json:"files"`
}

type fingerprintInputFile struct {
	Key    string `json:"key"`
	SHA256 string `json:"sha256"`
}
