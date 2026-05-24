package cardanodbsync

import (
	"context"
	"strings"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const testNetworkArtifactSchemaVersion = "yacd.meigma.io/cardano-network-artifacts/v1alpha1"

var testNetworkArtifactDataHash = "sha256:" + strings.Repeat("a", 64)

const driftedDBSyncConfig = "drifted"

func TestCardanoDBSyncReconcilerReconcileHandlesMissingObject(t *testing.T) {
	ctx := context.Background()
	reconciler := newTestReconciler(t)

	result, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKey{Namespace: "default", Name: "missing"}})

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestCardanoDBSyncReconcilerReconcileSkipsTerminatingObject(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("terminating", "devnet")
	now := metav1.Now()
	dbSync.DeletionTimestamp = &now
	dbSync.Finalizers = []string{"test.yacd.meigma.io/finalizer"}
	reconciler := newTestReconciler(t, dbSync)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Empty(t, result)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	assert.Empty(t, current.Status.Conditions)
}

func TestCardanoDBSyncReconcilerReconcileReportsUnsupportedManagedDatabase(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "devnet")
	dbSync.Spec.Database.External = nil
	dbSync.Spec.Database.Managed = &yacdv1alpha1.CardanoDBSyncManagedDatabaseSpec{
		Image:    "postgres:17.2-alpine",
		Database: "cexplorer",
		User:     "postgres",
	}
	reconciler := newTestReconciler(t, dbSync)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedDatabaseMode)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonUnsupportedDatabaseMode)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonUnsupportedDatabaseMode)
}

func TestCardanoDBSyncReconcilerReconcileReportsMissingExternalDatabaseSecret(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "devnet")
	reconciler := newTestReconciler(t, dbSync)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonExternalDatabaseSecretMissing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonExternalDatabaseSecretMissing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonExternalDatabaseSecretMissing)
}

func TestCardanoDBSyncReconcilerReconcileReportsExternalDatabaseSecretMissingKey(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "devnet")
	secret := externalDatabaseSecretFor(dbSync)
	secret.Data = map[string][]byte{"other": []byte("secret")}
	reconciler := newTestReconciler(t, dbSync, secret)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonExternalDatabaseSecretInvalid)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonExternalDatabaseSecretInvalid)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonExternalDatabaseSecretInvalid)
}

func TestCardanoDBSyncReconcilerReconcileReportsMissingNetwork(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "missing")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonNetworkUnavailable)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonNetworkUnavailable)
}

func TestCardanoDBSyncReconcilerReconcileReportsDeletingNetwork(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "deleting-network")
	network := readyCardanoNetwork("deleting-network")
	now := metav1.Now()
	network.DeletionTimestamp = &now
	network.Finalizers = []string{"test.yacd.meigma.io/finalizer"}
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonNetworkUnavailable)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonNetworkUnavailable)
}

func TestCardanoDBSyncReconcilerReconcileWaitsForFreshNetworkStatus(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "stale-network")
	network := readyCardanoNetwork("stale-network")
	network.Generation = 2
	network.Status.ObservedGeneration = 1
	network.Status.Conditions[0].ObservedGeneration = 1
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertDependencyWaiting(t, ctx, reconciler, dbSync, conditionReasonNetworkStatusStale)
}

func TestCardanoDBSyncReconcilerReconcileWaitsForNetworkArtifactsReady(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "pending-artifacts")
	network := readyCardanoNetwork("pending-artifacts")
	network.Status.Conditions = []metav1.Condition{{
		Type:               "ArtifactsReady",
		Status:             metav1.ConditionFalse,
		Reason:             "ArtifactsPending",
		Message:            "artifacts are pending",
		ObservedGeneration: network.Generation,
		LastTransitionTime: metav1.Now(),
	}}
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertDependencyWaiting(t, ctx, reconciler, dbSync, conditionReasonNetworkArtifactsPending)
}

func TestCardanoDBSyncReconcilerReconcileWaitsForArtifactStatusFields(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "missing-artifact-status")
	network := readyCardanoNetwork("missing-artifact-status")
	network.Status.Artifacts = nil
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertDependencyWaiting(t, ctx, reconciler, dbSync, conditionReasonNetworkArtifactsPending)
}

