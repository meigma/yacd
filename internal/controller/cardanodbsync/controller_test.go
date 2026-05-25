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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const testNetworkArtifactSchemaVersion = "yacd.meigma.io/cardano-network-artifacts/v1alpha1"

var testNetworkArtifactDataHash = computeNetworkArtifactDataHash(testNetworkArtifactsData())

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

func TestCardanoDBSyncReconcilerReconcileAppliesManagedPostgresAndGatesDBSyncWorkload(t *testing.T) {
	ctx := context.Background()
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, network, artifactConfigMapFor(network))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionFalse, conditionReasonWorkloadMissing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionFalse, conditionReasonWorkloadMissing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSynced, metav1.ConditionFalse, conditionReasonSyncNotProbed)

	authSecret := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresAuthSecretName(dbSync)}, authSecret))
	require.Len(t, authSecret.Data[managedPostgresPasswordKey], 43)
	firstPassword := string(authSecret.Data[managedPostgresPasswordKey])
	assert.NotEmpty(t, authSecret.Annotations[managedPostgresPasswordFingerprintAnno])
	assert.True(t, controlledBy(authSecret, dbSync))
	postgresPVC := &corev1.PersistentVolumeClaim{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresPVCName(dbSync)}, postgresPVC))
	assert.NotEmpty(t, postgresPVC.Annotations[managedPostgresIdentityAnno])
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresServiceName(dbSync)}, &corev1.Service{}))
	postgresDeployment := requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	assert.Equal(t, defaultManagedPostgresImage, requireContainerSpec(t, postgresDeployment, managedPostgresContainerName).Image)
	assert.NotEmpty(t, postgresDeployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])

	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Endpoints)
	require.NotNil(t, current.Status.Endpoints.Postgres)
	assert.Equal(t, managedPostgresServiceName(dbSync), current.Status.Endpoints.Postgres.ServiceName)
	assert.Equal(t, int32(5432), current.Status.Endpoints.Postgres.Port)
	assert.Equal(t, "postgres://dbsync-postgres.default.svc.cluster.local:5432/cexplorer", current.Status.Endpoints.Postgres.URL)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, managedPostgresAuthSecretName(dbSync), current.Status.Database.AuthSecretName)
	assert.Empty(t, current.Status.Database.AcceptedIdentityFingerprint)

	err = reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{})
	require.True(t, apierrors.IsNotFound(err), "expected missing db-sync Deployment, got %v", err)

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	authSecret = &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresAuthSecretName(dbSync)}, authSecret))
	assert.Equal(t, firstPassword, string(authSecret.Data[managedPostgresPasswordKey]))

	postgresDeployment = requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	markManagedPostgresDeploymentAvailable(t, ctx, reconciler, postgresDeployment)
	markManagedPostgresPodReady(t, ctx, reconciler, dbSync)

	result, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionTrue, conditionReasonPostgresReady)
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{}))
	pgpass := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncPGPassSecretName(dbSync)}, pgpass))
	assert.Equal(t, "dbsync-postgres.default.svc.cluster.local:5432:cexplorer:postgres:"+firstPassword+"\n", string(pgpass.Data[dbSyncPGPassFileName]))
	current = requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	assert.NotEmpty(t, current.Status.Database.AcceptedIdentityFingerprint)
	assert.Equal(t, managedPostgresAuthSecretName(dbSync), current.Status.Database.AuthSecretName)
}

func TestCardanoDBSyncReconcilerReconcileUsesProvidedManagedPostgresAuthSecret(t *testing.T) {
	ctx := context.Background()
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	dbSync.Spec.Database.Managed.AuthSecretRef = &yacdv1alpha1.CardanoDBSyncSecretReference{Name: "provided-postgres-auth"}
	authSecret := providedManagedPostgresAuthSecretFor(dbSync)
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, authSecret, network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	currentSecret := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKeyFromObject(authSecret), currentSecret))
	assert.Empty(t, currentSecret.OwnerReferences)
	assert.Equal(t, "provided-secret", string(currentSecret.Data[managedPostgresPasswordKey]))
	current := requireDBSync(t, ctx, reconciler, dbSync)
	if current.Status.Database != nil {
		assert.Empty(t, current.Status.Database.AuthSecretName)
	}
	deployment := requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	postgres := requireContainerSpec(t, deployment, managedPostgresContainerName)
	assert.Equal(t, "provided-postgres-auth", requireEnvVar(t, postgres, "POSTGRES_PASSWORD").ValueFrom.SecretKeyRef.Name)
}

