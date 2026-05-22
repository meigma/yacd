package cardanonetwork

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	conditionTypeProgressing = "Progressing"
	conditionTypeDegraded    = "Degraded"
	conditionTypeReady       = "Ready"
	conditionTypeNodeReady   = "NodeReady"
	conditionTypeOgmiosReady = "OgmiosReady"

	conditionReasonReconcileSucceeded          = "ReconcileSucceeded"
	conditionReasonUnsupportedSpec             = "UnsupportedSpec"
	conditionReasonUnsupportedLocalnetChange   = "UnsupportedLocalnetChange"
	conditionReasonMissingLocalnetFingerprint  = "MissingLocalnetFingerprint"
	conditionReasonUnsupportedStorageChange    = "UnsupportedStorageChange"
	conditionReasonUnsupportedWorkloadChange   = "UnsupportedWorkloadChange"
	conditionReasonResourceConflict            = "ResourceConflict"
	conditionReasonDeploymentProgressing       = "DeploymentProgressing"
	conditionReasonReady                       = "Ready"
	conditionReasonNodeReady                   = "NodeReady"
	conditionReasonOgmiosReady                 = "OgmiosReady"
	conditionReasonOgmiosDisabled              = "OgmiosDisabled"
	conditionReasonPrimaryWorkloadMissing      = "PrimaryWorkloadMissing"
	conditionMessagePrimaryWorkloadApplied     = "Primary node PVC, Deployment, Service, and chain API resources are applied"
	conditionMessagePrimaryWorkloadUnsupported = "Primary node workload is not supported for this CardanoNetwork spec"
	conditionMessageReady                      = "CardanoNetwork is usable through its published endpoints"
	conditionMessagePrimaryNodeReady           = "Primary node container is ready"
	conditionMessageOgmiosReady                = "Ogmios sidecar is connected and available through its Service"
	conditionMessageOgmiosDisabled             = "Ogmios chain API is disabled"
)

func (r *CardanoNetworkReconciler) patchStatusConditions(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	conditions ...metav1.Condition,
) error {
	return r.patchPrimaryWorkloadStatus(ctx, network, "", nil, nil, conditions...)
}

func (r *CardanoNetworkReconciler) patchPrimaryWorkloadAppliedStatus(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	localnetFingerprint string,
	nodeService *corev1.Service,
	ogmiosService *corev1.Service,
) (metav1.Condition, error) {
	nodeReady, err := r.primaryNodeReadyCondition(ctx, network)
	if err != nil {
		return metav1.Condition{}, err
	}
	ogmiosReady, err := r.primaryOgmiosReadyCondition(ctx, network, ogmiosService != nil)
	if err != nil {
		return metav1.Condition{}, err
	}
	ready := readyCondition(nodeReady, ogmiosReady)

	if err := r.patchPrimaryWorkloadStatus(ctx, network, localnetFingerprint, nodeService, ogmiosService,
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, conditionMessagePrimaryWorkloadApplied),
		progressingForReadyCondition(ready),
		ready,
		nodeReady,
		ogmiosReady,
	); err != nil {
		return metav1.Condition{}, err
	}

	return ready, nil
}

func (r *CardanoNetworkReconciler) patchPrimaryWorkloadStatus(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	localnetFingerprint string,
	nodeService *corev1.Service,
	ogmiosService *corev1.Service,
	conditions ...metav1.Condition,
) error {
	original := network.DeepCopy()
	network.Status.ObservedGeneration = network.Generation
	if localnetFingerprint != "" {
		setLocalnetIdentityStatus(network, localnetFingerprint)
	}
	if nodeService != nil {
		setEndpointStatus(network, nodeService, ogmiosService)
	}
	for _, condition := range conditions {
		condition.ObservedGeneration = network.Generation
		apimeta.SetStatusCondition(&network.Status.Conditions, condition)
	}

	if equality.Semantic.DeepEqual(original.Status, network.Status) {
		return nil
	}

	return r.Status().Patch(ctx, network, client.MergeFrom(original))
}

func setLocalnetIdentityStatus(network *yacdv1alpha1.CardanoNetwork, localnetFingerprint string) {
	if network.Status.Network == nil {
		network.Status.Network = &yacdv1alpha1.CardanoNetworkIdentityStatus{}
	}

	network.Status.Network.Mode = network.Spec.Mode
	network.Status.Network.LocalnetFingerprint = localnetFingerprint
	if network.Spec.Local == nil {
		return
	}

	networkMagic := network.Spec.Local.NetworkMagic
	network.Status.Network.NetworkMagic = &networkMagic
	era := network.Spec.Local.Era
	network.Status.Network.Era = &era
}

func setEndpointStatus(network *yacdv1alpha1.CardanoNetwork, nodeService *corev1.Service, ogmiosService *corev1.Service) {
	if network.Status.Endpoints == nil {
		network.Status.Endpoints = &yacdv1alpha1.CardanoNetworkEndpointsStatus{}
	}

	network.Status.Endpoints.NodeToNode = &yacdv1alpha1.ServiceEndpointStatus{
		ServiceName: nodeService.Name,
		Port:        network.Spec.Node.Port,
		URL:         fmt.Sprintf("tcp://%s.%s.svc.cluster.local:%d", nodeService.Name, nodeService.Namespace, network.Spec.Node.Port),
	}
	if ogmiosService == nil {
		network.Status.Endpoints.Ogmios = nil
		return
	}

	network.Status.Endpoints.Ogmios = &yacdv1alpha1.ServiceEndpointStatus{
		ServiceName: ogmiosService.Name,
		Port:        ogmiosService.Spec.Ports[0].Port,
		URL:         fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", ogmiosServiceURLType, ogmiosService.Name, ogmiosService.Namespace, ogmiosService.Spec.Ports[0].Port),
	}
}

