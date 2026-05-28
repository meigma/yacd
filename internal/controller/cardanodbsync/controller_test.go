package cardanodbsync

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/meigma/yacd/internal/cardano/primarypod"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	ctrlnetworkartifacts "github.com/meigma/yacd/internal/controller/networkartifacts"
	ctrlartifacts "github.com/meigma/yacd/internal/ctrlkit/artifacts"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const testNetworkArtifactSchemaVersion = networkartifacts.SchemaVersion

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
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSidecarMaterialReady, metav1.ConditionFalse, conditionReasonDedicatedFollowerPlacement)
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
	assertPlacementStatus(t, current, yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, false)
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
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionTrue, conditionReasonPostgresReady)
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{}))
	metricsService := &corev1.Service{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncMetricsServiceName(dbSync)}, metricsService))
	assert.Equal(t, dbSyncWorkloadSelectorLabels(dbSync), metricsService.Spec.Selector)
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

func TestCardanoDBSyncReconcilerReconcileAllowsProvidedManagedPostgresAuthSecretChurn(t *testing.T) {
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
	acceptedSecretVersion := deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno]
	require.NotEmpty(t, acceptedIdentity)
	require.NotEmpty(t, acceptedSecretVersion)

	currentSecret := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKeyFromObject(authSecret), currentSecret))
	currentSecret.Labels = map[string]string{"external-controller": "touched"}
	currentSecret.Annotations = map[string]string{"external-controller": "touched"}
	currentSecret.Data["unrelated"] = []byte("changed")
	require.NoError(t, reconciler.Update(ctx, currentSecret))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	deployment = requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	assert.Equal(t, acceptedIdentity, deployment.Spec.Template.Annotations[managedPostgresIdentityAnno])
	assert.Equal(t, acceptedSecretVersion, deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])
}

func TestCardanoDBSyncReconcilerReconcileRejectsProvidedManagedPostgresPasswordMutation(t *testing.T) {
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
	acceptedSecretVersion := deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno]
	require.NotEmpty(t, acceptedIdentity)
	require.NotEmpty(t, acceptedSecretVersion)

	currentSecret := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKeyFromObject(authSecret), currentSecret))
	currentSecret.Data[managedPostgresPasswordKey] = []byte("rotated-password")
	require.NoError(t, reconciler.Update(ctx, currentSecret))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedDatabaseIdentityChange)
	deployment = requireManagedPostgresDeployment(t, ctx, reconciler, dbSync)
	assert.Equal(t, acceptedIdentity, deployment.Spec.Template.Annotations[managedPostgresIdentityAnno])
	assert.Equal(t, acceptedSecretVersion, deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])
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

func TestCardanoDBSyncReconcilerReconcileAppliesPrimarySidecarResources(t *testing.T) {
	ctx := context.Background()
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", "ready-network"))
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionFalse, conditionReasonPrimarySidecarPlacement)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeNodeSocketReady, metav1.ConditionFalse, conditionReasonWorkloadMissing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSidecarMaterialReady, metav1.ConditionTrue, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionFalse, conditionReasonWorkloadMissing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonWorkloadMissing)

	configMap := &corev1.ConfigMap{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, configMap))
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar), configMap.Annotations[dbSyncPlacementModeAnno])
	pgpass := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncPGPassSecretName(dbSync)}, pgpass))
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar), pgpass.Annotations[dbSyncPlacementModeAnno])
	statePVC := &corev1.PersistentVolumeClaim{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncStatePVCName(dbSync)}, statePVC))
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar), statePVC.Annotations[dbSyncPlacementModeAnno])
	metricsService := &corev1.Service{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncMetricsServiceName(dbSync)}, metricsService))
	for key, value := range primarypod.SelectorLabels(network) {
		assert.Equal(t, value, metricsService.Spec.Selector[key])
	}
	assert.Equal(t, "dbsync", metricsService.Spec.Selector[labelDBSync])
	assert.Len(t, metricsService.Spec.Selector, len(primarypod.SelectorLabels(network))+1)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	assertPlacementStatus(t, current, yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar, true)
	sidecar := current.Status.Placement.PrimarySidecar
	assert.Equal(t, network.Name, sidecar.NetworkName)
	assert.True(t, strings.HasPrefix(sidecar.Revision, "sha256:"))
	assert.Equal(t, dbSyncConfigMapName(dbSync), sidecar.Resources.ConfigMapName)
	assert.Equal(t, dbSyncPGPassSecretName(dbSync), sidecar.Resources.PGPassSecretName)
	assert.Equal(t, dbSyncStatePVCName(dbSync), sidecar.Resources.StatePVCName)
	assert.Equal(t, dbSyncMetricsServiceName(dbSync), sidecar.Resources.MetricsServiceName)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar, current.Status.Database.AcceptedPlacementMode)

	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{})
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncFollowerPVCName(dbSync)}, &corev1.PersistentVolumeClaim{})
}

