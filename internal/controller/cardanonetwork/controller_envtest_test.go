package cardanonetwork

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestCardanoNetworkControllerManagerCreatesAndRecreatesPrimaryWorkload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "charts", "yacd", "crds")},
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.Eventually(t, func() bool {
			return testEnv.Stop() == nil
		}, time.Minute, time.Second)
	})

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	require.NoError(t, err)
	require.NoError(t, (&CardanoNetworkReconciler{
		Client: mgr.GetClient(),
		Reader: mgr.GetAPIReader(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr))

	errCh := make(chan error, 1)
	go func() {
		errCh <- mgr.Start(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-errCh)
	})
	require.Eventually(t, func() bool {
		return mgr.GetCache().WaitForCacheSync(ctx)
	}, 10*time.Second, 100*time.Millisecond)

	apiClient, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	namespace := &corev1.Namespace{}
	namespace.Name = "cardanonetwork-envtest"
	require.NoError(t, apiClient.Create(ctx, namespace))

	network := localCardanoNetwork("manager-owned")
	network.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, network))

	deploymentKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}
	require.Eventually(t, func() bool {
		return apiClient.Get(ctx, deploymentKey, &appsv1.Deployment{}) == nil
	}, 10*time.Second, 100*time.Millisecond)

	pvcKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryNodeStatePVCName(network)}
	require.Eventually(t, func() bool {
		return apiClient.Get(ctx, pvcKey, &corev1.PersistentVolumeClaim{}) == nil
	}, 10*time.Second, 100*time.Millisecond)

	serviceKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}
	require.Eventually(t, func() bool {
		return apiClient.Get(ctx, serviceKey, &corev1.Service{}) == nil
	}, 10*time.Second, 100*time.Millisecond)

	ogmiosServiceKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryOgmiosServiceName(network)}
	require.Eventually(t, func() bool {
		return apiClient.Get(ctx, ogmiosServiceKey, &corev1.Service{}) == nil
	}, 10*time.Second, 100*time.Millisecond)

	kupoServiceKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryKupoServiceName(network)}
	require.Eventually(t, func() bool {
		return apiClient.Get(ctx, kupoServiceKey, &corev1.Service{}) == nil
	}, 10*time.Second, 100*time.Millisecond)

	deployment := &appsv1.Deployment{}
	require.NoError(t, apiClient.Get(ctx, deploymentKey, deployment))
	originalUID := deployment.UID
	require.NoError(t, apiClient.Delete(ctx, deployment))

	require.Eventually(t, func() bool {
		got := &appsv1.Deployment{}
		err := apiClient.Get(ctx, deploymentKey, got)
		return err == nil && got.UID != originalUID
	}, 10*time.Second, 100*time.Millisecond)

	pvc := &corev1.PersistentVolumeClaim{}
	require.NoError(t, apiClient.Get(ctx, pvcKey, pvc))
	originalPVCUID := pvc.UID
	pvc.Finalizers = nil
	require.NoError(t, apiClient.Update(ctx, pvc))
	require.NoError(t, apiClient.Delete(ctx, pvc))

	require.Eventually(t, func() bool {
		got := &corev1.PersistentVolumeClaim{}
		err := apiClient.Get(ctx, pvcKey, got)
		return err == nil && got.UID != originalPVCUID
	}, 10*time.Second, 100*time.Millisecond)

	service := &corev1.Service{}
	require.NoError(t, apiClient.Get(ctx, serviceKey, service))
	originalServiceUID := service.UID
	require.NoError(t, apiClient.Delete(ctx, service))

	require.Eventually(t, func() bool {
		got := &corev1.Service{}
		err := apiClient.Get(ctx, serviceKey, got)
		return err == nil && got.UID != originalServiceUID
	}, 10*time.Second, 100*time.Millisecond)

	ogmiosService := &corev1.Service{}
	require.NoError(t, apiClient.Get(ctx, ogmiosServiceKey, ogmiosService))
	originalOgmiosServiceUID := ogmiosService.UID
	require.NoError(t, apiClient.Delete(ctx, ogmiosService))

	require.Eventually(t, func() bool {
		got := &corev1.Service{}
		err := apiClient.Get(ctx, ogmiosServiceKey, got)
		return err == nil && got.UID != originalOgmiosServiceUID
	}, 10*time.Second, 100*time.Millisecond)

	kupoService := &corev1.Service{}
	require.NoError(t, apiClient.Get(ctx, kupoServiceKey, kupoService))
	originalKupoServiceUID := kupoService.UID
	require.NoError(t, apiClient.Delete(ctx, kupoService))

	require.Eventually(t, func() bool {
		got := &corev1.Service{}
		err := apiClient.Get(ctx, kupoServiceKey, got)
		return err == nil && got.UID != originalKupoServiceUID
	}, 10*time.Second, 100*time.Millisecond)

	require.Eventually(t, func() bool {
		return statusHasProgressingEndpoints(ctx, apiClient, network)
	}, 10*time.Second, 100*time.Millisecond)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryWorkloadName(network) + "-pod",
			Namespace: network.Namespace,
			Labels:    primaryWorkloadSelectorLabels(network),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: cardanoNodeContainerName, Image: "example.com/cardano-node:test"},
				{Name: ogmiosContainerName, Image: "example.com/ogmios:test"},
				{Name: kupoContainerName, Image: "example.com/kupo:test"},
			},
		},
	}
	require.NoError(t, apiClient.Create(ctx, pod))
	pod.Status.Phase = corev1.PodRunning
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{
			Name:  cardanoNodeContainerName,
			Ready: true,
			State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.Now(),
				},
			},
		},
		{
			Name:  ogmiosContainerName,
			Ready: true,
			State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.Now(),
				},
			},
		},
		{
			Name:  kupoContainerName,
			Ready: true,
			State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.Now(),
				},
			},
		},
	}
	require.NoError(t, apiClient.Status().Update(ctx, pod))

	require.NoError(t, apiClient.Get(ctx, deploymentKey, deployment))
	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 1
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 1
	deployment.Status.AvailableReplicas = 1
	deployment.Status.Conditions = []appsv1.DeploymentCondition{
		{
			Type:               appsv1.DeploymentAvailable,
			Status:             corev1.ConditionTrue,
			Reason:             "MinimumReplicasAvailable",
			Message:            "Deployment has minimum availability.",
			LastUpdateTime:     metav1.Now(),
			LastTransitionTime: metav1.Now(),
		},
	}
	require.NoError(t, apiClient.Status().Update(ctx, deployment))

	require.Eventually(t, func() bool {
		return statusHasReadyConditions(ctx, apiClient, network)
	}, 10*time.Second, 100*time.Millisecond)
}

