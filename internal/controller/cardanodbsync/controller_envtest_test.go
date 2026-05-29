package cardanodbsync

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	ctrlartifacts "github.com/meigma/yacd/internal/ctrlkit/artifacts"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

func TestCardanoDBSyncControllerManagerReconcilesReferencedNetworkAndExternalDatabaseSecret(t *testing.T) {
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
		Controller:             cardanoDBSyncEnvtestControllerOptions(),
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	require.NoError(t, err)
	require.NoError(t, (&CardanoDBSyncReconciler{
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
	namespace.Name = "cardanodbsync-envtest"
	require.NoError(t, apiClient.Create(ctx, namespace))

	dbSync := localCardanoDBSync("dbsync", "watched-network")
	dbSync.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, externalDatabaseSecretFor(dbSync)))
	require.NoError(t, apiClient.Create(ctx, dbSync))

	network := localCardanoNetwork("watched-network")
	network.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, network))

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(dbSync), current); err != nil {
			return false
		}
		condition := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeProgressing))
		return condition != nil && condition.Reason == string(conditionReasonNetworkStatusStale)
	}, 10*time.Second, 100*time.Millisecond)

	artifactConfigMapName := "watched-network-network-artifacts"
	currentNetwork := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(network), currentNetwork))
	networkMagic := int64(42)
	era := yacdv1alpha1.CardanoEraConway
	currentNetwork.Status.ObservedGeneration = currentNetwork.Generation
	currentNetwork.Status.Network = &yacdv1alpha1.CardanoNetworkIdentityStatus{
		Mode:                yacdv1alpha1.CardanoNetworkModeLocal,
		LocalnetFingerprint: "fingerprint",
		NetworkFingerprint:  "fingerprint",
		NetworkMagic:        &networkMagic,
		Era:                 &era,
	}
	currentNetwork.Status.Endpoints = &yacdv1alpha1.CardanoNetworkEndpointsStatus{
		NodeToNode: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: "watched-network-node",
			Port:        3001,
			URL:         "tcp://watched-network-node.cardanodbsync-envtest.svc.cluster.local:3001",
		},
	}
	artifactData := testNetworkArtifactsDataFor(currentNetwork)
	artifactDataHash := ctrlartifacts.ComputeDataHash(artifactData)
	require.NoError(t, apiClient.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      artifactConfigMapName,
			Namespace: namespace.Name,
			Annotations: map[string]string{
				ctrlannotations.ArtifactSchemaVersion: testNetworkArtifactSchemaVersion,
				ctrlannotations.ArtifactDataHash:      artifactDataHash,
				ctrlannotations.NetworkFingerprint:    currentNetwork.Status.Network.NetworkFingerprint,
				ctrlannotations.LocalnetFingerprint:   currentNetwork.Status.Network.LocalnetFingerprint,
			},
		},
		Data: artifactData,
	}))
	currentNetwork.Status.Artifacts = &yacdv1alpha1.CardanoNetworkArtifactsStatus{
		NetworkConfigMapName: artifactConfigMapName,
		SchemaVersion:        testNetworkArtifactSchemaVersion,
		DataHash:             artifactDataHash,
	}
	currentNetwork.Status.Conditions = []metav1.Condition{{
		Type:               "ArtifactsReady",
		Status:             metav1.ConditionTrue,
		Reason:             "ArtifactsReady",
		Message:            "artifacts are ready",
		ObservedGeneration: currentNetwork.Generation,
		LastTransitionTime: metav1.Now(),
	}}
	require.NoError(t, apiClient.Status().Update(ctx, currentNetwork))

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(dbSync), current); err != nil {
			return false
		}
		progressing := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeProgressing))
		ready := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeReady))
		return current.Status.ObservedGeneration == current.Generation &&
			progressing != nil &&
			progressing.Status == metav1.ConditionTrue &&
			progressing.Reason == string(conditionReasonDeploymentProgressing) &&
			ready != nil &&
			ready.Status == metav1.ConditionFalse &&
			ready.Reason == string(conditionReasonDeploymentProgressing)
	}, 10*time.Second, 100*time.Millisecond)

	ownedConfigMap := &corev1.ConfigMap{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: dbSyncConfigMapName(dbSync)}, ownedConfigMap))
	ownedConfigMap.Data[dbSyncConfigFileName] = driftedDBSyncConfig
	require.NoError(t, apiClient.Update(ctx, ownedConfigMap))

	require.Eventually(t, func() bool {
		current := &corev1.ConfigMap{}
		if err := apiClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: dbSyncConfigMapName(dbSync)}, current); err != nil {
			return false
		}
		return current.Data[dbSyncConfigFileName] != driftedDBSyncConfig &&
			current.Data[dbSyncConfigFileName] != ""
	}, 10*time.Second, 100*time.Millisecond)

	ownedPGPassSecret := &corev1.Secret{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: dbSyncPGPassSecretName(dbSync)}, ownedPGPassSecret))
	ownedPGPassSecret.Data[dbSyncPGPassFileName] = []byte(driftedDBSyncConfig)
	require.NoError(t, apiClient.Update(ctx, ownedPGPassSecret))

	require.Eventually(t, func() bool {
		current := &corev1.Secret{}
		if err := apiClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: dbSyncPGPassSecretName(dbSync)}, current); err != nil {
			return false
		}
		return string(current.Data[dbSyncPGPassFileName]) != driftedDBSyncConfig &&
			string(current.Data[dbSyncPGPassFileName]) != ""
	}, 10*time.Second, 100*time.Millisecond)

	secretWatchedDBSync := localCardanoDBSync("dbsync-secret-watch", "watched-network")
	secretWatchedDBSync.Namespace = namespace.Name
	invalidSecret := externalDatabaseSecretFor(secretWatchedDBSync)
	invalidSecret.Data = map[string][]byte{"other": []byte("secret")}
	require.NoError(t, apiClient.Create(ctx, invalidSecret))
	require.NoError(t, apiClient.Create(ctx, secretWatchedDBSync))

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(secretWatchedDBSync), current); err != nil {
			return false
		}
		degraded := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeDegraded))
		return degraded != nil &&
			degraded.Status == metav1.ConditionTrue &&
			degraded.Reason == string(conditionReasonExternalDatabaseSecretInvalid)
	}, 10*time.Second, 100*time.Millisecond)

	currentSecret := &corev1.Secret{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(invalidSecret), currentSecret))
	currentSecret.Data = map[string][]byte{"password": []byte("secret")}
	require.NoError(t, apiClient.Update(ctx, currentSecret))

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(secretWatchedDBSync), current); err != nil {
			return false
		}
		progressing := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeProgressing))
		ready := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeReady))
		return current.Status.ObservedGeneration == current.Generation &&
			progressing != nil &&
			progressing.Status == metav1.ConditionTrue &&
			progressing.Reason == string(conditionReasonDeploymentProgressing) &&
			ready != nil &&
			ready.Status == metav1.ConditionFalse &&
			ready.Reason == string(conditionReasonDeploymentProgressing)
	}, 10*time.Second, 100*time.Millisecond)

	assertManagedPostgresSecretAndChildWatches(t, ctx, apiClient, namespace.Name)
}

