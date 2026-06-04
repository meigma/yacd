// The wallet Secret apply is a deliberate exception to ApplyOwnedObject because
// the key material is generated once on Create and must never be regenerated for
// an existing wallet — regenerating would change the address and strand the
// funds the wallet holds. Reads go through liveReader because Secrets are
// uncached.
//
// This file owns the well-known faucet wallet: a local-only payment key the
// controller generates and writes before the Deployment is built so the
// genesis-funding init container can fund its address at genesis. The shared
// apply core (applyWalletSecret/createWalletSecretWithKeys/reconcileWalletSecret)
// is generic key-bearing Secret reconciliation.

package cardanonetwork

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/wallet"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlresources "github.com/meigma/yacd/internal/ctrlkit/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// walletSigningKeyKey is the Secret data key for the wallet's payment
	// signing key text envelope.
	walletSigningKeyKey = "payment.skey"
	// walletVerificationKeyKey is the Secret data key for the wallet's payment
	// verification key text envelope.
	walletVerificationKeyKey = "payment.vkey"
	// walletAddressKey is the Secret data key for the wallet's bech32 testnet
	// address.
	walletAddressKey = "address"
)

// applyPrimaryFaucetWalletSecret reconciles the well-known faucet wallet
// Secret. The address is funded at genesis, so regenerating the key would
// strand the allocation; it delegates to the generate-once-then-preserve apply
// core.
func (r *CardanoNetworkReconciler) applyPrimaryFaucetWalletSecret(
	ctx context.Context,
	desired *corev1.Secret,
) (controllerutil.OperationResult, *corev1.Secret, error) {
	return r.applyWalletSecret(ctx, desired)
}

// faucetWalletApplyResult carries the outcome of the pre-build faucet wallet
// ensure step from Reconcile into the build (the address) and the apply-result
// accounting (the operation and live object).
type faucetWalletApplyResult struct {
	// enabled mirrors the gate so the apply phase knows whether to delete a
	// stale Secret instead of keeping one.
	enabled bool
	// address is the bech32 faucet wallet address, threaded into the builder so
	// the genesis-funding init container can carry it as an env literal.
	address string
	// operation is the create/unchanged result of the ensure step.
	operation controllerutil.OperationResult
	// object is the live faucet wallet Secret, or nil when not gated on.
	object *corev1.Secret
}

// ensurePrimaryFaucetWalletSecret guarantees the well-known faucet wallet Secret
// exists before the Deployment is built, generating its key material on first
// reconcile and live-reading it thereafter. The faucet wallet's address must be
// known at build time because the genesis-funding init container injects it as
// an env literal: editing the genesis after the node already booted from the
// unfunded one would rewrite the chain under a running node. When the faucet
// wallet is not gated on (non-local or faucet disabled) it returns a disabled
// result and leaves any stale Secret for the apply phase to delete.
func (r *CardanoNetworkReconciler) ensurePrimaryFaucetWalletSecret(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (faucetWalletApplyResult, error) {
	if !faucetWalletEnabled(network) {
		return faucetWalletApplyResult{}, nil
	}

	desired, err := (primaryWorkloadBuilder{scheme: r.Scheme}).faucetWalletSecret(network, faucetWalletSettings{secretName: primaryFaucetWalletSecretName(network)})
	if err != nil {
		return faucetWalletApplyResult{}, err
	}

	operation, secret, err := r.applyPrimaryFaucetWalletSecret(ctx, desired)
	if err != nil {
		return faucetWalletApplyResult{}, err
	}
	address := string(secret.Data[walletAddressKey])
	if address == "" {
		return faucetWalletApplyResult{}, fmt.Errorf("faucet wallet Secret %s has no address", primaryFaucetWalletSecretName(network))
	}

	return faucetWalletApplyResult{
		enabled:   true,
		address:   address,
		operation: operation,
		object:    secret,
	}, nil
}

// applyWalletSecret is the shared apply core for the developer and faucet
// wallets: live-read the Secret (Secrets are uncached), create it with freshly
// generated key material when absent, and preserve existing key material
// verbatim otherwise. Both wallets are funded against their derived address, so
// neither may ever regenerate the key.
func (r *CardanoNetworkReconciler) applyWalletSecret(
	ctx context.Context,
	desired *corev1.Secret,
) (controllerutil.OperationResult, *corev1.Secret, error) {
	desired = desired.DeepCopy()
	if err := r.defaultObject(desired); err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	current := &corev1.Secret{}
	err := r.liveReader().Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		return r.createWalletSecretWithKeys(ctx, desired)
	}
	if err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	return r.reconcileWalletSecret(ctx, current, desired)
}

// createWalletSecretWithKeys generates a fresh payment key pair, populates the
// Secret data with the key envelopes and derived address, and persists it.
func (r *CardanoNetworkReconciler) createWalletSecretWithKeys(
	ctx context.Context,
	desired *corev1.Secret,
) (controllerutil.OperationResult, *corev1.Secret, error) {
	material, err := wallet.New()
	if err != nil {
		return controllerutil.OperationResultNone, nil, fmt.Errorf("generate wallet key material: %w", err)
	}
	desired.Data = map[string][]byte{
		walletSigningKeyKey:      material.SigningKeyEnvelope,
		walletVerificationKeyKey: material.VerificationKeyEnvelope,
		walletAddressKey:         []byte(material.Address),
	}
	if err := r.Create(ctx, desired); err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	return controllerutil.OperationResultCreated, desired, nil
}

// reconcileWalletSecret handles the live-Secret-exists branch. Unlike the
// faucet auth Secret, the wallet's key material is never regenerated: the wallet
// holds the funds at its derived address, so an existing wallet's Data is
// preserved verbatim and only metadata is reconciled.
func (r *CardanoNetworkReconciler) reconcileWalletSecret(
	ctx context.Context,
	current *corev1.Secret,
	desired *corev1.Secret,
) (controllerutil.OperationResult, *corev1.Secret, error) {
	if err := validateControllerOwner(current, desired); err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	before := current.DeepCopy()
	ctrlresources.MutateObjectMetadata(current, desired, nil)
	current.Type = corev1.SecretTypeOpaque

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, current, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	return controllerutil.OperationResultUpdated, current, nil
}