func TestCardanoDBSyncReconcilerReconcileRejectsProvidedManagedPostgresAuthSecretChange(t *testing.T) {
	ctx := context.Background()
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	dbSync.Spec.Database.Managed.AuthSecretRef = &yacdv1alpha1.CardanoDBSyncSecretReference{Name: "provided-postgres-auth"}
	authSecret := providedManagedPostgresAuthSecretFor(dbSync)
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, authSecret, network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	deployment := requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	acceptedIdentity := deployment.Spec.Template.Annotations[managedPostgresIdentityAnno]
	require.NotEmpty(t, acceptedIdentity)

	replacementSecret := providedManagedPostgresAuthSecretFor(dbSync)
	replacementSecret.Name = "replacement-postgres-auth"
	require.NoError(t, reconciler.Create(ctx, replacementSecret))
	current := requireDBSync(t, ctx, reconciler, dbSync)
	current.Spec.Database.Managed.AuthSecretRef = &yacdv1alpha1.CardanoDBSyncSecretReference{Name: replacementSecret.Name}
	current.Generation = 2
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedDatabaseIdentityChange)
	deployment = requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	assert.Equal(t, acceptedIdentity, deployment.Spec.Template.Annotations[managedPostgresIdentityAnno])
	postgres := requireContainerSpec(t, deployment, managedPostgresContainerName)
	assert.Equal(t, "provided-postgres-auth", requireEnvVar(t, postgres, "POSTGRES_PASSWORD").ValueFrom.SecretKeyRef.Name)
}

func TestCardanoDBSyncReconcilerReconcileReportsInvalidProvidedManagedPostgresAuthSecret(t *testing.T) {
	ctx := context.Background()
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	dbSync.Spec.Database.Managed.AuthSecretRef = &yacdv1alpha1.CardanoDBSyncSecretReference{Name: "provided-postgres-auth"}
	authSecret := providedManagedPostgresAuthSecretFor(dbSync)
	authSecret.Data = map[string][]byte{"other": []byte("secret")}
	reconciler := newTestReconciler(t, dbSync, authSecret)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonManagedDatabaseSecretInvalid)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonManagedDatabaseSecretInvalid)
	currentSecret := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKeyFromObject(authSecret), currentSecret))
	assert.Empty(t, currentSecret.OwnerReferences)
}

func TestCardanoDBSyncReconcilerReconcileReportsUnsupportedOnlyUTxOPresetWithLSM(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	dbSync.Spec.Config.Insert = &yacdv1alpha1.CardanoDBSyncInsertSpec{
		Preset: yacdv1alpha1.CardanoDBSyncInsertPresetOnlyUTxO,
	}
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
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

func TestCardanoDBSyncReconcilerReconcileReportsExternalDatabaseSecretNewlinePassword(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "devnet")
	secret := externalDatabaseSecretFor(dbSync)
	secret.Data["password"] = []byte("line-one\nline-two")
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

func TestCardanoDBSyncReconcilerReconcileWaitsForValidArtifactConfigMapData(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "invalid-configmap-data")
	network := readyCardanoNetwork("invalid-configmap-data")
	configMap := artifactConfigMapFor(network)
	delete(configMap.Data, "configuration.yaml")
	configMap.Annotations[networkArtifactDataHashAnno] = computeNetworkArtifactDataHash(configMap.Data)
	network.Status.Artifacts.DataHash = configMap.Annotations[networkArtifactDataHashAnno]
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

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionFalse, conditionReasonExternalDatabaseNotProbed)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSynced, metav1.ConditionFalse, conditionReasonSyncNotProbed)

	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Endpoints)
	require.NotNil(t, current.Status.Endpoints.Postgres)
	assert.Equal(t, int32(5432), current.Status.Endpoints.Postgres.Port)
	assert.Equal(t, "postgres://postgres.default.svc.cluster.local:5432/cexplorer", current.Status.Endpoints.Postgres.URL)
	require.NotNil(t, current.Status.Endpoints.Metrics)
	assert.Equal(t, "dbsync-dbsync-metrics", current.Status.Endpoints.Metrics.ServiceName)
	assert.Equal(t, int32(8080), current.Status.Endpoints.Metrics.Port)
	assert.Equal(t, "http://dbsync-dbsync-metrics.default.svc.cluster.local:8080/metrics", current.Status.Endpoints.Metrics.URL)
	require.NotNil(t, current.Status.Database)
	assert.NotEmpty(t, current.Status.Database.AcceptedIdentityFingerprint)

	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, &corev1.ConfigMap{}))
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncPGPassSecretName(dbSync)}, &corev1.Secret{}))
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncStatePVCName(dbSync)}, &corev1.PersistentVolumeClaim{}))
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncFollowerPVCName(dbSync)}, &corev1.PersistentVolumeClaim{}))
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{}))
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncMetricsServiceName(dbSync)}, &corev1.Service{}))
}

