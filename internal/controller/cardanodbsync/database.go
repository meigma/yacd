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

type databaseMode string

const (
	databaseModeExternal databaseMode = "external"
	databaseModeManaged  databaseMode = "managed"
)

type databaseRuntime struct {
	Mode                    databaseMode
	Database                dbsync.Database
	PasswordSecret          *corev1.Secret
	CredentialVersion       string
	PostgresEndpoint        *yacdv1alpha1.ServiceEndpointStatus
	GeneratedAuthSecretName string
}

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
			return nil, unsupportedStatusError{
				Reason: conditionReasonManagedDatabaseSecretMissing,
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

func generateManagedPostgresPassword() ([]byte, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("generate managed Postgres password: %w", err)
	}

	return []byte(base64.RawURLEncoding.EncodeToString(bytes)), nil
}
