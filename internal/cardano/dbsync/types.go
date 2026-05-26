package dbsync

// Spec describes normalized cardano-db-sync runtime inputs.
//
// JSON tags are stable: they participate in plan fingerprint computation and
// must not change after release. Adding fields is safe; renaming a field's
// JSON tag will re-roll every existing fingerprint.
type Spec struct {
	// NetworkName is the Cardano network identifier rendered into the db-sync
	// configuration (e.g. "mainnet", "preprod", a local network name).
	NetworkName string `json:"networkName"`

	// RequiresNetworkMagic toggles the db-sync RequiresNetworkMagic field
	// between RequiresMagic (true) and RequiresNoMagic (false).
	RequiresNetworkMagic bool `json:"requiresNetworkMagic"`

	// NetworkArtifactHash is the upstream network artifact content hash used to
	// invalidate the plan when artifacts change. Empty means unbound.
	NetworkArtifactHash string `json:"networkArtifactHash,omitempty"`

	// Image is the cardano-db-sync container image reference.
	Image string `json:"image"`

	// NodeToNode identifies the upstream Cardano node the follower connects to.
	NodeToNode NodeToNode `json:"nodeToNode"`

	// Database identifies the Postgres database db-sync writes to.
	Database Database `json:"database"`

	// Runtime controls cardano-db-sync command-line behavior.
	Runtime Runtime `json:"runtime"`

	// Storage controls durable db-sync state behavior.
	Storage Storage `json:"storage"`

	// Insert controls upstream cardano-db-sync insert_options.
	Insert InsertOptions `json:"insert"`

	// IPFSGateways is the optional list of IPFS gateways exposed to db-sync.
	IPFSGateways []string `json:"ipfsGateways,omitempty"`

	// Paths identifies the container filesystem locations used by the plan.
	Paths Paths `json:"paths"`
}

// NodeToNode describes the upstream Cardano node endpoint the follower node
// connects to.
type NodeToNode struct {
	// Host is the upstream node DNS name or address.
	Host string `json:"host"`

	// Port is the upstream node-to-node TCP port.
	Port int32 `json:"port"`
}

// Database describes the Postgres database endpoint used by db-sync.
type Database struct {
	// Host is the Postgres server DNS name or address.
	Host string `json:"host"`

	// Port is the Postgres TCP port; defaults to 5432.
	Port int32 `json:"port"`

	// Name is the Postgres database name; defaults to "cexplorer".
	Name string `json:"name"`

	// User is the Postgres role used by db-sync; defaults to "postgres".
	User string `json:"user"`

	// PasswordSecretName is the Kubernetes Secret holding the Postgres
	// password.
	PasswordSecretName string `json:"passwordSecretName"`

	// PasswordSecretKey is the key inside PasswordSecretName; defaults to
	// "password".
	PasswordSecretKey string `json:"passwordSecretKey"`

	// SSLMode is the libpq sslmode value; defaults to "disable".
	SSLMode string `json:"sslMode"`
}

// Runtime describes cardano-db-sync command-line behavior. The disable
// booleans map directly to db-sync's --disable-* flags: a false zero value
// leaves the feature active.
type Runtime struct {
	// DisableCache controls the db-sync --disable-cache flag.
	DisableCache bool `json:"disableCache"`

	// DisableEpochTable controls the db-sync --disable-epoch flag.
	DisableEpochTable bool `json:"disableEpochTable"`

	// ForceIndexes controls the db-sync --force-indexes flag.
	ForceIndexes bool `json:"forceIndexes"`

	// MetricsPort is the Prometheus metrics port db-sync exposes.
	MetricsPort int32 `json:"metricsPort"`
}

// Storage describes durable db-sync state behavior.
type Storage struct {
	// LedgerBackend selects the db-sync ledger backend (e.g. "lsm").
	LedgerBackend string `json:"ledgerBackend"`

	// NearTipEpoch is the epoch threshold where db-sync considers itself near
	// the chain tip for snapshot frequency.
	NearTipEpoch int64 `json:"nearTipEpoch"`

	// StateStorageSize is the PVC size for durable db-sync state.
	StateStorageSize string `json:"stateStorageSize"`
}

