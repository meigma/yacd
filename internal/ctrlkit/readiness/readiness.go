package readiness

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// DeploymentContainerState classifies why a Deployment-backed container is or
// is not ready.
type DeploymentContainerState string

const (
	// DeploymentContainerReady reports that the Deployment is fresh, available,
	// and at least one selected Pod has the named container ready.
	DeploymentContainerReady DeploymentContainerState = "Ready"
	// DeploymentContainerMissing reports that the Deployment was not available
	// to evaluate.
	DeploymentContainerMissing DeploymentContainerState = "DeploymentMissing"
	// DeploymentContainerStale reports that the Deployment controller has not
	// observed the latest Deployment generation.
	DeploymentContainerStale DeploymentContainerState = "DeploymentStale"
	// DeploymentContainerUnavailable reports that the Deployment has not reached
	// its desired available replica count.
	DeploymentContainerUnavailable DeploymentContainerState = "DeploymentUnavailable"
	// DeploymentContainerNotReady reports that selected Pods exist, but none has
	// the named container ready and running.
	DeploymentContainerNotReady DeploymentContainerState = "ContainerNotReady"
)

// DeploymentContainerResult is the generic readiness state for a named
// container in a Deployment-managed Pod set.
type DeploymentContainerResult struct {
	State DeploymentContainerState
}

// Ready returns true when the Deployment and named container are ready.
func (r DeploymentContainerResult) Ready() bool {
	return r.State == DeploymentContainerReady
}

// DeploymentAvailable returns true when the Deployment has at least the desired
// number of updated, ready, and available replicas and reports Available=True.
func DeploymentAvailable(deployment *appsv1.Deployment) bool {
	if deployment == nil {
		return false
	}

	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}
	if desiredReplicas < 1 {
		return false
	}
	if deployment.Status.UpdatedReplicas < desiredReplicas ||
		deployment.Status.ReadyReplicas < desiredReplicas ||
		deployment.Status.AvailableReplicas < desiredReplicas {
		return false
	}

	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

// DeploymentContainerReadiness evaluates the shared readiness mechanics for a
// Deployment-backed container. Callers own object reads, selectors, condition
// messages, and status reason mapping.
func DeploymentContainerReadiness(
	deployment *appsv1.Deployment,
	pods []corev1.Pod,
	containerName string,
) DeploymentContainerResult {
	if deployment == nil {
		return DeploymentContainerResult{State: DeploymentContainerMissing}
	}
	if deployment.Status.ObservedGeneration != deployment.Generation {
		return DeploymentContainerResult{State: DeploymentContainerStale}
	}
	if !DeploymentAvailable(deployment) {
		return DeploymentContainerResult{State: DeploymentContainerUnavailable}
	}
	for i := range pods {
		if PodContainerReady(&pods[i], containerName) {
			return DeploymentContainerResult{State: DeploymentContainerReady}
		}
	}

	return DeploymentContainerResult{State: DeploymentContainerNotReady}
}

// PodContainerReady returns true only for a running, non-deleting Pod whose
// named container is Ready and currently running.
func PodContainerReady(pod *corev1.Pod, containerName string) bool {
	if pod == nil || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName && status.Ready && status.State.Running != nil {
			return true
		}
	}

	return false
}
