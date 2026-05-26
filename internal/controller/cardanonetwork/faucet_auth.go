// The faucet auth Secret apply is a deliberate exception to the otherwise
// uniform [github.com/meigma/yacd/internal/ctrlkit/apply.ApplyOwnedObject]
// pattern used everywhere else in this package. Two hard constraints make
// the shared helper a poor fit:
//
//  1. ApplyOwnedObject reads through [sigs.k8s.io/controller-runtime/pkg/client.Client]
//     (the cached client). Secrets are not watched here (SetupWithManager
//     does not Owns(&corev1.Secret{}) — adding Secret watches would
//     materially increase watch traffic in YACD-managed namespaces, a cost
//     the package deliberately avoided), so live Secret reads must go
//     through r.liveReader() to bypass the manager cache.
//  2. ApplyOwnedObject's Mutate callback only runs for existing objects, so
//     create-once data (here: the random auth token) cannot flow through
//     it without a second pass.
//
// The shape below mirrors the [applyNetworkArtifactsConfigMap] exception
// in apply.go: a small dispatcher reads through liveReader, then routes to
// a create-with-token path or a reconcile-existing path.

package cardanonetwork

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"unicode"

	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlresources "github.com/meigma/yacd/internal/ctrlkit/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// faucetAuthTokenByteLength is the random byte count fed into the faucet
// auth token before base64url encoding. 32 bytes (256 bits) is the lower
// bound validFaucetAuthToken enforces; matching the validator's length
// requirement keeps Create and validate-on-Update consistent.
const faucetAuthTokenByteLength = 32

// applyPrimaryFaucetAuthSecret reconciles the faucet auth Secret through a
// live read (Secrets are uncached) and then dispatches to the create or
// reconcile path. See the file-level comment for why this Secret does not
// flow through ApplyOwnedObject.
func (r *CardanoNetworkReconciler) applyPrimaryFaucetAuthSecret(
	ctx context.Context,
	desired *corev1.Secret,
) (controllerutil.OperationResult, error) {
	desired = desired.DeepCopy()
	if err := r.defaultObject(desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	current := &corev1.Secret{}
	err := r.liveReader().Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		return r.createFaucetAuthSecretWithToken(ctx, desired)
	}
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	return r.reconcileFaucetAuthSecret(ctx, current, desired)
}

// createFaucetAuthSecretWithToken handles the not-yet-created branch:
// generate a fresh token, populate Secret.Data, persist with Create.
func (r *CardanoNetworkReconciler) createFaucetAuthSecretWithToken(
	ctx context.Context,
	desired *corev1.Secret,
) (controllerutil.OperationResult, error) {
	token, err := generateFaucetAuthToken()
	if err != nil {
		return controllerutil.OperationResultNone, err
	}
	desired.Data = map[string][]byte{
		faucetAuthTokenKey: []byte(token),
	}
	if err := r.Create(ctx, desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultCreated, nil
}

// reconcileFaucetAuthSecret handles the live-Secret-exists branch:
// validate ownership, preserve an existing valid token, regenerate when the
// live data fails the validator, and persist with a diff-aware Patch. This
// is the create-once-then-preserve contract the faucet sidecar depends on
// across reconcile loops.
func (r *CardanoNetworkReconciler) reconcileFaucetAuthSecret(
	ctx context.Context,
	current *corev1.Secret,
	desired *corev1.Secret,
) (controllerutil.OperationResult, error) {
	if err := validateControllerOwner(current, desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	before := current.DeepCopy()
	ctrlresources.MutateObjectMetadata(current, desired, nil)
	current.Type = corev1.SecretTypeOpaque
	if current.Data == nil {
		current.Data = map[string][]byte{}
	}
	if !validFaucetAuthToken(string(current.Data[faucetAuthTokenKey])) {
		token, err := generateFaucetAuthToken()
		if err != nil {
			return controllerutil.OperationResultNone, err
		}
		current.Data[faucetAuthTokenKey] = []byte(token)
	}

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

// generateFaucetAuthToken returns a base64url-encoded random token. The
// random source is crypto/rand; failure is surfaced as an error rather than
// panicking so the reconciler can requeue.
func generateFaucetAuthToken() (string, error) {
	var tokenBytes [faucetAuthTokenByteLength]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return "", fmt.Errorf("generate faucet auth token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(tokenBytes[:]), nil
}

// validFaucetAuthToken reports whether the given token meets the minimum
// length and character constraints (no whitespace, no control characters).
// Pure: no side effects, used both during apply (to decide whether to
// regenerate) and during readiness probing (to decide whether the live
// Secret is usable).
func validFaucetAuthToken(token string) bool {
	if len(token) < faucetAuthTokenByteLength {
		return false
	}
	for _, char := range token {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return false
		}
	}

	return true
}
