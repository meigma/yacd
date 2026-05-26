package annotations

const (
	// ArtifactDataHash holds the canonical hash of a published artifact payload.
	ArtifactDataHash = "yacd.meigma.io/artifact-data-hash"

	// ArtifactSchemaVersion holds the schema version of a published artifact payload.
	ArtifactSchemaVersion = "yacd.meigma.io/artifact-schema-version"

	// LocalnetFingerprint holds the accepted localnet identity fingerprint.
	LocalnetFingerprint = "yacd.meigma.io/localnet-fingerprint"

	// RequestedStorageClass holds the storage class requested when a
	// controller-owned PVC was created.
	RequestedStorageClass = "yacd.meigma.io/requested-storage-class"
)