// InsertOptions describes upstream cardano-db-sync insert_options.
//
// The planner treats a fully-zero InsertOptions as a request for
// DefaultInsertOptions(); callers that want to start from defaults and
// override individual fields should construct via DefaultInsertOptions to
// avoid that collapse.
type InsertOptions struct {
	// TxCBOR is the tx_cbor insert mode ("enable" or "disable").
	TxCBOR string `json:"txCBOR"`

	// TxOut configures the upstream tx_out section.
	TxOut TxOutOption `json:"txOut"`

	// Ledger is the ledger insert mode ("enable" or "disable").
	Ledger string `json:"ledger"`

	// Shelley toggles the Shelley feature insert.
	Shelley FeatureSelection `json:"shelley"`

	// MultiAsset toggles the multi_asset feature insert.
	MultiAsset FeatureSelection `json:"multiAsset"`

	// Metadata toggles the metadata feature insert.
	Metadata FeatureSelection `json:"metadata"`

	// Plutus toggles the plutus feature insert.
	Plutus FeatureSelection `json:"plutus"`

	// Governance is the governance insert mode.
	Governance string `json:"governance"`

	// OffchainPoolData is the offchain_pool_data insert mode.
	OffchainPoolData string `json:"offchainPoolData"`

	// OffchainVoteData is the offchain_vote_data insert mode.
	OffchainVoteData string `json:"offchainVoteData"`

	// PoolStats is the pool_stat insert mode.
	PoolStats string `json:"poolStats"`

	// JSONType is the json_type insert mode.
	JSONType string `json:"jsonType"`

	// RemoveJSONBFromSchema is the remove_jsonb_from_schema insert mode.
	RemoveJSONBFromSchema string `json:"removeJSONBFromSchema"`
}

// TxOutOption describes the upstream tx_out option.
type TxOutOption struct {
	// Mode is the tx_out value (e.g. "enable", "disable", "consumed",
	// "bootstrap").
	Mode string `json:"mode"`

	// ForceTxIn maps to tx_out.force_tx_in.
	ForceTxIn bool `json:"forceTxIn"`

	// UseAddressTable maps to tx_out.use_address_table.
	UseAddressTable bool `json:"useAddressTable"`
}

// FeatureSelection describes a boolean insert_options feature gate with
// optional filters.
type FeatureSelection struct {
	// Enabled toggles the feature.
	Enabled bool `json:"enabled"`

	// StakeAddresses limits the feature to specific stake addresses.
	StakeAddresses []string `json:"stakeAddresses,omitempty"`

	// Policies limits the feature to specific asset policies.
	Policies []string `json:"policies,omitempty"`

	// Keys limits the feature to specific metadata keys.
	Keys []int64 `json:"keys,omitempty"`

	// ScriptHashes limits the feature to specific script hashes.
	ScriptHashes []string `json:"scriptHashes,omitempty"`
}

// Paths describes stable container filesystem paths.
type Paths struct {
	// ConfigFile is the rendered db-sync configuration mount path.
	ConfigFile string `json:"configFile"`

	// TopologyFile is the rendered follower topology mount path.
	TopologyFile string `json:"topologyFile"`

	// NodeConfig is the upstream cardano-node configuration mount path.
	NodeConfig string `json:"nodeConfig"`

	// SocketPath is the cardano-node IPC socket path shared with db-sync.
	SocketPath string `json:"socketPath"`

	// StateDir is the durable db-sync state directory.
	StateDir string `json:"stateDir"`

	// PGPassFile is the libpq password file path.
	PGPassFile string `json:"pgPassFile"`
}

// Plan is the deterministic db-sync runtime plan consumed by Kubernetes
// workload builders.
type Plan struct {
	// Spec is the normalized db-sync input specification.
	Spec Spec

	// ConfigYAML is the rendered db-sync configuration file.
	ConfigYAML string

	// TopologyJSON is the rendered follower topology file.
	TopologyJSON string

	// Run is the cardano-db-sync command invocation (arguments only; the
	// container image owns the binary).
	Run Invocation

	// Fingerprint identifies the full normalized Spec. It changes whenever any
	// planner input changes.
	Fingerprint Fingerprint

	// DatabaseIdentityFingerprint identifies the subset of Spec inputs that
	// require the underlying Postgres data directory to be recreated when they
	// change (network identity, image, database connection identity, ledger
	// backend, insert options).
	DatabaseIdentityFingerprint Fingerprint
}

// Invocation describes a command invocation without executing it.
type Invocation struct {
	// Command is the executable name or path; the container image owns the
	// binary, so this is typically empty.
	Command string

	// Args are the arguments passed to Command.
	Args []string
}

// Fingerprint identifies a normalized db-sync plan input.
type Fingerprint struct {
	// Algorithm is the digest algorithm used to compute Value.
	Algorithm string `json:"algorithm"`

	// Value is the hex-encoded digest.
	Value string `json:"value"`
}

// EnvVar describes a non-secret process environment variable.
type EnvVar struct {
	// Name is the environment variable name.
	Name string

	// Value is the environment variable value.
	Value string
}
