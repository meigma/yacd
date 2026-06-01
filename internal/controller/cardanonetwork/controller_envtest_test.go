package cardanonetwork

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrldbsync "github.com/meigma/yacd/internal/controller/cardanodbsync"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
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

	skipNameValidation := true
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Controller:             config.Controller{SkipNameValidation: &skipNameValidation},
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	require.NoError(t, err)
	envtestNow := time.Date(2026, 5, 28, 18, 0, 0, 0, time.UTC)
	require.NoError(t, (&CardanoNetworkReconciler{
		Client:             mgr.GetClient(),
		Reader:             mgr.GetAPIReader(),
		Scheme:             mgr.GetScheme(),
		Now:                func() time.Time { return envtestNow },
		syncProberOverride: syncedNodeSyncProber(),
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
	enableFaucet(network)
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

	faucetServiceKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryFaucetServiceName(network)}
	require.Eventually(t, func() bool {
		return apiClient.Get(ctx, faucetServiceKey, &corev1.Service{}) == nil
	}, 10*time.Second, 100*time.Millisecond)

	artifactsServiceKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryArtifactsServiceName(network)}
	require.Eventually(t, func() bool {
		return apiClient.Get(ctx, artifactsServiceKey, &corev1.Service{}) == nil
	}, 10*time.Second, 100*time.Millisecond)

	faucetAuthSecretKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryFaucetAuthSecretName(network)}
	require.Eventually(t, func() bool {
		secret := &corev1.Secret{}
		return apiClient.Get(ctx, faucetAuthSecretKey, secret) == nil &&
			validFaucetAuthToken(string(secret.Data[faucetAuthTokenKey]))
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

	faucetService := &corev1.Service{}
	require.NoError(t, apiClient.Get(ctx, faucetServiceKey, faucetService))
	originalFaucetServiceUID := faucetService.UID
	require.NoError(t, apiClient.Delete(ctx, faucetService))

	require.Eventually(t, func() bool {
		got := &corev1.Service{}
		err := apiClient.Get(ctx, faucetServiceKey, got)
		return err == nil && got.UID != originalFaucetServiceUID
	}, 10*time.Second, 100*time.Millisecond)

	artifactsService := &corev1.Service{}
	require.NoError(t, apiClient.Get(ctx, artifactsServiceKey, artifactsService))
	originalArtifactsServiceUID := artifactsService.UID
	require.NoError(t, apiClient.Delete(ctx, artifactsService))

	require.Eventually(t, func() bool {
		got := &corev1.Service{}
		err := apiClient.Get(ctx, artifactsServiceKey, got)
		return err == nil && got.UID != originalArtifactsServiceUID
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
				{Name: faucetContainerName, Image: "example.com/faucet:test"},
				{Name: serveContainerName, Image: "example.com/serve:test"},
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
		{
			Name:  faucetContainerName,
			Ready: true,
			State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.Now(),
				},
			},
		},
		{
			Name:  serveContainerName,
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

	recoverDeletedFaucetAuthSecret(t, ctx, apiClient, network, faucetAuthSecretKey, deploymentKey)

	forgedNetwork := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(network), forgedNetwork))
	require.NotNil(t, forgedNetwork.Status.Network)
	baselineNetworkFingerprint := forgedNetwork.Status.Network.NetworkFingerprint
	baselineLocalnetFingerprint := forgedNetwork.Status.Network.LocalnetFingerprint
	require.NotEmpty(t, baselineNetworkFingerprint)
	require.NotEmpty(t, baselineLocalnetFingerprint)
	forgedNetwork.Status.Network.NetworkFingerprint = "deadbeef-forged-network"
	forgedNetwork.Status.Network.LocalnetFingerprint = forgedLocalnetFingerprint
	require.NoError(t, apiClient.Status().Update(ctx, forgedNetwork))

	require.Eventually(t, func() bool {
		repaired := &yacdv1alpha1.CardanoNetwork{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(network), repaired); err != nil {
			return false
		}
		return repaired.Status.Network != nil &&
			repaired.Status.Network.NetworkFingerprint == baselineNetworkFingerprint &&
			repaired.Status.Network.LocalnetFingerprint == baselineLocalnetFingerprint &&
			conditionHas(repaired, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	}, 10*time.Second, 100*time.Millisecond)

	current := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(network), current))
	current.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Faucet: &yacdv1alpha1.FaucetSpec{
			Enabled:          false,
			Port:             defaultFaucetPort,
			DefaultSource:    defaultFaucetSource,
			MinTopUpLovelace: defaultFaucetMinLovelace,
			MaxTopUpLovelace: defaultFaucetMaxLovelace,
		},
	}
	require.NoError(t, apiClient.Update(ctx, current))

	require.Eventually(t, func() bool {
		err := apiClient.Get(ctx, faucetServiceKey, &corev1.Service{})
		return apierrors.IsNotFound(err)
	}, 10*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		err := apiClient.Get(ctx, faucetAuthSecretKey, &corev1.Secret{})
		return apierrors.IsNotFound(err)
	}, 10*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		got := &appsv1.Deployment{}
		if err := apiClient.Get(ctx, deploymentKey, got); err != nil {
			return false
		}
		// node + ogmios + kupo + the always-on serve sidecar (faucet disabled).
		return len(got.Spec.Template.Spec.Containers) == 4
	}, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, apiClient.Get(ctx, deploymentKey, deployment))
	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 1
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 1
	deployment.Status.AvailableReplicas = 1
	require.NoError(t, apiClient.Status().Update(ctx, deployment))

	require.Eventually(t, func() bool {
		return statusHasDisabledFaucetReadyConditions(ctx, apiClient, network)
	}, 10*time.Second, 100*time.Millisecond)
}

