package cardanonetwork

import (
	"context"
	"encoding/json"
	"maps"
	"path/filepath"
	"strings"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	ctrldbsync "github.com/meigma/yacd/internal/controller/cardanodbsync"
	ctrlartifacts "github.com/meigma/yacd/internal/ctrlkit/artifacts"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

	faucetAuthSecretKey := client.ObjectKey{Namespace: network.Namespace, Name: primaryFaucetAuthSecretName(network)}
	require.Eventually(t, func() bool {
		secret := &corev1.Secret{}
		return apiClient.Get(ctx, faucetAuthSecretKey, secret) == nil &&
			validFaucetAuthToken(string(secret.Data[faucetAuthTokenKey]))
	}, 10*time.Second, 100*time.Millisecond)

	artifactsConfigMapKey := client.ObjectKey{Namespace: network.Namespace, Name: networkArtifactsConfigMapName(network)}
	require.Eventually(t, func() bool {
		configMap := &corev1.ConfigMap{}
		return apiClient.Get(ctx, artifactsConfigMapKey, configMap) == nil &&
			configMap.Annotations[localnetFingerprintAnno] != ""
	}, 10*time.Second, 100*time.Millisecond)

	artifactPublisherServiceAccountKey := client.ObjectKey{Namespace: network.Namespace, Name: artifactPublisherServiceAccountName(network)}
	require.Eventually(t, func() bool {
		serviceAccount := &corev1.ServiceAccount{}
		return apiClient.Get(ctx, artifactPublisherServiceAccountKey, serviceAccount) == nil &&
			serviceAccount.AutomountServiceAccountToken != nil &&
			!*serviceAccount.AutomountServiceAccountToken
	}, 10*time.Second, 100*time.Millisecond)

	artifactPublisherRoleKey := client.ObjectKey{Namespace: network.Namespace, Name: artifactPublisherRoleName(network)}
	require.Eventually(t, func() bool {
		role := &rbacv1.Role{}
		return apiClient.Get(ctx, artifactPublisherRoleKey, role) == nil &&
			len(role.Rules) == 1 &&
			len(role.Rules[0].ResourceNames) == 1 &&
			role.Rules[0].ResourceNames[0] == artifactsConfigMapKey.Name
	}, 10*time.Second, 100*time.Millisecond)

	artifactPublisherRoleBindingKey := client.ObjectKey{Namespace: network.Namespace, Name: artifactPublisherRoleBindingName(network)}
	require.Eventually(t, func() bool {
		roleBinding := &rbacv1.RoleBinding{}
		return apiClient.Get(ctx, artifactPublisherRoleBindingKey, roleBinding) == nil &&
			len(roleBinding.Subjects) == 1 &&
			roleBinding.Subjects[0].Kind == rbacv1.ServiceAccountKind &&
			roleBinding.Subjects[0].Name == artifactPublisherServiceAccountKey.Name
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

	artifactsConfigMap := &corev1.ConfigMap{}
	require.NoError(t, apiClient.Get(ctx, artifactsConfigMapKey, artifactsConfigMap))
	originalArtifactsConfigMapUID := artifactsConfigMap.UID
	require.NoError(t, apiClient.Delete(ctx, artifactsConfigMap))
	require.Eventually(t, func() bool {
		got := &corev1.ConfigMap{}
		err := apiClient.Get(ctx, artifactsConfigMapKey, got)
		return err == nil && got.UID != originalArtifactsConfigMapUID
	}, 10*time.Second, 100*time.Millisecond)

	artifactPublisherServiceAccount := &corev1.ServiceAccount{}
	require.NoError(t, apiClient.Get(ctx, artifactPublisherServiceAccountKey, artifactPublisherServiceAccount))
	originalArtifactPublisherServiceAccountUID := artifactPublisherServiceAccount.UID
	require.NoError(t, apiClient.Delete(ctx, artifactPublisherServiceAccount))
	require.Eventually(t, func() bool {
		got := &corev1.ServiceAccount{}
		err := apiClient.Get(ctx, artifactPublisherServiceAccountKey, got)
		return err == nil && got.UID != originalArtifactPublisherServiceAccountUID
	}, 10*time.Second, 100*time.Millisecond)

	artifactPublisherRole := &rbacv1.Role{}
	require.NoError(t, apiClient.Get(ctx, artifactPublisherRoleKey, artifactPublisherRole))
	originalArtifactPublisherRoleUID := artifactPublisherRole.UID
	require.NoError(t, apiClient.Delete(ctx, artifactPublisherRole))
	require.Eventually(t, func() bool {
		got := &rbacv1.Role{}
		err := apiClient.Get(ctx, artifactPublisherRoleKey, got)
		return err == nil && got.UID != originalArtifactPublisherRoleUID
	}, 10*time.Second, 100*time.Millisecond)

	artifactPublisherRoleBinding := &rbacv1.RoleBinding{}
	require.NoError(t, apiClient.Get(ctx, artifactPublisherRoleBindingKey, artifactPublisherRoleBinding))
	originalArtifactPublisherRoleBindingUID := artifactPublisherRoleBinding.UID
	require.NoError(t, apiClient.Delete(ctx, artifactPublisherRoleBinding))
	require.Eventually(t, func() bool {
		got := &rbacv1.RoleBinding{}
		err := apiClient.Get(ctx, artifactPublisherRoleBindingKey, got)
		return err == nil && got.UID != originalArtifactPublisherRoleBindingUID
	}, 10*time.Second, 100*time.Millisecond)

	require.Eventually(t, func() bool {
		return statusHasProgressingEndpoints(ctx, apiClient, network)
	}, 10*time.Second, 100*time.Millisecond)
	publishNetworkArtifactsWithClient(t, ctx, apiClient, network)

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

	recoverCorruptedNetworkArtifactsConfigMapWithFinalizer(t, ctx, apiClient, network, artifactsConfigMapKey, deploymentKey)
	publishNetworkArtifactsWithClient(t, ctx, apiClient, network)

	require.NoError(t, apiClient.Get(ctx, deploymentKey, deployment))
	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 1
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 1
	deployment.Status.AvailableReplicas = 1
	require.NoError(t, apiClient.Status().Update(ctx, deployment))
	require.Eventually(t, func() bool {
		return statusHasReadyConditions(ctx, apiClient, network)
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

	recoverCorruptedNetworkArtifactsConfigMap(t, ctx, apiClient, network, artifactsConfigMapKey, deploymentKey, envtestNow)
	publishNetworkArtifactsWithClient(t, ctx, apiClient, network)

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

	envtestNow = envtestNow.Add(30 * time.Second)
	suppressCorruptedNetworkArtifactsConfigMapDuringCooldown(t, ctx, apiClient, network, artifactsConfigMapKey, deploymentKey)
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

func TestCardanoNetworkControllerManagerHandlesCustomProfileSources(t *testing.T) {
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

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cardanonetwork-custom-envtest"}}
	require.NoError(t, apiClient.Create(ctx, namespace))

	configMapNetwork := publicCardanoNetwork("custom-configmap", yacdv1alpha1.PublicNetworkProfileCustom)
	configMapNetwork.Namespace = namespace.Name
	configMapNetwork.Spec.Public.ConfigSource = &yacdv1alpha1.NetworkConfigSource{
		ConfigMapRef: &corev1.LocalObjectReference{Name: "custom-profile"},
	}
	configMapSource := customProfileConfigMap(namespace.Name, "custom-profile", customPublicProfileBundle(t))
	require.NoError(t, apiClient.Create(ctx, configMapSource))
	require.NoError(t, apiClient.Create(ctx, configMapNetwork))

	secretNetwork := publicCardanoNetwork("custom-secret", yacdv1alpha1.PublicNetworkProfileCustom)
	secretNetwork.Namespace = namespace.Name
	secretNetwork.Spec.Public.ConfigSource = &yacdv1alpha1.NetworkConfigSource{
		SecretRef: &corev1.LocalObjectReference{Name: "custom-profile-secret"},
	}
	secretSource := customProfileSecret(namespace.Name, "custom-profile-secret", customPublicProfileBundle(t))
	require.NoError(t, apiClient.Create(ctx, secretSource))
	require.NoError(t, apiClient.Create(ctx, secretNetwork))

	require.Eventually(t, func() bool {
		return statusHasCustomPublicArtifacts(ctx, apiClient, configMapNetwork)
	}, 10*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		return statusHasCustomPublicArtifacts(ctx, apiClient, secretNetwork)
	}, 10*time.Second, 100*time.Millisecond)

	currentSource := &corev1.ConfigMap{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(configMapSource), currentSource))
	currentSource.Data["topology.json"] = "{\"Producers\":[]}\n"
	require.NoError(t, apiClient.Update(ctx, currentSource))

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoNetwork{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(configMapNetwork), current); err != nil {
			return false
		}
		return conditionHas(current, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedNetworkChange)
	}, 10*time.Second, 100*time.Millisecond)
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
	}, 10*time.Second, 100*time.Millisecond)

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
	}, 10*time.Second, 100*time.Millisecond)

	artifactKey := client.ObjectKey{Namespace: network.Namespace, Name: networkArtifactsConfigMapName(network)}
	require.Eventually(t, func() bool {
		configMap := &corev1.ConfigMap{}
		return apiClient.Get(ctx, artifactKey, configMap) == nil
	}, 10*time.Second, 100*time.Millisecond)
	artifactConfigMap := &corev1.ConfigMap{}
	require.NoError(t, apiClient.Get(ctx, artifactKey, artifactConfigMap))

	currentNetwork := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(network), currentNetwork))
	networkFingerprint := ""
	localnetFingerprint := ""
	if currentNetwork.Status.Network != nil {
		networkFingerprint = currentNetwork.Status.Network.NetworkFingerprint
		localnetFingerprint = currentNetwork.Status.Network.LocalnetFingerprint
	}
	if networkFingerprint == "" || localnetFingerprint == "" {
		currentDeployment := &appsv1.Deployment{}
		require.NoError(t, apiClient.Get(ctx, deploymentKey, currentDeployment))
		if networkFingerprint == "" {
			networkFingerprint = currentDeployment.Spec.Template.Annotations[networkFingerprintAnno]
		}
		if localnetFingerprint == "" {
			localnetFingerprint = currentDeployment.Spec.Template.Annotations[localnetFingerprintAnno]
		}
	}
	require.NotEmpty(t, networkFingerprint)
	require.NotEmpty(t, localnetFingerprint)
	networkMagic := currentNetwork.Spec.Local.NetworkMagic
	era := currentNetwork.Spec.Local.Era
	currentNetwork.Status.Network = &yacdv1alpha1.CardanoNetworkIdentityStatus{
		Mode:                yacdv1alpha1.CardanoNetworkModeLocal,
		LocalnetFingerprint: localnetFingerprint,
		NetworkFingerprint:  networkFingerprint,
		NetworkMagic:        &networkMagic,
		Era:                 &era,
	}
	currentNetwork.Status.Endpoints = &yacdv1alpha1.CardanoNetworkEndpointsStatus{
		NodeToNode: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: primaryWorkloadName(network),
			Port:        network.Spec.Node.Port,
			URL:         "tcp://sidecar-network-node.cardanonetwork-dbsync-envtest.svc.cluster.local:3001",
		},
		Ogmios: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: primaryOgmiosServiceName(network),
			Port:        defaultOgmiosPort,
			URL:         "ws://sidecar-network-ogmios.cardanonetwork-dbsync-envtest.svc.cluster.local:1337",
		},
	}
	artifactData := dbSyncEnvtestNetworkArtifactsData(currentNetwork)
	artifactDataHash := ctrlartifacts.ComputeDataHash(artifactData)
	if artifactConfigMap.Annotations == nil {
		artifactConfigMap.Annotations = map[string]string{}
	}
	artifactConfigMap.Annotations[ctrlannotations.ArtifactSchemaVersion] = networkartifacts.SchemaVersion
	artifactConfigMap.Annotations[ctrlannotations.ArtifactDataHash] = artifactDataHash
	artifactConfigMap.Annotations[ctrlannotations.NetworkFingerprint] = currentNetwork.Status.Network.NetworkFingerprint
	artifactConfigMap.Annotations[ctrlannotations.LocalnetFingerprint] = currentNetwork.Status.Network.LocalnetFingerprint
	artifactConfigMap.Data = artifactData
	require.NoError(t, apiClient.Update(ctx, artifactConfigMap))

	currentNetwork.Status.ObservedGeneration = currentNetwork.Generation
	currentNetwork.Status.Artifacts = &yacdv1alpha1.CardanoNetworkArtifactsStatus{
		NetworkConfigMapName: artifactConfigMap.Name,
		SchemaVersion:        networkartifacts.SchemaVersion,
		DataHash:             artifactDataHash,
	}
	currentNetwork.Status.Conditions = []metav1.Condition{{
		Type:               string(conditionTypeArtifactsReady),
		Status:             metav1.ConditionTrue,
		Reason:             string(conditionReasonArtifactsReady),
		Message:            "artifacts are ready",
		ObservedGeneration: currentNetwork.Generation,
		LastTransitionTime: metav1.Now(),
	}}
	require.NoError(t, apiClient.Status().Update(ctx, currentNetwork))

	first := readyPrimarySidecarDBSync("first", network)
	first.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, primarySidecarExternalSecret(first)))
	require.NoError(t, apiClient.Create(ctx, first))

	require.Eventually(t, func() bool {
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
	}, 10*time.Second, 100*time.Millisecond)

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
	}, 10*time.Second, 100*time.Millisecond)

	second := readyPrimarySidecarDBSync("second", network)
	second.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, primarySidecarExternalSecret(second)))
	require.NoError(t, apiClient.Create(ctx, second))

	requireDeploymentContainerEventually(t, ctx, apiClient, deploymentKey, "cardano-db-sync", true)
	requireDeploymentDBSyncSidecarRevisionEventually(t, ctx, apiClient, deploymentKey, "sha256:2222222222222222222222222222222222222222222222222222222222222222")

	require.NoError(t, apiClient.Delete(ctx, currentFirst))
	require.Eventually(t, func() bool {
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
	}, 10*time.Second, 100*time.Millisecond)
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
	}, 10*time.Second, 100*time.Millisecond)
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
	}, 10*time.Second, 100*time.Millisecond)
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
		conditionHas(current, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonArtifactsPending) &&
		current.Status.Artifacts == nil &&
		nodeToNodeEndpointMatches(current, network) &&
		ogmiosEndpointMatches(current, network) &&
		kupoEndpointMatches(current, network) &&
		faucetEndpointMatches(current, network) &&
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
		conditionHas(current, conditionTypeArtifactsReady, metav1.ConditionTrue, conditionReasonArtifactsReady) &&
		networkArtifactsStatusMatches(current, network)
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
		networkArtifactsStatusMatches(current, network) &&
		current.Status.Endpoints != nil &&
		current.Status.Endpoints.Faucet == nil &&
		current.Status.Faucet == nil
}

