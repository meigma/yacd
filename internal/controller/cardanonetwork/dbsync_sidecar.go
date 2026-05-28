package cardanonetwork

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrldbsync "github.com/meigma/yacd/internal/controller/cardanodbsync"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// primaryDBSyncAttachmentResult carries the optional primary Pod attachment
// fragment and the network-level condition explaining why attachment is or is
// not rendered.
type primaryDBSyncAttachmentResult struct {
	Attachment *ctrldbsync.PrimarySidecarAttachment
	Condition  metav1.Condition
}

// statusCondition returns the condition to publish for the attachment result.
// A zero-value result means no primary-sidecar claim was requested.
func (result primaryDBSyncAttachmentResult) statusCondition() metav1.Condition {
	if result.Condition.Type != "" {
		return result.Condition
	}

	return dbSyncAttachmentReadyCondition(
		metav1.ConditionFalse,
		conditionReasonDBSyncAttachmentNotRequested,
		conditionMessageDBSyncAttachmentNotRequested,
	)
}

// primaryDBSyncAttachment resolves the CardanoDBSync primary-sidecar claim for
// the network and builds the pod-template fragment when exactly one claim has
// fresh attachable status.
func (r *CardanoNetworkReconciler) primaryDBSyncAttachment(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (primaryDBSyncAttachmentResult, error) {
	claims, err := r.primarySidecarClaims(ctx, network)
	if err != nil {
		return primaryDBSyncAttachmentResult{}, err
	}
	if len(claims) == 0 {
		return primaryDBSyncAttachmentResult{
			Condition: dbSyncAttachmentReadyCondition(
				metav1.ConditionFalse,
				conditionReasonDBSyncAttachmentNotRequested,
				conditionMessageDBSyncAttachmentNotRequested,
			),
		}, nil
	}
	if len(claims) != 1 {
		return primaryDBSyncAttachmentResult{
			Condition: dbSyncAttachmentReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPlacementConflict,
				fmt.Sprintf("CardanoNetwork %q has multiple primary-sidecar CardanoDBSync claims; exactly one primary-sidecar claim is allowed", network.Name),
			),
		}, nil
	}

	claim := claims[0]
	sidecarStatus, ok := ctrldbsync.PrimarySidecarClaimReadyForAttachment(&claim, network.Name)
	if !ok {
		return primaryDBSyncAttachmentResult{
			Condition: dbSyncAttachmentReadyCondition(
				metav1.ConditionFalse,
				conditionReasonDBSyncAttachmentPending,
				conditionMessageDBSyncAttachmentPending,
			),
		}, nil
	}
	if err := ctrldbsync.ValidatePrimarySidecarNetwork(&claim, network); err != nil {
		logf.FromContext(ctx).V(1).Info("Skipping unsupported CardanoDBSync primary-sidecar attachment", "cardanodbsync", client.ObjectKeyFromObject(&claim), "error", err.Error())
		return primaryDBSyncAttachmentResult{
			Condition: dbSyncAttachmentReadyCondition(
				metav1.ConditionFalse,
				conditionReasonUnsupportedSpec,
				err.Error(),
			),
		}, nil
	}

	database, ok := ctrldbsync.PrimarySidecarDatabase(&claim)
	if !ok {
		return primaryDBSyncAttachmentResult{
			Condition: dbSyncAttachmentReadyCondition(
				metav1.ConditionFalse,
				conditionReasonDBSyncAttachmentPending,
				conditionMessageDBSyncAttachmentPending,
			),
		}, nil
	}
	if network.Status.Artifacts == nil || network.Status.Artifacts.NetworkConfigMapName == "" {
		return primaryDBSyncAttachmentResult{
			Condition: dbSyncAttachmentReadyCondition(
				metav1.ConditionFalse,
				conditionReasonDBSyncAttachmentPending,
				conditionMessageDBSyncAttachmentPending,
			),
		}, nil
	}
	resources := ctrldbsync.PrimarySidecarAttachmentResources{
		NetworkArtifactsConfigMapName: network.Status.Artifacts.NetworkConfigMapName,
		ConfigMapName:                 sidecarStatus.Resources.ConfigMapName,
		PGPassSecretName:              sidecarStatus.Resources.PGPassSecretName,
		StatePVCName:                  sidecarStatus.Resources.StatePVCName,
		Revision:                      sidecarStatus.Revision,
	}

	attachment, err := ctrldbsync.BuildPrimarySidecarAttachment(&claim, network, database, resources)
	if err != nil {
		logf.FromContext(ctx).V(1).Info("Skipping CardanoDBSync primary-sidecar attachment", "cardanodbsync", client.ObjectKeyFromObject(&claim), "error", err.Error())
		return primaryDBSyncAttachmentResult{
			Condition: dbSyncAttachmentReadyCondition(
				metav1.ConditionFalse,
				conditionReasonUnsupportedSpec,
				err.Error(),
			),
		}, nil
	}

	return primaryDBSyncAttachmentResult{Attachment: attachment}, nil
}

