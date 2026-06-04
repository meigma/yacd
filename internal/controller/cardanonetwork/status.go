package cardanonetwork

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// patchStatusConditionsClearingRuntime writes a status patch that clears the
// runtime (sync) status while applying the caller-supplied conditions. Used on
// the Degraded paths (unsupported spec, apply error) where the runtime status
// must not lag and the conditions must reflect the failure reason.
func (r *CardanoNetworkReconciler) patchStatusConditionsClearingRuntime(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	networkPlan primaryNetworkPlan,
	acceptedIdentity acceptedNetworkIdentity,
	conditions ...metav1.Condition,
) error {
	return r.patchPrimaryWorkloadStatus(ctx, network, networkPlan, acceptedIdentity, nil, nil, nil, nil, nil, true, conditions...)
}

// patchPrimaryWorkloadAppliedStatus computes per-component readiness for
// the freshly applied primary workload and writes the aggregated status
// patch. Returns the aggregate Ready condition so the reconciler can use
// it to decide requeue behavior.
func (r *CardanoNetworkReconciler) patchPrimaryWorkloadAppliedStatus(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	networkPlan primaryNetworkPlan,
	acceptedIdentity acceptedNetworkIdentity,
	nodeService *corev1.Service,
	ogmiosService *corev1.Service,
	kupoService *corev1.Service,
	artifactsService *corev1.Service,
	dbSyncAttached bool,
	dbSyncAttachmentCondition metav1.Condition,
) (metav1.Condition, error) {
	dbSyncAttachmentReady, err := r.primaryDBSyncAttachmentReadyCondition(ctx, network, dbSyncAttached, dbSyncAttachmentCondition)
	if err != nil {
		return metav1.Condition{}, err
	}
	nodeReady, err := r.primaryNodeReadyCondition(ctx, network)
	if err != nil {
		return metav1.Condition{}, err
	}
	ogmiosReady, err := r.primaryOgmiosReadyCondition(ctx, network, ogmiosService != nil)
	if err != nil {
		return metav1.Condition{}, err
	}
	kupoReady, err := r.primaryKupoReadyCondition(ctx, network, kupoService != nil)
	if err != nil {
		return metav1.Condition{}, err
	}

	// Derive ArtifactsReady from served-artifacts availability (the artifacts
	// Service and the always-on serve sidecar container's readiness) rather than
	// from a ConfigMap. Every supported network serves artifacts, so the
	// disabled branch is effectively unreachable.
	artifactsReady, err := r.primaryArtifactsReadyCondition(ctx, network, stagesServedArtifacts(network))
	if err != nil {
		return metav1.Condition{}, err
	}
	syncStatus, nodeSynchronized, nodeProgressing := r.primaryNodeSyncStatusConditions(ctx, network, ogmiosService, artifactsReady.Status == metav1.ConditionTrue, artifactsReady.Message)
	ready := readyCondition(dbSyncAttachmentReady, nodeReady, ogmiosReady, kupoReady, artifactsReady, dbSyncAttached, kupoService != nil)

	degraded := degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, conditionMessagePrimaryWorkloadApplied)

	if err := r.patchPrimaryWorkloadStatus(ctx, network, networkPlan, acceptedIdentity, nodeService, ogmiosService, kupoService, artifactsService, syncStatus, false,
		degraded,
		progressingForReadyCondition(ready),
		ready,
		dbSyncAttachmentReady,
		nodeReady,
		nodeSynchronized,
		nodeProgressing,
		ogmiosReady,
		kupoReady,
		artifactsReady,
	); err != nil {
		return metav1.Condition{}, err
	}

	return ready, nil
}

// patchPrimaryWorkloadStatus writes the CardanoNetwork status patch.
// Setter helpers below carry the actual mutations; this function owns the
// observedGeneration stamp and the diff-aware patch through ctrlstatus.
func (r *CardanoNetworkReconciler) patchPrimaryWorkloadStatus(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	networkPlan primaryNetworkPlan,
	acceptedIdentity acceptedNetworkIdentity,
	nodeService *corev1.Service,
	ogmiosService *corev1.Service,
	kupoService *corev1.Service,
	artifactsService *corev1.Service,
	syncStatus *yacdv1alpha1.CardanoNetworkSyncStatus,
	clearRuntime bool,
	conditions ...metav1.Condition,
) error {
	original := network.DeepCopy()
	network.Status.ObservedGeneration = network.Generation
	if networkPlan.Fingerprint != "" {
		setNetworkIdentityStatus(network, networkPlan, acceptedIdentity)
	}
	if nodeService != nil {
		setEndpointStatus(network, nodeService, ogmiosService, kupoService, artifactsService)
		setSyncStatus(network, syncStatus)
	} else if clearRuntime {
		clearSyncStatus(network)
	}
	ctrlstatus.SetObserved(&network.Status.Conditions, network.Generation, conditions...)

	return ctrlstatus.PatchIfChanged(ctx, r.Status(), network, original)
}