func TestCardanoDBSyncReconcilerReconcilePrimarySidecarWaitsForDedicatedPodsToTerminate(t *testing.T) {
	ctx := context.Background()
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", "ready-network"))
	dbSync.UID = types.UID("dbsync-uid")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(
		t,
		dbSync,
		externalDatabaseSecretFor(dbSync),
		network,
		artifactConfigMapFor(network),
		ownedDedicatedDBSyncDeployment(dbSync, 1),
		runningDedicatedDBSyncPod(dbSync),
	)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, result)
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 0)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonPlacementTransitionPending)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonPlacementTransitionPending)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSidecarMaterialReady, metav1.ConditionFalse, conditionReasonPlacementTransitionPending)
	assertPlacementStatus(t, requireDBSync(t, ctx, reconciler, dbSync), yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar, false)
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, &corev1.ConfigMap{})
}

func TestCardanoDBSyncReconcilerReconcileDedicatedFollowerWaitsForPrimarySidecarPodsToTerminate(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(
		t,
		dbSync,
		externalDatabaseSecretFor(dbSync),
		network,
		artifactConfigMapFor(network),
		primaryNetworkDeploymentWithDBSyncSidecar(network),
		runningPrimaryNetworkPodWithDBSyncSidecar(network),
	)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncWorkloadReadinessRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonPlacementTransitionPending)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonPlacementTransitionPending)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSidecarMaterialReady, metav1.ConditionFalse, conditionReasonPlacementTransitionPending)
	assertPlacementStatus(t, requireDBSync(t, ctx, reconciler, dbSync), yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, false)
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{})
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, &corev1.ConfigMap{})
}

func TestCardanoDBSyncReconcilerReconcileRejectsPrimarySidecarToDedicatedAfterAcceptance(t *testing.T) {
	ctx := context.Background()
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", "ready-network"))
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar, current.Status.Database.AcceptedPlacementMode)

	current.Spec.Placement = &yacdv1alpha1.CardanoDBSyncPlacementSpec{
		Mode: yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower,
	}
	current.Generation = 2
	require.NoError(t, reconciler.Update(ctx, current))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Empty(t, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedDatabaseIdentityChange)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonUnsupportedDatabaseIdentityChange)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonUnsupportedDatabaseIdentityChange)
	assertDegradedMessage(t, ctx, reconciler, dbSync, `CardanoDBSync placement changed from accepted placement "primarySidecar" to "dedicatedFollower"; delete and recreate the CardanoDBSync with a fresh or compatible database`)
	current = requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar, current.Status.Database.AcceptedPlacementMode)
	assertPlacementStatus(t, current, yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, false)
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{})
}

func TestCardanoDBSyncReconcilerReconcileRejectsDedicatedToPrimarySidecarAfterAcceptance(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	dbSync.UID = types.UID("dbsync-uid")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, current.Status.Database.AcceptedPlacementMode)
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 1)

	current.Spec.Placement = &yacdv1alpha1.CardanoDBSyncPlacementSpec{
		Mode: yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar,
	}
	current.Generation = 2
	require.NoError(t, reconciler.Update(ctx, current))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Empty(t, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedDatabaseIdentityChange)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSidecarMaterialReady, metav1.ConditionFalse, conditionReasonUnsupportedDatabaseIdentityChange)
	assertDeploymentReplicas(t, ctx, reconciler, dbSync, 0)
	current = requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, current.Status.Database.AcceptedPlacementMode)
	assertPlacementStatus(t, current, yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar, false)
}

func TestCardanoDBSyncReconcilerReconcileBackfillsLegacyAcceptedPlacementMode(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	dbSync.UID = types.UID("dbsync-uid")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	require.NotEmpty(t, current.Status.Database.AcceptedIdentityFingerprint)
	current.Status.Database.AcceptedPlacementMode = ""
	require.NoError(t, reconciler.Status().Update(ctx, current))
	statePVC := &corev1.PersistentVolumeClaim{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncStatePVCName(dbSync)}, statePVC))
	delete(statePVC.Annotations, dbSyncPlacementModeAnno)
	require.NoError(t, reconciler.Update(ctx, statePVC))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	current = requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, current.Status.Database.AcceptedPlacementMode)
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncStatePVCName(dbSync)}, statePVC))
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower), statePVC.Annotations[dbSyncPlacementModeAnno])
}

