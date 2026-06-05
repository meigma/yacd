package cardanonetwork

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// operationResultDeleted is the package-private OperationResult that
// represents "an owned child was deleted." controllerutil itself does not
// ship a Deleted variant, so we extend its open type.
const operationResultDeleted controllerutil.OperationResult = "deleted"

// deletePrimaryOgmiosService deletes the optional ogmios Service when the
// CardanoNetwork spec turns ogmios off after it had been enabled.
func (r *CardanoNetworkReconciler) deletePrimaryOgmiosService(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (controllerutil.OperationResult, error) {
	return r.deletePrimaryChainAPIService(ctx, network, primaryOgmiosServiceName(network), "Ogmios")
}

// deletePrimaryKupoService deletes the optional kupo Service when the
// CardanoNetwork spec turns kupo off after it had been enabled.
func (r *CardanoNetworkReconciler) deletePrimaryKupoService(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (controllerutil.OperationResult, error) {
	return r.deletePrimaryChainAPIService(ctx, network, primaryKupoServiceName(network), "Kupo")
}

// deletePrimaryArtifactsService deletes the optional artifacts Service when
// the network no longer runs the serve sidecar (for example after a switch to
// a custom public profile).
func (r *CardanoNetworkReconciler) deletePrimaryArtifactsService(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (controllerutil.OperationResult, error) {
	return r.deletePrimaryChainAPIService(ctx, network, primaryArtifactsServiceName(network), "artifacts")
}

// deletePrimaryFaucetWalletSecret deletes the well-known faucet wallet Secret
// when the CardanoNetwork no longer gates it on (a switch away from local
// mode). An explicit switch is a deliberate request to discard the wallet, so
// the owned Secret is removed.
func (r *CardanoNetworkReconciler) deletePrimaryFaucetWalletSecret(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (controllerutil.OperationResult, error) {
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryFaucetWalletSecretName(network),
			Namespace: network.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(network, desired, r.Scheme); err != nil {
		return controllerutil.OperationResultNone, fmt.Errorf("set desired faucet wallet Secret owner reference: %w", err)
	}

	current := &corev1.Secret{}
	// Secrets are not in the manager cache; live-read to avoid a cache miss
	// looking like a non-existent object.
	err := r.liveReader().Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		return controllerutil.OperationResultNone, nil
	}
	if err != nil {
		return controllerutil.OperationResultNone, err
	}
	if err := validateControllerOwner(current, desired); err != nil {
		return controllerutil.OperationResultNone, err
	}
	if err := r.Delete(ctx, current); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return operationResultDeleted, nil
}

// deletePrimaryChainAPIService deletes a named optional Service after
// verifying we own it. Used by the three chain API sidecar deletions to
// share the get-validate-delete skeleton.
func (r *CardanoNetworkReconciler) deletePrimaryChainAPIService(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	name string,
	label string,
) (controllerutil.OperationResult, error) {
	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: network.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(network, desired, r.Scheme); err != nil {
		return controllerutil.OperationResultNone, fmt.Errorf("set desired %s Service owner reference: %w", label, err)
	}

	current := &corev1.Service{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		return controllerutil.OperationResultNone, nil
	}
	if err != nil {
		return controllerutil.OperationResultNone, err
	}
	if err := validateControllerOwner(current, desired); err != nil {
		return controllerutil.OperationResultNone, err
	}
	if err := r.Delete(ctx, current); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return operationResultDeleted, nil
}
