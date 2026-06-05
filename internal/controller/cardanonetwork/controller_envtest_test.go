package cardanonetwork

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
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
		Client:               mgr.GetClient(),
		Reader:               mgr.GetAPIReader(),
		Scheme:               mgr.GetScheme(),
		Now:                  func() time.Time { return envtestNow },
		syncProberOverride:   syncedNodeSyncProber(),
		timingProberOverride: syncedNodeTimingProber(),
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

	artifactsServiceKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryArtifactsServiceName(network)}
	require.Eventually(t, func() bool {
		return apiClient.Get(ctx, artifactsServiceKey, &corev1.Service{}) == nil
	}, 10*time.Second, 100*time.Millisecond)

	// The well-known faucet wallet Secret is generated before the Deployment so
	// its genesis-funded address can be injected into the genesis-funding init
	// container. It is owned by the CardanoNetwork and carries a derived address.
	faucetWalletSecretKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryFaucetWalletSecretName(network)}
	require.Eventually(t, func() bool {
		secret := &corev1.Secret{}
		if apiClient.Get(ctx, faucetWalletSecretKey, secret) != nil {
			return false
		}
		owner := metav1.GetControllerOf(secret)
		return owner != nil && owner.Name == network.Name &&
			secret.Labels[walletNameLabel] == faucetWalletName &&
			strings.HasPrefix(string(secret.Data[walletAddressKey]), "addr_test1") &&
			len(secret.Data[walletSigningKeyKey]) > 0
	}, 10*time.Second, 100*time.Millisecond)

	// The Deployment carries the genesis-funding init container with the faucet
	// wallet address env, ordered before the served-artifact stage init so the
	// staged copy and the node both boot from the funded genesis. (Envtest does
	// not run the init; the actual on-chain funding is proven on the dev stack.)
	require.Eventually(t, func() bool {
		deployment := &appsv1.Deployment{}
		if apiClient.Get(ctx, deploymentKey, deployment) != nil {
			return false
		}
		faucetWalletSecret := &corev1.Secret{}
		if apiClient.Get(ctx, faucetWalletSecretKey, faucetWalletSecret) != nil {
			return false
		}
		return deploymentFundsFaucetWalletAtGenesis(deployment, string(faucetWalletSecret.Data[walletAddressKey]))
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
}

func TestCardanoNetworkControllerManagerMirrorsExternalAccess(t *testing.T) {
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
		Client:               mgr.GetClient(),
		Reader:               mgr.GetAPIReader(),
		Scheme:               mgr.GetScheme(),
		Now:                  func() time.Time { return envtestNow },
		syncProberOverride:   syncedNodeSyncProber(),
		timingProberOverride: syncedNodeTimingProber(),
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
	namespace.Name = "cardanonetwork-external-access-envtest"
	require.NoError(t, apiClient.Create(ctx, namespace))

	// The CRD nodePort Maximum marker rejects an out-of-range value at
	// admission, proving the marker reached the served CRD.
	badNodePort := localCardanoNetwork("bad-nodeport")
	badNodePort.Namespace = namespace.Name
	badNodePort.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Ogmios: &yacdv1alpha1.OgmiosSpec{
			Enabled: true,
			Image:   defaultOgmiosImage,
			Port:    defaultOgmiosPort,
			Service: &yacdv1alpha1.ServiceExposureSpec{
				Type:     yacdv1alpha1.ChainAPIServiceTypeNodePort,
				NodePort: 40000,
			},
		},
	}
	require.Error(t, apiClient.Create(ctx, badNodePort))

	network := localCardanoNetwork("external-access")
	network.Namespace = namespace.Name
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Ogmios: &yacdv1alpha1.OgmiosSpec{
			Enabled:     true,
			Image:       defaultOgmiosImage,
			Port:        defaultOgmiosPort,
			ExternalURL: "wss://ogmios.example.com",
			Service: &yacdv1alpha1.ServiceExposureSpec{
				Type:     yacdv1alpha1.ChainAPIServiceTypeNodePort,
				NodePort: 30137,
			},
		},
		Kupo: &yacdv1alpha1.KupoSpec{
			Enabled:     true,
			Image:       defaultKupoImage,
			Port:        defaultKupoPort,
			ExternalURL: "https://kupo.example.com",
		},
	}
	require.NoError(t, apiClient.Create(ctx, network))

	// The ogmios Service is rendered NodePort with the pinned node port.
	ogmiosServiceKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryOgmiosServiceName(network)}
	require.Eventually(t, func() bool {
		service := &corev1.Service{}
		if apiClient.Get(ctx, ogmiosServiceKey, service) != nil {
			return false
		}
		return service.Spec.Type == corev1.ServiceTypeNodePort &&
			len(service.Spec.Ports) == 1 &&
			service.Spec.Ports[0].NodePort == 30137
	}, 10*time.Second, 100*time.Millisecond)

	// Both externalURLs are mirrored additively into status; the in-cluster url
	// fields keep their cluster-local scheme.
	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoNetwork{}
		if apiClient.Get(ctx, client.ObjectKeyFromObject(network), current) != nil {
			return false
		}
		endpoints := current.Status.Endpoints
		return endpoints != nil &&
			endpoints.Ogmios != nil && endpoints.Ogmios.ExternalURL == "wss://ogmios.example.com" &&
			strings.HasPrefix(endpoints.Ogmios.URL, "ws://") &&
			endpoints.Kupo != nil && endpoints.Kupo.ExternalURL == "https://kupo.example.com" &&
			strings.HasPrefix(endpoints.Kupo.URL, "http://")
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
		Client:               mgr.GetClient(),
		Reader:               mgr.GetAPIReader(),
		Scheme:               mgr.GetScheme(),
		syncProberOverride:   syncedNodeSyncProber(),
		timingProberOverride: syncedNodeTimingProber(),
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