func TestCardanoDBSyncReconcilerReconcileReportsRuntimeReadyContainers(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	deployment := requireDBSyncDeployment(t, ctx, reconciler, dbSync)
	markDBSyncDeploymentAvailable(t, ctx, reconciler, deployment)
	markDBSyncPodContainersReady(t, ctx, reconciler, dbSync, followerNodeContainerName, dbSyncContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Empty(t, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonRuntimeProbesPending)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonRuntimeProbesPending)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionTrue, conditionReasonFollowerNodeReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionFalse, conditionReasonExternalDatabaseNotProbed)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionTrue, conditionReasonDBSyncReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSynced, metav1.ConditionFalse, conditionReasonSyncNotProbed)
}

func TestCardanoDBSyncReconcilerReconcileKeepsFollowerAndDBSyncReadinessSeparate(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	deployment := requireDBSyncDeployment(t, ctx, reconciler, dbSync)
	markDBSyncDeploymentAvailable(t, ctx, reconciler, deployment)
	markDBSyncPodContainersReady(t, ctx, reconciler, dbSync, followerNodeContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionTrue, conditionReasonFollowerNodeReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
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

func TestCardanoDBSyncReconcilerReconcileRepairsManagedPostgresChildDrift(t *testing.T) {
	ctx := context.Background()
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)

	authSecret := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresAuthSecretName(dbSync)}, authSecret))
	delete(authSecret.Labels, labelAppManagedBy)
	require.NoError(t, reconciler.Update(ctx, authSecret))
	service := &corev1.Service{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresServiceName(dbSync)}, service))
	service.Spec.Ports[0].Port = 15432
	require.NoError(t, reconciler.Update(ctx, service))
	deployment := requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	for i := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[i].Name == managedPostgresContainerName {
			deployment.Spec.Template.Spec.Containers[i].Image = "postgres:old"
		}
	}
	require.NoError(t, reconciler.Update(ctx, deployment))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresAuthSecretName(dbSync)}, authSecret))
	assert.Equal(t, "yacd", authSecret.Labels[labelAppManagedBy])
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresServiceName(dbSync)}, service))
	assert.Equal(t, managedPostgresPort, service.Spec.Ports[0].Port)
	deployment = requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	assert.Equal(t, defaultManagedPostgresImage, requireContainerSpec(t, deployment, managedPostgresContainerName).Image)
}

func TestCardanoDBSyncReconcilerReconcileRejectsManagedPostgresPVCDrift(t *testing.T) {
	ctx := context.Background()
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	storageClass := "fast"
	dbSync.Spec.Database.Managed.Storage = &yacdv1alpha1.CardanoDBSyncStorageSpec{
		StorageClassName: &storageClass,
	}
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)

	current := requireDBSync(t, ctx, reconciler, dbSync)
	smaller := resource.MustParse("5Gi")
	current.Spec.Database.Managed.Storage = &yacdv1alpha1.CardanoDBSyncStorageSpec{
		Size:             &smaller,
		StorageClassName: &storageClass,
	}
	current.Generation = 2
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedStorageChange)

	current = requireDBSync(t, ctx, reconciler, dbSync)
	slowStorageClass := "slow"
	current.Spec.Database.Managed.Storage = &yacdv1alpha1.CardanoDBSyncStorageSpec{
		StorageClassName: &slowStorageClass,
	}
	current.Generation = 3
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedStorageChange)
}

func TestCardanoDBSyncReconcilerReconcileRejectsManagedPostgresIdentityMutationBeforeApply(t *testing.T) {
	ctx := context.Background()
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	postgresDeployment := requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	acceptedImage := requireContainerSpec(t, postgresDeployment, managedPostgresContainerName).Image
	acceptedIdentity := postgresDeployment.Spec.Template.Annotations[managedPostgresIdentityAnno]
	require.NotEmpty(t, acceptedIdentity)

	current := requireDBSync(t, ctx, reconciler, dbSync)
	current.Spec.Database.Managed.Image = "postgres:17.3-alpine"
	current.Generation = 2
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedDatabaseIdentityChange)
	postgresDeployment = requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	assert.Equal(t, acceptedImage, requireContainerSpec(t, postgresDeployment, managedPostgresContainerName).Image)
	assert.Equal(t, acceptedIdentity, postgresDeployment.Spec.Template.Annotations[managedPostgresIdentityAnno])
	err = reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{})
	require.True(t, apierrors.IsNotFound(err), "expected missing db-sync Deployment, got %v", err)
}