func TestCardanoDBSyncReconcilerReconcileWaitsForArtifactConfigMap(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "missing-configmap")
	network := readyCardanoNetwork("missing-configmap")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertDependencyWaiting(t, ctx, reconciler, dbSync, conditionReasonNetworkArtifactsPending)
}

func TestCardanoDBSyncReconcilerReconcileWaitsForMatchingArtifactConfigMapMetadata(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "mismatched-configmap")
	network := readyCardanoNetwork("mismatched-configmap")
	configMap := artifactConfigMapFor(network)
	configMap.Annotations[networkArtifactDataHashAnno] = "sha256:" + strings.Repeat("b", 64)
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, configMap)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertDependencyWaiting(t, ctx, reconciler, dbSync, conditionReasonNetworkArtifactsMismatch)
}

func TestCardanoDBSyncReconcilerReconcileWaitsForNodeToNodeEndpoint(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "missing-node-endpoint")
	network := readyCardanoNetwork("missing-node-endpoint")
	network.Status.Endpoints = nil
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertDependencyWaiting(t, ctx, reconciler, dbSync, conditionReasonNodeToNodeEndpointMissing)
}

func TestCardanoDBSyncReconcilerReconcileAppliesExternalDatabaseWorkloads(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonWorkloadsApplied)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonWorkloadsApplied)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionFalse, conditionReasonWorkloadsApplied)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionFalse, conditionReasonExternalDatabaseNotProbed)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionFalse, conditionReasonWorkloadsApplied)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSynced, metav1.ConditionFalse, conditionReasonWorkloadsApplied)

	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Endpoints)
	require.NotNil(t, current.Status.Endpoints.Metrics)
	assert.Equal(t, "dbsync-dbsync-metrics", current.Status.Endpoints.Metrics.ServiceName)
	assert.Equal(t, int32(8080), current.Status.Endpoints.Metrics.Port)
	assert.Equal(t, "http://dbsync-dbsync-metrics.default.svc.cluster.local:8080/metrics", current.Status.Endpoints.Metrics.URL)

	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, &corev1.ConfigMap{}))
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncStatePVCName(dbSync)}, &corev1.PersistentVolumeClaim{}))
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{}))
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncMetricsServiceName(dbSync)}, &corev1.Service{}))
}

func TestCardanoDBSyncReconcilerReconcileRepairsOwnedConfigMapDrift(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)

	configMap := &corev1.ConfigMap{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, configMap))
	configMap.Data[dbSyncConfigFileName] = driftedDBSyncConfig
	require.NoError(t, reconciler.Update(ctx, configMap))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	repaired := &corev1.ConfigMap{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, repaired))
	assert.NotEqual(t, driftedDBSyncConfig, repaired.Data[dbSyncConfigFileName])
	assert.Contains(t, repaired.Data[dbSyncConfigFileName], "NetworkName: ready-network")
}

func TestCardanoDBSyncReconcilerReconcileReportsResourceConflict(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	conflictingConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncConfigMapName(dbSync),
			Namespace: dbSync.Namespace,
		},
	}
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network), conflictingConfigMap)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.NotZero(t, result.RequeueAfter)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonResourceConflict)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonResourceConflict)
}

func assertDependencyWaiting(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	dbSync *yacdv1alpha1.CardanoDBSync,
	reason string,
) {
	t.Helper()

	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, reason)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, reason)
}

func assertCondition(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	dbSync *yacdv1alpha1.CardanoDBSync,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
) {
	t.Helper()

	current := requireDBSync(t, ctx, reconciler, dbSync)
	condition := apimeta.FindStatusCondition(current.Status.Conditions, conditionType)
	require.NotNil(t, condition, "expected condition %s", conditionType)
	assert.Equal(t, status, condition.Status)
	assert.Equal(t, reason, condition.Reason)
	assert.Equal(t, current.Generation, condition.ObservedGeneration)
	assert.Equal(t, current.Generation, current.Status.ObservedGeneration)
}

