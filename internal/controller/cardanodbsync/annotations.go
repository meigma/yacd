package cardanodbsync

import (
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
)

const (
	// dbSyncPlanFingerprintAnno is the annotation key carrying the dbsync
	// plan fingerprint on the ConfigMap, pgpass Secret, and Deployment
	// pod-template. Changes here roll the dbsync Pod.
	dbSyncPlanFingerprintAnno = "yacd.meigma.io/dbsync-plan-fingerprint"

	// dbSyncDatabaseIdentityAnno is the annotation key carrying the
	// accepted database identity fingerprint on the dbsync state PVC,
	// follower PVC, ConfigMap, pgpass Secret, and Deployment pod-template.
	// Drift between accepted and desired is a hard error.
	dbSyncDatabaseIdentityAnno = "yacd.meigma.io/dbsync-database-identity"

	// dbSyncSecretVersionAnno is the annotation key carrying the live
	// ResourceVersion of the database password Secret consumed by the
	// dbsync workload. A new ResourceVersion rolls the Pod through the
	// standard Deployment hash-change path.
	dbSyncSecretVersionAnno = "yacd.meigma.io/external-database-secret-resource-version"

	// dbSyncArtifactDataHashAnno is the annotation key carrying the
	// network artifact ConfigMap data hash consumed by the dbsync
	// workload. A new hash rolls the Pod through the standard Deployment
	// hash-change path.
	dbSyncArtifactDataHashAnno = "yacd.meigma.io/network-artifact-data-hash"

	// managedPostgresIdentityAnno is the annotation key carrying the
	// accepted managed-Postgres bootstrap identity fingerprint on the
	// managed Postgres PVC and Deployment.
	managedPostgresIdentityAnno = "yacd.meigma.io/managed-postgres-identity"

	// managedPostgresPasswordFingerprintAnno is the annotation key carrying
	// the SHA-256 fingerprint of the generated managed-Postgres password.
	// Stored on the controller-generated auth Secret so the controller can
	// detect password drift after database initialization.
	managedPostgresPasswordFingerprintAnno = "yacd.meigma.io/managed-postgres-password-fingerprint"
)

// cardanoDBSyncOwnedAnnotations enumerates every annotation key this
// controller takes ownership of on its owned objects. mergeDBSyncOwnedAnnotations
// preserves these keys on existing objects and discards every other
// annotation that has crept onto the live object.
//
// Add new owned annotations here so future extensions of
// mergeDBSyncOwnedAnnotations pick them up automatically.
var cardanoDBSyncOwnedAnnotations = []string{
	dbSyncPlanFingerprintAnno,
	dbSyncDatabaseIdentityAnno,
	dbSyncSecretVersionAnno,
	dbSyncArtifactDataHashAnno,
	managedPostgresIdentityAnno,
	managedPostgresPasswordFingerprintAnno,
	ctrlannotations.RequestedStorageClass,
}

// mergeDBSyncOwnedAnnotations preserves the cardanodbsync-owned annotation
// set from current onto desired and discards any unrelated annotations that
// live on the cluster object but are not owned by this controller.
func mergeDBSyncOwnedAnnotations(current map[string]string, desired map[string]string) map[string]string {
	return ctrlmetadata.MergeOwnedAnnotations(current, desired, cardanoDBSyncOwnedAnnotations...)
}
