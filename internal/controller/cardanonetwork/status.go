package cardanonetwork

import (
	"context"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlconditions "github.com/meigma/yacd/internal/ctrlkit/conditions"
	ctrlreadiness "github.com/meigma/yacd/internal/ctrlkit/readiness"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	conditionTypeProgressing    = "Progressing"
	conditionTypeDegraded       = "Degraded"
	conditionTypeReady          = "Ready"
	conditionTypeNodeReady      = "NodeReady"
	conditionTypeOgmiosReady    = "OgmiosReady"
	conditionTypeKupoReady      = "KupoReady"
	conditionTypeFaucetReady    = "FaucetReady"
	conditionTypeArtifactsReady = "ArtifactsReady"

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
	conditionReasonKupoReady                   = "KupoReady"
	conditionReasonKupoDisabled                = "KupoDisabled"
	conditionReasonFaucetReady                 = "FaucetReady"
	conditionReasonFaucetDisabled              = "FaucetDisabled"
	conditionReasonArtifactsReady              = "ArtifactsReady"
	conditionReasonArtifactsPending            = "ArtifactsPending"
	conditionReasonPrimaryWorkloadMissing      = "PrimaryWorkloadMissing"
	conditionMessagePrimaryWorkloadApplied     = "Primary node, artifact publisher, and chain API resources are applied"
	conditionMessagePrimaryWorkloadUnsupported = "Primary node workload is not supported for this CardanoNetwork spec"
	conditionMessageReady                      = "CardanoNetwork is usable through its published endpoints"
	conditionMessagePrimaryNodeReady           = "Primary node container is ready"
	conditionMessageOgmiosReady                = "Ogmios sidecar is connected and available through its Service"
	conditionMessageOgmiosDisabled             = "Ogmios chain API is disabled"
	conditionMessageKupoReady                  = "Kupo sidecar is available through its Service"
	conditionMessageKupoDisabled               = "Kupo chain index API is disabled"
	conditionMessageFaucetReady                = "Faucet sidecar is available through its Service"
	conditionMessageFaucetDisabled             = "Faucet API is disabled"
	conditionMessageArtifactsReady             = "Network artifact ConfigMap is published and verified"
)