func TestPrimarySidecarMaterialRevisionChangesWithMaterialInputs(t *testing.T) {
	base := testPrimarySidecarResources()
	baseRevision, err := primarySidecarMaterialRevision(base)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(baseRevision, "sha256:"))

	tests := []struct {
		name   string
		mutate func(*primarySidecarDBSyncResources)
	}{
		{
			name: "plan fingerprint",
			mutate: func(resources *primarySidecarDBSyncResources) {
				resources.ConfigMap.Annotations[dbSyncPlanFingerprintAnno] = "plan-2"
			},
		},
		{
			name: "database identity",
			mutate: func(resources *primarySidecarDBSyncResources) {
				resources.ConfigMap.Annotations[dbSyncDatabaseIdentityAnno] = "identity-2"
			},
		},
		{
			name: "credential fingerprint",
			mutate: func(resources *primarySidecarDBSyncResources) {
				resources.PGPassSecret.Annotations[dbSyncSecretVersionAnno] = "secret-2"
			},
		},
		{
			name: "artifact hash",
			mutate: func(resources *primarySidecarDBSyncResources) {
				resources.ConfigMap.Annotations[dbSyncArtifactDataHashAnno] = "artifacts-2"
			},
		},
		{
			name: "configmap name",
			mutate: func(resources *primarySidecarDBSyncResources) {
				resources.ConfigMap.Name = "dbsync-config-2"
			},
		},
		{
			name: "pgpass secret name",
			mutate: func(resources *primarySidecarDBSyncResources) {
				resources.PGPassSecret.Name = "dbsync-pgpass-2"
			},
		},
		{
			name: "state pvc name",
			mutate: func(resources *primarySidecarDBSyncResources) {
				resources.PersistentVolumeClaim.Name = "dbsync-state-2"
			},
		},
		{
			name: "metrics service name",
			mutate: func(resources *primarySidecarDBSyncResources) {
				resources.MetricsService.Name = "dbsync-metrics-2"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources := clonePrimarySidecarResources(base)
			tt.mutate(resources)

			revision, err := primarySidecarMaterialRevision(resources)

			require.NoError(t, err)
			assert.NotEqual(t, baseRevision, revision)
		})
	}
}

func TestCardanoDBSyncReconcilerReconcileRejectsPrimarySidecarForNonLocalNetwork(t *testing.T) {
	ctx := context.Background()
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", "ready-network"))
	network := readyCardanoNetwork("ready-network")
	network.Spec.Mode = yacdv1alpha1.CardanoNetworkModePublic
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Empty(t, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSidecarMaterialReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	assertPlacementStatus(t, requireDBSync(t, ctx, reconciler, dbSync), yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar, false)
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{})
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, &corev1.ConfigMap{})
}

func TestCardanoDBSyncReconcilerReconcileRejectsPrimarySidecarPortConflict(t *testing.T) {
	ctx := context.Background()
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", "ready-network"))
	network := readyCardanoNetwork("ready-network")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Faucet: &yacdv1alpha1.FaucetSpec{
			Enabled: true,
			Port:    8080,
		},
	}
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Empty(t, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSidecarMaterialReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	assertPlacementStatus(t, requireDBSync(t, ctx, reconciler, dbSync), yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar, false)
	assertDegradedMessage(t, ctx, reconciler, dbSync, "db-sync metrics port 8080 conflicts with faucet port in the primary Pod")
}

func TestCardanoDBSyncReconcilerReconcileReportsPrimarySidecarPlacementConflict(t *testing.T) {
	ctx := context.Background()
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", "shared-network"))
	peer := primarySidecarCardanoDBSync(localCardanoDBSync("peer", "shared-network"))
	reconciler := newTestReconciler(t, dbSync, peer)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Empty(t, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonPlacementConflict)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonPlacementConflict)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonPlacementConflict)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSidecarMaterialReady, metav1.ConditionFalse, conditionReasonPlacementConflict)
	assertPlacementStatus(t, requireDBSync(t, ctx, reconciler, dbSync), yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar, false)
	assertDegradedMessage(t, ctx, reconciler, dbSync, placementConflictMessage("shared-network"))
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{})
}

func TestCardanoDBSyncReconcilerReconcileIgnoresDedicatedFollowerInPrimarySidecarConflict(t *testing.T) {
	ctx := context.Background()
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", "shared-network"))
	dedicated := localCardanoDBSync("dedicated", "shared-network")
	dedicated.Spec.Placement = &yacdv1alpha1.CardanoDBSyncPlacementSpec{
		Mode: yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower,
	}
	reconciler := newTestReconciler(t, dbSync, dedicated)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonNetworkUnavailable)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonNetworkUnavailable)
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
	configMap.Annotations[ctrlannotations.ArtifactDataHash] = "sha256:" + strings.Repeat("b", 64)
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
	delete(configMap.Data, networkartifacts.ConfigurationKey)
	configMap.Annotations[ctrlannotations.ArtifactDataHash] = ctrlartifacts.ComputeDataHash(configMap.Data)
	network.Status.Artifacts.DataHash = configMap.Annotations[ctrlannotations.ArtifactDataHash]
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, configMap)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertDependencyWaiting(t, ctx, reconciler, dbSync, conditionReasonNetworkArtifactsMismatch)
}

func TestCardanoDBSyncReconcilerReconcileWaitsForNodeToNodeEndpoint(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "missing-node-endpoint")
	network := readyCardanoNetwork("missing-node-endpoint")
	configMap := artifactConfigMapFor(network)
	network.Status.Endpoints = nil
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, configMap)

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
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionTrue, conditionReasonPostgresReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSynced, metav1.ConditionFalse, conditionReasonRuntimeProbesPending)

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

func TestCardanoDBSyncReconcilerReconcileAppliesExplicitDedicatedFollowerWorkloads(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	dbSync.Spec.Placement = &yacdv1alpha1.CardanoDBSyncPlacementSpec{
		Mode: yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower,
	}
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSidecarMaterialReady, metav1.ConditionFalse, conditionReasonDedicatedFollowerPlacement)
	assertPlacementStatus(t, requireDBSync(t, ctx, reconciler, dbSync), yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, false)
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{}))
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncFollowerPVCName(dbSync)}, &corev1.PersistentVolumeClaim{}))
}

