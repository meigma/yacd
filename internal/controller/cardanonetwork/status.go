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
	conditionTypeNodeReady   = "NodeReady"

	conditionReasonReconcileSucceeded          = "ReconcileSucceeded"
	conditionReasonUnsupportedSpec             = "UnsupportedSpec"
	conditionReasonUnsupportedLocalnetChange   = "UnsupportedLocalnetChange"
	conditionReasonMissingLocalnetFingerprint  = "MissingLocalnetFingerprint"
	conditionReasonUnsupportedStorageChange    = "UnsupportedStorageChange"
	conditionReasonUnsupportedWorkloadChange   = "UnsupportedWorkloadChange"
	conditionReasonResourceConflict            = "ResourceConflict"
	conditionReasonDeploymentProgressing       = "DeploymentProgressing"
	conditionReasonNodeReady                   = "NodeReady"
	conditionReasonPrimaryWorkloadMissing      = "PrimaryWorkloadMissing"
	conditionMessagePrimaryWorkloadApplied     = "Primary node PVC, Deployment, and Service are applied"
	conditionMessagePrimaryWorkloadUnsupported = "Primary node workload is not supported for this CardanoNetwork spec"
	conditionMessagePrimaryNodeReady           = "Primary node Deployment is available"
)

func (r *CardanoNetworkReconciler) patchStatusConditions(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	conditions ...metav1.Condition,
) error {
	return r.patchPrimaryWorkloadStatus(ctx, network, "", nil, conditions...)
}

func (r *CardanoNetworkReconciler) patchPrimaryWorkloadAppliedStatus(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	localnetFingerprint string,
	service *corev1.Service,
) error {
	nodeReady, err := r.primaryNodeReadyCondition(ctx, network)
	if err != nil {
		return err
	}

	return r.patchPrimaryWorkloadStatus(ctx, network, localnetFingerprint, service,
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, conditionMessagePrimaryWorkloadApplied),
		progressingForNodeReadyCondition(nodeReady),
		nodeReady,
	)
}

func (r *CardanoNetworkReconciler) patchPrimaryWorkloadStatus(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	localnetFingerprint string,
	service *corev1.Service,
	conditions ...metav1.Condition,
) error {
	original := network.DeepCopy()
	network.Status.ObservedGeneration = network.Generation
	if localnetFingerprint != "" {
		setLocalnetIdentityStatus(network, localnetFingerprint)
	}
	if service != nil {
		setNodeToNodeEndpointStatus(network, service)
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

func setNodeToNodeEndpointStatus(network *yacdv1alpha1.CardanoNetwork, service *corev1.Service) {
	if network.Status.Endpoints == nil {
		network.Status.Endpoints = &yacdv1alpha1.CardanoNetworkEndpointsStatus{}
	}

	network.Status.Endpoints.NodeToNode = &yacdv1alpha1.ServiceEndpointStatus{
		ServiceName: service.Name,
		Port:        network.Spec.Node.Port,
		URL:         fmt.Sprintf("tcp://%s.%s.svc.cluster.local:%d", service.Name, service.Namespace, network.Spec.Node.Port),
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
	if !deploymentAvailable(deployment) ||
		deployment.Status.ReadyReplicas < 1 ||
		deployment.Status.AvailableReplicas < 1 {
		return nodeReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Primary node Deployment is not available",
		), nil
	}

	return nodeReadyCondition(
		metav1.ConditionTrue,
		conditionReasonNodeReady,
		conditionMessagePrimaryNodeReady,
	), nil
}

func deploymentAvailable(deployment *appsv1.Deployment) bool {
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable {
			return condition.Status == corev1.ConditionTrue
		}
	}

	return false
}

func degradedCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return metav1.Condition{
		Type:               conditionTypeDegraded,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
}

func nodeReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return metav1.Condition{
		Type:               conditionTypeNodeReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
}

func progressingCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return metav1.Condition{
		Type:               conditionTypeProgressing,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
}

func progressingForNodeReadyCondition(nodeReady metav1.Condition) metav1.Condition {
	if nodeReady.Status == metav1.ConditionTrue {
		return progressingCondition(metav1.ConditionFalse, conditionReasonNodeReady, conditionMessagePrimaryNodeReady)
	}

	return progressingCondition(metav1.ConditionTrue, nodeReady.Reason, nodeReady.Message)
}