// primarySidecarClaims lists non-deleting CardanoDBSync resources in the
// namespace that explicitly request primarySidecar placement for this network.
func (r *CardanoNetworkReconciler) primarySidecarClaims(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) ([]yacdv1alpha1.CardanoDBSync, error) {
	dbSyncs := &yacdv1alpha1.CardanoDBSyncList{}
	if err := r.List(ctx, dbSyncs, client.InNamespace(network.Namespace)); err != nil {
		return nil, err
	}

	claims := make([]yacdv1alpha1.CardanoDBSync, 0, len(dbSyncs.Items))
	for _, candidate := range dbSyncs.Items {
		if !candidate.DeletionTimestamp.IsZero() {
			continue
		}
		if candidate.Spec.NetworkRef.Name != network.Name {
			continue
		}
		if candidate.Spec.Placement == nil || candidate.Spec.Placement.Mode != yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar {
			continue
		}
		claims = append(claims, candidate)
	}

	return claims, nil
}

// cardanoNetworksForDBSyncPlacement maps a primarySidecar CardanoDBSync claim
// to its referenced CardanoNetwork reconcile request.
func (r *CardanoNetworkReconciler) cardanoNetworksForDBSyncPlacement(object client.Object) []reconcile.Request {
	dbSync, ok := object.(*yacdv1alpha1.CardanoDBSync)
	if !ok {
		return nil
	}
	if dbSync.Spec.Placement == nil || dbSync.Spec.Placement.Mode != yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar {
		return nil
	}
	if dbSync.Spec.NetworkRef.Name == "" {
		return nil
	}

	return []reconcile.Request{{
		NamespacedName: client.ObjectKey{
			Namespace: dbSync.Namespace,
			Name:      dbSync.Spec.NetworkRef.Name,
		},
	}}
}

// dbSyncPlacementEventHandler requeues the referenced CardanoNetwork when a
// primarySidecar claim is created, deleted, or updated.
func (r *CardanoNetworkReconciler) dbSyncPlacementEventHandler() handler.EventHandler {
	return handler.Funcs{
		CreateFunc: func(ctx context.Context, event event.CreateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			r.enqueueDBSyncPlacementNetwork(queue, event.Object)
		},
		UpdateFunc: func(ctx context.Context, event event.UpdateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			r.enqueueDBSyncPlacementNetwork(queue, event.ObjectOld)
			r.enqueueDBSyncPlacementNetwork(queue, event.ObjectNew)
		},
		DeleteFunc: func(ctx context.Context, event event.DeleteEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			r.enqueueDBSyncPlacementNetwork(queue, event.Object)
		},
	}
}

// enqueueDBSyncPlacementNetwork adds the CardanoNetwork request referenced by
// the supplied CardanoDBSync object, when it is a primarySidecar claim.
func (r *CardanoNetworkReconciler) enqueueDBSyncPlacementNetwork(
	queue workqueue.TypedRateLimitingInterface[reconcile.Request],
	object client.Object,
) {
	for _, request := range r.cardanoNetworksForDBSyncPlacement(object) {
		queue.Add(request)
	}
}