func TestCardanoDBSyncReconcilerReconcileRejectsGeneratedManagedPostgresPasswordMutation(t *testing.T) {
	ctx := context.Background()
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	postgresDeployment := requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	markManagedPostgresDeploymentAvailable(t, ctx, reconciler, postgresDeployment)
	markManagedPostgresPodReady(t, ctx, reconciler, dbSync)
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)

	pgpass := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncPGPassSecretName(dbSync)}, pgpass))
	acceptedPGPass := string(pgpass.Data[dbSyncPGPassFileName])
	postgresDeployment = requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	acceptedPostgresVersion := postgresDeployment.Spec.Template.Annotations[dbSyncSecretVersionAnno]
	dbSyncDeployment := requireDBSyncDeployment(t, ctx, reconciler, dbSync)
	acceptedDBSyncVersion := dbSyncDeployment.Spec.Template.Annotations[dbSyncSecretVersionAnno]
	authSecret := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresAuthSecretName(dbSync)}, authSecret))
	authSecret.Data[managedPostgresPasswordKey] = []byte("rotated-password")
	require.NoError(t, reconciler.Update(ctx, authSecret))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedDatabaseIdentityChange)
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncPGPassSecretName(dbSync)}, pgpass))
	assert.Equal(t, acceptedPGPass, string(pgpass.Data[dbSyncPGPassFileName]))
	postgresDeployment = requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	assert.Equal(t, acceptedPostgresVersion, postgresDeployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])
	dbSyncDeployment = requireDBSyncDeployment(t, ctx, reconciler, dbSync)
	assert.Equal(t, acceptedDBSyncVersion, dbSyncDeployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 0)
}

func TestCardanoDBSyncReconcilerReconcilePreservesRuntimeStatusWhenManagedPostgresRegresses(t *testing.T) {
	ctx := context.Background()
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	postgresDeployment := requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	markManagedPostgresDeploymentAvailable(t, ctx, reconciler, postgresDeployment)
	markManagedPostgresPodReady(t, ctx, reconciler, dbSync)
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)

	dbSyncDeployment := requireDBSyncDeployment(t, ctx, reconciler, dbSync)
	markDBSyncDeploymentAvailable(t, ctx, reconciler, dbSyncDeployment)
	markDBSyncPodContainersReady(t, ctx, reconciler, dbSync, followerNodeContainerName, dbSyncContainerName)
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	acceptedIdentity := current.Status.Database.AcceptedIdentityFingerprint
	require.NotEmpty(t, acceptedIdentity)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionTrue, conditionReasonFollowerNodeReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionTrue, conditionReasonDBSyncReady)

	postgresDeployment = requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	markDeploymentUnavailable(t, ctx, reconciler, postgresDeployment)

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionTrue, conditionReasonFollowerNodeReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionTrue, conditionReasonDBSyncReady)
	current = requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, acceptedIdentity, current.Status.Database.AcceptedIdentityFingerprint)
}

func TestCardanoDBSyncReconcilerReconcileRejectsDatabaseIdentityMutation(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	acceptedIdentity := current.Status.Database.AcceptedIdentityFingerprint
	require.NotEmpty(t, acceptedIdentity)
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 1)
	configMap := &corev1.ConfigMap{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, configMap))
	acceptedPlan := configMap.Annotations[dbSyncPlanFingerprintAnno]

	current.Spec.Database.External.Database = "other"
	current.Generation = 2
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedDatabaseIdentityChange)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonUnsupportedDatabaseIdentityChange)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonUnsupportedDatabaseIdentityChange)
	current = requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, acceptedIdentity, current.Status.Database.AcceptedIdentityFingerprint)
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, configMap))
	assert.Equal(t, acceptedPlan, configMap.Annotations[dbSyncPlanFingerprintAnno])
	assert.Equal(t, acceptedIdentity, configMap.Annotations[dbSyncDatabaseIdentityAnno])
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 0)
}

