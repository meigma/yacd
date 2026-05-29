package cardanodbsync

import (
	"context"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	corev1 "k8s.io/api/core/v1"
)

// databaseMode names the two CardanoDBSync database modes. The
// reconciler rejects specs that set both modes (or neither).
type databaseMode string

const (
	// databaseModeExternal indicates the user supplied an external
	// Postgres reference under spec.database.external.
	databaseModeExternal databaseMode = "external"

	// databaseModeManaged indicates the controller should run an owned
	// Postgres workload (spec.database.managed).
	databaseModeManaged databaseMode = "managed"
)

// databaseRuntime is the resolved per-reconcile database context. The
// reconciler builds it once at the top of Reconcile and passes it down to
// the workload builder and the status patchers; it carries enough
// material for both the planner ([dbsync.Database]) and the status
// endpoints.
type databaseRuntime struct {
	// Mode is the selected database mode (external or managed).
	Mode databaseMode
	// Database is the resolved planner-shaped connection input.
	Database dbsync.Database
	// PasswordSecret is the live Secret carrying the libpq password.
	PasswordSecret *corev1.Secret
	// CredentialVersion is the managed-Postgres password fingerprint used
	// by managed database identity checks and Postgres Pod-template
	// annotations. It is empty for external Postgres because the db-sync
	// workload hashes the rendered pgpass material directly.
	CredentialVersion string
	// PostgresEndpoint is the Service endpoint payload published into
	// CardanoDBSync.Status.Endpoints.Postgres.
	PostgresEndpoint *yacdv1alpha1.ServiceEndpointStatus
	// GeneratedAuthSecretName is the name of the controller-generated
	// managed-Postgres auth Secret. Empty for the user-provided and
	// external paths.
	GeneratedAuthSecretName string
}

// resolveDatabase dispatches to the external or managed resolver. The
// returned bool is true when resolution succeeded; false when the
// function already published a Degraded / Waiting status patch (the
// caller should bail out of the reconcile and wait for the next event).
func (r *CardanoDBSyncReconciler) resolveDatabase(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (databaseRuntime, bool, error) {
	switch {
	case dbSync.Spec.Database.External != nil && dbSync.Spec.Database.Managed == nil:
		return r.resolveExternalDatabase(ctx, dbSync)
	case dbSync.Spec.Database.Managed != nil && dbSync.Spec.Database.External == nil:
		return r.resolveManagedDatabase(ctx, dbSync)
	default:
		err := r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonUnsupportedDatabaseMode,
			"CardanoDBSync requires exactly one of spec.database.external or spec.database.managed",
		)
		return databaseRuntime{}, false, err
	}
}

// resolveExternalDatabase reads the external Postgres password Secret
// and returns a databaseRuntime pointing at it. Returns (zero, false,
// err) when validation failed; err may be nil if the function already
// surfaced the failure through a status patch.
func (r *CardanoDBSyncReconciler) resolveExternalDatabase(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (databaseRuntime, bool, error) {
	external := dbSync.Spec.Database.External
	secret, ok, err := r.validateExternalDatabaseSecret(ctx, dbSync, external)
	if err != nil || !ok {
		return databaseRuntime{}, false, err
	}
	database := dbSyncDatabaseFromExternal(external)

	return databaseRuntime{
		Mode:             databaseModeExternal,
		Database:         database,
		PasswordSecret:   secret,
		PostgresEndpoint: postgresEndpointFor(database, ""),
	}, true, nil
}

// workloadPasswordSecret returns the password Secret the builder embeds
// into the pgpass Secret. For managed Postgres with a controller-managed
// password, the returned Secret carries the password fingerprint as its
// ResourceVersion for compatibility with code paths that still inspect the
// Secret version; db-sync rollout now uses the rendered pgpass fingerprint.
func (runtime databaseRuntime) workloadPasswordSecret() *corev1.Secret {
	if runtime.PasswordSecret == nil {
		return nil
	}
	if runtime.Mode != databaseModeManaged || runtime.CredentialVersion == "" {
		return runtime.PasswordSecret
	}

	secret := runtime.PasswordSecret.DeepCopy()
	secret.ResourceVersion = runtime.CredentialVersion

	return secret
}

// resolveManagedDatabase resolves the managed Postgres auth Secret and
// returns the resulting databaseRuntime. When the user supplies
// spec.database.managed.authSecretRef the controller validates the
// referenced Secret; otherwise the controller ensures its own generated
// Secret exists. The Secret apply is the documented exception to the
// otherwise uniform ApplyOwnedObject flow because the password is
// create-once: see ensureManagedPostgresAuthSecret.
func (r *CardanoDBSyncReconciler) resolveManagedDatabase(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (databaseRuntime, bool, error) {
	managed := dbSync.Spec.Database.Managed
	var secret *corev1.Secret
	generatedAuthSecretName := ""

	if managed.AuthSecretRef != nil {
		var ok bool
		var err error
		secret, ok, err = r.validateProvidedManagedPostgresAuthSecret(ctx, dbSync, managed.AuthSecretRef.Name)
		if err != nil || !ok {
			return databaseRuntime{}, false, err
		}
	} else {
		var err error
		secret, err = r.ensureManagedPostgresAuthSecret(ctx, dbSync)
		if err != nil {
			return databaseRuntime{}, false, err
		}
		generatedAuthSecretName = secret.Name
		ok, err := r.validateManagedPostgresAuthSecret(ctx, dbSync, secret)
		if err != nil || !ok {
			return databaseRuntime{}, false, err
		}
	}

	database := dbSyncDatabaseFromManaged(dbSync, secret.Name)
	credentialVersion, err := managedPostgresCredentialVersion(dbSync, secret)
	if err != nil {
		return databaseRuntime{}, false, err
	}

	return databaseRuntime{
		Mode:                    databaseModeManaged,
		Database:                database,
		PasswordSecret:          secret,
		CredentialVersion:       credentialVersion,
		PostgresEndpoint:        postgresEndpointFor(database, managedPostgresServiceName(dbSync)),
		GeneratedAuthSecretName: generatedAuthSecretName,
	}, true, nil
}
