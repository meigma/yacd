package cardanonetwork

import (
	"context"
	"errors"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// deletePrimaryFaucetService deletes the optional faucet Service when the
// CardanoNetwork spec turns the faucet off after it had been enabled.
func (r *CardanoNetworkReconciler) deletePrimaryFaucetService(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (controllerutil.OperationResult, error) {
	return r.deletePrimaryChainAPIService(ctx, network, primaryFaucetServiceName(network), "faucet")
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

// deletePrimaryFaucetAuthSecret deletes the faucet auth Secret when the
// CardanoNetwork spec turns the faucet off after it had been enabled.
func (r *CardanoNetworkReconciler) deletePrimaryFaucetAuthSecret(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (controllerutil.OperationResult, error) {
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryFaucetAuthSecretName(network),
			Namespace: network.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(network, desired, r.Scheme); err != nil {
		return controllerutil.OperationResultNone, fmt.Errorf("set desired faucet auth Secret owner reference: %w", err)
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

// revokePrimaryFaucetExposure tears down the faucet sidecar's externally
// visible surface (Service, auth Secret, Deployment containers/volumes)
// when the CardanoNetwork enters a Degraded state. Errors from the three
// teardown steps are joined so partial cleanup still tries every step
// before surfacing failure to the reconciler.
func (r *CardanoNetworkReconciler) revokePrimaryFaucetExposure(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) error {
	return errors.Join(
		r.deletePrimaryFaucetServiceIfOwned(ctx, network),
		r.deletePrimaryFaucetAuthSecretIfOwned(ctx, network),
		r.removePrimaryFaucetFromDeploymentIfOwned(ctx, network),
	)
}

// deletePrimaryFaucetServiceIfOwned deletes the faucet Service when present
// and owned by this controller. Used by revokePrimaryFaucetExposure for the
// best-effort cleanup path.
func (r *CardanoNetworkReconciler) deletePrimaryFaucetServiceIfOwned(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) error {
	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryFaucetServiceName(network),
			Namespace: network.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(network, desired, r.Scheme); err != nil {
		return fmt.Errorf("set desired faucet Service owner reference: %w", err)
	}

	return r.deleteObjectIfOwned(ctx, desired, &corev1.Service{})
}

// deletePrimaryFaucetAuthSecretIfOwned deletes the faucet auth Secret when
// present and owned by this controller. Reads through liveReader because
// Secrets are not in the manager cache.
func (r *CardanoNetworkReconciler) deletePrimaryFaucetAuthSecretIfOwned(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) error {
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryFaucetAuthSecretName(network),
			Namespace: network.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(network, desired, r.Scheme); err != nil {
		return fmt.Errorf("set desired faucet auth Secret owner reference: %w", err)
	}

	return r.deleteObjectIfOwnedWithReader(ctx, desired, &corev1.Secret{}, r.liveReader())
}

// deleteObjectIfOwned reads through the cached client, then deletes the
// object when present and owned by this controller. Best-effort: ownership
// mismatch is silently skipped rather than surfaced as an error.
func (r *CardanoNetworkReconciler) deleteObjectIfOwned(
	ctx context.Context,
	desired client.Object,
	current client.Object,
) error {
	return r.deleteObjectIfOwnedWithReader(ctx, desired, current, r.Client)
}

// deleteObjectIfOwnedWithReader is the read-through variant used when the
// caller must bypass the manager cache (for example: Secrets, which are not
// cached).
func (r *CardanoNetworkReconciler) deleteObjectIfOwnedWithReader(
	ctx context.Context,
	desired client.Object,
	current client.Object,
	reader client.Reader,
) error {
	err := reader.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if validateControllerOwner(current, desired) != nil {
		return nil
	}

	return r.Delete(ctx, current)
}

// removePrimaryFaucetFromDeploymentIfOwned strips the faucet container,
// faucet source-address init container, and the faucet auth volume from
// the live primary Deployment. Used during the Degraded faucet revocation
// path so the in-cluster Pod no longer references a Secret the controller
// is about to delete.
func (r *CardanoNetworkReconciler) removePrimaryFaucetFromDeploymentIfOwned(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) error {
	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryWorkloadName(network),
			Namespace: network.Namespace,
		},
	}
	if err := controllerutil.SetControllerReference(network, desired, r.Scheme); err != nil {
		return fmt.Errorf("set desired primary Deployment owner reference: %w", err)
	}

	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), deployment)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if validateControllerOwner(deployment, desired) != nil {
		return nil
	}

	before := deployment.DeepCopy()
	deployment.Spec.Template.Spec.InitContainers = removeContainersByName(
		deployment.Spec.Template.Spec.InitContainers,
		faucetSourceAddressInitContainerName,
	)
	deployment.Spec.Template.Spec.Containers = removeContainersByName(
		deployment.Spec.Template.Spec.Containers,
		faucetContainerName,
	)
	deployment.Spec.Template.Spec.Volumes = removeVolumesByName(
		deployment.Spec.Template.Spec.Volumes,
		faucetAuthVolumeName,
	)
	if equality.Semantic.DeepEqual(before, deployment) {
		return nil
	}

	return r.Patch(ctx, deployment, client.MergeFrom(before))
}

// removeContainersByName returns a copy of containers with every entry whose
// Name matches removed. The implementation reuses the input slice's
// underlying array (filter-in-place pattern) because the caller (the
// faucet-revocation patch path) discards the input afterward.
func removeContainersByName(containers []corev1.Container, name string) []corev1.Container {
	filtered := containers[:0]
	for _, container := range containers {
		if container.Name == name {
			continue
		}
		filtered = append(filtered, container)
	}

	return filtered
}

// removeVolumesByName mirrors removeContainersByName for Volume slices.
func removeVolumesByName(volumes []corev1.Volume, name string) []corev1.Volume {
	filtered := volumes[:0]
	for _, volume := range volumes {
		if volume.Name == name {
			continue
		}
		filtered = append(filtered, volume)
	}

	return filtered
}