func TestCardanoDBSyncControllerManagerReconcilesPublicPreprodDedicatedFollower(t *testing.T) {
	ctx := context.Background()
	apiClient := startCardanoDBSyncTestManager(t, ctx)

	namespace := &corev1.Namespace{}
	namespace.Name = "cardanodbsync-public-envtest"
	require.NoError(t, apiClient.Create(ctx, namespace))

	publishedNetwork := readyPublicCardanoNetwork("public-preprod-network", yacdv1alpha1.PublicNetworkProfilePreprod)
	moveReadyNetworkToNamespace(publishedNetwork, namespace.Name)
	createdNetwork := publishedNetwork.DeepCopy()
	createdNetwork.Status = yacdv1alpha1.CardanoNetworkStatus{}
	require.NoError(t, apiClient.Create(ctx, createdNetwork))
	require.NoError(t, apiClient.Create(ctx, artifactConfigMapFor(publishedNetwork)))

	currentNetwork := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(createdNetwork), currentNetwork))
	currentNetwork.Status = publishedNetwork.Status
	require.NoError(t, apiClient.Status().Update(ctx, currentNetwork))

	dbSync := localCardanoDBSync("public-dbsync", publishedNetwork.Name)
	dbSync.Namespace = namespace.Name
	dbSync.Spec.Placement = &yacdv1alpha1.CardanoDBSyncPlacementSpec{
		Mode: yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower,
	}
	require.NoError(t, apiClient.Create(ctx, externalDatabaseSecretFor(dbSync)))
	require.NoError(t, apiClient.Create(ctx, dbSync))

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(dbSync), current); err != nil {
			return false
		}
		progressing := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeProgressing))
		ready := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeReady))
		return current.Status.ObservedGeneration == current.Generation &&
			progressing != nil &&
			progressing.Status == metav1.ConditionTrue &&
			progressing.Reason == string(conditionReasonDeploymentProgressing) &&
			ready != nil &&
			ready.Status == metav1.ConditionFalse &&
			ready.Reason == string(conditionReasonDeploymentProgressing)
	}, 10*time.Second, 100*time.Millisecond)

	ownedConfigMap := &corev1.ConfigMap{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: dbSyncConfigMapName(dbSync)}, ownedConfigMap))
	require.Contains(t, ownedConfigMap.Data[dbSyncConfigFileName], "NetworkName: preprod")
	require.Contains(t, ownedConfigMap.Data[dbSyncConfigFileName], "RequiresNetworkMagic: RequiresMagic")

	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(createdNetwork), currentNetwork))
	currentNetwork.Status.Artifacts.DataHash = "sha256:" + strings.Repeat("b", 64)
	require.NoError(t, apiClient.Status().Update(ctx, currentNetwork))

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(dbSync), current); err != nil {
			return false
		}
		progressing := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeProgressing))
		return progressing != nil &&
			progressing.Status == metav1.ConditionTrue &&
			progressing.Reason == string(conditionReasonNetworkArtifactsMismatch)
	}, 10*time.Second, 100*time.Millisecond)

	ownedDeployment := &appsv1.Deployment{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: dbSyncWorkloadName(dbSync)}, ownedDeployment))
	require.NotNil(t, ownedDeployment.Spec.Replicas)
	require.Equal(t, int32(0), *ownedDeployment.Spec.Replicas)
}

