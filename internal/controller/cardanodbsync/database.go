package cardanodbsync

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"maps"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlresources "github.com/meigma/yacd/internal/ctrlkit/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

// validateProvidedManagedPostgresAuthSecret reads and validates a
// user-supplied managed-Postgres auth Secret (spec.database.managed.
// authSecretRef). Returns (zero, false, err) on failure; err may be nil
// when the function already surfaced the failure through a status patch.
func (r *CardanoDBSyncReconciler) validateProvidedManagedPostgresAuthSecret(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	secretName string,
) (*corev1.Secret, bool, error) {
	if strings.TrimSpace(secretName) == "" {
		err := r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonManagedDatabaseSecretInvalid,
			"Managed Postgres auth Secret reference is incomplete",
		)
		return nil, false, err
	}

	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: secretName}, secret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err := r.patchDependencyUnavailableStatus(ctx, dbSync,
				conditionReasonManagedDatabaseSecretMissing,
				"Managed Postgres auth Secret does not exist",
			)
			return nil, false, err
		}
		return nil, false, err
	}
	if !secret.DeletionTimestamp.IsZero() {
		err := r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonManagedDatabaseSecretMissing,
			"Managed Postgres auth Secret is deleting",
		)
		return nil, false, err
	}

	ok, err := r.validateManagedPostgresAuthSecret(ctx, dbSync, secret)
	if err != nil || !ok {
		return nil, false, err
	}

	return secret, true, nil
}

// validateManagedPostgresAuthSecret checks that the auth Secret carries
// a usable password value. Returns (false, err) on failure; err may be
// nil when the function already surfaced the failure through a status
// patch.
func (r *CardanoDBSyncReconciler) validateManagedPostgresAuthSecret(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	secret *corev1.Secret,
) (bool, error) {
	if len(secret.Data[managedPostgresPasswordKey]) == 0 {
		err := r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonManagedDatabaseSecretInvalid,
			"Managed Postgres auth Secret does not contain key password",
		)
		return false, err
	}
	if strings.ContainsAny(string(secret.Data[managedPostgresPasswordKey]), "\r\n") {
		err := r.patchDependencyUnavailableStatus(ctx, dbSync,
			conditionReasonManagedDatabaseSecretInvalid,
			"Managed Postgres auth Secret password cannot contain newlines",
		)
		return false, err
	}

	return true, nil
}

// ensureManagedPostgresAuthSecret reconciles the controller-generated
// managed-Postgres auth Secret. It is the deliberate exception to the
// otherwise uniform ApplyOwnedObject flow because the password is
// create-once: the Secret is created with a freshly generated password
// the first time the dbsync is reconciled, then mutated only to track
// metadata. Two safety checks prevent silent data loss:
//
//  1. If the Secret is missing but a managed-Postgres identity has been
//     accepted (the PVC carries an identity annotation), the function
//     refuses to generate a new password — that would orphan the
//     initialized Postgres data directory.
//  2. If the live Secret carries an accepted-fingerprint annotation that
//     does not match the live password, the function returns a typed
//     UnsupportedDatabaseIdentityChange error rather than overwriting.
func (r *CardanoDBSyncReconciler) ensureManagedPostgresAuthSecret(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (*corev1.Secret, error) {
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedPostgresAuthSecretName(dbSync),
			Namespace: dbSync.Namespace,
			Labels:    managedPostgresLabels(dbSync),
		},
		Type: corev1.SecretTypeOpaque,
	}
	if err := controllerutil.SetControllerReference(dbSync, desired, r.Scheme); err != nil {
		return nil, fmt.Errorf("set managed Postgres auth Secret owner reference: %w", err)
	}

	current := &corev1.Secret{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		acceptedIdentity, err := r.acceptedManagedPostgresIdentity(ctx, dbSync)
		if err != nil {
			return nil, err
		}
		if acceptedIdentity != "" {
			return nil, statusConditionError{
				Reason: string(conditionReasonManagedDatabaseSecretMissing),
				Message: fmt.Sprintf(
					"Managed Postgres generated auth Secret %s is missing after database initialization; restore the original Secret or recreate the CardanoDBSync with a fresh database",
					ctrlmetadata.ObjectKey(desired),
				),
			}
		}
		password, err := generateManagedPostgresPassword()
		if err != nil {
			return nil, err
		}
		desired.Annotations = map[string]string{
			managedPostgresPasswordFingerprintAnno: managedPostgresPasswordFingerprint(password),
		}
		desired.Data = map[string][]byte{
			managedPostgresPasswordKey: password,
		}
		if err := r.Create(ctx, desired); err != nil {
			return nil, err
		}

		return desired, nil
	}
	if err != nil {
		return nil, err
	}
	if err := validateControllerOwner(current, desired); err != nil {
		return nil, err
	}
	password := current.Data[managedPostgresPasswordKey]
	if len(password) > 0 {
		passwordFingerprint := managedPostgresPasswordFingerprint(password)
		if acceptedFingerprint := current.Annotations[managedPostgresPasswordFingerprintAnno]; acceptedFingerprint != "" && acceptedFingerprint != passwordFingerprint {
			return nil, unsupportedDatabaseIdentityChange(
				"Managed Postgres generated auth Secret password changed after database initialization; delete and recreate the CardanoDBSync with a fresh database",
			)
		}
		desired.Annotations = map[string]string{
			managedPostgresPasswordFingerprintAnno: passwordFingerprint,
		}
	}

	before := current.DeepCopy()
	ctrlresources.MutateObjectMetadata(current, desired, mergeDBSyncOwnedAnnotations)
	current.Type = corev1.SecretTypeOpaque
	current.Data = maps.Clone(current.Data)
	current.StringData = nil

	if !equality.Semantic.DeepEqual(before, current) {
		if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
			return nil, err
		}
	}

	return current, nil
}

// generateManagedPostgresPassword returns a 32-byte random password
// base64-encoded for safe inclusion in env-var Secret data. This is the
// only crypto/rand caller in the package.
func generateManagedPostgresPassword() ([]byte, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("generate managed Postgres password: %w", err)
	}

	return []byte(base64.RawURLEncoding.EncodeToString(bytes)), nil
}