func TestCardanoNetworkControllerManagerDegradesOnPrimaryPVCDeletion(t *testing.T) {
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
		Client:             mgr.GetClient(),
		Reader:             mgr.GetAPIReader(),
		Scheme:             mgr.GetScheme(),
		syncProberOverride: syncedNodeSyncProber(),
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
	namespace.Name = "cardanonetwork-pvc-deletion"
	require.NoError(t, apiClient.Create(ctx, namespace))

	network := localCardanoNetwork("state-loss")
	network.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, network))

	pvcKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryNodeStatePVCName(network)}
	deploymentKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}
	require.Eventually(t, func() bool {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := apiClient.Get(ctx, pvcKey, pvc); err != nil {
			return false
		}
		deployment := &appsv1.Deployment{}
		if err := apiClient.Get(ctx, deploymentKey, deployment); err != nil {
			return false
		}

		return pvc.Annotations[localnetFingerprintAnno] != "" &&
			deployment.Spec.Template.Annotations[localnetFingerprintAnno] != ""
	}, 10*time.Second, 100*time.Millisecond)

	pvc := &corev1.PersistentVolumeClaim{}
	require.NoError(t, apiClient.Get(ctx, pvcKey, pvc))
	originalPVCUID := pvc.UID
	pvc.Finalizers = []string{"test.example.io/never-removed"}
	require.NoError(t, apiClient.Update(ctx, pvc))
	require.NoError(t, apiClient.Delete(ctx, pvc))

	require.Eventually(t, func() bool {
		gotPVC := &corev1.PersistentVolumeClaim{}
		if err := apiClient.Get(ctx, pvcKey, gotPVC); err != nil {
			return false
		}
		current := &yacdv1alpha1.CardanoNetwork{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(network), current); err != nil {
			return false
		}
		degraded := findCondition(current, conditionTypeDegraded)

		return gotPVC.UID == originalPVCUID &&
			!gotPVC.DeletionTimestamp.IsZero() &&
			degraded != nil &&
			degraded.Status == metav1.ConditionTrue &&
			degraded.Reason == string(conditionReasonChildBeingDeleted) &&
			strings.Contains(degraded.Message, pvcKey.Name) &&
			strings.Contains(degraded.Message, "test.example.io/never-removed")
	}, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, apiClient.Get(ctx, pvcKey, pvc))
	pvc.Finalizers = nil
	require.NoError(t, apiClient.Update(ctx, pvc))

	require.Eventually(t, func() bool {
		err := apiClient.Get(ctx, pvcKey, &corev1.PersistentVolumeClaim{})
		return apierrors.IsNotFound(err)
	}, 10*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoNetwork{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(network), current); err != nil {
			return false
		}
		return conditionHas(current, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonPrimaryStateLost) &&
			conditionHas(current, conditionTypeReady, metav1.ConditionFalse, conditionReasonPrimaryStateLost) &&
			conditionHas(current, conditionTypeNodeReady, metav1.ConditionFalse, conditionReasonPrimaryStateLost)
	}, 10*time.Second, 100*time.Millisecond)

	err = apiClient.Get(ctx, pvcKey, &corev1.PersistentVolumeClaim{})
	require.True(t, apierrors.IsNotFound(err), "expected primary state PVC to remain absent, got %v", err)
}

func TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync(t *testing.T) {
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

	skipNameValidation := true
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Controller:             config.Controller{SkipNameValidation: &skipNameValidation},
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	require.NoError(t, err)
	require.NoError(t, (&CardanoNetworkReconciler{
		Client:             mgr.GetClient(),
		Reader:             mgr.GetAPIReader(),
		Scheme:             mgr.GetScheme(),
		syncProberOverride: syncedNodeSyncProber(),
	}).SetupWithManager(mgr))
	require.NoError(t, (&ctrldbsync.CardanoDBSyncReconciler{
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
	}, time.Minute, 100*time.Millisecond)

	apiClient, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cardanonetwork-dbsync-envtest"}}
	require.NoError(t, apiClient.Create(ctx, namespace))

	network := localCardanoNetwork("sidecar-network")
	network.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, network))

	deploymentKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}
	require.Eventually(t, func() bool {
		return apiClient.Get(ctx, deploymentKey, &appsv1.Deployment{}) == nil
	}, time.Minute, 100*time.Millisecond)

	// The cardanonetwork controller publishes the network identity, the serve
	// endpoint, and ArtifactsReady from the live primary workload, so mark the
	// primary Deployment available with a ready serve sidecar Pod. The db-sync
	// sidecar then sources artifacts over HTTP from the published serve endpoint
	// rather than a network-artifacts ConfigMap.
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
				{Name: serveContainerName, Image: "example.com/serve:test"},
			},
		},
	}
	require.NoError(t, apiClient.Create(ctx, pod))
	pod.Status.Phase = corev1.PodRunning
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{
		{Name: cardanoNodeContainerName, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}},
		{Name: ogmiosContainerName, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}},
		{Name: kupoContainerName, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}},
		{Name: serveContainerName, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}},
	}
	require.NoError(t, apiClient.Status().Update(ctx, pod))

	// The primary Deployment's generation bumps each time the db-sync sidecar
	// attaches, which would flip the serve sidecar's readiness (and ArtifactsReady)
	// to progressing. markAvailable re-stamps the Deployment status observed and
	// available so ArtifactsReady self-heals; call it whenever the network must
	// observe a fresh ArtifactsReady=True.
	markAvailable := func() {
		require.Eventually(t, func() bool {
			current := &appsv1.Deployment{}
			if err := apiClient.Get(ctx, deploymentKey, current); err != nil {
				return false
			}
			current.Status.ObservedGeneration = current.Generation
			current.Status.Replicas = 1
			current.Status.UpdatedReplicas = 1
			current.Status.ReadyReplicas = 1
			current.Status.AvailableReplicas = 1
			current.Status.Conditions = []appsv1.DeploymentCondition{{
				Type:               appsv1.DeploymentAvailable,
				Status:             corev1.ConditionTrue,
				Reason:             "MinimumReplicasAvailable",
				Message:            "Deployment has minimum availability.",
				LastUpdateTime:     metav1.Now(),
				LastTransitionTime: metav1.Now(),
			}}
			return apiClient.Status().Update(ctx, current) == nil
		}, 10*time.Second, 100*time.Millisecond)
	}

	awaitArtifactsReady := func() {
		require.Eventually(t, func() bool {
			markAvailable()
			current := &yacdv1alpha1.CardanoNetwork{}
			if err := apiClient.Get(ctx, client.ObjectKeyFromObject(network), current); err != nil {
				return false
			}
			return current.Status.Endpoints != nil &&
				current.Status.Endpoints.Artifacts != nil &&
				conditionHas(current, conditionTypeArtifactsReady, metav1.ConditionTrue, conditionReasonArtifactsReady)
		}, time.Minute, 200*time.Millisecond)
	}

	awaitArtifactsReady()

	first := readyPrimarySidecarDBSync("first", network)
	first.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, primarySidecarExternalSecret(first)))
	require.NoError(t, apiClient.Create(ctx, first))

	require.Eventually(t, func() bool {
		markAvailable()
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(first), current); err != nil {
			return false
		}
		sidecarMaterialReady := apimeta.FindStatusCondition(current.Status.Conditions, "SidecarMaterialReady")
		return current.Status.ObservedGeneration == current.Generation &&
			current.Status.Placement != nil &&
			current.Status.Placement.PrimarySidecar != nil &&
			sidecarMaterialReady != nil &&
			sidecarMaterialReady.Status == metav1.ConditionTrue
	}, time.Minute, 200*time.Millisecond)

	requireDeploymentContainerEventually(t, ctx, apiClient, deploymentKey, "cardano-db-sync", true)
	currentFirst := &yacdv1alpha1.CardanoDBSync{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(first), currentFirst))
	requireDeploymentDBSyncSidecarRevisionEventually(t, ctx, apiClient, deploymentKey, currentFirst.Status.Placement.PrimarySidecar.Revision)

	currentFirst.Status.Placement.PrimarySidecar.Revision = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	require.NoError(t, apiClient.Status().Update(ctx, currentFirst))
	requireDeploymentDBSyncSidecarRevisionEventually(t, ctx, apiClient, deploymentKey, "sha256:2222222222222222222222222222222222222222222222222222222222222222")

	require.Eventually(t, func() bool {
		err := apiClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: "first-dbsync"}, &appsv1.Deployment{})
		return apierrors.IsNotFound(err)
	}, time.Minute, 100*time.Millisecond)

	second := readyPrimarySidecarDBSync("second", network)
	second.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, primarySidecarExternalSecret(second)))
	require.NoError(t, apiClient.Create(ctx, second))

	requireDeploymentContainerEventually(t, ctx, apiClient, deploymentKey, "cardano-db-sync", true)
	requireDeploymentDBSyncSidecarRevisionEventually(t, ctx, apiClient, deploymentKey, "sha256:2222222222222222222222222222222222222222222222222222222222222222")

	require.NoError(t, apiClient.Delete(ctx, currentFirst))
	require.Eventually(t, func() bool {
		markAvailable()
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(second), current); err != nil {
			return false
		}
		sidecarMaterialReady := apimeta.FindStatusCondition(current.Status.Conditions, "SidecarMaterialReady")
		return current.Status.ObservedGeneration == current.Generation &&
			current.Status.Placement != nil &&
			current.Status.Placement.PrimarySidecar != nil &&
			sidecarMaterialReady != nil &&
			sidecarMaterialReady.Status == metav1.ConditionTrue
	}, time.Minute, 100*time.Millisecond)
	requireDeploymentContainerEventually(t, ctx, apiClient, deploymentKey, "cardano-db-sync", true)
	currentSecond := &yacdv1alpha1.CardanoDBSync{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(second), currentSecond))
	requireDeploymentDBSyncSidecarRevisionEventually(t, ctx, apiClient, deploymentKey, currentSecond.Status.Placement.PrimarySidecar.Revision)
}