func TestCardanoDBSyncControllerManagerReconcilesPrimarySidecarPlacementPeers(t *testing.T) {
	ctx := context.Background()
	apiClient := startCardanoDBSyncTestManager(t, ctx)

	namespace := &corev1.Namespace{}
	namespace.Name = "cardanodbsync-placement-envtest"
	require.NoError(t, apiClient.Create(ctx, namespace))

	first := primarySidecarCardanoDBSync(localCardanoDBSync("first", "shared-network"))
	first.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, first))

	requireDBSyncDegradedReasonEventually(t, ctx, apiClient, client.ObjectKeyFromObject(first), conditionReasonNetworkUnavailable)

	time.Sleep(1100 * time.Millisecond)

	second := primarySidecarCardanoDBSync(localCardanoDBSync("second", "shared-network"))
	second.Namespace = namespace.Name
	require.NoError(t, apiClient.Create(ctx, second))

	requireDBSyncDegradedReasonEventually(t, ctx, apiClient, client.ObjectKeyFromObject(first), conditionReasonNetworkUnavailable)
	requireDBSyncDegradedReasonEventually(t, ctx, apiClient, client.ObjectKeyFromObject(second), conditionReasonPlacementConflict)

	currentSecond := &yacdv1alpha1.CardanoDBSync{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(second), currentSecond))
	currentSecond.Spec.Placement.Mode = yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower
	require.NoError(t, apiClient.Update(ctx, currentSecond))

	requireDBSyncDegradedReasonEventually(t, ctx, apiClient, client.ObjectKeyFromObject(first), conditionReasonNetworkUnavailable)
	requireDBSyncDegradedReasonEventually(t, ctx, apiClient, client.ObjectKeyFromObject(second), conditionReasonExternalDatabaseSecretMissing)
}

