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

// CardanoDBSyncPlacementMode selects where db-sync consumes its local node
// socket.
// +kubebuilder:validation:Enum=dedicatedFollower;primarySidecar
type CardanoDBSyncPlacementMode string

const (
	// CardanoDBSyncPlacementModeDedicatedFollower keeps the existing
	// two-container workload with a colocated follower node owned by the
	// CardanoDBSync controller.
	CardanoDBSyncPlacementModeDedicatedFollower CardanoDBSyncPlacementMode = "dedicatedFollower"
	// CardanoDBSyncPlacementModePrimarySidecar requests db-sync placement in
	// the referenced CardanoNetwork primary node Pod.
	CardanoDBSyncPlacementModePrimarySidecar CardanoDBSyncPlacementMode = "primarySidecar"
)

// CardanoDBSyncSpec defines the desired db-sync supporting service.
// +kubebuilder:validation:XValidation:rule="!has(self.placement) || self.placement.mode != 'primarySidecar' || !has(self.followerNode)",message="followerNode cannot be set when placement.mode is primarySidecar"
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

	// placement selects where db-sync runs relative to the referenced network.
	// When omitted, the controller preserves the existing dedicated follower
	// workload behavior.
	// +optional
	Placement *CardanoDBSyncPlacementSpec `json:"placement,omitempty"`

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
	// +optional
	Config CardanoDBSyncConfigSpec `json:"config,omitempty"`
}

