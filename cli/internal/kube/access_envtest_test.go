package kube

import (
	"context"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPrimaryPodNameResolvesReadyPodFromPublishedService(t *testing.T) {
	ctx := context.Background()
	kubeClient, apiClient := newEnvtestClient(t)
	namespace := createNamespace(t, ctx, apiClient, "cli-podfinder")

	selector := map[string]string{"app.kubernetes.io/instance": "devnet"}
	publishNodeService(t, ctx, apiClient, kubeClient, namespace, "devnet", "devnet-node", selector)

	// A non-matching pod and a not-ready matching pod must both be ignored.
	createPod(t, ctx, apiClient, namespace, "unrelated", map[string]string{"app.kubernetes.io/instance": "other"}, true)
	createPod(t, ctx, apiClient, namespace, "devnet-node-starting", selector, false)
	createPod(t, ctx, apiClient, namespace, "devnet-node-abcde", selector, true)

	name, err := kubeClient.PrimaryPodName(ctx, namespace, "devnet")
	require.NoError(t, err)
	assert.Equal(t, "devnet-node-abcde", name)
}

func TestPrimaryPodNameErrorsWithoutPublishedService(t *testing.T) {
	ctx := context.Background()
	kubeClient, apiClient := newEnvtestClient(t)
	namespace := createNamespace(t, ctx, apiClient, "cli-podfinder-nosvc")

	require.NoError(t, kubeClient.ApplyCardanoNetwork(ctx, localCardanoNetwork(namespace, "devnet")))

	_, err := kubeClient.PrimaryPodName(ctx, namespace, "devnet")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not publish a primary node service")
}

func TestPrimaryPodNameErrorsWhenNoPodReady(t *testing.T) {
	ctx := context.Background()
	kubeClient, apiClient := newEnvtestClient(t)
	namespace := createNamespace(t, ctx, apiClient, "cli-podfinder-notready")

	selector := map[string]string{"app.kubernetes.io/instance": "devnet"}
	publishNodeService(t, ctx, apiClient, kubeClient, namespace, "devnet", "devnet-node", selector)
	createPod(t, ctx, apiClient, namespace, "devnet-node-starting", selector, false)

	_, err := kubeClient.PrimaryPodName(ctx, namespace, "devnet")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no ready primary pod")
}

// publishNodeService applies the network, then publishes a node-to-node Service
// in its status and creates the matching Service with the given selector, so
// PrimaryPodName has both the published contract and the live Service to read.
func publishNodeService(
	t *testing.T,
	ctx context.Context,
	apiClient crclient.Client,
	kubeClient *Adapter,
	namespace string,
	networkName string,
	serviceName string,
	selector map[string]string,
) {
	t.Helper()

	require.NoError(t, kubeClient.ApplyCardanoNetwork(ctx, localCardanoNetwork(namespace, networkName)))

	current := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, apiClient.Get(ctx, crclient.ObjectKey{Namespace: namespace, Name: networkName}, current))
	current.Status.Endpoints = &yacdv1alpha1.CardanoNetworkEndpointsStatus{
		NodeToNode: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: serviceName,
			Port:        3001,
		},
	}
	require.NoError(t, apiClient.Status().Update(ctx, current))

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: serviceName, Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports:    []corev1.ServicePort{{Name: "node-to-node", Port: 3001}},
		},
	}
	require.NoError(t, apiClient.Create(ctx, service))
}

// createPod creates a minimal Pod with the given labels and, when ready, stamps
// a True PodReady condition on its status (envtest has no kubelet to do so).
func createPod(
	t *testing.T,
	ctx context.Context,
	apiClient crclient.Client,
	namespace string,
	name string,
	labels map[string]string,
	ready bool,
) {
	t.Helper()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "cardano-node", Image: "cardano-node:test"}},
		},
	}
	require.NoError(t, apiClient.Create(ctx, pod))

	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}
	require.NoError(t, apiClient.Status().Update(ctx, pod))
}