func findCondition(network *yacdv1alpha1.CardanoNetwork, conditionType string) *metav1.Condition {
	return apimeta.FindStatusCondition(network.Status.Conditions, conditionType)
}

func statusHasProgressingEndpoints(
	ctx context.Context,
	apiClient client.Client,
	network *yacdv1alpha1.CardanoNetwork,
) bool {
	current := &yacdv1alpha1.CardanoNetwork{}
	if err := apiClient.Get(ctx, client.ObjectKeyFromObject(network), current); err != nil {
		return false
	}

	return conditionHas(current, conditionTypeDegraded, metav1.ConditionFalse, "") &&
		conditionHas(current, conditionTypeProgressing, metav1.ConditionTrue, "") &&
		conditionHas(current, conditionTypeReady, metav1.ConditionFalse, "") &&
		conditionHas(current, conditionTypeNodeReady, metav1.ConditionFalse, "") &&
		conditionHas(current, conditionTypeOgmiosReady, metav1.ConditionFalse, "") &&
		conditionHas(current, conditionTypeKupoReady, metav1.ConditionFalse, "") &&
		nodeToNodeEndpointMatches(current, network) &&
		ogmiosEndpointMatches(current, network) &&
		kupoEndpointMatches(current, network)
}

func statusHasReadyConditions(
	ctx context.Context,
	apiClient client.Client,
	network *yacdv1alpha1.CardanoNetwork,
) bool {
	current := &yacdv1alpha1.CardanoNetwork{}
	if err := apiClient.Get(ctx, client.ObjectKeyFromObject(network), current); err != nil {
		return false
	}

	return conditionHas(current, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonReady) &&
		conditionHas(current, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady) &&
		conditionHas(current, conditionTypeNodeReady, metav1.ConditionTrue, conditionReasonNodeReady) &&
		conditionHas(current, conditionTypeOgmiosReady, metav1.ConditionTrue, conditionReasonOgmiosReady) &&
		conditionHas(current, conditionTypeKupoReady, metav1.ConditionTrue, conditionReasonKupoReady)
}

func conditionHas(
	network *yacdv1alpha1.CardanoNetwork,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
) bool {
	condition := findCondition(network, conditionType)
	if condition == nil || condition.Status != status {
		return false
	}

	return reason == "" || condition.Reason == reason
}

func nodeToNodeEndpointMatches(current *yacdv1alpha1.CardanoNetwork, network *yacdv1alpha1.CardanoNetwork) bool {
	if current.Status.Endpoints == nil || current.Status.Endpoints.NodeToNode == nil {
		return false
	}

	return current.Status.Endpoints.NodeToNode.ServiceName == primaryWorkloadName(network) &&
		current.Status.Endpoints.NodeToNode.Port == network.Spec.Node.Port &&
		current.Status.Endpoints.NodeToNode.URL == "tcp://manager-owned-node.cardanonetwork-envtest.svc.cluster.local:3001"
}

func ogmiosEndpointMatches(current *yacdv1alpha1.CardanoNetwork, network *yacdv1alpha1.CardanoNetwork) bool {
	if current.Status.Endpoints == nil || current.Status.Endpoints.Ogmios == nil {
		return false
	}

	return current.Status.Endpoints.Ogmios.ServiceName == primaryOgmiosServiceName(network) &&
		current.Status.Endpoints.Ogmios.Port == defaultOgmiosPort &&
		current.Status.Endpoints.Ogmios.URL == "ws://manager-owned-ogmios.cardanonetwork-envtest.svc.cluster.local:1337"
}

func kupoEndpointMatches(current *yacdv1alpha1.CardanoNetwork, network *yacdv1alpha1.CardanoNetwork) bool {
	if current.Status.Endpoints == nil || current.Status.Endpoints.Kupo == nil {
		return false
	}

	return current.Status.Endpoints.Kupo.ServiceName == primaryKupoServiceName(network) &&
		current.Status.Endpoints.Kupo.Port == defaultKupoPort &&
		current.Status.Endpoints.Kupo.URL == "http://manager-owned-kupo.cardanonetwork-envtest.svc.cluster.local:1442"
}