// CardanoDBSyncPlacementSpec configures db-sync workload placement.
type CardanoDBSyncPlacementSpec struct {
	// mode selects whether db-sync uses a dedicated follower node or asks the
	// referenced CardanoNetwork primary Pod to host it as a sidecar.
	// +kubebuilder:default=dedicatedFollower
	// +required
	Mode CardanoDBSyncPlacementMode `json:"mode"`
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
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`

	// storageClassName optionally selects the Kubernetes StorageClass used for
	// the persistent volume claim.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// CardanoDBSyncDatabaseSpec configures the Postgres database used by db-sync.
// Exactly one database mode must be selected.
// +kubebuilder:validation:XValidation:rule="has(self.external) != has(self.managed)",message="exactly one of database.external or database.managed must be set"
type CardanoDBSyncDatabaseSpec struct {
	// external references a Postgres instance managed outside this
	// CardanoDBSync resource.
	// +optional
	External *CardanoDBSyncExternalDatabaseSpec `json:"external,omitempty"`

	// managed configures YACD-managed Postgres for local development
	// CardanoDBSync resources.
	// +optional
	Managed *CardanoDBSyncManagedDatabaseSpec `json:"managed,omitempty"`
}

// CardanoDBSyncExternalDatabaseSpec references an externally supplied Postgres
// database.
type CardanoDBSyncExternalDatabaseSpec struct {
	// host is the DNS name or IP address of the Postgres server.
	// +kubebuilder:validation:MinLength=1
	// +required
	Host string `json:"host"`

	// port is the Postgres server port.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=5432
	// +required
	Port int32 `json:"port"`

	// database is the Postgres database name.
	// +kubebuilder:default="cexplorer"
	// +required
	Database string `json:"database"`

	// user is the Postgres user name.
	// +kubebuilder:default="postgres"
	// +required
	User string `json:"user"`

	// passwordSecretRef references the same-namespace Secret containing the
	// Postgres password.
	// +required
	PasswordSecretRef CardanoDBSyncSecretKeyReference `json:"passwordSecretRef"`

	// sslMode controls Postgres TLS behavior.
	// +kubebuilder:validation:Enum=disable;require;verify-ca;verify-full
	// +kubebuilder:default=disable
	// +required
	SSLMode CardanoDBSyncPostgresSSLMode `json:"sslMode"`
}

// CardanoDBSyncManagedDatabaseSpec configures YACD-managed Postgres.
type CardanoDBSyncManagedDatabaseSpec struct {
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

// CardanoDBSyncPostgresSSLMode selects the libpq sslmode setting used by
// db-sync for Postgres connections.
// +kubebuilder:validation:Enum=disable;require;verify-ca;verify-full
type CardanoDBSyncPostgresSSLMode string

const (
	// CardanoDBSyncPostgresSSLModeDisable disables Postgres TLS.
	CardanoDBSyncPostgresSSLModeDisable CardanoDBSyncPostgresSSLMode = "disable"
	// CardanoDBSyncPostgresSSLModeRequire requires TLS without certificate verification.
	CardanoDBSyncPostgresSSLModeRequire CardanoDBSyncPostgresSSLMode = "require"
	// CardanoDBSyncPostgresSSLModeVerifyCA requires TLS and verifies the CA.
	CardanoDBSyncPostgresSSLModeVerifyCA CardanoDBSyncPostgresSSLMode = "verify-ca"
	// CardanoDBSyncPostgresSSLModeVerifyFull requires TLS and verifies CA plus hostname.
	CardanoDBSyncPostgresSSLModeVerifyFull CardanoDBSyncPostgresSSLMode = "verify-full"
)

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

// CardanoDBSyncSecretKeyReference identifies a same-namespace Secret key.
type CardanoDBSyncSecretKeyReference struct {
	// name is the name of the referenced Secret.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// key is the Secret data key containing the value.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:default=password
	// +required
	Key string `json:"key"`
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
	// are interpreted as overrides by the controller.
	// +kubebuilder:default=full
	// +required
	Preset CardanoDBSyncInsertPreset `json:"preset"`

	// txCbor controls transaction CBOR collection.
	// +optional
	TxCBOR *bool `json:"txCbor,omitempty"`

	// txOut configures transaction output storage.
	// +optional
	TxOut *CardanoDBSyncTxOutSpec `json:"txOut,omitempty"`

	// ledger controls ledger state maintenance and use.
	// +optional
	Ledger *CardanoDBSyncLedgerMode `json:"ledger,omitempty"`

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
	// +optional
	Governance *bool `json:"governance,omitempty"`

	// offchainPoolData controls stake pool offchain metadata fetching.
	// +optional
	OffchainPoolData *bool `json:"offchainPoolData,omitempty"`

	// offchainVoteData controls governance offchain metadata fetching.
	// +optional
	OffchainVoteData *bool `json:"offchainVoteData,omitempty"`

	// poolStats controls pool stats inserts.
	// +optional
	PoolStats *bool `json:"poolStats,omitempty"`

	// jsonType controls the upstream json_type insert option.
	// +optional
	JSONType *CardanoDBSyncJSONType `json:"jsonType,omitempty"`

	// removeJsonbFromSchema controls whether db-sync removes jsonb data types
	// from affected schema columns.
	// +optional
	RemoveJSONBFromSchema *bool `json:"removeJsonbFromSchema,omitempty"`
}

// CardanoDBSyncTxOutSpec configures upstream tx_out insert_options.
type CardanoDBSyncTxOutSpec struct {
	// mode selects the upstream tx_out value.
	// +optional
	Mode *CardanoDBSyncTxOutMode `json:"mode,omitempty"`

	// forceTxIn keeps tx_in populated for consumed, prune, or bootstrap modes.
	// +optional
	ForceTxIn *bool `json:"forceTxIn,omitempty"`

	// useAddressTable enables the normalized address table schema variant.
	// +optional
	UseAddressTable *bool `json:"useAddressTable,omitempty"`
}

// CardanoDBSyncShelleyInsertSpec configures Shelley-era inserts.
type CardanoDBSyncShelleyInsertSpec struct {
	// enabled controls Shelley-era data inserts.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// stakeAddresses optionally limits Shelley data to specific stake
	// addresses.
	// +optional
	StakeAddresses []string `json:"stakeAddresses,omitempty"`
}

// CardanoDBSyncMultiAssetInsertSpec configures multi-asset inserts.
type CardanoDBSyncMultiAssetInsertSpec struct {
	// enabled controls multi-asset data inserts.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// policies optionally limits multi-asset data to specific policy hashes.
	// +optional
	Policies []string `json:"policies,omitempty"`
}

// CardanoDBSyncMetadataInsertSpec configures metadata inserts.
type CardanoDBSyncMetadataInsertSpec struct {
	// enabled controls transaction metadata inserts.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// keys optionally limits metadata inserts to specific numeric labels.
	// +optional
	Keys []int64 `json:"keys,omitempty"`
}

// CardanoDBSyncPlutusInsertSpec configures Plutus inserts.
type CardanoDBSyncPlutusInsertSpec struct {
	// enabled controls Plutus and script data inserts.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

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

	// placement reports the effective placement mode and, when attachable,
	// the primary-sidecar material contract consumed by CardanoNetwork.
	// +optional
	Placement *CardanoDBSyncPlacementStatus `json:"placement,omitempty"`

	// conditions represent the current state of the CardanoDBSync resource.
	//
	// Expected condition types include:
	// - "Ready": db-sync is usable through its published database endpoint
	// - "FollowerNodeReady": the colocated follower node is running
	// - "NodeSocketReady": the node socket used by db-sync is reachable
	// - "SidecarMaterialReady": primary-sidecar mounted material is attachable
	// - "PostgresReady": Postgres is running and accepting local connections
	// - "DBSyncReady": the db-sync process is running
	// - "Synced": db-sync has caught up to the node tip
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// CardanoDBSyncPlacementStatus reports the effective db-sync placement.
type CardanoDBSyncPlacementStatus struct {
	// mode is the effective placement mode for this reconcile.
	// +optional
	Mode CardanoDBSyncPlacementMode `json:"mode,omitempty"`

	// primarySidecar publishes the attachable material contract when
	// SidecarMaterialReady=True.
	// +optional
	PrimarySidecar *CardanoDBSyncPrimarySidecarStatus `json:"primarySidecar,omitempty"`
}

// CardanoDBSyncPrimarySidecarStatus reports the primary-sidecar attachment
// contract consumed by CardanoNetwork.
type CardanoDBSyncPrimarySidecarStatus struct {
	// networkName is the referenced CardanoNetwork name this sidecar material
	// is valid for.
	// +optional
	NetworkName string `json:"networkName,omitempty"`

	// revision is an opaque sha256 rollout revision over all sidecar-mounted
	// material.
	// +optional
	Revision string `json:"revision,omitempty"`

	// resources names the CardanoDBSync-owned resources mounted by the primary
	// Pod sidecar.
	// +optional
	Resources CardanoDBSyncPrimarySidecarResourcesStatus `json:"resources,omitempty"`
}

// CardanoDBSyncPrimarySidecarResourcesStatus reports the DB Sync-owned
// resource names CardanoNetwork may mount into the primary Pod.
type CardanoDBSyncPrimarySidecarResourcesStatus struct {
	// configMapName is the db-sync configuration ConfigMap name.
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`

	// pgpassSecretName is the db-sync pgpass Secret name.
	// +optional
	PGPassSecretName string `json:"pgpassSecretName,omitempty"`

	// statePVCName is the db-sync state PVC name.
	// +optional
	StatePVCName string `json:"statePVCName,omitempty"`

	// metricsServiceName is the db-sync metrics Service name.
	// +optional
	MetricsServiceName string `json:"metricsServiceName,omitempty"`
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
	// acceptedIdentityFingerprint is the database-affecting plan identity that
	// the controller accepted on owned runtime material. Status mirrors the
	// value from the db-sync state PVC annotation and is not the authority for
	// identity validation.
	// +optional
	AcceptedIdentityFingerprint string `json:"acceptedIdentityFingerprint,omitempty"`

	// acceptedPlacementMode is the placement mode accepted for the current
	// db-sync state. Changing it requires deleting and recreating the
	// CardanoDBSync with a fresh or compatible database.
	// +kubebuilder:validation:Enum=dedicatedFollower;primarySidecar
	// +optional
	AcceptedPlacementMode CardanoDBSyncPlacementMode `json:"acceptedPlacementMode,omitempty"`

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
