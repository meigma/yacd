// The developer wallet Secret apply mirrors the faucet auth Secret (see
// faucet_auth.go): it is a deliberate exception to ApplyOwnedObject because the
// key material is generated once on Create and must never be regenerated for an
// existing wallet — regenerating would change the address and strand the funds
// the developer owns. Reads go through liveReader because Secrets are uncached.
//
// Funding is a runtime side effect: once the faucet and Kupo are ready, the
// controller funds the wallet through the faucet's top-up endpoint and confirms
// the funding on-chain through Kupo. On-chain balance is the source of truth, so
// the funding step self-heals across Secret or network recreation.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// applyPrimaryWalletSecret reconciles the developer wallet Secret through a
// live read and dispatches to the create-with-keys or preserve path. See the
// file-level comment for why this Secret does not flow through ApplyOwnedObject.
func (r *CardanoNetworkReconciler) applyPrimaryWalletSecret(
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
// faucet auth Secret, the wallet's key material is never regenerated: the
// developer owns the address and its funds, so an existing wallet's Data is
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

// primaryWalletReadyCondition computes the WalletReady condition and the wallet
// status to publish. When the wallet is enabled it drives the bootstrap
// lifecycle: wait for the faucet and Kupo, confirm on-chain funding through
// Kupo, and otherwise submit a one-time faucet top-up. The returned degraded
// flag is true only for an active funding failure (the faucet rejected the
// request or Kupo errored), which the caller projects onto the Degraded
// condition; "awaiting confirmation" is reported as a pending (progressing)
// state, not a failure.
func (r *CardanoNetworkReconciler) primaryWalletReadyCondition(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	settings walletSettings,
	faucetReady metav1.Condition,
	kupoReady metav1.Condition,
	kupoService *corev1.Service,
	faucetService *corev1.Service,
) (metav1.Condition, *yacdv1alpha1.WalletStatus, bool, error) {
	if !settings.enabled {
		return walletReadyCondition(metav1.ConditionFalse, conditionReasonWalletDisabled, conditionMessageWalletDisabled), nil, false, nil
	}

	secret := &corev1.Secret{}
	if err := r.liveReader().Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: settings.secretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return walletReadyCondition(metav1.ConditionFalse, conditionReasonWalletKeyMissing, "Developer wallet Secret is missing"), nil, false, nil
		}
		return metav1.Condition{}, nil, false, err
	}
	address := string(secret.Data[walletAddressKey])
	if address == "" {
		return walletReadyCondition(metav1.ConditionFalse, conditionReasonWalletKeyMissing, "Developer wallet Secret has no address"), nil, false, nil
	}

	walletStatus := &yacdv1alpha1.WalletStatus{Address: address, KeySecretName: secret.Name}
	if network.Status.Wallet != nil {
		// Preserve the last submitted funding transaction so we do not resubmit
		// while the first one is still being confirmed.
		walletStatus.FundedTxID = network.Status.Wallet.FundedTxID
	}

	if faucetReady.Status != metav1.ConditionTrue || kupoReady.Status != metav1.ConditionTrue {
		return walletReadyCondition(metav1.ConditionFalse, conditionReasonWalletFundingPending, "Waiting for the faucet and Kupo to become ready before funding the wallet"), walletStatus, false, nil
	}

	kupoURL, err := chainAPIServiceClusterURL(kupoService, kupoServiceURLType)
	if err != nil {
		return metav1.Condition{}, nil, false, err
	}
	confirmCtx, cancelConfirm := context.WithTimeout(ctx, walletConfirmTimeout)
	confirmed, err := r.walletConfirmer().Confirmed(confirmCtx, kupoURL, address, settings.fundingLovelace)
	cancelConfirm()
	if err != nil {
		return walletReadyCondition(metav1.ConditionFalse, conditionReasonWalletFundingFailed, fmt.Sprintf("Confirming wallet funding through Kupo failed: %v", err)), walletStatus, true, nil
	}
	if confirmed {
		walletStatus.Funded = true
		return walletReadyCondition(metav1.ConditionTrue, conditionReasonWalletReady, conditionMessageWalletReady), walletStatus, false, nil
	}

	if walletStatus.FundedTxID != "" {
		return walletReadyCondition(metav1.ConditionFalse, conditionReasonWalletFundingPending, "Awaiting on-chain confirmation of the wallet funding transaction"), walletStatus, false, nil
	}

	token, notReady, err := r.faucetAuthTokenForFunding(ctx, network)
	if err != nil {
		return metav1.Condition{}, nil, false, err
	}
	if notReady != nil {
		return *notReady, walletStatus, false, nil
	}

	faucetURL, err := chainAPIServiceClusterURL(faucetService, faucetServiceURLType)
	if err != nil {
		return metav1.Condition{}, nil, false, err
	}
	fundCtx, cancelFund := context.WithTimeout(ctx, walletFundTimeout)
	txID, err := r.walletFunder().Fund(fundCtx, faucetURL, token, address, settings.fundingLovelace)
	cancelFund()
	if err != nil {
		return walletReadyCondition(metav1.ConditionFalse, conditionReasonWalletFundingFailed, fmt.Sprintf("Funding the wallet through the faucet failed: %v", err)), walletStatus, true, nil
	}

	walletStatus.FundedTxID = txID
	return walletReadyCondition(metav1.ConditionFalse, conditionReasonWalletFundingPending, "Submitted the wallet funding transaction; awaiting on-chain confirmation"), walletStatus, false, nil
}

// faucetAuthTokenForFunding live-reads the faucet auth Secret and returns its
// token. When the Secret is missing or its token is not yet usable it returns a
// pending WalletReady condition instead of an error so funding waits rather than
// failing.
func (r *CardanoNetworkReconciler) faucetAuthTokenForFunding(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (string, *metav1.Condition, error) {
	secret := &corev1.Secret{}
	if err := r.liveReader().Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryFaucetAuthSecretName(network)}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			pending := walletReadyCondition(metav1.ConditionFalse, conditionReasonWalletFundingPending, "Waiting for the faucet auth Secret before funding the wallet")
			return "", &pending, nil
		}
		return "", nil, err
	}
	token := string(secret.Data[faucetAuthTokenKey])
	if !validFaucetAuthToken(token) {
		pending := walletReadyCondition(metav1.ConditionFalse, conditionReasonWalletFundingPending, "Waiting for a usable faucet auth token before funding the wallet")
		return "", &pending, nil
	}

	return token, nil, nil
}

// chainAPIServiceClusterURL renders the in-cluster URL for a chain API Service
// using its first port and the given scheme.
func chainAPIServiceClusterURL(service *corev1.Service, scheme string) (string, error) {
	if service == nil {
		return "", fmt.Errorf("service is missing")
	}
	if len(service.Spec.Ports) == 0 {
		return "", fmt.Errorf("service %s/%s has no ports", service.Namespace, service.Name)
	}

	return fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", scheme, service.Name, service.Namespace, service.Spec.Ports[0].Port), nil
}
