package cardanodbsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// managedPostgresIdentityInput is the wire shape hashed into the managed
// Postgres identity fingerprint. The JSON tags are frozen: changing them
// would re-roll every existing identity fingerprint.
type managedPostgresIdentityInput struct {
	Kind         string                           `json:"kind"`
	Image        string                           `json:"image"`
	Database     string                           `json:"database"`
	User         string                           `json:"user"`
	Port         int32                            `json:"port"`
	PasswordKey  string                           `json:"passwordKey"`
	AuthSecret   managedPostgresAuthIdentityInput `json:"authSecret"`
	AuthProvided bool                             `json:"authProvided"`
}

// managedPostgresAuthIdentityInput is the auth-Secret portion of the
// managed Postgres identity fingerprint. The Version field is the
// password fingerprint (generated path) or the Secret ResourceVersion
// (user-provided path).
type managedPostgresAuthIdentityInput struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// managedPostgresIdentityFingerprint hashes the bootstrap-affecting
// managed Postgres inputs into a stable identity. Returns an error when
// the auth Secret is missing the inputs the fingerprint needs.
func managedPostgresIdentityFingerprint(dbSync *yacdv1alpha1.CardanoDBSync, authSecret *corev1.Secret) (string, error) {
	managed := dbSync.Spec.Database.Managed
	credentialVersion, err := managedPostgresCredentialVersion(dbSync, authSecret)
	if err != nil {
		return "", err
	}

	input, err := json.Marshal(managedPostgresIdentityInput{
		Kind:         "managed-postgres/v1",
		Image:        managedPostgresImage(managed),
		Database:     managedPostgresDatabaseName(managed),
		User:         managedPostgresUser(managed),
		Port:         managedPostgresPort,
		PasswordKey:  managedPostgresPasswordKey,
		AuthProvided: managed.AuthSecretRef != nil,
		AuthSecret: managedPostgresAuthIdentityInput{
			Name:    authSecret.Name,
			Version: credentialVersion,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal managed Postgres identity input: %w", err)
	}
	sum := sha256.Sum256(input)

	return hex.EncodeToString(sum[:]), nil
}

// managedPostgresCredentialVersion returns the version string used in the
// auth-Secret portion of the identity fingerprint. The generated path
// uses the password fingerprint annotation (so a rotated password rolls
// identity); the provided path uses the password material directly.
func managedPostgresCredentialVersion(dbSync *yacdv1alpha1.CardanoDBSync, authSecret *corev1.Secret) (string, error) {
	if dbSync == nil || dbSync.Spec.Database.Managed == nil {
		return "", unsupportedSpec("managed database spec is required")
	}
	if authSecret == nil {
		return "", fmt.Errorf("managed Postgres auth Secret is required")
	}
	if dbSync.Spec.Database.Managed.AuthSecretRef == nil {
		fingerprint := authSecret.Annotations[managedPostgresPasswordFingerprintAnno]
		if fingerprint == "" {
			return "", unsupportedSpec("managed Postgres generated auth Secret is missing password fingerprint")
		}

		return fingerprint, nil
	}

	password := authSecret.Data[managedPostgresPasswordKey]
	if len(password) == 0 {
		return "", unsupportedSpec("managed Postgres auth Secret does not contain key password")
	}

	return managedPostgresPasswordFingerprint(password), nil
}

// managedPostgresPasswordFingerprint hashes a managed Postgres password
// into a stable identity. Used both as the auth-Secret identity component
// and as the annotation written onto controller-generated auth Secrets.
func managedPostgresPasswordFingerprint(password []byte) string {
	sum := sha256.Sum256(password)

	return hex.EncodeToString(sum[:])
}