func findCondition(network *yacdv1alpha1.CardanoNetwork, ct conditionType) *metav1.Condition {
	return apimeta.FindStatusCondition(network.Status.Conditions, string(ct))
}

// deploymentFundsFaucetWalletAtGenesis reports whether the Deployment carries
// the genesis-funding init container with the given faucet wallet address in its
// fund-genesis arguments, ordered before the served-artifact stage init.
func deploymentFundsFaucetWalletAtGenesis(deployment *appsv1.Deployment, address string) bool {
	if address == "" {
		return false
	}
	inits := deployment.Spec.Template.Spec.InitContainers
	genesisIdx, stageIdx := -1, -1
	for i, c := range inits {
		switch c.Name {
		case faucetWalletGenesisInitContainerName:
			genesisIdx = i
			hasAddress := false
			for _, arg := range c.Args {
				if arg == address {
					hasAddress = true
				}
			}
			if !hasAddress {
				return false
			}
		case servedArtifactsInitContainerName:
			stageIdx = i
		}
	}

	return genesisIdx >= 0 && stageIdx >= 0 && genesisIdx < stageIdx
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
		conditionHas(current, conditionTypeArtifactsReady, metav1.ConditionFalse, "") &&
		nodeToNodeEndpointMatches(current, network) &&
		ogmiosEndpointMatches(current, network) &&
		kupoEndpointMatches(current, network) &&
		artifactsEndpointMatches(current, network)
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
		conditionHas(current, conditionTypeArtifactsReady, metav1.ConditionTrue, conditionReasonArtifactsReady)
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

func artifactsEndpointMatches(current *yacdv1alpha1.CardanoNetwork, network *yacdv1alpha1.CardanoNetwork) bool {
	if current.Status.Endpoints == nil || current.Status.Endpoints.Artifacts == nil {
		return false
	}

	return current.Status.Endpoints.Artifacts.ServiceName == primaryArtifactsServiceName(network) &&
		current.Status.Endpoints.Artifacts.Port == defaultServePort &&
		current.Status.Endpoints.Artifacts.URL == "http://manager-owned-artifacts.cardanonetwork-envtest.svc.cluster.local:8090"
}
