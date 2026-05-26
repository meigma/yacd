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

// sidecarReadinessConfig captures the per-sidecar variation consumed by
// primarySidecarReadyCondition. The node readiness path stays separate
// because it has no disabled branch and probes a PVC as well as a Service.
type sidecarReadinessConfig struct {
	// serviceName resolves the per-CardanoNetwork Service name.
	serviceName func(*yacdv1alpha1.CardanoNetwork) string
	// containerName names the sidecar's container in the primary Deployment.
	containerName string
	// condition constructs the typed component condition (e.g. ogmiosReadyCondition).
	condition primaryDeploymentConditionFunc
	// disabledReason and disabledMessage describe the no-cluster-read branch
	// taken when the sidecar is turned off by spec.
	disabledReason  conditionReason
	disabledMessage string
	// readyReason and readyMessage describe the success branch.
	readyReason  conditionReason
	readyMessage string
	// missingServiceMessage, unavailableMessage, and containerNotReadyMessage
	// flow into primaryDeploymentContainerBlockedCondition.
	missingServiceMessage    string
	unavailableMessage       string
	containerNotReadyMessage string
	// preReadinessCheck runs after the Service get and before the container
	// readiness probe. Non-nil only for the faucet, which must also verify
	// its uncached auth Secret carries a usable token.
	preReadinessCheck func(ctx context.Context, network *yacdv1alpha1.CardanoNetwork) (notReady *metav1.Condition, err error)
}

// primarySidecarReadyCondition is the shared body used by the three optional
// sidecars (ogmios, kupo, faucet). Each one customizes the variation through
// sidecarReadinessConfig; the orchestration (disabled short circuit, Service
// get, optional pre-readiness check, container readiness probe, blocked
// mapping, success condition) lives here once.
func (r *CardanoNetworkReconciler) primarySidecarReadyCondition(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	enabled bool,
	cfg sidecarReadinessConfig,
) (metav1.Condition, error) {
	if !enabled {
		return cfg.condition(metav1.ConditionFalse, cfg.disabledReason, cfg.disabledMessage), nil
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: cfg.serviceName(network)}, service); err != nil {
		if apierrors.IsNotFound(err) {
			return cfg.condition(metav1.ConditionFalse, conditionReasonPrimaryWorkloadMissing, cfg.missingServiceMessage), nil
		}
		return metav1.Condition{}, err
	}

	if cfg.preReadinessCheck != nil {
		notReady, err := cfg.preReadinessCheck(ctx, network)
		if err != nil {
			return metav1.Condition{}, err
		}
		if notReady != nil {
			return *notReady, nil
		}
	}

	readiness, err := r.primaryDeploymentContainerReadiness(ctx, network, cfg.containerName)
	if err != nil {
		return metav1.Condition{}, err
	}
	if blocked := primaryDeploymentContainerBlockedCondition(readiness, cfg.unavailableMessage, cfg.containerNotReadyMessage, cfg.condition); blocked != nil {
		return *blocked, nil
	}

	return cfg.condition(metav1.ConditionTrue, cfg.readyReason, cfg.readyMessage), nil
}

// primaryOgmiosReadyCondition computes the OgmiosReady condition. See
// primarySidecarReadyCondition for the shared shape.
func (r *CardanoNetworkReconciler) primaryOgmiosReadyCondition(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	enabled bool,
) (metav1.Condition, error) {
	return r.primarySidecarReadyCondition(ctx, network, enabled, sidecarReadinessConfig{
		serviceName:              primaryOgmiosServiceName,
		containerName:            ogmiosContainerName,
		condition:                ogmiosReadyCondition,
		disabledReason:           conditionReasonOgmiosDisabled,
		disabledMessage:          conditionMessageOgmiosDisabled,
		readyReason:              conditionReasonOgmiosReady,
		readyMessage:             conditionMessageOgmiosReady,
		missingServiceMessage:    "Ogmios Service is missing",
		unavailableMessage:       "Ogmios sidecar is not available",
		containerNotReadyMessage: "Ogmios sidecar is not ready",
	})
}

// primaryKupoReadyCondition computes the KupoReady condition. See
// primarySidecarReadyCondition for the shared shape.
func (r *CardanoNetworkReconciler) primaryKupoReadyCondition(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	enabled bool,
) (metav1.Condition, error) {
	return r.primarySidecarReadyCondition(ctx, network, enabled, sidecarReadinessConfig{
		serviceName:              primaryKupoServiceName,
		containerName:            kupoContainerName,
		condition:                kupoReadyCondition,
		disabledReason:           conditionReasonKupoDisabled,
		disabledMessage:          conditionMessageKupoDisabled,
		readyReason:              conditionReasonKupoReady,
		readyMessage:             conditionMessageKupoReady,
		missingServiceMessage:    "Kupo Service is missing",
		unavailableMessage:       "Kupo sidecar is not available",
		containerNotReadyMessage: "Kupo sidecar is not ready",
	})
}

// primaryFaucetReadyCondition computes the FaucetReady condition. The
// faucet's preReadinessCheck verifies the uncached auth Secret carries a
// usable token before reporting ready.
func (r *CardanoNetworkReconciler) primaryFaucetReadyCondition(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	enabled bool,
) (metav1.Condition, error) {
	return r.primarySidecarReadyCondition(ctx, network, enabled, sidecarReadinessConfig{
		serviceName:              primaryFaucetServiceName,
		containerName:            faucetContainerName,
		condition:                faucetReadyCondition,
		disabledReason:           conditionReasonFaucetDisabled,
		disabledMessage:          conditionMessageFaucetDisabled,
		readyReason:              conditionReasonFaucetReady,
		readyMessage:             conditionMessageFaucetReady,
		missingServiceMessage:    "Faucet Service is missing",
		unavailableMessage:       "Faucet sidecar is not available",
		containerNotReadyMessage: "Faucet sidecar is not ready",
		preReadinessCheck:        r.faucetAuthSecretReady,
	})
}

// faucetAuthSecretReady is the faucet's preReadinessCheck. It reads the
// uncached auth Secret and returns a non-ready FaucetReady condition when
// the Secret is missing or carries an invalid token; returns (nil, nil) when
// the Secret is healthy and the caller should proceed to the container
// readiness probe.
func (r *CardanoNetworkReconciler) faucetAuthSecretReady(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (*metav1.Condition, error) {
	// Secrets are not cached; liveReader bypasses the manager cache.
	secret := &corev1.Secret{}
	if err := r.liveReader().Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryFaucetAuthSecretName(network)}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			blocked := faucetReadyCondition(
				metav1.ConditionFalse,
				conditionReasonPrimaryWorkloadMissing,
				"Faucet auth Secret is missing",
			)
			return &blocked, nil
		}
		return nil, err
	}
	if !validFaucetAuthToken(string(secret.Data[faucetAuthTokenKey])) {
		blocked := faucetReadyCondition(
			metav1.ConditionFalse,
			conditionReasonDeploymentProgressing,
			"Faucet auth Secret token is not ready",
		)
		return &blocked, nil
	}

	return nil, nil
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
