package dbsync

// Spec describes normalized cardano-db-sync runtime inputs.
type Spec struct {
	NetworkName          string
	RequiresNetworkMagic bool
	NodeToNode           NodeToNode
	Database             Database
	Runtime              Runtime
	Storage              Storage
	Insert               InsertOptions
	IPFSGateways         []string
	Paths                Paths
}

// NodeToNode describes the upstream Cardano node endpoint the follower node
// should connect to.
type NodeToNode struct {
	Host string
	Port int32
}

// Database describes an externally supplied Postgres database.
type Database struct {
	Host               string
	Port               int32
	Name               string
	User               string
	PasswordSecretName string
	PasswordSecretKey  string
	SSLMode            string
}

// Runtime describes cardano-db-sync command-line behavior.
type Runtime struct {
	Cache        bool
	EpochTable   bool
	ForceIndexes bool
	MetricsPort  int32
}

// Storage describes durable db-sync state behavior.
type Storage struct {
	LedgerBackend    string
	NearTipEpoch     int64
	StateStorageSize string
}

// InsertOptions describes upstream cardano-db-sync insert_options.
type InsertOptions struct {
	TxCBOR                string
	TxOut                 TxOutOption
	Ledger                string
	Shelley               FeatureSelection
	MultiAsset            FeatureSelection
	Metadata              FeatureSelection
	Plutus                FeatureSelection
	Governance            string
	OffchainPoolData      string
	OffchainVoteData      string
	PoolStats             string
	JSONType              string
	RemoveJSONBFromSchema bool
}

// TxOutOption describes the upstream tx_out option.
type TxOutOption struct {
	Mode            string
	ForceTxIn       bool
	UseAddressTable bool
}

// FeatureSelection describes a boolean insert_options feature gate with
// optional filters.
type FeatureSelection struct {
	Enabled        bool
	StakeAddresses []string
	Policies       []string
	Keys           []int64
	ScriptHashes   []string
}

// Paths describes stable container filesystem paths.
type Paths struct {
	ConfigFile   string
	TopologyFile string
	NodeConfig   string
	SocketPath   string
	StateDir     string
	SchemaDir    string
	PGPassFile   string
}

// Plan is the deterministic db-sync runtime plan consumed by Kubernetes
// workload builders.
type Plan struct {
	Spec         Spec
	ConfigYAML   string
	TopologyJSON string
	Run          Invocation
	Fingerprint  Fingerprint
}

// Invocation describes a command invocation without executing it.
type Invocation struct {
	Command string
	Args    []string
}

// Fingerprint identifies a normalized db-sync plan.
type Fingerprint struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}
