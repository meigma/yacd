package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CardanoDBSyncLedgerBackend selects how db-sync stores its ledger UTxO set.
// +kubebuilder:validation:Enum=inmemory;lsm
type CardanoDBSyncLedgerBackend string

const (
	// CardanoDBSyncLedgerBackendInMemory keeps the ledger UTxO set in memory.
	CardanoDBSyncLedgerBackendInMemory CardanoDBSyncLedgerBackend = "inmemory"
	// CardanoDBSyncLedgerBackendLSM stores the ledger UTxO set on disk using LSM trees.
	CardanoDBSyncLedgerBackendLSM CardanoDBSyncLedgerBackend = "lsm"
)

// CardanoDBSyncInsertPreset selects an upstream db-sync insert profile.
// +kubebuilder:validation:Enum=full;only_utxo;only_governance;disable_all
type CardanoDBSyncInsertPreset string

const (
	// CardanoDBSyncInsertPresetFull enables the normal full db-sync schema surface.
	CardanoDBSyncInsertPresetFull CardanoDBSyncInsertPreset = "full"
	// CardanoDBSyncInsertPresetOnlyUTxO keeps only UTxO-oriented data.
	CardanoDBSyncInsertPresetOnlyUTxO CardanoDBSyncInsertPreset = "only_utxo"
	// CardanoDBSyncInsertPresetOnlyGovernance keeps governance-oriented data.
	CardanoDBSyncInsertPresetOnlyGovernance CardanoDBSyncInsertPreset = "only_governance"
	// CardanoDBSyncInsertPresetDisableAll keeps only the minimum block and tx data.
	CardanoDBSyncInsertPresetDisableAll CardanoDBSyncInsertPreset = "disable_all"
)

// CardanoDBSyncTxOutMode selects the upstream tx_out insert mode.
// +kubebuilder:validation:Enum=enable;disable;consumed;prune;bootstrap
type CardanoDBSyncTxOutMode string

const (
	// CardanoDBSyncTxOutModeEnable stores all transaction inputs and outputs.
	CardanoDBSyncTxOutModeEnable CardanoDBSyncTxOutMode = "enable"
	// CardanoDBSyncTxOutModeDisable disables transaction input and output tables.
	CardanoDBSyncTxOutModeDisable CardanoDBSyncTxOutMode = "disable"
	// CardanoDBSyncTxOutModeConsumed stores consumed_by_tx_id for direct UTxO queries.
	CardanoDBSyncTxOutModeConsumed CardanoDBSyncTxOutMode = "consumed"
	// CardanoDBSyncTxOutModePrune periodically prunes consumed transaction outputs.
	CardanoDBSyncTxOutModePrune CardanoDBSyncTxOutMode = "prune"
	// CardanoDBSyncTxOutModeBootstrap delays UTxO insertion until db-sync reaches the tip.
	CardanoDBSyncTxOutModeBootstrap CardanoDBSyncTxOutMode = "bootstrap"
)

// CardanoDBSyncLedgerMode selects whether db-sync maintains and uses ledger state.
// +kubebuilder:validation:Enum=enable;disable;ignore
type CardanoDBSyncLedgerMode string

const (
	// CardanoDBSyncLedgerModeEnable maintains ledger state and uses ledger-derived data.
	CardanoDBSyncLedgerModeEnable CardanoDBSyncLedgerMode = "enable"
	// CardanoDBSyncLedgerModeDisable avoids maintaining ledger state.
	CardanoDBSyncLedgerModeDisable CardanoDBSyncLedgerMode = "disable"
	// CardanoDBSyncLedgerModeIgnore maintains ledger state but does not use its data.
	CardanoDBSyncLedgerModeIgnore CardanoDBSyncLedgerMode = "ignore"
)

// CardanoDBSyncJSONType selects the database JSON storage representation.
// +kubebuilder:validation:Enum=text;jsonb;disable
type CardanoDBSyncJSONType string

const (
	// CardanoDBSyncJSONTypeText stores JSON data as text.
	CardanoDBSyncJSONTypeText CardanoDBSyncJSONType = "text"
	// CardanoDBSyncJSONTypeJSONB stores JSON data as jsonb.
	CardanoDBSyncJSONTypeJSONB CardanoDBSyncJSONType = "jsonb"
	// CardanoDBSyncJSONTypeDisable disables JSON data storage where supported.
	CardanoDBSyncJSONTypeDisable CardanoDBSyncJSONType = "disable"
)