func newTestReconciler(t *testing.T, objects ...client.Object) *CardanoDBSyncReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&yacdv1alpha1.CardanoDBSync{}, &yacdv1alpha1.CardanoNetwork{})
	builder.WithObjects(objects...)
	fakeClient := builder.Build()

	return &CardanoDBSyncReconciler{
		Client: fakeClient,
		Reader: fakeClient,
		Scheme: scheme,
	}
}

func requireDBSync(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	dbSync *yacdv1alpha1.CardanoDBSync,
) *yacdv1alpha1.CardanoDBSync {
	t.Helper()

	current := &yacdv1alpha1.CardanoDBSync{}
	require.NoError(t, reconciler.Get(ctx, reconcileRequestFor(dbSync).NamespacedName, current))

	return current
}

func reconcileRequestFor(object client.Object) reconcile.Request {
	return reconcile.Request{NamespacedName: client.ObjectKeyFromObject(object)}
}

func localCardanoDBSync(name string, networkName string) *yacdv1alpha1.CardanoDBSync {
	return &yacdv1alpha1.CardanoDBSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 1,
		},
		Spec: yacdv1alpha1.CardanoDBSyncSpec{
			NetworkRef: yacdv1alpha1.CardanoDBSyncNetworkReference{Name: networkName},
			Image:      "ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0",
			Database: yacdv1alpha1.CardanoDBSyncDatabaseSpec{
				External: &yacdv1alpha1.CardanoDBSyncExternalDatabaseSpec{
					Host:     "postgres.default.svc.cluster.local",
					Port:     5432,
					Database: "cexplorer",
					User:     "postgres",
					PasswordSecretRef: yacdv1alpha1.CardanoDBSyncSecretKeyReference{
						Name: name + "-postgres",
						Key:  "password",
					},
					SSLMode: yacdv1alpha1.CardanoDBSyncPostgresSSLModeDisable,
				},
			},
			Config: yacdv1alpha1.CardanoDBSyncConfigSpec{
				LedgerBackend: yacdv1alpha1.CardanoDBSyncLedgerBackendLSM,
			},
		},
	}
}

func externalDatabaseSecretFor(dbSync *yacdv1alpha1.CardanoDBSync) *corev1.Secret {
	secretName := dbSync.Spec.Database.External.PasswordSecretRef.Name
	secretKey := externalDatabasePasswordKey(dbSync.Spec.Database.External)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: dbSync.Namespace,
		},
		Data: map[string][]byte{
			secretKey: []byte("secret"),
		},
	}
}

func readyCardanoNetwork(name string) *yacdv1alpha1.CardanoNetwork {
	return &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 1,
		},
		Spec: yacdv1alpha1.CardanoNetworkSpec{
			Node: yacdv1alpha1.CardanoNodeSpec{
				Version: "11.0.1",
			},
		},
		Status: yacdv1alpha1.CardanoNetworkStatus{
			ObservedGeneration: 1,
			Artifacts: &yacdv1alpha1.CardanoNetworkArtifactsStatus{
				NetworkConfigMapName: name + "-network-artifacts",
				SchemaVersion:        testNetworkArtifactSchemaVersion,
				DataHash:             testNetworkArtifactDataHash,
			},
			Endpoints: &yacdv1alpha1.CardanoNetworkEndpointsStatus{
				NodeToNode: &yacdv1alpha1.ServiceEndpointStatus{
					ServiceName: name + "-node",
					Port:        3001,
					URL:         "tcp://" + name + "-node.default.svc.cluster.local:3001",
				},
			},
			Conditions: []metav1.Condition{{
				Type:               "ArtifactsReady",
				Status:             metav1.ConditionTrue,
				Reason:             "ArtifactsReady",
				Message:            "artifacts are ready",
				ObservedGeneration: 1,
				LastTransitionTime: metav1.Now(),
			}},
		},
	}
}

func artifactConfigMapFor(network *yacdv1alpha1.CardanoNetwork) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.Status.Artifacts.NetworkConfigMapName,
			Namespace: network.Namespace,
			Annotations: map[string]string{
				networkArtifactSchemaVersionAnno: network.Status.Artifacts.SchemaVersion,
				networkArtifactDataHashAnno:      network.Status.Artifacts.DataHash,
			},
		},
	}
}
