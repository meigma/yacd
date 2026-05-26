package cardanodbsync

import (
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
)

const (
	// dbSyncNameSuffix is the suffix appended to the CardanoDBSync name for
	// the dbsync workload Deployment.
	dbSyncNameSuffix = "dbsync"

	// dbSyncConfigMapSuffix is the suffix for the rendered db-sync + follower
	// configuration ConfigMap.
	dbSyncConfigMapSuffix = "dbsync-config"

	// dbSyncStatePVCSuffix is the suffix for the durable db-sync state PVC.
	dbSyncStatePVCSuffix = "dbsync-state"

	// dbSyncFollowerPVCSuffix is the suffix for the follower node state PVC.
	dbSyncFollowerPVCSuffix = "follower-state"

	// dbSyncPGPassSecretSuffix is the suffix for the pgpass Secret consumed
	// by the db-sync container.
	dbSyncPGPassSecretSuffix = "dbsync-pgpass"

	// dbSyncMetricsSuffix is the suffix for the db-sync metrics Service.
	dbSyncMetricsSuffix = "dbsync-metrics"

	// managedPostgresAuthSecretSuffix is the suffix for the auth Secret used
	// by the managed Postgres workload (only the generated path; the user
	// can override with spec.database.managed.authSecretRef).
	managedPostgresAuthSecretSuffix = "postgres-auth"

	// managedPostgresStatePVCSuffix is the suffix for the durable managed
	// Postgres data PVC.
	managedPostgresStatePVCSuffix = "postgres-state"

	// managedPostgresSuffix is the suffix for the managed Postgres
	// Deployment and Service. They share a name because the Service selects
	// the Deployment's Pods directly.
	managedPostgresSuffix = "postgres"
)

// dbSyncWorkloadName returns the DNS-label name of the dbsync workload
// Deployment (the two-container follower-node + db-sync Deployment).
func dbSyncWorkloadName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, dbSyncNameSuffix)
}

// dbSyncConfigMapName returns the DNS-label name of the ConfigMap holding
// the rendered db-sync configuration and follower-node topology.
func dbSyncConfigMapName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, dbSyncConfigMapSuffix)
}

// dbSyncStatePVCName returns the DNS-label name of the PVC backing the
// db-sync container's durable state.
func dbSyncStatePVCName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, dbSyncStatePVCSuffix)
}

// dbSyncFollowerPVCName returns the DNS-label name of the PVC backing the
// follower node's durable state.
func dbSyncFollowerPVCName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, dbSyncFollowerPVCSuffix)
}

// dbSyncPGPassSecretName returns the DNS-label name of the Secret holding
// the rendered pgpass file the db-sync container mounts.
func dbSyncPGPassSecretName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, dbSyncPGPassSecretSuffix)
}

// dbSyncMetricsServiceName returns the DNS-label name of the metrics
// Service that fronts the db-sync container's Prometheus endpoint.
func dbSyncMetricsServiceName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, dbSyncMetricsSuffix)
}

// managedPostgresAuthSecretName returns the DNS-label name of the
// controller-generated managed-Postgres auth Secret. Used only when the
// user does not provide spec.database.managed.authSecretRef.
func managedPostgresAuthSecretName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, managedPostgresAuthSecretSuffix)
}

// managedPostgresPVCName returns the DNS-label name of the PVC backing the
// managed Postgres data directory.
func managedPostgresPVCName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, managedPostgresStatePVCSuffix)
}

// managedPostgresServiceName returns the DNS-label name of the managed
// Postgres Service.
func managedPostgresServiceName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, managedPostgresSuffix)
}

// managedPostgresDeploymentName returns the DNS-label name of the managed
// Postgres Deployment. It matches the Service name because the Service
// selects the Deployment's Pods directly.
func managedPostgresDeploymentName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, managedPostgresSuffix)
}