func TestCardanoDBSyncReconcilerReconcileAppliesPublicDedicatedFollowerWorkloads(t *testing.T) {
	ctx := context.Background()
	network := readyPublicCardanoNetwork("preprod-network", yacdv1alpha1.PublicNetworkProfilePreprod)
	dbSync := localCardanoDBSync("dbsync", network.Name)
	dbSync.Spec.Placement = &yacdv1alpha1.CardanoDBSyncPlacementSpec{
		Mode: yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower,
	}
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertPlacementStatus(t, requireDBSync(t, ctx, reconciler, dbSync), yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, false)
	configMap := &corev1.ConfigMap{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, configMap))
	assert.Contains(t, configMap.Data[dbSyncConfigFileName], "NetworkName: preprod")
	assert.Contains(t, configMap.Data[dbSyncConfigFileName], "RequiresNetworkMagic: RequiresMagic")
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{}))
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncFollowerPVCName(dbSync)}, &corev1.PersistentVolumeClaim{}))
}

func TestCardanoDBSyncReconcilerReconcileRejectsPublicMainnet(t *testing.T) {
	ctx := context.Background()
	network := readyPublicCardanoNetwork("mainnet-network", yacdv1alpha1.PublicNetworkProfileMainnet)
	dbSync := localCardanoDBSync("dbsync", network.Name)
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Empty(t, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedSpec)
	assertDegradedMessage(t, ctx, reconciler, dbSync, "public mainnet CardanoDBSync is not supported until mainnet sizing and bootstrap support are implemented")
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncConfigMapName(dbSync)}, &corev1.ConfigMap{})
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{})
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncFollowerPVCName(dbSync)}, &corev1.PersistentVolumeClaim{})
}

func TestCardanoDBSyncReconcilerReconcileRejectsPublicPrimarySidecar(t *testing.T) {
	ctx := context.Background()
	network := readyPublicCardanoNetwork("preview-network", yacdv1alpha1.PublicNetworkProfilePreview)
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", network.Name))
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Empty(t, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedSpec)
	assertDegradedMessage(t, ctx, reconciler, dbSync, "primarySidecar placement is supported only for local CardanoNetwork resources")
	assertMissingObject(t, ctx, reconciler, client.ObjectKey{Namespace: dbSync.Namespace, Name: dbSyncWorkloadName(dbSync)}, &appsv1.Deployment{})
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
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionTrue, conditionReasonFollowerNodeReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionTrue, conditionReasonPostgresReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionTrue, conditionReasonDBSyncReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSynced, metav1.ConditionTrue, conditionReasonSynced)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Sync)
	assert.Equal(t, int64(99), *current.Status.Sync.DBBlockHeight)
	assert.Equal(t, int64(100), *current.Status.Sync.NodeBlockHeight)
	assert.Equal(t, int64(1), *current.Status.Sync.LagBlocks)
	prober := reconciler.runtimeProberOverride.(*fakeCardanoDBSyncRuntimeProber)
	require.Len(t, prober.calls, 1)
	assert.Equal(t, "postgres.default.svc.cluster.local", prober.calls[0].Database.Host)
	assert.Equal(t, "ws://ready-network-ogmios.default.svc.cluster.local:1337", prober.calls[0].OgmiosURL)
}

func TestCardanoDBSyncReconcilerReconcileReportsPostgresUnavailableProbe(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))
	reconciler.runtimeProberOverride = &fakeCardanoDBSyncRuntimeProber{result: dbSyncRuntimeProbeResult{
		Sync:          nil,
		PostgresReady: postgresReadyCondition(metav1.ConditionFalse, conditionReasonPostgresUnavailable, "Postgres progress query failed: dial refused"),
		Synced:        syncedCondition(metav1.ConditionFalse, conditionReasonPostgresUnavailable, "Postgres progress is unavailable"),
	}}

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	deployment := requireDBSyncDeployment(t, ctx, reconciler, dbSync)
	markDBSyncDeploymentAvailable(t, ctx, reconciler, deployment)
	markDBSyncPodContainersReady(t, ctx, reconciler, dbSync, followerNodeContainerName, dbSyncContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionFalse, conditionReasonPostgresUnavailable)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSynced, metav1.ConditionFalse, conditionReasonPostgresUnavailable)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonPostgresUnavailable)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonPostgresUnavailable)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	assert.Nil(t, current.Status.Sync)
}