func primarySidecarExternalSecret(dbSync *yacdv1alpha1.CardanoDBSync) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSync.Spec.Database.External.PasswordSecretRef.Name,
			Namespace: dbSync.Namespace,
		},
		Data: map[string][]byte{
			dbSync.Spec.Database.External.PasswordSecretRef.Key: []byte("secret"),
		},
	}
}

func requireDeploymentContainerEventually(
	t *testing.T,
	ctx context.Context,
	apiClient client.Client,
	deploymentKey client.ObjectKey,
	containerName string,
	wantPresent bool,
) {
	t.Helper()

	require.Eventually(t, func() bool {
		deployment := &appsv1.Deployment{}
		if err := apiClient.Get(ctx, deploymentKey, deployment); err != nil {
			return false
		}
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.Name == containerName {
				return wantPresent
			}
		}
		return !wantPresent
	}, time.Minute, 100*time.Millisecond)
}

func requireDeploymentDBSyncSidecarRevisionEventually(
	t *testing.T,
	ctx context.Context,
	apiClient client.Client,
	deploymentKey client.ObjectKey,
	value string,
) {
	t.Helper()

	require.Eventually(t, func() bool {
		deployment := &appsv1.Deployment{}
		if err := apiClient.Get(ctx, deploymentKey, deployment); err != nil {
			return false
		}

		return deployment.Spec.Template.Annotations[dbSyncSidecarRevisionAnno] == value
	}, time.Minute, 100*time.Millisecond)
}

func findCondition(network *yacdv1alpha1.CardanoNetwork, ct conditionType) *metav1.Condition {
	return apimeta.FindStatusCondition(network.Status.Conditions, string(ct))
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
		conditionHas(current, conditionTypeFaucetReady, metav1.ConditionFalse, "") &&
		conditionHas(current, conditionTypeArtifactsReady, metav1.ConditionFalse, "") &&
		nodeToNodeEndpointMatches(current, network) &&
		ogmiosEndpointMatches(current, network) &&
		kupoEndpointMatches(current, network) &&
		faucetEndpointMatches(current, network) &&
		artifactsEndpointMatches(current, network) &&
		faucetStatusMatches(current, network)
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
		conditionHas(current, conditionTypeKupoReady, metav1.ConditionTrue, conditionReasonKupoReady) &&
		conditionHas(current, conditionTypeFaucetReady, metav1.ConditionTrue, conditionReasonFaucetReady) &&
		conditionHas(current, conditionTypeArtifactsReady, metav1.ConditionTrue, conditionReasonArtifactsReady)
}

