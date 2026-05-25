package cardanonetwork

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"unicode"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlapply "github.com/meigma/yacd/internal/ctrlkit/apply"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlstorage "github.com/meigma/yacd/internal/ctrlkit/storage"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type unsupportedApplyError = ctrlapply.UnsupportedError

const operationResultDeleted controllerutil.OperationResult = "deleted"

func (r *CardanoNetworkReconciler) applyPrimaryPersistentVolumeClaim(
	ctx context.Context,
	desired *corev1.PersistentVolumeClaim,
) (controllerutil.OperationResult, error) {
	current := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired.DeepCopy()); err != nil {
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

	if err := validateLocalnetFingerprint(current, desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	if err := validateRequestedStorageClass(current, desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	if !ctrlstorage.StorageClassCompatible(current.Spec.StorageClassName, desired.Spec.StorageClassName) {
		return controllerutil.OperationResultNone, unsupportedStorageChange(
			"PVC %s storageClassName cannot be changed from %s to %s",
			ctrlmetadata.ObjectKey(desired),
			ctrlstorage.StringPtrValue(current.Spec.StorageClassName),
			ctrlstorage.StringPtrValue(desired.Spec.StorageClassName),
		)
	}
	if !reflect.DeepEqual(current.Spec.AccessModes, desired.Spec.AccessModes) {
		return controllerutil.OperationResultNone, unsupportedStorageChange(
			"PVC %s accessModes drifted from desired value",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	currentStorage := current.Spec.Resources.Requests[corev1.ResourceStorage]
	desiredStorage := desired.Spec.Resources.Requests[corev1.ResourceStorage]
	if currentStorage.Cmp(desiredStorage) > 0 {
		return controllerutil.OperationResultNone, unsupportedStorageChange(
			"PVC %s storage cannot be decreased from %s to %s",
			ctrlmetadata.ObjectKey(desired),
			currentStorage.String(),
			desiredStorage.String(),
		)
	}

	before := current.DeepCopy()
	current.Labels = ctrlmetadata.MergeStringMap(current.Labels, desired.Labels)
	current.Annotations = mergeOwnedAnnotations(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	if current.Spec.Resources.Requests == nil {
		current.Spec.Resources.Requests = corev1.ResourceList{}
	}
	if currentStorage.Cmp(desiredStorage) < 0 {
		current.Spec.Resources.Requests[corev1.ResourceStorage] = desiredStorage
	}

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Update(ctx, current); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func (r *CardanoNetworkReconciler) applyPrimaryDeployment(
	ctx context.Context,
	desired *appsv1.Deployment,
) (controllerutil.OperationResult, error) {
	desired = desired.DeepCopy()
	if err := r.defaultObject(desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	current := &appsv1.Deployment{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
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

	if !equality.Semantic.DeepEqual(current.Spec.Selector, desired.Spec.Selector) {
		return controllerutil.OperationResultNone, unsupportedWorkloadChange(
			"Deployment %s selector drifted from desired value",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	before := current.DeepCopy()
	current.Labels = ctrlmetadata.MergeStringMap(current.Labels, desired.Labels)
	current.Annotations = mergeOwnedAnnotations(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Spec.Paused = desired.Spec.Paused
	current.Spec.Replicas = desired.Spec.Replicas
	current.Spec.Strategy = desired.Spec.Strategy
	current.Spec.Template.Labels = ctrlmetadata.MergeStringMap(current.Spec.Template.Labels, desired.Spec.Template.Labels)
	current.Spec.Template.Annotations = mergeOwnedAnnotations(current.Spec.Template.Annotations, desired.Spec.Template.Annotations)
	current.Spec.Template.Spec.ServiceAccountName = desired.Spec.Template.Spec.ServiceAccountName
	current.Spec.Template.Spec.AutomountServiceAccountToken = desired.Spec.Template.Spec.AutomountServiceAccountToken
	current.Spec.Template.Spec.SecurityContext = desired.Spec.Template.Spec.SecurityContext
	current.Spec.Template.Spec.InitContainers = desired.Spec.Template.Spec.InitContainers
	current.Spec.Template.Spec.Containers = desired.Spec.Template.Spec.Containers
	current.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func (r *CardanoNetworkReconciler) applyNetworkArtifactsConfigMap(
	ctx context.Context,
	desired *corev1.ConfigMap,
) (controllerutil.OperationResult, *corev1.ConfigMap, error) {
	desired = desired.DeepCopy()
	current := &corev1.ConfigMap{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return controllerutil.OperationResultNone, nil, err
		}

		return controllerutil.OperationResultCreated, desired, nil
	}
	if err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	if err := validateControllerOwner(current, desired); err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	if !current.DeletionTimestamp.IsZero() {
		return controllerutil.OperationResultUpdated, current, nil
	}

	if artifactConfigMapNeedsRecovery(current, desired.Annotations[localnetFingerprintAnno]) {
		if err := r.Delete(ctx, current); err != nil && !apierrors.IsNotFound(err) {
			return controllerutil.OperationResultNone, nil, err
		}

		return controllerutil.OperationResultUpdated, current, nil
	}

	before := current.DeepCopy()
	current.Labels = ctrlmetadata.MergeStringMap(current.Labels, desired.Labels)
	current.Annotations = ctrlmetadata.MergeStringMap(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, current, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, nil, err
	}

	return controllerutil.OperationResultUpdated, current, nil
}

func (r *CardanoNetworkReconciler) applyArtifactPublisherServiceAccount(
	ctx context.Context,
	desired *corev1.ServiceAccount,
) (controllerutil.OperationResult, error) {
	desired = desired.DeepCopy()
	current := &corev1.ServiceAccount{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
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
	current.Labels = ctrlmetadata.MergeStringMap(current.Labels, desired.Labels)
	current.Annotations = ctrlmetadata.MergeStringMap(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.AutomountServiceAccountToken = desired.AutomountServiceAccountToken

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func (r *CardanoNetworkReconciler) applyArtifactPublisherRole(
	ctx context.Context,
	desired *rbacv1.Role,
) (controllerutil.OperationResult, error) {
	desired = desired.DeepCopy()
	current := &rbacv1.Role{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
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
	current.Labels = ctrlmetadata.MergeStringMap(current.Labels, desired.Labels)
	current.Annotations = ctrlmetadata.MergeStringMap(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Rules = desired.Rules

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func (r *CardanoNetworkReconciler) applyArtifactPublisherRoleBinding(
	ctx context.Context,
	desired *rbacv1.RoleBinding,
) (controllerutil.OperationResult, error) {
	desired = desired.DeepCopy()
	current := &rbacv1.RoleBinding{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
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
	if !equality.Semantic.DeepEqual(current.RoleRef, desired.RoleRef) {
		return controllerutil.OperationResultNone, unsupportedWorkloadChange(
			"RoleBinding %s roleRef drifted from desired value",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	before := current.DeepCopy()
	current.Labels = ctrlmetadata.MergeStringMap(current.Labels, desired.Labels)
	current.Annotations = ctrlmetadata.MergeStringMap(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Subjects = desired.Subjects

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

func (r *CardanoNetworkReconciler) applyPrimaryService(
	ctx context.Context,
	desired *corev1.Service,
) (controllerutil.OperationResult, error) {
	desired = desired.DeepCopy()
	if err := r.defaultObject(desired); err != nil {
		return controllerutil.OperationResultNone, err
	}

	current := &corev1.Service{}
	err := r.Get(ctx, ctrlmetadata.ObjectKey(desired), current)
	if apierrors.IsNotFound(err) {
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
	current.Labels = ctrlmetadata.MergeStringMap(current.Labels, desired.Labels)
	current.Annotations = ctrlmetadata.MergeStringMap(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
	current.Spec.Type = desired.Spec.Type
	current.Spec.Selector = maps.Clone(desired.Spec.Selector)
	current.Spec.Ports = desired.Spec.Ports
	current.Spec.ExternalName = desired.Spec.ExternalName

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, nil
	}
	if err := r.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, err
	}

	return controllerutil.OperationResultUpdated, nil
}

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
	current.Labels = ctrlmetadata.MergeStringMap(current.Labels, desired.Labels)
	current.Annotations = ctrlmetadata.MergeStringMap(current.Annotations, desired.Annotations)
	current.OwnerReferences = desired.OwnerReferences
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

func (r *CardanoNetworkReconciler) deletePrimaryOgmiosService(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (controllerutil.OperationResult, error) {
	return r.deletePrimaryChainAPIService(ctx, network, primaryOgmiosServiceName(network), "Ogmios")
}

func (r *CardanoNetworkReconciler) deletePrimaryKupoService(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (controllerutil.OperationResult, error) {
	return r.deletePrimaryChainAPIService(ctx, network, primaryKupoServiceName(network), "Kupo")
}

func (r *CardanoNetworkReconciler) deletePrimaryFaucetService(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (controllerutil.OperationResult, error) {
	return r.deletePrimaryChainAPIService(ctx, network, primaryFaucetServiceName(network), "faucet")
}

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

func (r *CardanoNetworkReconciler) deleteObjectIfOwned(
	ctx context.Context,
	desired client.Object,
	current client.Object,
) error {
	return r.deleteObjectIfOwnedWithReader(ctx, desired, current, r.Client)
}

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

func generateFaucetAuthToken() (string, error) {
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return "", fmt.Errorf("generate faucet auth token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(tokenBytes[:]), nil
}

func validFaucetAuthToken(token string) bool {
	if len(token) < 32 {
		return false
	}
	for _, char := range token {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return false
		}
	}

	return true
}

func (r *CardanoNetworkReconciler) defaultObject(object client.Object) error {
	if r.Scheme == nil {
		return fmt.Errorf("scheme is required")
	}
	r.Scheme.Default(object)

	return nil
}

func mergeOwnedAnnotations(current map[string]string, desired map[string]string) map[string]string {
	return ctrlmetadata.MergeOwnedAnnotations(
		current,
		desired,
		localnetFingerprintAnno,
		ctrlstorage.RequestedStorageClassAnnotation,
		networkArtifactsConfigMapUIDAnno,
	)
}

func resourceConflict(format string, args ...any) unsupportedApplyError {
	return ctrlapply.Unsupported(conditionReasonResourceConflict, format, args...)
}

func unsupportedStorageChange(format string, args ...any) unsupportedApplyError {
	return ctrlapply.Unsupported(conditionReasonUnsupportedStorageChange, format, args...)
}

func unsupportedWorkloadChange(format string, args ...any) unsupportedApplyError {
	return ctrlapply.Unsupported(conditionReasonUnsupportedWorkloadChange, format, args...)
}

func unsupportedLocalnetChange(format string, args ...any) unsupportedApplyError {
	return ctrlapply.Unsupported(conditionReasonUnsupportedLocalnetChange, format, args...)
}

func missingLocalnetFingerprint(format string, args ...any) unsupportedApplyError {
	return ctrlapply.Unsupported(conditionReasonMissingLocalnetFingerprint, format, args...)
}

func validateControllerOwner(current metav1.Object, desired metav1.Object) error {
	if err := ctrlmetadata.ValidateControllerOwner(current, desired); err != nil {
		return resourceConflict("%s", err.Error())
	}

	return nil
}

func validateAcceptedLocalnetFingerprint(network *yacdv1alpha1.CardanoNetwork, desiredFingerprint string) error {
	if network.Status.Network == nil || network.Status.Network.LocalnetFingerprint == "" {
		return nil
	}
	if network.Status.Network.LocalnetFingerprint == desiredFingerprint {
		return nil
	}

	return unsupportedLocalnetChange(
		"CardanoNetwork localnet inputs changed from accepted fingerprint; delete and recreate the CardanoNetwork to change network parameters",
	)
}

func validateLocalnetFingerprint(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim) error {
	currentFingerprint := current.Annotations[localnetFingerprintAnno]
	if currentFingerprint == "" {
		return missingLocalnetFingerprint(
			"PVC %s is missing localnet fingerprint annotation; delete and recreate the CardanoNetwork to recreate localnet state",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	desiredFingerprint := desired.Annotations[localnetFingerprintAnno]
	if currentFingerprint != desiredFingerprint {
		return unsupportedLocalnetChange(
			"CardanoNetwork localnet inputs changed for PVC %s; delete and recreate the CardanoNetwork to change network parameters",
			ctrlmetadata.ObjectKey(desired),
		)
	}

	return nil
}

func validateRequestedStorageClass(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim) error {
	currentStorageClass, currentHasStorageClassRequest := ctrlstorage.RequestedStorageClass(current.Annotations)
	desiredStorageClass, desiredHasStorageClassRequest := ctrlstorage.RequestedStorageClass(desired.Annotations)
	if currentHasStorageClassRequest == desiredHasStorageClassRequest && currentStorageClass == desiredStorageClass {
		return nil
	}

	return unsupportedStorageChange(
		"PVC %s requested storageClassName cannot be changed from %s to %s",
		ctrlmetadata.ObjectKey(desired),
		ctrlstorage.AnnotationValue(currentStorageClass, currentHasStorageClassRequest),
		ctrlstorage.AnnotationValue(desiredStorageClass, desiredHasStorageClassRequest),
	)
}