func (r *CardanoNetworkReconciler) primaryNodeReadyCondition(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (metav1.Condition, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryNodeStatePVCName(network)}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return nodeReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Primary node PVC is missing",
			), nil
		}
		return metav1.Condition{}, err
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}, service); err != nil {
		if apierrors.IsNotFound(err) {
			return nodeReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Primary node Service is missing",
			), nil
		}
		return metav1.Condition{}, err
	}

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return nodeReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Primary node Deployment is missing",
			), nil
		}
		return metav1.Condition{}, err
	}

	if deployment.Status.ObservedGeneration != deployment.Generation {
		return nodeReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Primary node Deployment has not observed the latest generation",
		), nil
	}
	if deployment.Status.UpdatedReplicas < 1 {
		return nodeReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Primary node Deployment is not available",
		), nil
	}

	containerReady, err := r.primaryPodContainerReady(ctx, network, cardanoNodeContainerName)
	if err != nil {
		return metav1.Condition{}, err
	}
	if !containerReady {
		return nodeReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Primary node container is not ready",
		), nil
	}

	return nodeReadyCondition(
		metav1.ConditionTrue,
		conditionReasonNodeReady,
		conditionMessagePrimaryNodeReady,
	), nil
}

func (r *CardanoNetworkReconciler) primaryOgmiosReadyCondition(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	enabled bool,
) (metav1.Condition, error) {
	if !enabled {
		return ogmiosReadyCondition(
			metav1.ConditionFalse,
			conditionReasonOgmiosDisabled,
			conditionMessageOgmiosDisabled,
		), nil
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryOgmiosServiceName(network)}, service); err != nil {
		if apierrors.IsNotFound(err) {
			return ogmiosReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Ogmios Service is missing",
			), nil
		}
		return metav1.Condition{}, err
	}

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ogmiosReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Primary node Deployment is missing",
			), nil
		}
		return metav1.Condition{}, err
	}
	if deployment.Status.ObservedGeneration != deployment.Generation {
		return ogmiosReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Primary node Deployment has not observed the latest generation",
		), nil
	}
	if deployment.Status.UpdatedReplicas < 1 {
		return ogmiosReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Ogmios sidecar is not available",
		), nil
	}

	containerReady, err := r.primaryPodContainerReady(ctx, network, ogmiosContainerName)
	if err != nil {
		return metav1.Condition{}, err
	}
	if !containerReady {
		return ogmiosReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Ogmios sidecar is not ready",
		), nil
	}

	return ogmiosReadyCondition(
		metav1.ConditionTrue,
		conditionReasonOgmiosReady,
		conditionMessageOgmiosReady,
	), nil
}

func (r *CardanoNetworkReconciler) primaryPodContainerReady(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	containerName string,
) (bool, error) {
	pods := &corev1.PodList{}
	if err := r.statusReader().List(
		ctx,
		pods,
		client.InNamespace(network.Namespace),
		client.MatchingLabels(primaryWorkloadSelectorLabels(network)),
	); err != nil {
		return false, err
	}

	for i := range pods.Items {
		if podContainerReady(&pods.Items[i], containerName) {
			return true, nil
		}
	}

	return false, nil
}

func (r *CardanoNetworkReconciler) statusReader() client.Reader {
	if r.Reader != nil {
		return r.Reader
	}

	return r.Client
}

func podContainerReady(pod *corev1.Pod, containerName string) bool {
	if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName && status.Ready && status.State.Running != nil {
			return true
		}
	}

	return false
}

func readyCondition(nodeReady metav1.Condition, ogmiosReady metav1.Condition) metav1.Condition {
	if nodeReady.Status == metav1.ConditionTrue && ogmiosReady.Status == metav1.ConditionTrue {
		return condition(conditionTypeReady, metav1.ConditionTrue, conditionReasonReady, conditionMessageReady)
	}
	if nodeReady.Status != metav1.ConditionTrue {
		return condition(conditionTypeReady, metav1.ConditionFalse, nodeReady.Reason, nodeReady.Message)
	}

	return condition(conditionTypeReady, metav1.ConditionFalse, ogmiosReady.Reason, ogmiosReady.Message)
}

func degradedCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeDegraded, status, reason, message)
}

func nodeReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeNodeReady, status, reason, message)
}

func ogmiosReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeOgmiosReady, status, reason, message)
}

func progressingCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return condition(conditionTypeProgressing, status, reason, message)
}

func condition(conditionType string, status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
}

func progressingForReadyCondition(ready metav1.Condition) metav1.Condition {
	if ready.Status == metav1.ConditionTrue {
		return progressingCondition(metav1.ConditionFalse, conditionReasonReady, conditionMessageReady)
	}
	if ready.Reason == conditionReasonDeploymentProgressing || ready.Reason == conditionReasonPrimaryWorkloadMissing {
		return progressingCondition(metav1.ConditionTrue, ready.Reason, ready.Message)
	}

	return progressingCondition(metav1.ConditionFalse, ready.Reason, ready.Message)
}
