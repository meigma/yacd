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

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoNetwork{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(network), current); err != nil {
			return false
		}
		degraded := findCondition(current, conditionTypeDegraded)
		return degraded != nil &&
			degraded.Status == metav1.ConditionFalse &&
			current.Status.Endpoints != nil &&
			current.Status.Endpoints.NodeToNode != nil &&
			current.Status.Endpoints.NodeToNode.ServiceName == primaryWorkloadName(network) &&
			current.Status.Endpoints.NodeToNode.Port == network.Spec.Node.Port &&
			current.Status.Endpoints.NodeToNode.URL == "tcp://manager-owned-node.cardanonetwork-envtest.svc.cluster.local:3001" &&
			current.Status.Endpoints.Ogmios == nil
	}, 10*time.Second, 100*time.Millisecond)
}

func findCondition(network *yacdv1alpha1.CardanoNetwork, conditionType string) *metav1.Condition {
	return apimeta.FindStatusCondition(network.Status.Conditions, conditionType)
}