func TestCardanoDBSyncReconcilerReconcileReportsPostgresUnavailableBeforeDBSyncReady(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	postgresResult := dbSyncRuntimeProbeResult{
		Sync:          nil,
		PostgresReady: postgresReadyCondition(metav1.ConditionFalse, conditionReasonPostgresUnavailable, "Postgres progress query failed: password authentication failed"),
		Synced:        syncedCondition(metav1.ConditionFalse, conditionReasonPostgresUnavailable, "Postgres progress is unavailable"),
	}
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))
	reconciler.runtimeProberOverride = &fakeCardanoDBSyncRuntimeProber{postgresResult: &postgresResult}

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	deployment := requireDBSyncDeployment(t, ctx, reconciler, dbSync)
	markDBSyncDeploymentAvailable(t, ctx, reconciler, deployment)
	markDBSyncPodContainersReady(t, ctx, reconciler, dbSync, followerNodeContainerName)
	prober := reconciler.runtimeProberOverride.(*fakeCardanoDBSyncRuntimeProber)
	prober.postgresCalls = nil

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionTrue, conditionReasonFollowerNodeReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionFalse, conditionReasonPostgresUnavailable)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDBSyncReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSynced, metav1.ConditionFalse, conditionReasonPostgresUnavailable)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonPostgresUnavailable)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonPostgresUnavailable)
	require.Len(t, prober.postgresCalls, 1)
	assert.Empty(t, prober.calls)
}

func TestCardanoDBSyncReconcilerReconcilePreservesDBProgressWhenOgmiosUnavailable(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))
	reconciler.runtimeProberOverride = &fakeCardanoDBSyncRuntimeProber{result: dbSyncRuntimeProbeResult{
		Sync: &yacdv1alpha1.CardanoDBSyncProgressStatus{
			DBBlockHeight: ptr.To[int64](41),
			DBSlotHeight:  ptr.To[int64](4100),
			Epoch:         ptr.To[int64](3),
		},
		PostgresReady: ctrlstatus.Condition(string(conditionTypePostgresReady), metav1.ConditionTrue, string(conditionReasonPostgresReady), "Postgres is reachable and db-sync progress query succeeded"),
		Synced:        syncedCondition(metav1.ConditionFalse, conditionReasonNodeTipUnavailable, "Ogmios node tip query failed: unavailable"),
	}}

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	deployment := requireDBSyncDeployment(t, ctx, reconciler, dbSync)
	markDBSyncDeploymentAvailable(t, ctx, reconciler, deployment)
	markDBSyncPodContainersReady(t, ctx, reconciler, dbSync, followerNodeContainerName, dbSyncContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionTrue, conditionReasonPostgresReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSynced, metav1.ConditionFalse, conditionReasonNodeTipUnavailable)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, conditionReasonNodeTipUnavailable)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Sync)
	assert.Equal(t, int64(41), *current.Status.Sync.DBBlockHeight)
	assert.Equal(t, int64(4100), *current.Status.Sync.DBSlotHeight)
	assert.Nil(t, current.Status.Sync.NodeBlockHeight)
}

func TestCardanoDBSyncReconcilerReconcileLeavesStatusUnchangedForSameProbe(t *testing.T) {
	ctx := context.Background()
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, externalDatabaseSecretFor(dbSync), network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	deployment := requireDBSyncDeployment(t, ctx, reconciler, dbSync)
	markDBSyncDeploymentAvailable(t, ctx, reconciler, deployment)
	markDBSyncPodContainersReady(t, ctx, reconciler, dbSync, followerNodeContainerName, dbSyncContainerName)

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	first := requireDBSync(t, ctx, reconciler, dbSync).Status.DeepCopy()

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	assert.Equal(t, first, &current.Status)
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
	assert.Equal(t, ctrl.Result{RequeueAfter: dbSyncRuntimeProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeFollowerNodeReady, metav1.ConditionTrue, conditionReasonFollowerNodeReady)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypePostgresReady, metav1.ConditionTrue, conditionReasonPostgresReady)
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

func TestCardanoDBSyncReconcilerReconcileDoesNotRegenerateGeneratedManagedPostgresAuthSecret(t *testing.T) {
	ctx := context.Background()
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	reconciler := newTestReconciler(t, dbSync, network, artifactConfigMapFor(network))

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))
	require.NoError(t, err)
	postgresPVC := &corev1.PersistentVolumeClaim{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresPVCName(dbSync)}, postgresPVC))
	require.NotEmpty(t, postgresPVC.Annotations[managedPostgresIdentityAnno])
	authSecret := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresAuthSecretName(dbSync)}, authSecret))
	require.NoError(t, reconciler.Delete(ctx, authSecret))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(dbSync))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonManagedDatabaseSecretMissing)
	err = reconciler.Get(ctx, client.ObjectKey{Namespace: dbSync.Namespace, Name: managedPostgresAuthSecretName(dbSync)}, &corev1.Secret{})
	require.True(t, apierrors.IsNotFound(err), "expected missing managed auth Secret, got %v", err)
	current := requireDBSync(t, ctx, reconciler, dbSync)
	require.NotNil(t, current.Status.Database)
	assert.Equal(t, managedPostgresAuthSecretName(dbSync), current.Status.Database.AuthSecretName)
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

	configMap.Annotations[ctrlannotations.ArtifactDataHash] = "sha256:" + strings.Repeat("b", 64)
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
	reason conditionReason,
) {
	t.Helper()

	assertCondition(t, ctx, reconciler, dbSync, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeProgressing, metav1.ConditionTrue, reason)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeReady, metav1.ConditionFalse, reason)
	assertCondition(t, ctx, reconciler, dbSync, conditionTypeSidecarMaterialReady, metav1.ConditionFalse, reason)
}