func statusHasDisabledFaucetReadyConditions(
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
		conditionHas(current, conditionTypeKupoReady, metav1.ConditionTrue, conditionReasonKupoReady) &&
		conditionHas(current, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled) &&
		conditionHas(current, conditionTypeArtifactsReady, metav1.ConditionTrue, conditionReasonArtifactsReady) &&
		current.Status.Endpoints != nil &&
		current.Status.Endpoints.Faucet == nil &&
		current.Status.Faucet == nil
}

func conditionHas(
	network *yacdv1alpha1.CardanoNetwork,
	ct conditionType,
	status metav1.ConditionStatus,
	reason conditionReason,
) bool {
	condition := findCondition(network, ct)
	if condition == nil || condition.Status != status {
		return false
	}

	return reason == "" || condition.Reason == string(reason)
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

func faucetEndpointMatches(current *yacdv1alpha1.CardanoNetwork, network *yacdv1alpha1.CardanoNetwork) bool {
	if current.Status.Endpoints == nil || current.Status.Endpoints.Faucet == nil {
		return false
	}

	return current.Status.Endpoints.Faucet.ServiceName == primaryFaucetServiceName(network) &&
		current.Status.Endpoints.Faucet.Port == defaultFaucetPort &&
		current.Status.Endpoints.Faucet.URL == "http://manager-owned-faucet.cardanonetwork-envtest.svc.cluster.local:8080"
}

func artifactsEndpointMatches(current *yacdv1alpha1.CardanoNetwork, network *yacdv1alpha1.CardanoNetwork) bool {
	if current.Status.Endpoints == nil || current.Status.Endpoints.Artifacts == nil {
		return false
	}

	return current.Status.Endpoints.Artifacts.ServiceName == primaryArtifactsServiceName(network) &&
		current.Status.Endpoints.Artifacts.Port == defaultServePort &&
		current.Status.Endpoints.Artifacts.URL == "http://manager-owned-artifacts.cardanonetwork-envtest.svc.cluster.local:8090"
}

func faucetStatusMatches(current *yacdv1alpha1.CardanoNetwork, network *yacdv1alpha1.CardanoNetwork) bool {
	return current.Status.Faucet != nil &&
		current.Status.Faucet.AuthSecretName == primaryFaucetAuthSecretName(network)
}

func recoverDeletedFaucetAuthSecret(
	t *testing.T,
	ctx context.Context,
	apiClient client.Client,
	network *yacdv1alpha1.CardanoNetwork,
	faucetAuthSecretKey client.ObjectKey,
	deploymentKey client.ObjectKey,
) {
	t.Helper()

	secret := &corev1.Secret{}
	require.NoError(t, apiClient.Get(ctx, faucetAuthSecretKey, secret))
	originalSecretUID := secret.UID
	originalToken := string(secret.Data[faucetAuthTokenKey])
	originalHash := faucetAuthTokenHash(secret)

	deployment := &appsv1.Deployment{}
	require.NoError(t, apiClient.Get(ctx, deploymentKey, deployment))
	require.Equal(t, originalHash, deployment.Spec.Template.Annotations[faucetAuthTokenHashAnno])

	require.NoError(t, apiClient.Delete(ctx, secret))

	require.Eventually(t, func() bool {
		gotSecret := &corev1.Secret{}
		if err := apiClient.Get(ctx, faucetAuthSecretKey, gotSecret); err != nil {
			return false
		}
		gotDeployment := &appsv1.Deployment{}
		if err := apiClient.Get(ctx, deploymentKey, gotDeployment); err != nil {
			return false
		}
		currentNetwork := &yacdv1alpha1.CardanoNetwork{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(network), currentNetwork); err != nil {
			return false
		}

		repairedHash := faucetAuthTokenHash(gotSecret)
		return gotSecret.UID != originalSecretUID &&
			string(gotSecret.Data[faucetAuthTokenKey]) != originalToken &&
			gotDeployment.Spec.Template.Annotations[faucetAuthTokenHashAnno] == repairedHash &&
			repairedHash != originalHash &&
			conditionHas(currentNetwork, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing) &&
			conditionHas(currentNetwork, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	}, 10*time.Second, 100*time.Millisecond)
}
