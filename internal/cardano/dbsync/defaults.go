package dbsync

// Database defaults applied when the caller leaves Database fields zero.
const (
	// defaultDatabasePort is the standard Postgres TCP port.
	defaultDatabasePort int32 = 5432

	// defaultDatabaseName is the db-sync convention for the application
	// database name.
	defaultDatabaseName = "cexplorer"

	// defaultDatabaseUser is the default Postgres role used by db-sync.
	defaultDatabaseUser = "postgres"

	// defaultDatabasePasswordKey is the default key looked up inside the
	// database password Secret.
	defaultDatabasePasswordKey = "password"

	// defaultDatabaseSSLMode is the libpq sslmode used by default.
	defaultDatabaseSSLMode = "disable"
)

// Runtime defaults applied when the caller leaves Runtime fields zero.
const (
	// defaultMetricsPort is the Prometheus metrics port db-sync exposes.
	defaultMetricsPort int32 = 8080
)

// Storage defaults applied when the caller leaves Storage fields zero.
const (
	// defaultLedgerBackend is the db-sync ledger backend used by YACD.
	defaultLedgerBackend = "lsm"

	// defaultNearTipEpoch is the snapshot near-tip threshold used by YACD.
	defaultNearTipEpoch int64 = 580

	// defaultStateStorageSize is the durable db-sync state PVC size.
	defaultStateStorageSize = "10Gi"
)

// Path defaults applied when the caller leaves Paths fields blank.
const (
	// defaultConfigFile is the rendered db-sync configuration mount path.
	defaultConfigFile = "/config/db-sync-config.yaml"

	// defaultTopologyFile is the rendered follower topology mount path.
	defaultTopologyFile = "/config/follower-topology.json"

	// defaultNodeConfig is the upstream cardano-node configuration mount path.
	defaultNodeConfig = "/network-artifacts/configuration.yaml"

	// defaultSocketPath is the cardano-node IPC socket path shared with
	// db-sync.
	defaultSocketPath = "/ipc/node.socket"

	// defaultStateDir is the durable db-sync state directory.
	defaultStateDir = "/var/lib/cexplorer"

	// defaultPGPassFile is the libpq password file path.
	defaultPGPassFile = "/configuration/pgpass"
)

// InsertOptions defaults applied when the caller leaves a fully-zero
// InsertOptions value.
const (
	// defaultTxCBOR is the tx_cbor insert mode used by YACD.
	defaultTxCBOR = "disable"

	// defaultTxOutMode is the tx_out insert mode used by YACD.
	defaultTxOutMode = "enable"

	// defaultLedger is the ledger insert mode used by YACD.
	defaultLedger = "enable"

	// defaultJSONType is the json_type insert mode used by YACD.
	defaultJSONType = "text"

	// insertOptionDisable is the upstream "disable" insert mode literal.
	insertOptionDisable = "disable"

	// insertOptionEnable is the upstream "enable" insert mode literal.
	insertOptionEnable = "enable"
)

// DefaultInsertOptions returns the YACD-recommended cardano-db-sync
// insert_options baseline. Callers that want to start from defaults and
// override individual fields should construct via this function so that
// zero-valued boolean fields (e.g. FeatureSelection.Enabled) are populated
// correctly.
func DefaultInsertOptions() InsertOptions {
	return InsertOptions{
		TxCBOR:                defaultTxCBOR,
		TxOut:                 TxOutOption{Mode: defaultTxOutMode},
		Ledger:                defaultLedger,
		Shelley:               FeatureSelection{Enabled: true},
		MultiAsset:            FeatureSelection{Enabled: true},
		Metadata:              FeatureSelection{Enabled: true},
		Plutus:                FeatureSelection{Enabled: true},
		Governance:            insertOptionEnable,
		OffchainPoolData:      insertOptionDisable,
		OffchainVoteData:      insertOptionDisable,
		PoolStats:             insertOptionEnable,
		JSONType:              defaultJSONType,
		RemoveJSONBFromSchema: insertOptionDisable,
	}
}