// setSyncStatus copies the node sync payload onto the CardanoNetwork or clears
// it when nil. The payload is deep-copied so the caller can mutate the source
// freely.
func setSyncStatus(network *yacdv1alpha1.CardanoNetwork, syncStatus *yacdv1alpha1.CardanoNetworkSyncStatus) {
	if syncStatus == nil {
		network.Status.Sync = nil
		return
	}

	network.Status.Sync = syncStatus.DeepCopy()
}

// clearSyncStatus removes the sync payload from CardanoNetwork status. Used on
// failure paths where retaining the previous probe result would be stale.
func clearSyncStatus(network *yacdv1alpha1.CardanoNetwork) {
	network.Status.Sync = nil
}

// setNetworkIdentityStatus publishes the resolved network identity to
// CardanoNetwork status. Fingerprints are mirrored from accepted owned
// runtime material when present; status itself is not an acceptance source.
func setNetworkIdentityStatus(network *yacdv1alpha1.CardanoNetwork, plan primaryNetworkPlan, acceptedIdentity acceptedNetworkIdentity) {
	if network.Status.Network == nil {
		network.Status.Network = &yacdv1alpha1.CardanoNetworkIdentityStatus{}
	}

	network.Status.Network.Mode = plan.Mode
	localnetFingerprint := plan.localnetFingerprint()
	if acceptedIdentity.LocalnetFingerprint != "" {
		localnetFingerprint = acceptedIdentity.LocalnetFingerprint
	}
	networkFingerprint := plan.Fingerprint
	if acceptedIdentity.NetworkFingerprint != "" {
		networkFingerprint = acceptedIdentity.NetworkFingerprint
	}
	network.Status.Network.LocalnetFingerprint = localnetFingerprint
	network.Status.Network.NetworkFingerprint = networkFingerprint
	network.Status.Network.Profile = nil
	if plan.Profile != nil {
		profile := *plan.Profile
		network.Status.Network.Profile = &profile
	}

	networkMagic := plan.NetworkMagic
	network.Status.Network.NetworkMagic = &networkMagic
	network.Status.Network.Era = nil
	if plan.Era != nil {
		era := *plan.Era
		network.Status.Network.Era = &era
	}
}

// setEndpointStatus publishes the in-cluster endpoint URLs for the primary
// node-to-node Service and any enabled chain API sidecars.
func setEndpointStatus(network *yacdv1alpha1.CardanoNetwork, nodeService *corev1.Service, ogmiosService *corev1.Service, kupoService *corev1.Service, artifactsService *corev1.Service) {
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
	} else {
		network.Status.Endpoints.Ogmios = &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: ogmiosService.Name,
			Port:        ogmiosService.Spec.Ports[0].Port,
			URL:         fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", ogmiosServiceURLType, ogmiosService.Name, ogmiosService.Namespace, ogmiosService.Spec.Ports[0].Port),
		}
	}
	if kupoService == nil {
		network.Status.Endpoints.Kupo = nil
	} else {
		network.Status.Endpoints.Kupo = &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: kupoService.Name,
			Port:        kupoService.Spec.Ports[0].Port,
			URL:         fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", kupoServiceURLType, kupoService.Name, kupoService.Namespace, kupoService.Spec.Ports[0].Port),
		}
	}

	if artifactsService == nil {
		network.Status.Endpoints.Artifacts = nil
		return
	}

	network.Status.Endpoints.Artifacts = &yacdv1alpha1.ServiceEndpointStatus{
		ServiceName: artifactsService.Name,
		Port:        artifactsService.Spec.Ports[0].Port,
		URL:         fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", serveServiceURLType, artifactsService.Name, artifactsService.Namespace, artifactsService.Spec.Ports[0].Port),
	}
}
