package cardanodbsync

import (
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	managedPostgresContainerName = "postgres"
	managedPostgresPortName      = "postgres"
	managedPostgresDataVolume    = "postgres-state"
	managedPostgresDataMountDir  = "/var/lib/postgresql/data"
	managedPostgresDataDir       = "/var/lib/postgresql/data/pgdata"
)

// managedPostgresResources is the desired-state bundle for the managed
// Postgres workload that backs the dbsync workload when
// spec.database.managed is set. The bundle's IdentityFingerprint is the
// SHA-256 of every bootstrap-affecting input; the reconciler rejects
// applies when the live PVC or Deployment carries a different accepted
// fingerprint.
type managedPostgresResources struct {
	// PersistentVolumeClaim is the durable data PVC for the managed
	// Postgres container.
	PersistentVolumeClaim *corev1.PersistentVolumeClaim
	// Service is the ClusterIP Service the dbsync workload uses to reach
	// the managed Postgres container.
	Service *corev1.Service
	// Deployment is the single-container managed Postgres Deployment.
	Deployment *appsv1.Deployment
	// IdentityFingerprint is the SHA-256 of the bootstrap-affecting
	// inputs (image, database, user, port, password key, auth Secret
	// identity). Stored on the PVC and Deployment so identity drift fails
	// fast at the callback level.
	IdentityFingerprint string
}

// managedPostgresResources builds the managed Postgres workload bundle.
// The auth Secret is required; the controller resolves or creates it
// before calling into the builder.
func (b dbSyncWorkloadBuilder) managedPostgresResources(
	dbSync *yacdv1alpha1.CardanoDBSync,
	authSecret *corev1.Secret,
) (*managedPostgresResources, error) {
	if dbSync == nil {
		return nil, fmt.Errorf("cardanodbsync is required")
	}
	if dbSync.Spec.Database.Managed == nil {
		return nil, unsupportedSpec("managed database spec is required")
	}
	if authSecret == nil {
		return nil, fmt.Errorf("managed Postgres auth Secret is required")
	}
	if b.scheme == nil {
		return nil, fmt.Errorf("scheme is required")
	}

	identityFingerprint, err := managedPostgresIdentityFingerprint(dbSync, authSecret)
	if err != nil {
		return nil, err
	}
	pvc, err := b.managedPostgresPersistentVolumeClaim(dbSync, identityFingerprint)
	if err != nil {
		return nil, err
	}
	service, err := b.managedPostgresService(dbSync)
	if err != nil {
		return nil, err
	}
	deployment, err := b.managedPostgresDeployment(dbSync, authSecret, identityFingerprint)
	if err != nil {
		return nil, err
	}

	return &managedPostgresResources{
		PersistentVolumeClaim: pvc,
		Service:               service,
		Deployment:            deployment,
		IdentityFingerprint:   identityFingerprint,
	}, nil
}