func assertManagedPostgresSecretAndChildWatches(
	t *testing.T,
	ctx context.Context,
	apiClient client.Client,
	namespace string,
) {
	t.Helper()

	managedSecretWatchedDBSync := managedCardanoDBSync("dbsync-managed-secret-watch", "watched-network")
	managedSecretWatchedDBSync.Namespace = namespace
	managedSecretWatchedDBSync.Spec.Database.Managed.AuthSecretRef = &yacdv1alpha1.CardanoDBSyncSecretReference{Name: "managed-auth"}
	managedAuthSecret := providedManagedPostgresAuthSecretFor(managedSecretWatchedDBSync)
	managedAuthSecret.Data = map[string][]byte{"other": []byte("secret")}
	require.NoError(t, apiClient.Create(ctx, managedAuthSecret))
	require.NoError(t, apiClient.Create(ctx, managedSecretWatchedDBSync))

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(managedSecretWatchedDBSync), current); err != nil {
			return false
		}
		degraded := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeDegraded))
		return degraded != nil &&
			degraded.Status == metav1.ConditionTrue &&
			degraded.Reason == string(conditionReasonManagedDatabaseSecretInvalid)
	}, 10*time.Second, 100*time.Millisecond)

	currentManagedSecret := &corev1.Secret{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(managedAuthSecret), currentManagedSecret))
	currentManagedSecret.Data = map[string][]byte{"password": []byte("secret")}
	require.NoError(t, apiClient.Update(ctx, currentManagedSecret))

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, client.ObjectKeyFromObject(managedSecretWatchedDBSync), current); err != nil {
			return false
		}
		postgres := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypePostgresReady))
		return current.Status.ObservedGeneration == current.Generation &&
			postgres != nil &&
			postgres.Status == metav1.ConditionFalse &&
			postgres.Reason == string(conditionReasonDeploymentProgressing)
	}, 10*time.Second, 100*time.Millisecond)

	managedPostgresService := &corev1.Service{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: managedPostgresServiceName(managedSecretWatchedDBSync)}, managedPostgresService))
	managedPostgresService.Spec.Ports[0].Port = 15432
	require.NoError(t, apiClient.Update(ctx, managedPostgresService))

	require.Eventually(t, func() bool {
		current := &corev1.Service{}
		if err := apiClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: managedPostgresServiceName(managedSecretWatchedDBSync)}, current); err != nil {
			return false
		}
		return len(current.Spec.Ports) == 1 && current.Spec.Ports[0].Port == managedPostgresPort
	}, 10*time.Second, 100*time.Millisecond)
}

func startCardanoDBSyncTestManager(t *testing.T, ctx context.Context) client.Client {
	t.Helper()

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
		Controller:             cardanoDBSyncEnvtestControllerOptions(),
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
	})
	require.NoError(t, err)
	require.NoError(t, (&CardanoDBSyncReconciler{
		Client: mgr.GetClient(),
		Reader: mgr.GetAPIReader(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr))

	managerCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- mgr.Start(managerCtx)
	}()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-errCh)
	})
	require.Eventually(t, func() bool {
		return mgr.GetCache().WaitForCacheSync(managerCtx)
	}, 10*time.Second, 100*time.Millisecond)

	apiClient, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)
	return apiClient
}

func cardanoDBSyncEnvtestControllerOptions() config.Controller {
	skipNameValidation := true
	return config.Controller{SkipNameValidation: &skipNameValidation}
}

func requireDBSyncDegradedReasonEventually(
	t *testing.T,
	ctx context.Context,
	apiClient client.Client,
	key client.ObjectKey,
	reason conditionReason,
) {
	t.Helper()

	require.Eventually(t, func() bool {
		current := &yacdv1alpha1.CardanoDBSync{}
		if err := apiClient.Get(ctx, key, current); err != nil {
			return false
		}
		condition := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeDegraded))
		return current.Status.ObservedGeneration == current.Generation &&
			condition != nil &&
			condition.Status == metav1.ConditionTrue &&
			condition.Reason == string(reason)
	}, 10*time.Second, 100*time.Millisecond)
}

func localCardanoNetwork(name string) *yacdv1alpha1.CardanoNetwork {
	return &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: yacdv1alpha1.CardanoNetworkSpec{
			Mode: yacdv1alpha1.CardanoNetworkModeLocal,
			Node: yacdv1alpha1.CardanoNodeSpec{
				Version: "11.0.1",
				Port:    3001,
			},
			Local: &yacdv1alpha1.LocalNetworkSpec{
				NetworkMagic: 42,
				Era:          yacdv1alpha1.CardanoEraConway,
				Timing: yacdv1alpha1.LocalNetworkTimingSpec{
					SlotLength:  metav1.Duration{Duration: 100 * time.Millisecond},
					EpochLength: 500,
				},
				Topology: yacdv1alpha1.LocalNetworkTopologySpec{
					Pools: yacdv1alpha1.LocalPoolTopologySpec{
						Count: 1,
					},
				},
			},
		},
	}
}