func assertCondition(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	dbSync *yacdv1alpha1.CardanoDBSync,
	ct conditionType,
	status metav1.ConditionStatus,
	reason conditionReason,
) {
	t.Helper()

	current := requireDBSync(t, ctx, reconciler, dbSync)
	condition := apimeta.FindStatusCondition(current.Status.Conditions, string(ct))
	require.NotNil(t, condition, "expected condition %s", ct)
	assert.Equal(t, status, condition.Status)
	assert.Equal(t, string(reason), condition.Reason)
	assert.Equal(t, current.Generation, condition.ObservedGeneration)
	assert.Equal(t, current.Generation, current.Status.ObservedGeneration)
}

func assertPlacementStatus(
	t *testing.T,
	dbSync *yacdv1alpha1.CardanoDBSync,
	mode yacdv1alpha1.CardanoDBSyncPlacementMode,
	wantPrimarySidecar bool,
) {
	t.Helper()

	require.NotNil(t, dbSync.Status.Placement)
	assert.Equal(t, mode, dbSync.Status.Placement.Mode)
	if wantPrimarySidecar {
		require.NotNil(t, dbSync.Status.Placement.PrimarySidecar)
		return
	}
	assert.Nil(t, dbSync.Status.Placement.PrimarySidecar)
}

func assertDegradedMessage(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	dbSync *yacdv1alpha1.CardanoDBSync,
	message string,
) {
	t.Helper()

	current := requireDBSync(t, ctx, reconciler, dbSync)
	condition := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeDegraded))
	require.NotNil(t, condition, "expected condition %s", conditionTypeDegraded)
	assert.Equal(t, message, condition.Message)
}

func assertMissingObject(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoDBSyncReconciler,
	key client.ObjectKey,
	object client.Object,
) {
	t.Helper()

	err := reconciler.Get(ctx, key, object)
	require.True(t, apierrors.IsNotFound(err), "expected missing %T %s, got %v", object, key, err)
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
	builder.WithIndex(&yacdv1alpha1.CardanoDBSync{}, cardanoDBSyncNetworkRefNameField, cardanoDBSyncNetworkRefIndexer)
	builder.WithObjects(objects...)
	fakeClient := builder.Build()

	return &CardanoDBSyncReconciler{
		Client:                fakeClient,
		Reader:                fakeClient,
		Scheme:                scheme,
		runtimeProberOverride: &fakeCardanoDBSyncRuntimeProber{result: syncedRuntimeProbeResult(99, 100)},
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

func primarySidecarCardanoDBSync(dbSync *yacdv1alpha1.CardanoDBSync) *yacdv1alpha1.CardanoDBSync {
	dbSync.Spec.Placement = &yacdv1alpha1.CardanoDBSyncPlacementSpec{
		Mode: yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar,
	}
	return dbSync
}

func ownedDedicatedDBSyncDeployment(dbSync *yacdv1alpha1.CardanoDBSync, replicas int32) *appsv1.Deployment {
	controller := true
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncWorkloadName(dbSync),
			Namespace: dbSync.Namespace,
			Labels:    dbSyncWorkloadLabels(dbSync),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: yacdv1alpha1.GroupVersion.String(),
				Kind:       "CardanoDBSync",
				Name:       dbSync.Name,
				UID:        dbSync.UID,
				Controller: &controller,
			}},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: dbSyncWorkloadSelectorLabels(dbSync),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: dbSyncWorkloadSelectorLabels(dbSync),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  dbSyncContainerName,
						Image: dbSync.Spec.Image,
					}},
				},
			},
		},
	}
}

func runningDedicatedDBSyncPod(dbSync *yacdv1alpha1.CardanoDBSync) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncWorkloadName(dbSync) + "-running",
			Namespace: dbSync.Namespace,
			Labels:    dbSyncWorkloadSelectorLabels(dbSync),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  dbSyncContainerName,
				Image: dbSync.Spec.Image,
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}

func primaryNetworkDeploymentWithDBSyncSidecar(network *yacdv1alpha1.CardanoNetwork) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryNetworkDeploymentName(network),
			Namespace: network.Namespace,
			Labels:    primaryNetworkSelectorLabels(network),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: primaryNetworkSelectorLabels(network),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: primaryNetworkSelectorLabels(network),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  dbSyncContainerName,
						Image: "ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0",
					}},
				},
			},
		},
	}
}

func runningPrimaryNetworkPodWithDBSyncSidecar(network *yacdv1alpha1.CardanoNetwork) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryNetworkDeploymentName(network) + "-running",
			Namespace: network.Namespace,
			Labels:    primaryNetworkSelectorLabels(network),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  dbSyncContainerName,
				Image: "ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0",
			}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}

func testPrimarySidecarResources() *primarySidecarDBSyncResources {
	return &primarySidecarDBSyncResources{
		ConfigMap: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dbsync-config",
				Annotations: map[string]string{
					dbSyncPlanFingerprintAnno:  "plan-1",
					dbSyncDatabaseIdentityAnno: "identity-1",
					dbSyncArtifactDataHashAnno: "artifacts-1",
				},
			},
		},
		PGPassSecret: &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "dbsync-pgpass",
				Annotations: map[string]string{
					dbSyncSecretVersionAnno: "secret-1",
				},
			},
		},
		PersistentVolumeClaim: &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "dbsync-state"},
		},
		MetricsService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "dbsync-metrics"},
		},
	}
}

