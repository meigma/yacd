package cardanodbsync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/require"
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
		condition := apimeta.FindStatusCondition(current.Status.Conditions, conditionTypeProgressing)
		return condition != nil && condition.Reason == conditionReasonNetworkStatusStale
	}, 10*time.Second, 100*time.Millisecond)

	artifactConfigMapName := "watched-network-network-artifacts"
	require.NoError(t, apiClient.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      artifactConfigMapName,
			Namespace: namespace.Name,
			Annotations: map[string]string{
				networkArtifactSchemaVersionAnno: testNetworkArtifactSchemaVersion,
				networkArtifactDataHashAnno:      testNetworkArtifactDataHash,
			},
		},
		Data: testNetworkArtifactsData(),
	}))

	currentNetwork := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(network), currentNetwork))
	currentNetwork.Status.ObservedGeneration = currentNetwork.Generation
	currentNetwork.Status.Artifacts = &yacdv1alpha1.CardanoNetworkArtifactsStatus{
		NetworkConfigMapName: artifactConfigMapName,
		SchemaVersion:        testNetworkArtifactSchemaVersion,
		DataHash:             testNetworkArtifactDataHash,
	}
	currentNetwork.Status.Endpoints = &yacdv1alpha1.CardanoNetworkEndpointsStatus{
		NodeToNode: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: "watched-network-node",
			Port:        3001,
			URL:         "tcp://watched-network-node.cardanodbsync-envtest.svc.cluster.local:3001",
		},
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
		progressing := apimeta.FindStatusCondition(current.Status.Conditions, conditionTypeProgressing)
		ready := apimeta.FindStatusCondition(current.Status.Conditions, conditionTypeReady)
		return current.Status.ObservedGeneration == current.Generation &&
			progressing != nil &&
			progressing.Status == metav1.ConditionTrue &&
			progressing.Reason == conditionReasonDeploymentProgressing &&
			ready != nil &&
			ready.Status == metav1.ConditionFalse &&
			ready.Reason == conditionReasonDeploymentProgressing
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
		degraded := apimeta.FindStatusCondition(current.Status.Conditions, conditionTypeDegraded)
		return degraded != nil &&
			degraded.Status == metav1.ConditionTrue &&
			degraded.Reason == conditionReasonExternalDatabaseSecretInvalid
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
		progressing := apimeta.FindStatusCondition(current.Status.Conditions, conditionTypeProgressing)
		ready := apimeta.FindStatusCondition(current.Status.Conditions, conditionTypeReady)
		return current.Status.ObservedGeneration == current.Generation &&
			progressing != nil &&
			progressing.Status == metav1.ConditionTrue &&
			progressing.Reason == conditionReasonDeploymentProgressing &&
			ready != nil &&
			ready.Status == metav1.ConditionFalse &&
			ready.Reason == conditionReasonDeploymentProgressing
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
