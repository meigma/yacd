package cardanodbsync

const (
	// defaultExternalDatabasePasswordKey is the data key the controller
	// reads from an external Postgres password Secret when the spec does
	// not override it.
	defaultExternalDatabasePasswordKey = "password"

	// defaultFollowerNodeImageRepository is the OCI repository the
	// follower-node container image is sourced from when the spec does not
	// override it.
	defaultFollowerNodeImageRepository = "ghcr.io/meigma/yacd/cardano-testnet"

	// defaultFollowerNodeImageRevision is the YACD packaging revision
	// appended to the cardano-node version tag when the spec does not
	// override the follower-node image.
	defaultFollowerNodeImageRevision = "yacd.4"

	// defaultFollowerNodeStorageSize is the requested PVC size for the
	// follower node state volume when the CardanoDBSync spec does not
	// override it.
	defaultFollowerNodeStorageSize = "10Gi"

	// dbSyncRunAsID is the numeric UID and GID the dbsync workload runs
	// containers as. The follower-node and db-sync containers share this
	// ID so they can both read/write the IPC socket EmptyDir.
	dbSyncRunAsID = int64(10001)

	// defaultLedgerBackend is the ledger backend the db-sync planner uses
	// when the CardanoDBSync spec does not override it. "lsm" matches the
	// upstream default for current Cardano network sizes.
	defaultLedgerBackend = "lsm"

	// defaultNearTipEpoch is the epoch boundary the db-sync planner uses
	// for the "near-tip" snapshot threshold when the spec does not
	// override it.
	defaultNearTipEpoch = int64(580)

	// defaultManagedPostgresImage is the Postgres container image used by
	// the managed Postgres workload when the CardanoDBSync spec does not
	// override it.
	defaultManagedPostgresImage = "postgres:17.2-alpine"

	// defaultManagedPostgresDatabase is the Postgres database name used by
	// the managed Postgres workload when the CardanoDBSync spec does not
	// override it.
	defaultManagedPostgresDatabase = "cexplorer"

	// defaultManagedPostgresUser is the Postgres superuser name used by
	// the managed Postgres workload when the CardanoDBSync spec does not
	// override it.
	defaultManagedPostgresUser = "postgres"

	// defaultManagedPostgresStorageSize is the requested PVC size for the
	// managed Postgres data volume when the CardanoDBSync spec does not
	// override it.
	defaultManagedPostgresStorageSize = "10Gi"

	// defaultManagedPostgresSSLMode is the libpq sslmode the dbsync
	// workload uses when connecting to the managed Postgres workload.
	// The managed Postgres deployment does not configure TLS, so disable
	// is the only supported value today.
	defaultManagedPostgresSSLMode = "disable"

	// managedPostgresPort is the in-cluster TCP port the managed Postgres
	// Service publishes. Standard Postgres port; not configurable through
	// the CardanoDBSync spec.
	managedPostgresPort = int32(5432)

	// managedPostgresRunAsID is the numeric UID and GID the managed
	// Postgres container runs as. Matches the upstream postgres image
	// "postgres" user.
	managedPostgresRunAsID = int64(70)

	// managedPostgresPasswordKey is the data key the managed Postgres
	// auth Secret stores the password under. Not configurable through the
	// spec because the controller renders the Secret directly.
	managedPostgresPasswordKey = "password"
)