func TestCardanoDBSyncReconcilerReconcileRejectsImageMutation(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	acceptedIdentity := current.Status.Database.AcceptedIdentityFingerprint
	require.NotEmpty(t, acceptedIdentity)
	configMap := &corev1.ConfigMap{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, configMap))
	acceptedPlan := configMap.Annotations[dbSyncPlanFingerprintAnno]

	current.Spec.Image = "ghcr.io/intersectmbo/cardano-db-sync:13.8.0.0"
	current.Generation = 2
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedDatabaseIdentityChange)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonUnsupportedDatabaseIdentityChange)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonUnsupportedDatabaseIdentityChange)
	current = requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, acceptedIdentity, current.Status.Database.AcceptedIdentityFingerprint)
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, configMap))
	assert.Equal(t, acceptedPlan, configMap.Annotations[dbSyncPlanFingerprintAnno])
	assert.Equal(t, acceptedIdentity, configMap.Annotations[dbSyncDatabaseIdentityAnno])
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 0)
}

func TestCardanoDBSyncReconcilerReconcileAllowsRuntimeOnlyMutation(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	acceptedIdentity := current.Status.Database.AcceptedIdentityFingerprint
	configMap := &corev1.ConfigMap{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, configMap))
	acceptedPlan := configMap.Annotations[dbSyncPlanFingerprintAnno]

	current.Spec.Config.Runtime = &yacdv1alpha1.CardanoDBSyncRuntimeSpec{
		Cache:        true,
		EpochTable:   true,
		ForceIndexes: true,
		MetricsPort:  8080,
	}
	current.Generation = 2
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, configMap))
	assert.NotEqual(t, acceptedPlan, configMap.Annotations[dbSyncPlanFingerprintAnno])
	assert.Equal(t, acceptedIdentity, configMap.Annotations[dbSyncDatabaseIdentityAnno])
	deployment := requireDBSyncDeployment(t, ctx, reconciler, dbSync)
	assert.Contains(t, requireContainerSpec(t, deployment, dbSyncContainerName).Args, "--force-indexes")
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

func TestCardanoDBSyncReconcilerReconcileSuspendsWorkloadWhenSecretBecomesInvalid(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	secret := externalDatabaseSecretFor(dbSync)
	reconciler := newTestReconciler(t, dbSync, secret, network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 1)

	secret.Data = map[string][]byte{"other": []byte("secret")}
	require.NoError(t, reconciler.Update(ctx, secret))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonExternalDatabaseSecretInvalid)
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 0)
}

func TestCardanoDBSyncReconcilerReconcileSuspendsWorkloadWhenArtifactsMismatch(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	configMap := artifactConfigMapFor(network)
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, configMap)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 1)

	configMap.Annotations[networkArtifactDataHashAnno] = "sha256:" + strings.Repeat("b", 64)
	require.NoError(t, reconciler.Update(ctx, configMap))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonNetworkArtifactsMismatch)
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 0)
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

func assertDeploymentReplicas(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	dbSync *yacdv1alpha1.CardanoDBSync,
	want int32,
) {
	t.Helper()

	deployment := &appsv1.Deployment{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, deployment))
	require.NotNil(t, deployment.Spec.Replicas)
	assert.Equal(t, want, *deployment.Spec.Replicas)
}

func requireDBSyncDeployment(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	dbSync *yacdv1alpha1.CardanoDBSync,
) *appsv1.Deployment {
	t.Helper()

	deployment := &appsv1.Deployment{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, deployment))
	return deployment
}

func requireManagedPostgresDeployment(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	dbSync *yacdv1alpha1.CardanoDBSync,
) *appsv1.Deployment {
	t.Helper()

	deployment := &appsv1.Deployment{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresDeploymentName(dbSync)}, deployment))
	return deployment
}

func requireContainerSpec(t *testing.T, deployment *appsv1.Deployment, name string) corev1.Container {
	t.Helper()

	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == name {
			return container
		}
	}
	require.Failf(t, "missing container", "expected container %s", name)
	return corev1.Container{}
}

func markDBSyncDeploymentAvailable(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	deployment *appsv1.Deployment,
) {
	t.Helper()

	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 1
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 1
	deployment.Status.AvailableReplicas = 1
	deployment.Status.Conditions = []appsv1.DeploymentCondition{{
		Type:               appsv1.DeploymentAvailable,
		Status:             corev1.ConditionTrue,
		Reason:             "MinimumReplicasAvailable",
		Message:            "Deployment has minimum availability.",
		LastUpdateTime:     metav1.Now(),
		LastTransitionTime: metav1.Now(),
	}}
	require.NoError(t, reconciler.Status().Update(ctx, deployment))
}

