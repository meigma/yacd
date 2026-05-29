package cardanodbsync

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"maps"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlresources "github.com/meigma/yacd/internal/ctrlkit/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

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
	if reason, message, invalid := managedPostgresAuthSecretDataProblem(secret); invalid {
		err := r.patchDependencyUnavailableStatus(ctx, dbSync, reason, message)
		return false, err
	}

	return true, nil
}

// ensureManagedPostgresAuthSecret reconciles the controller-generated
// managed-Postgres auth Secret. It is the deliberate exception to the
// otherwise uniform ApplyOwnedObject flow because the password is
// create-once: the Secret is created with a freshly generated password
// the first time the dbsync is reconciled, then mutated only to track
// metadata. Three safety checks prevent silent data loss or accidental
// adoption:
//
//  1. If the Secret is missing but a managed-Postgres identity has been
//     accepted (the PVC carries an identity annotation), the function
//     refuses to generate a new password.
//  2. If the same-name Secret is recreated without owner references, it is
//     adopted only when its password re-derives the accepted identity.
//  3. If the live Secret carries an accepted-fingerprint annotation that
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
					"Managed Postgres generated auth Secret %s is missing after database initialization; recreate the same-name Secret with the original data.password value, or recreate the CardanoDBSync with a fresh database",
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

	if metav1.GetControllerOf(current) == nil && len(current.OwnerReferences) == 0 {
		return r.adoptRestoredManagedPostgresAuthSecret(ctx, dbSync, current, desired)
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

	if err := patchManagedPostgresAuthSecret(ctx, r.Client, current, desired); err != nil {
		return nil, err
	}

	return current, nil
}

// adoptRestoredManagedPostgresAuthSecret adopts a plain user-restored generated
// auth Secret only when its password material recreates the managed-Postgres
// identity already accepted on runtime material.
func (r *CardanoDBSyncReconciler) adoptRestoredManagedPostgresAuthSecret(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
	current *corev1.Secret,
	desired *corev1.Secret,
) (*corev1.Secret, error) {
	acceptedIdentity, err := r.acceptedManagedPostgresIdentity(ctx, dbSync)
	if err != nil {
		return nil, err
	}
	if acceptedIdentity == "" {
		return nil, validateControllerOwner(current, desired)
	}
	if reason, message, invalid := managedPostgresAuthSecretDataProblem(current); invalid {
		return nil, statusConditionError{
			Reason:  string(reason),
			Message: message,
		}
	}

	password := current.Data[managedPostgresPasswordKey]
	passwordFingerprint := managedPostgresPasswordFingerprint(password)
	restoredIdentity, err := managedPostgresIdentityFingerprintForCredentialVersion(dbSync, current.Name, passwordFingerprint, false)
	if err != nil {
		return nil, err
	}
	if restoredIdentity != acceptedIdentity {
		return nil, unsupportedDatabaseIdentityChange(
			"Managed Postgres generated auth Secret password does not match the accepted database identity; recreate the Secret with the original data.password value, or recreate the CardanoDBSync with a fresh database",
		)
	}

	desired.Annotations = map[string]string{
		managedPostgresPasswordFingerprintAnno: passwordFingerprint,
	}
	if err := patchManagedPostgresAuthSecret(ctx, r.Client, current, desired); err != nil {
		return nil, err
	}

	return current, nil
}

func patchManagedPostgresAuthSecret(
	ctx context.Context,
	c client.Client,
	current *corev1.Secret,
	desired *corev1.Secret,
) error {
	before := current.DeepCopy()
	ctrlresources.MutateObjectMetadata(current, desired, mergeDBSyncOwnedAnnotations)
	current.Type = corev1.SecretTypeOpaque
	current.Data = maps.Clone(current.Data)
	current.StringData = nil

	if equality.Semantic.DeepEqual(before, current) {
		return nil
	}

	return c.Patch(ctx, current, client.MergeFrom(before))
}

func managedPostgresAuthSecretDataProblem(secret *corev1.Secret) (conditionReason, string, bool) {
	if len(secret.Data[managedPostgresPasswordKey]) == 0 {
		return conditionReasonManagedDatabaseSecretInvalid, "Managed Postgres auth Secret does not contain key password", true
	}
	if strings.ContainsAny(string(secret.Data[managedPostgresPasswordKey]), "\r\n") {
		return conditionReasonManagedDatabaseSecretInvalid, "Managed Postgres auth Secret password cannot contain newlines", true
	}

	return "", "", false
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
