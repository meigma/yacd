package cardanodbsync

import (
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
)

// Label key strategy.
//
// Standard "app.kubernetes.io/*" keys are consumed by generic dashboards and
// kubectl tooling and stay aligned with the Kubernetes recommended label set:
//
//   - labelAppName captures the application name (cardano-db-sync for both
//     dbsync and managed Postgres workloads, because the managed Postgres is
//     dedicated to this dbsync instance). It does NOT contain the
//     CardanoDBSync instance name.
//   - labelAppInstance is the per-CardanoDBSync instance discriminator and
//     matches the CR name after DNS-label sanitization.
//   - labelAppComponent describes the workload's role within the dbsync
//     topology (dbsync for the follower+db-sync workload, postgres for the
//     managed database). A future workload type should reuse this key with
//     its own value rather than inventing a new one.
//   - labelAppManagedBy is always "yacd" for resources this operator owns.
//
// YACD-specific "yacd.meigma.io/*" keys are the canonical selectors the
// operator's own predicates and Service selectors use:
//
//   - labelDBSync is the CardanoDBSync instance discriminator, mirroring
//     labelAppInstance. Selectors should prefer this key because it is
//     owned by the YACD label vocabulary.
//   - labelCardanoRole describes the workload role within the dbsync
//     topology and mirrors labelAppComponent for the same selector reason.
const (
	labelAppName       = "app.kubernetes.io/name"
	labelAppInstance   = "app.kubernetes.io/instance"
	labelAppComponent  = "app.kubernetes.io/component"
	labelAppManagedBy  = "app.kubernetes.io/managed-by"
	labelDBSync        = "yacd.meigma.io/cardanodbsync"
	labelCardanoRole   = "yacd.meigma.io/role"
	labelDBSyncAppName = "cardano-db-sync"

	// labelDBSyncRole is the labelAppComponent and labelCardanoRole value
	// for the dbsync workload (follower-node + db-sync containers).
	labelDBSyncRole = "dbsync"

	// managedPostgresRole is the labelAppComponent and labelCardanoRole
	// value for the managed Postgres workload.
	managedPostgresRole = "postgres"
)

// dbSyncWorkloadSelectorLabels returns the label set used for both the
// dbsync workload Pod template selector and its metrics Service selector.
// Must remain stable for the life of a CardanoDBSync because Kubernetes
// rejects selector drift on Deployments.
func dbSyncWorkloadSelectorLabels(dbSync *yacdv1alpha1.CardanoDBSync) map[string]string {
	instance := ctrlnames.LabelValue(dbSync.Name)

	return map[string]string{
		labelAppName:      labelDBSyncAppName,
		labelAppInstance:  instance,
		labelAppComponent: labelDBSyncRole,
		labelDBSync:       instance,
		labelCardanoRole:  labelDBSyncRole,
	}
}

// dbSyncWorkloadLabels returns the full label set applied to every dbsync
// workload-owned object (Deployment, PVCs, ConfigMap, pgpass Secret,
// metrics Service). The set adds the managed-by label on top of the
// selector labels.
func dbSyncWorkloadLabels(dbSync *yacdv1alpha1.CardanoDBSync) map[string]string {
	labels := dbSyncWorkloadSelectorLabels(dbSync)
	labels[labelAppManagedBy] = "yacd"

	return labels
}

// managedPostgresSelectorLabels returns the label set used for both the
// managed Postgres Pod template selector and the matching Service
// selector. The labels share the dbSync instance discriminator so the
// managed Postgres workload is clearly bound to its parent CardanoDBSync.
func managedPostgresSelectorLabels(dbSync *yacdv1alpha1.CardanoDBSync) map[string]string {
	instance := ctrlnames.LabelValue(dbSync.Name)

	return map[string]string{
		labelAppName:      labelDBSyncAppName,
		labelAppInstance:  instance,
		labelAppComponent: managedPostgresRole,
		labelDBSync:       instance,
		labelCardanoRole:  managedPostgresRole,
	}
}

// managedPostgresLabels returns the full label set applied to every managed
// Postgres-owned object (Deployment, PVC, Service, auth Secret).
func managedPostgresLabels(dbSync *yacdv1alpha1.CardanoDBSync) map[string]string {
	labels := managedPostgresSelectorLabels(dbSync)
	labels[labelAppManagedBy] = "yacd"

	return labels
}
