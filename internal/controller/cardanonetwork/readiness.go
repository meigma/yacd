package cardanonetwork

import (
	"context"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlreadiness "github.com/meigma/yacd/internal/ctrlkit/readiness"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// primaryNodeReadyCondition computes the NodeReady condition. It probes the
// owned PVC, the node-to-node Service, and the cardano-node container's
// pod-side readiness state.
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

	readiness, err := r.primaryDeploymentContainerReadiness(ctx, network, cardanoNodeContainerName)
	if err != nil {
		return metav1.Condition{}, err
	}
	if blocked := primaryDeploymentContainerBlockedCondition(readiness, "Primary node Deployment is not available", "Primary node container is not ready", nodeReadyCondition); blocked != nil {
		return *blocked, nil
	}

	return nodeReadyCondition(
		metav1.ConditionTrue,
		conditionReasonNodeReady,
		conditionMessagePrimaryNodeReady,
	), nil
}

// primaryOgmiosReadyCondition computes the OgmiosReady condition. When
// ogmios is disabled by the spec, returns the canonical OgmiosDisabled
// condition without performing any cluster reads.
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

	readiness, err := r.primaryDeploymentContainerReadiness(ctx, network, ogmiosContainerName)
	if err != nil {
		return metav1.Condition{}, err
	}
	if blocked := primaryDeploymentContainerBlockedCondition(readiness, "Ogmios sidecar is not available", "Ogmios sidecar is not ready", ogmiosReadyCondition); blocked != nil {
		return *blocked, nil
	}

	return ogmiosReadyCondition(
		metav1.ConditionTrue,
		conditionReasonOgmiosReady,
		conditionMessageOgmiosReady,
	), nil
}

// primaryKupoReadyCondition computes the KupoReady condition. When kupo is
// disabled by the spec, returns KupoDisabled without performing any cluster
// reads.
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

	readiness, err := r.primaryDeploymentContainerReadiness(ctx, network, kupoContainerName)
	if err != nil {
		return metav1.Condition{}, err
	}
	if blocked := primaryDeploymentContainerBlockedCondition(readiness, "Kupo sidecar is not available", "Kupo sidecar is not ready", kupoReadyCondition); blocked != nil {
		return *blocked, nil
	}

	return kupoReadyCondition(
		metav1.ConditionTrue,
		conditionReasonKupoReady,
		conditionMessageKupoReady,
	), nil
}

// primaryFaucetReadyCondition computes the FaucetReady condition. When the
// faucet is disabled by the spec, returns FaucetDisabled without performing
// any cluster reads. When enabled, the live Secret (uncached) must also
// carry a valid token before the faucet is reported ready.
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

	// Secrets are not cached; liveReader bypasses the manager cache.
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

	readiness, err := r.primaryDeploymentContainerReadiness(ctx, network, faucetContainerName)
	if err != nil {
		return metav1.Condition{}, err
	}
	if blocked := primaryDeploymentContainerBlockedCondition(readiness, "Faucet sidecar is not available", "Faucet sidecar is not ready", faucetReadyCondition); blocked != nil {
		return *blocked, nil
	}

	return faucetReadyCondition(
		metav1.ConditionTrue,
		conditionReasonFaucetReady,
		conditionMessageFaucetReady,
	), nil
}

// primaryDeploymentContainerReadiness returns the readiness state for a
// named container on the primary Deployment. It reads the live Deployment
// through the controller cache and Pod list through liveReader to avoid
// stale container statuses.
func (r *CardanoNetworkReconciler) primaryDeploymentContainerReadiness(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	containerName string,
) (ctrlreadiness.DeploymentReadinessState, error) {
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrlreadiness.DeploymentMissing, nil
		}
		return "", err
	}

	pods := &corev1.PodList{}
	if err := r.liveReader().List(
		ctx,
		pods,
		client.InNamespace(network.Namespace),
		client.MatchingLabels(primaryWorkloadSelectorLabels(network)),
	); err != nil {
		return "", err
	}

	return ctrlreadiness.DeploymentReadiness(deployment, pods.Items, containerName), nil
}

// primaryDeploymentContainerBlockedCondition maps a non-ready readiness
// state to a component-specific Condition using the caller-supplied
// constructor. Returns nil when the Deployment is fully ready, signaling
// the caller to compute its own success condition.
func primaryDeploymentContainerBlockedCondition(
	readiness ctrlreadiness.DeploymentReadinessState,
	unavailableMessage string,
	containerNotReadyMessage string,
	condition primaryDeploymentConditionFunc,
) *metav1.Condition {
	switch readiness {
	case ctrlreadiness.DeploymentReady:
		return nil
	case ctrlreadiness.DeploymentMissing:
		blocked := condition(
			metav1.ConditionFalse,
			conditionReasonPrimaryWorkloadMissing,
			"Primary node Deployment is missing",
		)
		return &blocked
	case ctrlreadiness.DeploymentStale:
		blocked := condition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Primary node Deployment has not observed the latest generation",
		)
		return &blocked
	case ctrlreadiness.DeploymentUnavailable:
		blocked := condition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			unavailableMessage,
		)
		return &blocked
	default:
		blocked := condition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			containerNotReadyMessage,
		)
		return &blocked
	}
}

// liveReader is the uncached reader for status checks that must observe the
// freshest cluster state. When the Reconciler was constructed with a Reader
// (typical for envtest) we use it; otherwise we fall back to the cached
// Client. Status readers and faucet-auth reads must always go through this
// path so stale cache reads cannot stamp out a misleading status.
func (r *CardanoNetworkReconciler) liveReader() client.Reader {
	if r.Reader != nil {
		return r.Reader
	}

	return r.Client
}
