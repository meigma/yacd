package annotations

const (
	// ArtifactDataHash holds the canonical hash of a published artifact payload.
	ArtifactDataHash = "yacd.meigma.io/artifact-data-hash"

	// ArtifactSchemaVersion holds the schema version of a published artifact payload.
	ArtifactSchemaVersion = "yacd.meigma.io/artifact-schema-version"

	// LocalnetFingerprint holds the accepted localnet identity fingerprint.
	LocalnetFingerprint = "yacd.meigma.io/localnet-fingerprint"

	// NetworkFingerprint holds the accepted mode-neutral network identity fingerprint.
	NetworkFingerprint = "yacd.meigma.io/network-fingerprint"

	// DBSyncSidecarRevision holds the opaque revision for db-sync sidecar
	// material consumed by a CardanoNetwork primary Pod.
	DBSyncSidecarRevision = "yacd.meigma.io/dbsync-sidecar-revision"

	// RequestedStorageClass holds the storage class requested when a
	// controller-owned PVC was created.
	RequestedStorageClass = "yacd.meigma.io/requested-storage-class"
)