func statusHasCustomPublicArtifacts(
	ctx context.Context,
	apiClient client.Client,
	network *yacdv1alpha1.CardanoNetwork,
) bool {
	current := &yacdv1alpha1.CardanoNetwork{}
	if err := apiClient.Get(ctx, client.ObjectKeyFromObject(network), current); err != nil {
		return false
	}
	if current.Status.Network == nil ||
		current.Status.Network.NetworkFingerprint == "" ||
		current.Status.Network.Profile == nil ||
		*current.Status.Network.Profile != yacdv1alpha1.PublicNetworkProfileCustom ||
		current.Status.Artifacts == nil {
		return false
	}

	return conditionHas(current, conditionTypeArtifactsReady, metav1.ConditionTrue, conditionReasonArtifactsReady) &&
		current.Status.Artifacts.NetworkConfigMapName == networkArtifactsConfigMapName(network) &&
		current.Status.Artifacts.SchemaVersion == networkartifacts.SchemaVersion &&
		current.Status.Artifacts.DataHash != ""
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

func recoverCorruptedNetworkArtifactsConfigMapWithFinalizer(
	t *testing.T,
	ctx context.Context,
	apiClient client.Client,
	network *yacdv1alpha1.CardanoNetwork,
	artifactsConfigMapKey client.ObjectKey,
	deploymentKey client.ObjectKey,
) {
	t.Helper()

	configMap := &corev1.ConfigMap{}
	require.NoError(t, apiClient.Get(ctx, artifactsConfigMapKey, configMap))
	verifiedArtifactsConfigMapUID := configMap.UID
	configMap.Finalizers = append(configMap.Finalizers, "yacd.meigma.io/test-artifacts-finalizer")
	delete(configMap.Data, networkartifacts.ConfigurationKey)
	require.NoError(t, apiClient.Update(ctx, configMap))

	require.Eventually(t, func() bool {
		got := &corev1.ConfigMap{}
		if err := apiClient.Get(ctx, artifactsConfigMapKey, got); err != nil {
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
		return got.UID == verifiedArtifactsConfigMapUID &&
			!got.DeletionTimestamp.IsZero() &&
			gotDeployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno] == string(verifiedArtifactsConfigMapUID) &&
			conditionHas(currentNetwork, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonArtifactsPending) &&
			currentNetwork.Status.Artifacts == nil
	}, 10*time.Second, 100*time.Millisecond)

	require.NoError(t, apiClient.Get(ctx, artifactsConfigMapKey, configMap))
	configMap.Finalizers = nil
	require.NoError(t, apiClient.Update(ctx, configMap))

	require.Eventually(t, func() bool {
		got := &corev1.ConfigMap{}
		if err := apiClient.Get(ctx, artifactsConfigMapKey, got); err != nil {
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
		return got.UID != verifiedArtifactsConfigMapUID &&
			len(got.Data) == 0 &&
			gotDeployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno] == string(got.UID) &&
			conditionHas(currentNetwork, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonArtifactsPending) &&
			currentNetwork.Status.Artifacts == nil
	}, 10*time.Second, 100*time.Millisecond)
}

func recoverCorruptedNetworkArtifactsConfigMap(
	t *testing.T,
	ctx context.Context,
	apiClient client.Client,
	network *yacdv1alpha1.CardanoNetwork,
	artifactsConfigMapKey client.ObjectKey,
	deploymentKey client.ObjectKey,
	rolloutAt time.Time,
) {
	t.Helper()

	configMap := &corev1.ConfigMap{}
	require.NoError(t, apiClient.Get(ctx, artifactsConfigMapKey, configMap))
	verifiedArtifactsConfigMapUID := configMap.UID
	configMap.Data[networkartifacts.PrimaryTopologyKey] = "corrupted-before-cooldown"
	require.NoError(t, apiClient.Update(ctx, configMap))

	require.Eventually(t, func() bool {
		got := &corev1.ConfigMap{}
		if err := apiClient.Get(ctx, artifactsConfigMapKey, got); err != nil {
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
		return got.UID != verifiedArtifactsConfigMapUID &&
			len(got.Data) == 0 &&
			gotDeployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno] == string(got.UID) &&
			gotDeployment.Annotations[networkArtifactsRecoveryRolloutAtAnno] == rolloutAt.UTC().Format(time.RFC3339) &&
			conditionHas(currentNetwork, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonArtifactsPending) &&
			currentNetwork.Status.Artifacts == nil
	}, 10*time.Second, 100*time.Millisecond)
}

func suppressCorruptedNetworkArtifactsConfigMapDuringCooldown(
	t *testing.T,
	ctx context.Context,
	apiClient client.Client,
	network *yacdv1alpha1.CardanoNetwork,
	artifactsConfigMapKey client.ObjectKey,
	deploymentKey client.ObjectKey,
) {
	t.Helper()

	configMap := &corev1.ConfigMap{}
	require.NoError(t, apiClient.Get(ctx, artifactsConfigMapKey, configMap))
	cooldownConfigMapUID := configMap.UID
	configMap.Data[networkartifacts.PrimaryTopologyKey] = "corrupted-during-cooldown"
	require.NoError(t, apiClient.Update(ctx, configMap))

	require.Eventually(t, func() bool {
		got := &corev1.ConfigMap{}
		if err := apiClient.Get(ctx, artifactsConfigMapKey, got); err != nil {
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
		return got.UID == cooldownConfigMapUID &&
			got.Data[networkartifacts.PrimaryTopologyKey] == "corrupted-during-cooldown" &&
			gotDeployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno] == string(cooldownConfigMapUID) &&
			conditionHas(currentNetwork, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonArtifactsPending) &&
			currentNetwork.Status.Artifacts == nil
	}, 10*time.Second, 100*time.Millisecond)
}

func publishNetworkArtifactsWithClient(
	t *testing.T,
	ctx context.Context,
	apiClient client.Client,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	configMap := &corev1.ConfigMap{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKey{
		Namespace: network.Namespace,
		Name:      networkArtifactsConfigMapName(network),
	}, configMap))
	if configMap.Annotations == nil {
		configMap.Annotations = map[string]string{}
	}
	configMap.Annotations[ctrlannotations.ArtifactSchemaVersion] = networkartifacts.SchemaVersion
	configMap.Annotations[ctrlannotations.ArtifactDataHash] = testNetworkArtifactsDataHash
	if configMap.Data == nil {
		configMap.Data = map[string]string{}
	}
	maps.Copy(configMap.Data, testNetworkArtifactsData())
	require.NoError(t, apiClient.Update(ctx, configMap))
}