func clonePrimarySidecarResources(resources *primarySidecarDBSyncResources) *primarySidecarDBSyncResources {
	return &primarySidecarDBSyncResources{
		ConfigMap:             resources.ConfigMap.DeepCopy(),
		PGPassSecret:          resources.PGPassSecret.DeepCopy(),
		PersistentVolumeClaim: resources.PersistentVolumeClaim.DeepCopy(),
		MetricsService:        resources.MetricsService.DeepCopy(),
		Plan:                  resources.Plan,
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
	networkMagic := int64(42)
	era := yacdv1alpha1.CardanoEraConway
	network := &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 1,
		},
		Spec: yacdv1alpha1.CardanoNetworkSpec{
			Mode: yacdv1alpha1.CardanoNetworkModeLocal,
			Node: yacdv1alpha1.CardanoNodeSpec{
				Version: "11.0.1",
				Port:    3001,
			},
		},
		Status: yacdv1alpha1.CardanoNetworkStatus{
			ObservedGeneration: 1,
			Network: &yacdv1alpha1.CardanoNetworkIdentityStatus{
				Mode:                yacdv1alpha1.CardanoNetworkModeLocal,
				LocalnetFingerprint: "fingerprint",
				NetworkFingerprint:  "fingerprint",
				NetworkMagic:        &networkMagic,
				Era:                 &era,
			},
			Artifacts: &yacdv1alpha1.CardanoNetworkArtifactsStatus{
				NetworkConfigMapName: name + "-network-artifacts",
				SchemaVersion:        testNetworkArtifactSchemaVersion,
			},
			Endpoints: &yacdv1alpha1.CardanoNetworkEndpointsStatus{
				NodeToNode: &yacdv1alpha1.ServiceEndpointStatus{
					ServiceName: name + "-node",
					Port:        3001,
					URL:         "tcp://" + name + "-node.default.svc.cluster.local:3001",
				},
				Ogmios: &yacdv1alpha1.ServiceEndpointStatus{
					ServiceName: name + "-ogmios",
					Port:        1337,
					URL:         "ws://" + name + "-ogmios.default.svc.cluster.local:1337",
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
	network.Status.Artifacts.DataHash = ctrlartifacts.ComputeDataHash(testNetworkArtifactsDataFor(network))
	return network
}

func readyPublicCardanoNetwork(name string, profile yacdv1alpha1.PublicNetworkProfile) *yacdv1alpha1.CardanoNetwork {
	networkMagic := int64(2)
	switch profile {
	case yacdv1alpha1.PublicNetworkProfilePreprod:
		networkMagic = 1
	case yacdv1alpha1.PublicNetworkProfileMainnet:
		networkMagic = 764824073
	case yacdv1alpha1.PublicNetworkProfileCustom:
		networkMagic = 42
	}
	era := yacdv1alpha1.CardanoEraConway
	network := &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  "default",
			Generation: 1,
		},
		Spec: yacdv1alpha1.CardanoNetworkSpec{
			Mode: yacdv1alpha1.CardanoNetworkModePublic,
			Node: yacdv1alpha1.CardanoNodeSpec{
				Version: "11.0.1",
				Port:    3001,
			},
			Public: &yacdv1alpha1.PublicNetworkSpec{Profile: profile},
		},
		Status: yacdv1alpha1.CardanoNetworkStatus{
			ObservedGeneration: 1,
			Network: &yacdv1alpha1.CardanoNetworkIdentityStatus{
				Mode:               yacdv1alpha1.CardanoNetworkModePublic,
				NetworkFingerprint: "fingerprint",
				NetworkMagic:       &networkMagic,
				Profile:            &profile,
				Era:                &era,
			},
			Artifacts: &yacdv1alpha1.CardanoNetworkArtifactsStatus{
				NetworkConfigMapName: name + "-network-artifacts",
				SchemaVersion:        testNetworkArtifactSchemaVersion,
			},
			Endpoints: &yacdv1alpha1.CardanoNetworkEndpointsStatus{
				NodeToNode: &yacdv1alpha1.ServiceEndpointStatus{
					ServiceName: name + "-node",
					Port:        3001,
					URL:         "tcp://" + name + "-node.default.svc.cluster.local:3001",
				},
				Ogmios: &yacdv1alpha1.ServiceEndpointStatus{
					ServiceName: name + "-ogmios",
					Port:        1337,
					URL:         "ws://" + name + "-ogmios.default.svc.cluster.local:1337",
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
	network.Status.Artifacts.DataHash = ctrlartifacts.ComputeDataHash(testNetworkArtifactsDataFor(network))
	return network
}

func moveReadyNetworkToNamespace(network *yacdv1alpha1.CardanoNetwork, namespace string) {
	network.Namespace = namespace
	if network.Status.Endpoints == nil {
		return
	}
	if network.Status.Endpoints.NodeToNode != nil {
		network.Status.Endpoints.NodeToNode.URL = "tcp://" + network.Status.Endpoints.NodeToNode.ServiceName + "." + namespace + ".svc.cluster.local:" + "3001"
	}
	if network.Status.Endpoints.Ogmios != nil {
		network.Status.Endpoints.Ogmios.URL = "ws://" + network.Status.Endpoints.Ogmios.ServiceName + "." + namespace + ".svc.cluster.local:" + "1337"
	}
	if network.Status.Artifacts != nil {
		network.Status.Artifacts.DataHash = ctrlartifacts.ComputeDataHash(testNetworkArtifactsDataFor(network))
	}
}

func artifactConfigMapFor(network *yacdv1alpha1.CardanoNetwork) *corev1.ConfigMap {
	data := testNetworkArtifactsDataFor(network)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      network.Status.Artifacts.NetworkConfigMapName,
			Namespace: network.Namespace,
			Annotations: map[string]string{
				ctrlannotations.ArtifactSchemaVersion: network.Status.Artifacts.SchemaVersion,
				ctrlannotations.ArtifactDataHash:      ctrlartifacts.ComputeDataHash(data),
				ctrlannotations.NetworkFingerprint:    network.Status.Network.NetworkFingerprint,
				ctrlannotations.LocalnetFingerprint:   network.Status.Network.LocalnetFingerprint,
			},
		},
		Data: data,
	}
}

func parsedConnectionForNetwork(t testing.TB, network *yacdv1alpha1.CardanoNetwork) ctrlnetworkartifacts.Connection {
	t.Helper()
	result := ctrlnetworkartifacts.ConsumerConnection(artifactConfigMapFor(network), network)
	require.True(t, result.Ready, result.Message)
	return result.Connection
}

func testNetworkArtifactsDataFor(network *yacdv1alpha1.CardanoNetwork) map[string]string {
	data := map[string]string{
		networkartifacts.ConfigurationKey:   "test configuration.yaml",
		networkartifacts.ByronGenesisKey:    "test byron-genesis.json",
		networkartifacts.ShelleyGenesisKey:  "test shelley-genesis.json",
		networkartifacts.AlonzoGenesisKey:   "test alonzo-genesis.json",
		networkartifacts.ConwayGenesisKey:   "test conway-genesis.json",
		networkartifacts.PrimaryTopologyKey: "test primary-topology.json",
		networkartifacts.ConnectionKey:      testConnectionJSONForNetwork(network),
	}
	if network.Status.Network.Mode == yacdv1alpha1.CardanoNetworkModePublic {
		data[networkartifacts.PublicProfileManifestKey] = "test yacd-public-profile.json"
	} else {
		data[networkartifacts.PlanManifestKey] = "test yacd-localnet-plan.json"
	}
	return data
}

func testConnectionJSONForNetwork(network *yacdv1alpha1.CardanoNetwork) string {
	networkFields := map[string]any{
		"name":         network.Name,
		"namespace":    network.Namespace,
		"mode":         string(network.Status.Network.Mode),
		"networkMagic": *network.Status.Network.NetworkMagic,
		"era":          string(*network.Status.Network.Era),
	}
	files := map[string]string{
		"configuration":   networkartifacts.ConfigurationKey,
		"byronGenesis":    networkartifacts.ByronGenesisKey,
		"shelleyGenesis":  networkartifacts.ShelleyGenesisKey,
		"alonzoGenesis":   networkartifacts.AlonzoGenesisKey,
		"conwayGenesis":   networkartifacts.ConwayGenesisKey,
		"primaryTopology": networkartifacts.PrimaryTopologyKey,
		"connection":      networkartifacts.ConnectionKey,
	}
	if network.Status.Network.Mode == yacdv1alpha1.CardanoNetworkModePublic {
		requiresMagic := true
		if network.Status.Network.Profile != nil && *network.Status.Network.Profile == yacdv1alpha1.PublicNetworkProfileMainnet {
			requiresMagic = false
		}
		networkFields["profile"] = string(*network.Status.Network.Profile)
		networkFields["requiresNetworkMagic"] = requiresMagic
		networkFields["networkFingerprint"] = network.Status.Network.NetworkFingerprint
		files["publicProfile"] = networkartifacts.PublicProfileManifestKey
	} else {
		networkFields["localnetFingerprint"] = network.Status.Network.LocalnetFingerprint
		files["localnetPlan"] = networkartifacts.PlanManifestKey
	}
	doc := struct {
		SchemaVersion     string            `json:"schemaVersion"`
		Network           map[string]any    `json:"network"`
		PrimaryNodeToNode map[string]any    `json:"primaryNodeToNode"`
		Files             map[string]string `json:"files"`
	}{
		SchemaVersion: networkartifacts.SchemaVersion,
		Network:       networkFields,
		PrimaryNodeToNode: map[string]any{
			"host": network.Status.Endpoints.NodeToNode.ServiceName + "." + network.Namespace + ".svc.cluster.local",
			"port": network.Status.Endpoints.NodeToNode.Port,
			"url":  network.Status.Endpoints.NodeToNode.URL,
		},
		Files: files,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(raw) + "\n"
}