func (r *CardanoNetworkReconciler) patchStatusConditionsClearingFaucet(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	conditions ...metav1.Condition,
) error {
	return r.patchPrimaryWorkloadStatus(ctx, network, "", nil, nil, nil, nil, nil, nil, true, conditions...)
}

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
) (metav1.Condition, error) {
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
	artifactResult := artifactConfigMapStatus(networkArtifactsConfigMap, localnetFingerprint)
	var artifactsStatus *yacdv1alpha1.CardanoNetworkArtifactsStatus
	artifactsReady := artifactsReadyCondition(
		metav1.ConditionFalse,
		conditionReasonArtifactsPending,
		artifactResult.reason,
	)
	if artifactResult.ready {
		artifactsStatus = &artifactResult.status
		artifactsReady = artifactsReadyCondition(
			metav1.ConditionTrue,
			conditionReasonArtifactsReady,
			conditionMessageArtifactsReady,
		)
	}
	ready := readyCondition(nodeReady, ogmiosReady, kupoReady, faucetReady, artifactsReady, kupoService != nil, faucetService != nil)

	if err := r.patchPrimaryWorkloadStatus(ctx, network, localnetFingerprint, nodeService, ogmiosService, kupoService, faucetService, faucetAuthSecret, artifactsStatus, false,
		degradedCondition(metav1.ConditionFalse, conditionReasonReconcileSucceeded, conditionMessagePrimaryWorkloadApplied),
		progressingForReadyCondition(ready),
		ready,
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
	ctrlconditions.SetObserved(&network.Status.Conditions, network.Generation, conditions...)

	if equality.Semantic.DeepEqual(original.Status, network.Status) {
		return nil
	}

	return r.Status().Patch(ctx, network, client.MergeFrom(original))
}

func setArtifactsStatus(network *yacdv1alpha1.CardanoNetwork, artifacts *yacdv1alpha1.CardanoNetworkArtifactsStatus) {
	if artifacts == nil {
		network.Status.Artifacts = nil
		return
	}

	copied := *artifacts
	network.Status.Artifacts = &copied
}

func clearFaucetStatus(network *yacdv1alpha1.CardanoNetwork) {
	if network.Status.Endpoints != nil {
		network.Status.Endpoints.Faucet = nil
	}
	network.Status.Faucet = nil
}

func clearArtifactsStatus(network *yacdv1alpha1.CardanoNetwork) {
	network.Status.Artifacts = nil
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

func setFaucetStatus(network *yacdv1alpha1.CardanoNetwork, faucetAuthSecret *corev1.Secret) {
	if faucetAuthSecret == nil {
		network.Status.Faucet = nil
		return
	}

	network.Status.Faucet = &yacdv1alpha1.FaucetStatus{
		AuthSecretName: faucetAuthSecret.Name,
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

func (r *CardanoNetworkReconciler) primaryKupoReadyCondition(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	enabled bool,
) (metav1.Condition, error) {
	if !enabled {
		return kupoReadyCondition(
			metav1.ConditionFalse,
			conditionReasonKupoDisabled,
			conditionMessageKupoDisabled,
		), nil
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryKupoServiceName(network)}, service); err != nil {
		if apierrors.IsNotFound(err) {
			return kupoReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Kupo Service is missing",
			), nil
		}
		return metav1.Condition{}, err
	}

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return kupoReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Primary node Deployment is missing",
			), nil
		}
		return metav1.Condition{}, err
	}
	if deployment.Status.ObservedGeneration != deployment.Generation {
		return kupoReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Primary node Deployment has not observed the latest generation",
		), nil
	}
	if deployment.Status.UpdatedReplicas < 1 {
		return kupoReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Kupo sidecar is not available",
		), nil
	}

	containerReady, err := r.primaryPodContainerReady(ctx, network, kupoContainerName)
	if err != nil {
		return metav1.Condition{}, err
	}
	if !containerReady {
		return kupoReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Kupo sidecar is not ready",
		), nil
	}

	return kupoReadyCondition(
		metav1.ConditionTrue,
		conditionReasonKupoReady,
		conditionMessageKupoReady,
	), nil
}

func (r *CardanoNetworkReconciler) primaryFaucetReadyCondition(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	enabled bool,
) (metav1.Condition, error) {
	if !enabled {
		return faucetReadyCondition(
			metav1.ConditionFalse,
			conditionReasonFaucetDisabled,
			conditionMessageFaucetDisabled,
		), nil
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryFaucetServiceName(network)}, service); err != nil {
		if apierrors.IsNotFound(err) {
			return faucetReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Faucet Service is missing",
			), nil
		}
		return metav1.Condition{}, err
	}

	secret := &corev1.Secret{}
	if err := r.liveReader().Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryFaucetAuthSecretName(network)}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return faucetReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Faucet auth Secret is missing",
			), nil
		}
		return metav1.Condition{}, err
	}
	if !validFaucetAuthToken(string(secret.Data[faucetAuthTokenKey])) {
		return faucetReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Faucet auth Secret token is not ready",
		), nil
	}

	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return faucetReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Primary node Deployment is missing",
			), nil
		}
		return metav1.Condition{}, err
	}
	if deployment.Status.ObservedGeneration != deployment.Generation {
		return faucetReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Primary node Deployment has not observed the latest generation",
		), nil
	}
	if deployment.Status.UpdatedReplicas < 1 {
		return faucetReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Faucet sidecar is not available",
		), nil
	}

	containerReady, err := r.primaryPodContainerReady(ctx, network, faucetContainerName)
	if err != nil {
		return metav1.Condition{}, err
	}
	if !containerReady {
		return faucetReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Faucet sidecar is not ready",
		), nil
	}

	return faucetReadyCondition(
		metav1.ConditionTrue,
		conditionReasonFaucetReady,
		conditionMessageFaucetReady,
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
		if ctrlreadiness.PodContainerReady(&pods.Items[i], containerName) {
			return true, nil
		}
	}

	return false, nil
}

