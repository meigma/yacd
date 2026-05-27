package cardanonetwork

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlnetworkartifacts "github.com/meigma/yacd/internal/controller/networkartifacts"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// patchStatusConditionsClearingFaucet writes a status patch that clears the
// faucet endpoints and artifact status while applying the caller-supplied
// conditions. Used on the Degraded paths (unsupported spec, apply error)
// where the faucet must be torn down and the conditions must reflect the
// failure reason.
func (r *CardanoNetworkReconciler) patchStatusConditionsClearingFaucet(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	conditions ...metav1.Condition,
) error {
	return r.patchPrimaryWorkloadStatus(ctx, network, "", nil, nil, nil, nil, nil, nil, true, conditions...)
}

// patchPrimaryWorkloadAppliedStatus computes per-component readiness for
// the freshly applied primary workload and writes the aggregated status
// patch. Returns the aggregate Ready condition so the reconciler can use
// it to decide requeue behavior.
func (r *CardanoNetworkReconciler) patchPrimaryWorkloadAppliedStatus(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	localnetFingerprint string,
	nodeService *corev1.Service,
	ogmiosService *corev1.Service,
	kupoService *corev1.Service,
	faucetService *corev1.Service,
	faucetAuthSecret *corev1.Secret,
	networkArtifactsConfigMap *corev1.ConfigMap,
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
	faucetReady, err := r.primaryFaucetReadyCondition(ctx, network, faucetService != nil)
	if err != nil {
		return metav1.Condition{}, err
	}

	// Project the artifact ConfigMap verification result into both a status
	// payload (Status.Artifacts) and a ready/pending condition.
	artifactResult := ctrlnetworkartifacts.ProducerConfigMap(networkArtifactsConfigMap, localnetFingerprint)
	var artifactsStatus *yacdv1alpha1.CardanoNetworkArtifactsStatus
	artifactsReady := artifactsReadyCondition(
		metav1.ConditionFalse,
		conditionReasonArtifactsPending,
		artifactResult.Message,
	)
	if artifactResult.Ready {
		artifactsStatus = &artifactResult.Status
		artifactsReady = artifactsReadyCondition(
			metav1.ConditionTrue,
			conditionReasonArtifactsReady,
			conditionMessageArtifactsReady,
		)
	}
	ready := readyCondition(dbSyncAttachmentReady, nodeReady, ogmiosReady, kupoReady, faucetReady, artifactsReady, dbSyncAttached, kupoService != nil, faucetService != nil)

	if err := r.patchPrimaryWorkloadStatus(ctx, network, localnetFingerprint, nodeService, ogmiosService, kupoService, faucetService, faucetAuthSecret, artifactsStatus, false,
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, conditionMessagePrimaryWorkloadApplied),
		progressingForReadyCondition(ready),
		ready,
		dbSyncAttachmentReady,
		nodeReady,
		ogmiosReady,
		kupoReady,
		faucetReady,
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
	localnetFingerprint string,
	nodeService *corev1.Service,
	ogmiosService *corev1.Service,
	kupoService *corev1.Service,
	faucetService *corev1.Service,
	faucetAuthSecret *corev1.Secret,
	artifactsStatus *yacdv1alpha1.CardanoNetworkArtifactsStatus,
	clearFaucet bool,
	conditions ...metav1.Condition,
) error {
	original := network.DeepCopy()
	network.Status.ObservedGeneration = network.Generation
	if localnetFingerprint != "" {
		setLocalnetIdentityStatus(network, localnetFingerprint)
	}
	if nodeService != nil {
		setEndpointStatus(network, nodeService, ogmiosService, kupoService, faucetService)
		setFaucetStatus(network, faucetAuthSecret)
		setArtifactsStatus(network, artifactsStatus)
	} else if clearFaucet {
		clearFaucetStatus(network)
		clearArtifactsStatus(network)
	}
	ctrlstatus.SetObserved(&network.Status.Conditions, network.Generation, conditions...)

	return ctrlstatus.PatchIfChanged(ctx, r.Status(), network, original)
}

// setArtifactsStatus copies the verified artifact status payload onto the
// CardanoNetwork or clears it when nil. The payload is deep-copied so the
// caller can mutate the source freely.
func setArtifactsStatus(network *yacdv1alpha1.CardanoNetwork, artifacts *yacdv1alpha1.CardanoNetworkArtifactsStatus) {
	if artifacts == nil {
		network.Status.Artifacts = nil
		return
	}

	copied := *artifacts
	network.Status.Artifacts = &copied
}

// clearFaucetStatus removes the faucet endpoint and auth secret name from
// CardanoNetwork status. Used on the Degraded path to ensure the faucet
// status does not lag the live faucet revocation.
func clearFaucetStatus(network *yacdv1alpha1.CardanoNetwork) {
	if network.Status.Endpoints != nil {
		network.Status.Endpoints.Faucet = nil
	}
	network.Status.Faucet = nil
}

// clearArtifactsStatus removes the artifact verification payload from
// CardanoNetwork status. Used on the Degraded path.
func clearArtifactsStatus(network *yacdv1alpha1.CardanoNetwork) {
	network.Status.Artifacts = nil
}

// setLocalnetIdentityStatus stamps the accepted localnet identity onto
// CardanoNetwork status. Used as the acceptance gate for the
// validateAcceptedLocalnetFingerprint check on subsequent reconciles.
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

// setEndpointStatus publishes the in-cluster endpoint URLs for the primary
// node-to-node Service and any enabled chain API sidecars.
func setEndpointStatus(network *yacdv1alpha1.CardanoNetwork, nodeService *corev1.Service, ogmiosService *corev1.Service, kupoService *corev1.Service, faucetService *corev1.Service) {
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

	if faucetService == nil {
		network.Status.Endpoints.Faucet = nil
		return
	}

	network.Status.Endpoints.Faucet = &yacdv1alpha1.ServiceEndpointStatus{
		ServiceName: faucetService.Name,
		Port:        faucetService.Spec.Ports[0].Port,
		URL:         fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", faucetServiceURLType, faucetService.Name, faucetService.Namespace, faucetService.Spec.Ports[0].Port),
	}
}

// setFaucetStatus publishes the faucet auth Secret reference into
// CardanoNetwork status. The CLI consumes this to locate the token.
func setFaucetStatus(network *yacdv1alpha1.CardanoNetwork, faucetAuthSecret *corev1.Secret) {
	if faucetAuthSecret == nil {
		network.Status.Faucet = nil
		return
	}

	network.Status.Faucet = &yacdv1alpha1.FaucetStatus{
		AuthSecretName: faucetAuthSecret.Name,
	}
}
