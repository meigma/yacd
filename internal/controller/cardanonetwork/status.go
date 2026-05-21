package cardanonetwork

import (
	"context"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	conditionTypeProgressing = "Progressing"
	conditionTypeDegraded    = "Degraded"

	conditionReasonReconcileSucceeded          = "ReconcileSucceeded"
	conditionReasonUnsupportedSpec             = "UnsupportedSpec"
	conditionReasonUnsupportedLocalnetChange   = "UnsupportedLocalnetChange"
	conditionReasonMissingLocalnetFingerprint  = "MissingLocalnetFingerprint"
	conditionReasonUnsupportedStorageChange    = "UnsupportedStorageChange"
	conditionReasonUnsupportedWorkloadChange   = "UnsupportedWorkloadChange"
	conditionReasonResourceConflict            = "ResourceConflict"
	conditionReasonWorkloadApplied             = "WorkloadApplied"
	conditionMessagePrimaryWorkloadApplied     = "Primary node PVC and Deployment are applied"
	conditionMessagePrimaryWorkloadUnsupported = "Primary node workload is not supported for this CardanoNetwork spec"
)

func (r *CardanoNetworkReconciler) patchStatusConditions(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	conditions ...metav1.Condition,
) error {
	return r.patchPrimaryWorkloadStatus(ctx, network, "", conditions...)
}

func (r *CardanoNetworkReconciler) patchPrimaryWorkloadAppliedStatus(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	localnetFingerprint string,
) error {
	return r.patchPrimaryWorkloadStatus(ctx, network, localnetFingerprint,
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, conditionMessagePrimaryWorkloadApplied),
		progressingCondition(metav1.ConditionFalse, conditionReasonWorkloadApplied, conditionMessagePrimaryWorkloadApplied),
	)
}

func (r *CardanoNetworkReconciler) patchPrimaryWorkloadStatus(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	localnetFingerprint string,
	conditions ...metav1.Condition,
) error {
	original := network.DeepCopy()
	network.Status.ObservedGeneration = network.Generation
	if localnetFingerprint != "" {
		setLocalnetIdentityStatus(network, localnetFingerprint)
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

func degradedCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return metav1.Condition{
		Type:               conditionTypeDegraded,
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