func (r *CardanoNetworkReconciler) statusReader() client.Reader {
	return r.liveReader()
}

func (r *CardanoNetworkReconciler) liveReader() client.Reader {
	if r.Reader != nil {
		return r.Reader
	}

	return r.Client
}

func readyCondition(nodeReady metav1.Condition, ogmiosReady metav1.Condition, kupoReady metav1.Condition, faucetReady metav1.Condition, artifactsReady metav1.Condition, kupoEnabled bool, faucetEnabled bool) metav1.Condition {
	if nodeReady.Status == metav1.ConditionTrue &&
		ogmiosReady.Status == metav1.ConditionTrue &&
		(!kupoEnabled || kupoReady.Status == metav1.ConditionTrue) &&
		(!faucetEnabled || faucetReady.Status == metav1.ConditionTrue) &&
		artifactsReady.Status == metav1.ConditionTrue {
		return ctrlconditions.Condition(conditionTypeReady, metav1.ConditionTrue, conditionReasonReady, conditionMessageReady)
	}
	if nodeReady.Status != metav1.ConditionTrue {
		return ctrlconditions.Condition(conditionTypeReady, metav1.ConditionFalse, nodeReady.Reason, nodeReady.Message)
	}
	if ogmiosReady.Status != metav1.ConditionTrue {
		return ctrlconditions.Condition(conditionTypeReady, metav1.ConditionFalse, ogmiosReady.Reason, ogmiosReady.Message)
	}
	if kupoEnabled && kupoReady.Status != metav1.ConditionTrue {
		return ctrlconditions.Condition(conditionTypeReady, metav1.ConditionFalse, kupoReady.Reason, kupoReady.Message)
	}
	if faucetEnabled && faucetReady.Status != metav1.ConditionTrue {
		return ctrlconditions.Condition(conditionTypeReady, metav1.ConditionFalse, faucetReady.Reason, faucetReady.Message)
	}
	if artifactsReady.Status != metav1.ConditionTrue {
		return ctrlconditions.Condition(conditionTypeReady, metav1.ConditionFalse, artifactsReady.Reason, artifactsReady.Message)
	}

	return ctrlconditions.Condition(conditionTypeReady, metav1.ConditionTrue, conditionReasonReady, conditionMessageReady)
}

func degradedCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlconditions.Condition(conditionTypeDegraded, status, reason, message)
}

func nodeReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlconditions.Condition(conditionTypeNodeReady, status, reason, message)
}

func ogmiosReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlconditions.Condition(conditionTypeOgmiosReady, status, reason, message)
}

func kupoReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlconditions.Condition(conditionTypeKupoReady, status, reason, message)
}

func faucetReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlconditions.Condition(conditionTypeFaucetReady, status, reason, message)
}

func artifactsReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlconditions.Condition(conditionTypeArtifactsReady, status, reason, message)
}

func progressingCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlconditions.Condition(conditionTypeProgressing, status, reason, message)
}

func progressingForReadyCondition(ready metav1.Condition) metav1.Condition {
	if ready.Status == metav1.ConditionTrue {
		return progressingCondition(metav1.ConditionFalse, conditionReasonReady, conditionMessageReady)
	}
	if ready.Reason == conditionReasonDeploymentProgressing ||
		ready.Reason == conditionReasonPrimaryWorkloadMissing ||
		ready.Reason == conditionReasonArtifactsPending {
		return progressingCondition(metav1.ConditionTrue, ready.Reason, ready.Message)
	}

	return progressingCondition(metav1.ConditionFalse, ready.Reason, ready.Message)
}