// CardanoDBSyncSpec defines the desired db-sync supporting service.
type CardanoDBSyncSpec struct {
	// networkRef references the same-namespace CardanoNetwork that db-sync
	// indexes.
	// +required
	NetworkRef CardanoDBSyncNetworkReference `json:"networkRef"`

	// image is the cardano-db-sync image reference.
	// +kubebuilder:default="ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0"
	// +required
	Image string `json:"image"`

	// resources configures the db-sync container resources.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// followerNode configures the dedicated follower node colocated with
	// db-sync for local node socket access.
	// +optional
	FollowerNode *CardanoDBSyncFollowerNodeSpec `json:"followerNode,omitempty"`

	// database configures the Postgres database used by db-sync.
	// +required
	Database CardanoDBSyncDatabaseSpec `json:"database"`

	// stateStorage configures persistent storage for db-sync ledger state.
	// +optional
	StateStorage *CardanoDBSyncStorageSpec `json:"stateStorage,omitempty"`

	// config configures upstream db-sync behavior using Kubernetes-style field
	// names. The controller translates this object into the upstream db-sync
	// configuration file.
	// +required
	Config CardanoDBSyncConfigSpec `json:"config"`
}

// CardanoDBSyncFollowerNodeSpec configures the follower node owned by db-sync.
type CardanoDBSyncFollowerNodeSpec struct {
	// image optionally overrides the follower cardano-node image. When omitted,
	// the controller derives an image from the referenced CardanoNetwork.
	// +optional
	Image *string `json:"image,omitempty"`

	// storage configures persistent follower node database storage.
	// +optional
	Storage *CardanoDBSyncStorageSpec `json:"storage,omitempty"`

	// resources configures the follower node container resources.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// CardanoDBSyncStorageSpec configures persistent storage for db-sync resources.
type CardanoDBSyncStorageSpec struct {
	// size is the requested persistent volume size.
	// +required
	Size resource.Quantity `json:"size"`

	// storageClassName optionally selects the Kubernetes StorageClass used for
	// the persistent volume claim.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// CardanoDBSyncDatabaseSpec configures the YACD-managed Postgres database.
type CardanoDBSyncDatabaseSpec struct {
	// image is the Postgres image reference.
	// +kubebuilder:default="postgres:17.2-alpine"
	// +required
	Image string `json:"image"`

	// database is the Postgres database name.
	// +kubebuilder:default="cexplorer"
	// +required
	Database string `json:"database"`

	// user is the Postgres user name.
	// +kubebuilder:default="postgres"
	// +required
	User string `json:"user"`

	// authSecretRef optionally references a same-namespace Secret containing
	// the Postgres password in the key "password". When omitted, the controller
	// creates an owned Secret and reports its name in status.
	// +optional
	AuthSecretRef *CardanoDBSyncSecretReference `json:"authSecretRef,omitempty"`

	// storage configures persistent Postgres data storage.
	// +optional
	Storage *CardanoDBSyncStorageSpec `json:"storage,omitempty"`

	// parameters configures basic Postgres startup parameters used by the
	// managed local prototype.
	// +optional
	Parameters *CardanoDBSyncPostgresParametersSpec `json:"parameters,omitempty"`

	// resources configures the Postgres container resources.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// CardanoDBSyncNetworkReference identifies a same-namespace CardanoNetwork.
type CardanoDBSyncNetworkReference struct {
	// name is the name of the referenced CardanoNetwork.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// CardanoDBSyncSecretReference identifies a same-namespace Secret.
type CardanoDBSyncSecretReference struct {
	// name is the name of the referenced Secret.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// CardanoDBSyncPostgresParametersSpec configures basic Postgres settings.
type CardanoDBSyncPostgresParametersSpec struct {
	// maintenanceWorkMem sets the Postgres maintenance_work_mem parameter.
	// +optional
	MaintenanceWorkMem *resource.Quantity `json:"maintenanceWorkMem,omitempty"`

	// maxParallelMaintenanceWorkers sets the Postgres
	// max_parallel_maintenance_workers parameter.
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxParallelMaintenanceWorkers *int32 `json:"maxParallelMaintenanceWorkers,omitempty"`
}

// CardanoDBSyncConfigSpec configures upstream db-sync behavior.
type CardanoDBSyncConfigSpec struct {
	// runtime configures db-sync runtime flags that are not part of
	// insert_options.
	// +optional
	Runtime *CardanoDBSyncRuntimeSpec `json:"runtime,omitempty"`

	// ledgerBackend selects how db-sync stores its ledger UTxO set.
	// +kubebuilder:default=lsm
	// +required
	LedgerBackend CardanoDBSyncLedgerBackend `json:"ledgerBackend"`

	// snapshot configures ledger state snapshot behavior.
	// +optional
	Snapshot *CardanoDBSyncSnapshotSpec `json:"snapshot,omitempty"`

	// insert configures upstream db-sync insert_options.
	// +optional
	Insert *CardanoDBSyncInsertSpec `json:"insert,omitempty"`

	// ipfsGateways lists gateways used for offchain metadata fetching.
	// +optional
	IPFSGateways []string `json:"ipfsGateways,omitempty"`
}

// CardanoDBSyncRuntimeSpec configures db-sync runtime flags.
type CardanoDBSyncRuntimeSpec struct {
	// cache controls whether db-sync caches are enabled.
	// +kubebuilder:default=true
	// +required
	Cache bool `json:"cache"`

	// epochTable controls whether db-sync populates the epoch table.
	// +kubebuilder:default=true
	// +required
	EpochTable bool `json:"epochTable"`

	// forceIndexes controls whether db-sync creates indexes at startup rather
	// than later in the sync lifecycle.
	// +kubebuilder:default=false
	// +required
	ForceIndexes bool `json:"forceIndexes"`

	// metricsPort is the db-sync Prometheus metrics port.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=8080
	// +required
	MetricsPort int32 `json:"metricsPort"`
}

// CardanoDBSyncSnapshotSpec configures db-sync ledger state snapshots.
type CardanoDBSyncSnapshotSpec struct {
	// nearTipEpoch is the epoch threshold where db-sync considers itself near
	// tip for snapshot frequency.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=580
	// +required
	NearTipEpoch int64 `json:"nearTipEpoch"`
}

// CardanoDBSyncInsertSpec configures upstream db-sync insert_options.
type CardanoDBSyncInsertSpec struct {
	// preset selects an upstream insert profile. Explicit fields in this object
	// are interpreted as overrides by the future controller.
	// +kubebuilder:default=full
	// +required
	Preset CardanoDBSyncInsertPreset `json:"preset"`

	// txCbor controls transaction CBOR collection.
	// +kubebuilder:default=false
	// +required
	TxCBOR bool `json:"txCbor"`

	// txOut configures transaction output storage.
	// +optional
	TxOut *CardanoDBSyncTxOutSpec `json:"txOut,omitempty"`

	// ledger controls ledger state maintenance and use.
	// +kubebuilder:default=enable
	// +required
	Ledger CardanoDBSyncLedgerMode `json:"ledger"`

	// shelley configures Shelley-era table inserts.
	// +optional
	Shelley *CardanoDBSyncShelleyInsertSpec `json:"shelley,omitempty"`

	// multiAsset configures multi-asset table inserts.
	// +optional
	MultiAsset *CardanoDBSyncMultiAssetInsertSpec `json:"multiAsset,omitempty"`

	// metadata configures transaction metadata inserts.
	// +optional
	Metadata *CardanoDBSyncMetadataInsertSpec `json:"metadata,omitempty"`

	// plutus configures Plutus and script table inserts.
	// +optional
	Plutus *CardanoDBSyncPlutusInsertSpec `json:"plutus,omitempty"`

	// governance controls governance-related data inserts.
	// +kubebuilder:default=true
	// +required
	Governance bool `json:"governance"`

	// offchainPoolData controls stake pool offchain metadata fetching.
	// +kubebuilder:default=false
	// +required
	OffchainPoolData bool `json:"offchainPoolData"`

	// offchainVoteData controls governance offchain metadata fetching.
	// +kubebuilder:default=false
	// +required
	OffchainVoteData bool `json:"offchainVoteData"`

	// poolStats controls pool stats inserts.
	// +kubebuilder:default=false
	// +required
	PoolStats bool `json:"poolStats"`

	// jsonType controls the upstream json_type insert option.
	// +kubebuilder:default=text
	// +required
	JSONType CardanoDBSyncJSONType `json:"jsonType"`

	// removeJsonbFromSchema controls whether db-sync removes jsonb data types
	// from affected schema columns.
	// +kubebuilder:default=false
	// +required
	RemoveJSONBFromSchema bool `json:"removeJsonbFromSchema"`
}

// CardanoDBSyncTxOutSpec configures upstream tx_out insert_options.
type CardanoDBSyncTxOutSpec struct {
	// mode selects the upstream tx_out value.
	// +kubebuilder:default=enable
	// +required
	Mode CardanoDBSyncTxOutMode `json:"mode"`

	// forceTxIn keeps tx_in populated for consumed, prune, or bootstrap modes.
	// +kubebuilder:default=false
	// +required
	ForceTxIn bool `json:"forceTxIn"`

	// useAddressTable enables the normalized address table schema variant.
	// +kubebuilder:default=false
	// +required
	UseAddressTable bool `json:"useAddressTable"`
}

// CardanoDBSyncShelleyInsertSpec configures Shelley-era inserts.
type CardanoDBSyncShelleyInsertSpec struct {
	// enabled controls Shelley-era data inserts.
	// +kubebuilder:default=true
	// +required
	Enabled bool `json:"enabled"`

	// stakeAddresses optionally limits Shelley data to specific stake
	// addresses.
	// +optional
	StakeAddresses []string `json:"stakeAddresses,omitempty"`
}

// CardanoDBSyncMultiAssetInsertSpec configures multi-asset inserts.
type CardanoDBSyncMultiAssetInsertSpec struct {
	// enabled controls multi-asset data inserts.
	// +kubebuilder:default=true
	// +required
	Enabled bool `json:"enabled"`

	// policies optionally limits multi-asset data to specific policy hashes.
	// +optional
	Policies []string `json:"policies,omitempty"`
}

// CardanoDBSyncMetadataInsertSpec configures metadata inserts.
type CardanoDBSyncMetadataInsertSpec struct {
	// enabled controls transaction metadata inserts.
	// +kubebuilder:default=true
	// +required
	Enabled bool `json:"enabled"`

	// keys optionally limits metadata inserts to specific numeric labels.
	// +optional
	Keys []int64 `json:"keys,omitempty"`
}

// CardanoDBSyncPlutusInsertSpec configures Plutus inserts.
type CardanoDBSyncPlutusInsertSpec struct {
	// enabled controls Plutus and script data inserts.
	// +kubebuilder:default=true
	// +required
	Enabled bool `json:"enabled"`

	// scriptHashes optionally limits Plutus data to specific script hashes.
	// +optional
	ScriptHashes []string `json:"scriptHashes,omitempty"`
}

// CardanoDBSyncStatus defines the observed state of CardanoDBSync.
type CardanoDBSyncStatus struct {
	// observedGeneration is the most recent generation observed by the
	// controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// endpoints publishes cluster-local connection details for db-sync
	// dependencies and observability.
	// +optional
	Endpoints *CardanoDBSyncEndpointsStatus `json:"endpoints,omitempty"`

	// database reports database-specific runtime details.
	// +optional
	Database *CardanoDBSyncDatabaseStatus `json:"database,omitempty"`

	// sync reports db-sync indexing progress.
	// +optional
	Sync *CardanoDBSyncProgressStatus `json:"sync,omitempty"`

	// conditions represent the current state of the CardanoDBSync resource.
	//
	// Expected condition types include:
	// - "Ready": db-sync is usable through its published database endpoint
	// - "FollowerNodeReady": the colocated follower node is running
	// - "PostgresReady": Postgres is running and accepting local connections
	// - "DBSyncReady": the db-sync process is running
	// - "Synced": db-sync has caught up to the follower node
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// CardanoDBSyncEndpointsStatus reports discovered Service endpoints.
type CardanoDBSyncEndpointsStatus struct {
	// postgres is the Postgres endpoint used by db-sync and clients.
	// +optional
	Postgres *ServiceEndpointStatus `json:"postgres,omitempty"`

	// metrics is the db-sync Prometheus metrics endpoint.
	// +optional
	Metrics *ServiceEndpointStatus `json:"metrics,omitempty"`
}

// CardanoDBSyncDatabaseStatus reports database-specific runtime details.
type CardanoDBSyncDatabaseStatus struct {
	// authSecretName is the same-namespace Secret containing generated
	// database credentials when the user did not provide authSecretRef.
	// +optional
	AuthSecretName string `json:"authSecretName,omitempty"`
}

// CardanoDBSyncProgressStatus reports db-sync indexing progress.
type CardanoDBSyncProgressStatus struct {
	// nodeBlockHeight is the latest block height reported by the follower node.
	// +optional
	NodeBlockHeight *int64 `json:"nodeBlockHeight,omitempty"`

	// dbBlockHeight is the latest block height inserted into Postgres.
	// +optional
	DBBlockHeight *int64 `json:"dbBlockHeight,omitempty"`

	// dbSlotHeight is the latest slot inserted into Postgres.
	// +optional
	DBSlotHeight *int64 `json:"dbSlotHeight,omitempty"`

	// dbQueueLength is the current db-sync database event queue length.
	// +optional
	DBQueueLength *int64 `json:"dbQueueLength,omitempty"`

	// lagBlocks is the difference between nodeBlockHeight and dbBlockHeight.
	// +optional
	LagBlocks *int64 `json:"lagBlocks,omitempty"`

	// epoch is the latest epoch observed by db-sync.
	// +optional
	Epoch *int64 `json:"epoch,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Network",type=string,JSONPath=`.spec.networkRef.name`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// CardanoDBSync is the Schema for the cardanodbsyncs API.
type CardanoDBSync struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of CardanoDBSync.
	// +required
	Spec CardanoDBSyncSpec `json:"spec"`

	// status defines the observed state of CardanoDBSync.
	// +optional
	Status CardanoDBSyncStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CardanoDBSyncList contains a list of CardanoDBSync.
type CardanoDBSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CardanoDBSync `json:"items"`
}