func networkArtifactsStatusMatches(current *yacdv1alpha1.CardanoNetwork, network *yacdv1alpha1.CardanoNetwork) bool {
	return current.Status.Artifacts != nil &&
		current.Status.Artifacts.NetworkConfigMapName == networkArtifactsConfigMapName(network) &&
		current.Status.Artifacts.SchemaVersion == networkartifacts.SchemaVersion &&
		current.Status.Artifacts.DataHash == testNetworkArtifactsDataHash
}

func dbSyncEnvtestNetworkArtifactsData(network *yacdv1alpha1.CardanoNetwork) map[string]string {
	data := testNetworkArtifactsData()
	data[networkartifacts.ConnectionKey] = dbSyncEnvtestConnectionJSON(network)
	return data
}

func dbSyncEnvtestConnectionJSON(network *yacdv1alpha1.CardanoNetwork) string {
	doc := struct {
		SchemaVersion     string            `json:"schemaVersion"`
		Network           map[string]any    `json:"network"`
		PrimaryNodeToNode map[string]any    `json:"primaryNodeToNode"`
		Files             map[string]string `json:"files"`
	}{
		SchemaVersion: networkartifacts.SchemaVersion,
		Network: map[string]any{
			"name":                network.Name,
			"namespace":           network.Namespace,
			"mode":                string(network.Status.Network.Mode),
			"networkMagic":        *network.Status.Network.NetworkMagic,
			"era":                 string(*network.Status.Network.Era),
			"localnetFingerprint": network.Status.Network.LocalnetFingerprint,
		},
		PrimaryNodeToNode: map[string]any{
			"host": network.Status.Endpoints.NodeToNode.ServiceName + "." + network.Namespace + ".svc.cluster.local",
			"port": network.Status.Endpoints.NodeToNode.Port,
			"url":  network.Status.Endpoints.NodeToNode.URL,
		},
		Files: map[string]string{
			"configuration":   networkartifacts.ConfigurationKey,
			"byronGenesis":    networkartifacts.ByronGenesisKey,
			"shelleyGenesis":  networkartifacts.ShelleyGenesisKey,
			"alonzoGenesis":   networkartifacts.AlonzoGenesisKey,
			"conwayGenesis":   networkartifacts.ConwayGenesisKey,
			"primaryTopology": networkartifacts.PrimaryTopologyKey,
			"connection":      networkartifacts.ConnectionKey,
			"localnetPlan":    networkartifacts.PlanManifestKey,
		},
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(raw) + "\n"
}