func markDeploymentUnavailable(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	deployment *appsv1.Deployment,
) {
	t.Helper()

	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 1
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 0
	deployment.Status.AvailableReplicas = 0
	deployment.Status.Conditions = []appsv1.DeploymentCondition{{
		Type:               appsv1.DeploymentAvailable,
		Status:             corev1.ConditionFalse,
		Reason:             "MinimumReplicasUnavailable",
		Message:            "Deployment does not have minimum availability.",
		LastUpdateTime:     metav1.Now(),
		LastTransitionTime: metav1.Now(),
	}}
	require.NoError(t, reconciler.Status().Update(ctx, deployment))
}

func markDBSyncPodContainersReady(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	dbSync *yacdv1alpha1.CardanoDBSync,
	containerNames ...string,
) {
	t.Helper()

	containers := make([]corev1.Container, 0, len(containerNames))
	containerStatuses := make([]corev1.ContainerStatus, 0, len(containerNames))
	for _, containerName := range containerNames {
		containers = append(containers, corev1.Container{Name: containerName, Image: "example.com/" + containerName + ":test"})
		containerStatuses = append(containerStatuses, corev1.ContainerStatus{
			Name:  containerName,
			Ready: true,
			State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.Now(),
				},
			},
		})
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncWorkloadName(dbSync) + "-pod",
			Namespace: dbSync.Namespace,
			Labels:    dbSyncWorkloadSelectorLabels(dbSync),
		},
		Spec: corev1.PodSpec{
			Containers: containers,
		},
	}
	require.NoError(t, reconciler.Create(ctx, pod))
	pod.Status.Phase = corev1.PodRunning
	pod.Status.ContainerStatuses = containerStatuses
	require.NoError(t, reconciler.Status().Update(ctx, pod))
}

func markManagedPostgresDeploymentAvailable(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	deployment *appsv1.Deployment,
) {
	t.Helper()

	markDBSyncDeploymentAvailable(t, ctx, reconciler, deployment)
}

func markManagedPostgresPodReady(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	dbSync *yacdv1alpha1.CardanoDBSync,
) {
	t.Helper()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedPostgresDeploymentName(dbSync) + "-pod",
			Namespace: dbSync.Namespace,
			Labels:    managedPostgresSelectorLabels(dbSync),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  managedPostgresContainerName,
				Image: defaultManagedPostgresImage,
			}},
		},
	}
	require.NoError(t, reconciler.Create(ctx, pod))
	pod.Status.Phase = corev1.PodRunning
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name:  managedPostgresContainerName,
		Ready: true,
		State: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{
				StartedAt: metav1.Now(),
			},
		},
	}}
	require.NoError(t, reconciler.Status().Update(ctx, pod))
}

func newTestReconciler(t *testing.T, objects ...client.Object) *CardanoDBSyncReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&yacdv1alpha1.CardanoDBSync{}, &yacdv1alpha1.CardanoNetwork{}, &appsv1.Deployment{}, &corev1.Pod{})
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

func managedCardanoDBSync(name string, networkName string) *yacdv1alpha1.CardanoDBSync {
	dbSync := localCardanoDBSync(name, networkName)
	dbSync.Spec.Database.External = nil
	dbSync.Spec.Database.Managed = &yacdv1alpha1.CardanoDBSyncManagedDatabaseSpec{
		Image:    defaultManagedPostgresImage,
		Database: defaultManagedPostgresDatabase,
		User:     defaultManagedPostgresUser,
	}
	return dbSync
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

func providedManagedPostgresAuthSecretFor(dbSync *yacdv1alpha1.CardanoDBSync) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSync.Spec.Database.Managed.AuthSecretRef.Name,
			Namespace: dbSync.Namespace,
		},
		Data: map[string][]byte{
			managedPostgresPasswordKey: []byte("provided-secret"),
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
		Data: testNetworkArtifactsData(),
	}
}

func testNetworkArtifactsData() map[string]string {
	return map[string]string{
		"configuration.yaml":      "test configuration.yaml",
		"byron-genesis.json":      "test byron-genesis.json",
		"shelley-genesis.json":    "test shelley-genesis.json",
		"alonzo-genesis.json":     "test alonzo-genesis.json",
		"conway-genesis.json":     "test conway-genesis.json",
		"primary-topology.json":   "test primary-topology.json",
		"yacd-localnet-plan.json": "test yacd-localnet-plan.json",
		"connection.json":         "test connection.json",
	}
}
