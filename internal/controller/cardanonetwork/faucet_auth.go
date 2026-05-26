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

// applyPrimaryFaucetAuthSecret reconciles the faucet auth Secret. On
// creation a fresh token is generated; on update an existing valid token is
// preserved and an invalid one is regenerated. Reads go through liveReader
// because Secrets are not in the manager cache.
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
	if err != nil {
		return controllerutil.OperationResultNone, err
	}

	if err := validateControllerOwner(current, desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	before := current.DeepCopy()
	ctrlresources.MutateObjectMetadata(current, desired, nil)
	current.Type = corev1.SecretTypeOpaque
	if current.Data == nil {
		current.Data = map[string][]byte{}
	}
	// Preserve existing valid tokens; regenerate only when the live data
	// fails the validator. This is the create-once-then-preserve contract
	// the faucet sidecar depends on across reconcile loops.
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
