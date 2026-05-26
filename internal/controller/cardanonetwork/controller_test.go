package cardanonetwork

import (
	"context"
	"fmt"
	"maps"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	ctrlartifacts "github.com/meigma/yacd/internal/ctrlkit/artifacts"
	ctrlstorage "github.com/meigma/yacd/internal/ctrlkit/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const wrongManagedByLabelValue = "wrong"

var testNetworkArtifactsDataHash = ctrlartifacts.ComputeDataHash(testNetworkArtifactsData())

// TestCardanoNetworkReconcilerReconcileHandlesMissingObject verifies deleted
// resources are ignored without requeueing.
func TestCardanoNetworkReconcilerReconcileHandlesMissingObject(t *testing.T) {
	reconciler := newTestReconciler(t)

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "missing",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestCardanoNetworkReconcilerReconcileSkipsTerminatingObject(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("terminating")
	now := metav1.Now()
	network.DeletionTimestamp = &now
	network.Finalizers = []string{"yacd.meigma.io/test-finalizer"}
	reconciler := newTestReconciler(t, network)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	assertNoPrimaryChildren(t, ctx, reconciler, network)
	current := requireNetwork(t, ctx, reconciler, network)
	assert.Empty(t, current.Status.Conditions)
}

// TestCardanoNetworkReconcilerReconcileCreatesPrimaryWorkload verifies a
// supported resource creates the singleton primary node PVC, Deployment, and Services.
func TestCardanoNetworkReconcilerReconcileCreatesPrimaryWorkload(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("creates-workload")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, result)
	requirePrimaryPVC(t, ctx, reconciler, network)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	networkArtifactsConfigMap := requireNetworkArtifactsConfigMap(t, ctx, reconciler, network)
	artifactPublisherServiceAccount := requireArtifactPublisherServiceAccount(t, ctx, reconciler, network)
	artifactPublisherRole := requireArtifactPublisherRole(t, ctx, reconciler, network)
	artifactPublisherRoleBinding := requireArtifactPublisherRoleBinding(t, ctx, reconciler, network)
	service := requirePrimaryService(t, ctx, reconciler, network)
	ogmiosService := requirePrimaryOgmiosService(t, ctx, reconciler, network)
	kupoService := requirePrimaryKupoService(t, ctx, reconciler, network)
	faucetService := requirePrimaryFaucetService(t, ctx, reconciler, network)
	faucetAuthSecret := requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	assert.Equal(t, "creates-workload-network-artifacts", networkArtifactsConfigMap.Name)
	assert.Equal(t, deployment.Spec.Template.Annotations[localnetFingerprintAnno], networkArtifactsConfigMap.Annotations[localnetFingerprintAnno])
	assert.Equal(t, "creates-workload-artifact-publisher", artifactPublisherServiceAccount.Name)
	require.NotNil(t, artifactPublisherServiceAccount.AutomountServiceAccountToken)
	assert.False(t, *artifactPublisherServiceAccount.AutomountServiceAccountToken)
	require.Len(t, artifactPublisherRole.Rules, 1)
	assert.Equal(t, []string{networkArtifactsConfigMap.Name}, artifactPublisherRole.Rules[0].ResourceNames)
	assert.Equal(t, []string{"get", "patch"}, artifactPublisherRole.Rules[0].Verbs)
	require.Len(t, artifactPublisherRoleBinding.Subjects, 1)
	assert.Equal(t, artifactPublisherServiceAccount.Name, artifactPublisherRoleBinding.Subjects[0].Name)
	assert.Equal(t, artifactPublisherServiceAccount.Name, deployment.Spec.Template.Spec.ServiceAccountName)
	require.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.False(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	require.Len(t, deployment.Spec.Template.Spec.InitContainers, 2)
	assert.Contains(t, deployment.Spec.Template.Spec.InitContainers[0].VolumeMounts, artifactPublisherVolumeMount())
	for _, container := range deployment.Spec.Template.Spec.Containers {
		assert.NotContains(t, container.VolumeMounts, artifactPublisherVolumeMount())
	}
	assert.Contains(t, deployment.Spec.Template.Spec.Volumes, artifactPublisherProjectedVolume())
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       cardanoNodePortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       network.Spec.Node.Port,
			TargetPort: intstr.FromString(cardanoNodePortName),
		},
	}, service.Spec.Ports)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       ogmiosPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultOgmiosPort,
			TargetPort: intstr.FromString(ogmiosPortName),
		},
	}, ogmiosService.Spec.Ports)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       kupoPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultKupoPort,
			TargetPort: intstr.FromString(kupoPortName),
		},
	}, kupoService.Spec.Ports)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       faucetPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultFaucetPort,
			TargetPort: intstr.FromString(faucetPortName),
		},
	}, faucetService.Spec.Ports)
	assert.True(t, validFaucetAuthToken(string(faucetAuthSecret.Data[faucetAuthTokenKey])))
	assert.Equal(t, deployment.Spec.Template.Annotations[localnetFingerprintAnno], requireAcceptedLocalnetFingerprint(t, ctx, reconciler, network))
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonArtifactsPending)
	assertNodeToNodeEndpoint(t, ctx, reconciler, network, service.Name, network.Spec.Node.Port)
	assertOgmiosEndpoint(t, ctx, reconciler, network, ogmiosService.Name, defaultOgmiosPort)
	assertKupoEndpoint(t, ctx, reconciler, network, kupoService.Name, defaultKupoPort)
	assertFaucetEndpoint(t, ctx, reconciler, network, faucetService.Name, defaultFaucetPort)
	assertFaucetStatus(t, ctx, reconciler, network, faucetAuthSecret.Name)
	current := requireNetwork(t, ctx, reconciler, network)
	assert.Nil(t, current.Status.Artifacts)
}

func TestCardanoNetworkReconcilerReconcileLeavesFaucetDisabledByDefault(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("faucet-default-disabled")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 3)
	assertNoPrimaryFaucetService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled)
	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Faucet)
	assert.Nil(t, current.Status.Faucet)
}

func TestCardanoNetworkReconcilerReconcilePublishesVerifiedNetworkArtifacts(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("artifact-status")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	configMap := publishNetworkArtifacts(t, ctx, reconciler, network)

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assertCondition(t, ctx, reconciler, network, conditionTypeArtifactsReady, metav1.ConditionTrue, conditionReasonArtifactsReady)
	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Artifacts)
	assert.Equal(t, configMap.Name, current.Status.Artifacts.NetworkConfigMapName)
	assert.Equal(t, networkartifacts.SchemaVersion, current.Status.Artifacts.SchemaVersion)
	assert.Equal(t, testNetworkArtifactsDataHash, current.Status.Artifacts.DataHash)
}

func TestCardanoNetworkReconcilerReconcileRecoversDeletedNetworkArtifactsConfigMapWithRollout(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("artifact-recreate")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	configMap := requireNetworkArtifactsConfigMap(t, ctx, reconciler, network)
	configMap.UID = types.UID("old-artifact-configmap")
	require.NoError(t, reconciler.Update(ctx, configMap))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.Equal(t, "old-artifact-configmap", deployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno])

	require.NoError(t, reconciler.Delete(ctx, configMap))
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	recreated := requireNetworkArtifactsConfigMap(t, ctx, reconciler, network)
	assert.Equal(t, networkArtifactsConfigMapName(network), recreated.Name)
	deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.NotEqual(t, "old-artifact-configmap", deployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno])
	assertCondition(t, ctx, reconciler, network, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonArtifactsPending)
	current := requireNetwork(t, ctx, reconciler, network)
	assert.Nil(t, current.Status.Artifacts)
}

func TestCardanoNetworkReconcilerReconcileRecoversCorruptedNetworkArtifactsConfigMapWithRollout(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("artifact-corrupt")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	configMap := publishNetworkArtifacts(t, ctx, reconciler, network)
	configMap.UID = types.UID("published-artifact-configmap")
	require.NoError(t, reconciler.Update(ctx, configMap))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.Equal(t, "published-artifact-configmap", deployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno])
	assertCondition(t, ctx, reconciler, network, conditionTypeArtifactsReady, metav1.ConditionTrue, conditionReasonArtifactsReady)

	corrupted := requireNetworkArtifactsConfigMap(t, ctx, reconciler, network)
	delete(corrupted.Data, networkartifacts.ConfigurationKey)
	require.NoError(t, reconciler.Update(ctx, corrupted))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	deleted := &corev1.ConfigMap{}
	err = reconciler.Get(ctx, client.ObjectKey{
		Namespace: network.Namespace,
		Name:      networkArtifactsConfigMapName(network),
	}, deleted)
	assert.True(t, apierrors.IsNotFound(err), "expected artifact ConfigMap to be deleted, got %v", err)
	deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.Equal(t, "published-artifact-configmap", deployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno])
	assertCondition(t, ctx, reconciler, network, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonArtifactsPending)
	current := requireNetwork(t, ctx, reconciler, network)
	assert.Nil(t, current.Status.Artifacts)

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	recreated := requireNetworkArtifactsConfigMap(t, ctx, reconciler, network)
	assert.Empty(t, recreated.Data)
	assert.NotEqual(t, "published-artifact-configmap", string(recreated.UID))
	deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.NotEqual(t, "published-artifact-configmap", deployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno])
}

func TestArtifactConfigMapStatusVerifiesNetworkArtifactsDataHash(t *testing.T) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "devnet-network-artifacts",
			Annotations: map[string]string{
				ctrlartifacts.SchemaVersionAnnotation: networkartifacts.SchemaVersion,
				localnetFingerprintAnno:               "fingerprint",
				ctrlartifacts.DataHashAnnotation:      "sha256:test",
			},
		},
		Data: testNetworkArtifactsData(),
	}

	result := networkartifacts.ProducerConfigMap(configMap, "fingerprint")
	assert.False(t, result.Ready)
	assert.Equal(t, "artifact ConfigMap data hash is not published", result.Message)

	configMap.Annotations[ctrlartifacts.DataHashAnnotation] = testNetworkArtifactsDataHash
	result = networkartifacts.ProducerConfigMap(configMap, "fingerprint")
	assert.True(t, result.Ready)
	assert.Equal(t, testNetworkArtifactsDataHash, result.Status.DataHash)

	configMap.Data[networkartifacts.ConfigurationKey] = "corrupted"
	result = networkartifacts.ProducerConfigMap(configMap, "fingerprint")
	assert.False(t, result.Ready)
	assert.Equal(t, "artifact ConfigMap data hash does not match data", result.Message)

	configMap.Data = testNetworkArtifactsData()
	configMap.Data[networkartifacts.DijkstraGenesisKey] = "test dijkstra-genesis.json"
	configMap.Annotations[ctrlartifacts.DataHashAnnotation] = ctrlartifacts.ComputeDataHash(configMap.Data)
	result = networkartifacts.ProducerConfigMap(configMap, "fingerprint")
	assert.True(t, result.Ready)
	assert.Equal(t, configMap.Annotations[ctrlartifacts.DataHashAnnotation], result.Status.DataHash)

	configMap.Data = testNetworkArtifactsData()
	configMap.Data["pool-keys/secret.skey"] = "do not publish"
	configMap.Annotations[ctrlartifacts.DataHashAnnotation] = ctrlartifacts.ComputeDataHash(configMap.Data)
	result = networkartifacts.ProducerConfigMap(configMap, "fingerprint")
	assert.False(t, result.Ready)
	assert.Equal(t, "artifact ConfigMap contains unsupported key pool-keys/secret.skey", result.Message)

	configMap.Data = testNetworkArtifactsData()
	configMap.BinaryData = map[string][]byte{"secret": []byte("do not publish")}
	configMap.Annotations[ctrlartifacts.DataHashAnnotation] = testNetworkArtifactsDataHash
	result = networkartifacts.ProducerConfigMap(configMap, "fingerprint")
	assert.False(t, result.Ready)
	assert.Equal(t, "artifact ConfigMap contains binary data", result.Message)
}

func TestCardanoNetworkReconcilerReconcileReportsNodeReadyWhenDeploymentAvailable(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("node-ready")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	publishNetworkArtifacts(t, ctx, reconciler, network)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName, ogmiosContainerName, kupoContainerName, faucetContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: faucetSecretRepairRequeueAfter}, result)

	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionTrue, conditionReasonNodeReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionTrue, conditionReasonOgmiosReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionTrue, conditionReasonKupoReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionTrue, conditionReasonFaucetReady)
}

func TestCardanoNetworkReconcilerReconcileKeepsNodeReadySeparateFromOgmios(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("node-ready-ogmios-waiting")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assert.Equal(t, ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionTrue, conditionReasonNodeReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
}

func TestCardanoNetworkReconcilerReconcileRequiresKupoReadyWhenEnabled(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("ogmios-ready-kupo-waiting")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName, ogmiosContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assert.Equal(t, ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionTrue, conditionReasonNodeReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionTrue, conditionReasonOgmiosReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
}

func TestCardanoNetworkReconcilerReconcileDisablesOgmios(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("ogmios-disabled")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Ogmios: &yacdv1alpha1.OgmiosSpec{
			Enabled: false,
		},
	}
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, cardanoNodeContainerName, deployment.Spec.Template.Spec.Containers[0].Name)
	assertNoPrimaryOgmiosService(t, ctx, reconciler, network)
	assertNoPrimaryKupoService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonOgmiosDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled)
	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Ogmios)
	assert.Nil(t, current.Status.Endpoints.Kupo)
	assert.Nil(t, current.Status.Endpoints.Faucet)
	assert.Nil(t, current.Status.Faucet)

	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName)
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionTrue, conditionReasonNodeReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonOgmiosDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonOgmiosDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonOgmiosDisabled)
}

func TestCardanoNetworkReconcilerReconcileDeletesOwnedOgmiosServiceWhenDisabled(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("deletes-ogmios-service")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	requirePrimaryOgmiosService(t, ctx, reconciler, network)
	requirePrimaryKupoService(t, ctx, reconciler, network)
	requirePrimaryFaucetService(t, ctx, reconciler, network)
	requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)

	current := requireNetwork(t, ctx, reconciler, network)
	current.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Ogmios: &yacdv1alpha1.OgmiosSpec{
			Enabled: false,
		},
	}
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assertNoPrimaryOgmiosService(t, ctx, reconciler, network)
	assertNoPrimaryKupoService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	current = requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Ogmios)
	assert.Nil(t, current.Status.Endpoints.Kupo)
	assert.Nil(t, current.Status.Endpoints.Faucet)
	assert.Nil(t, current.Status.Faucet)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonOgmiosDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled)
}

func TestCardanoNetworkReconcilerReconcileDisablesKupo(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("kupo-disabled")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Kupo: &yacdv1alpha1.KupoSpec{
			Enabled: false,
		},
	}
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 2)
	assert.Equal(t, cardanoNodeContainerName, deployment.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, ogmiosContainerName, deployment.Spec.Template.Spec.Containers[1].Name)
	requirePrimaryOgmiosService(t, ctx, reconciler, network)
	assertNoPrimaryKupoService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled)
	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Kupo)
	assert.Nil(t, current.Status.Endpoints.Faucet)
	assert.Nil(t, current.Status.Faucet)

	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName, ogmiosContainerName)
	publishNetworkArtifacts(t, ctx, reconciler, network)
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionTrue, conditionReasonNodeReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionTrue, conditionReasonOgmiosReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonReady)
}

func TestCardanoNetworkReconcilerReconcileDeletesOwnedKupoServiceWhenDisabled(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("deletes-kupo-service")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	requirePrimaryKupoService(t, ctx, reconciler, network)
	requirePrimaryFaucetService(t, ctx, reconciler, network)
	requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)

	current := requireNetwork(t, ctx, reconciler, network)
	current.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Kupo: &yacdv1alpha1.KupoSpec{
			Enabled: false,
		},
	}
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assertNoPrimaryKupoService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	current = requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Kupo)
	assert.Nil(t, current.Status.Endpoints.Faucet)
	assert.Nil(t, current.Status.Faucet)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled)
}

func TestCardanoNetworkReconcilerReconcileRequiresFaucetReadyWhenEnabled(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("kupo-ready-faucet-waiting")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName, ogmiosContainerName, kupoContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assert.Equal(t, ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionTrue, conditionReasonNodeReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionTrue, conditionReasonOgmiosReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionTrue, conditionReasonKupoReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
}

func TestCardanoNetworkReconcilerReconcileDisablesFaucet(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("faucet-disabled")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Faucet: &yacdv1alpha1.FaucetSpec{
			Enabled: false,
		},
	}
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 3)
	assert.Equal(t, cardanoNodeContainerName, deployment.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, ogmiosContainerName, deployment.Spec.Template.Spec.Containers[1].Name)
	assert.Equal(t, kupoContainerName, deployment.Spec.Template.Spec.Containers[2].Name)
	requirePrimaryOgmiosService(t, ctx, reconciler, network)
	requirePrimaryKupoService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled)
	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Faucet)
	assert.Nil(t, current.Status.Faucet)

	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName, ogmiosContainerName, kupoContainerName)
	publishNetworkArtifacts(t, ctx, reconciler, network)
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled)
}

func TestCardanoNetworkReconcilerReconcileDeletesOwnedFaucetChildrenWhenDisabled(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("deletes-faucet-children")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	requirePrimaryFaucetService(t, ctx, reconciler, network)
	requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)

	current := requireNetwork(t, ctx, reconciler, network)
	current.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Faucet: &yacdv1alpha1.FaucetSpec{
			Enabled: false,
		},
	}
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assertNoPrimaryFaucetService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	current = requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Faucet)
	assert.Nil(t, current.Status.Faucet)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonFaucetDisabled)
}

func TestCardanoNetworkReconcilerReconcileIsIdempotent(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("idempotent")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	var deployments appsv1.DeploymentList
	require.NoError(t, reconciler.List(ctx, &deployments))
	assert.Len(t, deployments.Items, 1)
	var persistentVolumeClaims corev1.PersistentVolumeClaimList
	require.NoError(t, reconciler.List(ctx, &persistentVolumeClaims))
	assert.Len(t, persistentVolumeClaims.Items, 1)
	var services corev1.ServiceList
	require.NoError(t, reconciler.List(ctx, &services))
	assert.Len(t, services.Items, 4)
	var secrets corev1.SecretList
	require.NoError(t, reconciler.List(ctx, &secrets))
	assert.Len(t, secrets.Items, 1)
}

func TestCardanoNetworkReconcilerReconcilePatchesMutableDeploymentTemplate(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("patches-template")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	originalFingerprint := deployment.Spec.Template.Annotations[localnetFingerprintAnno]

	current := requireNetwork(t, ctx, reconciler, network)
	image := "example.com/cardano-node:patched"
	current.Spec.Node.Image = &image
	current.Spec.Node.Port = 3002
	current.Spec.Node.Resources = &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("250m"),
		},
	}
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
	container := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, image, container.Image)
	assert.Contains(t, container.Args, "3002")
	service := requirePrimaryService(t, ctx, reconciler, network)
	require.Len(t, service.Spec.Ports, 1)
	assert.Equal(t, int32(3002), service.Spec.Ports[0].Port)
	assertNodeToNodeEndpoint(t, ctx, reconciler, network, service.Name, int32(3002))
	cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
	assert.Zero(t, cpuRequest.Cmp(resource.MustParse("250m")))
	assert.Equal(t, originalFingerprint, deployment.Spec.Template.Annotations[localnetFingerprintAnno])
}

func TestCardanoNetworkReconcilerReconcileCorrectsPausedDeployment(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("corrects-paused-deployment")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	deployment.Spec.Paused = true
	deployment.Labels["example.com/foreign-label"] = "keep"
	deployment.Annotations = map[string]string{"example.com/foreign-annotation": "keep"}
	require.NoError(t, reconciler.Update(ctx, deployment))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.False(t, deployment.Spec.Paused)
	assert.Equal(t, "keep", deployment.Labels["example.com/foreign-label"])
	assert.Equal(t, "keep", deployment.Annotations["example.com/foreign-annotation"])
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcileCorrectsPrimaryServiceAndPreservesMetadata(t *testing.T) {
	const (
		clusterIP            = "10.0.0.42"
		foreignMetadataValue = "keep"
	)

	ctx := context.Background()
	network := localCardanoNetwork("corrects-service")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	service := requirePrimaryService(t, ctx, reconciler, network)
	ipFamilyPolicy := corev1.IPFamilyPolicySingleStack
	service.Labels["example.com/foreign-label"] = foreignMetadataValue
	service.Labels[labelAppManagedBy] = wrongManagedByLabelValue
	service.Annotations = map[string]string{"example.com/foreign-annotation": foreignMetadataValue}
	service.Spec.Type = corev1.ServiceTypeNodePort
	service.Spec.Selector = map[string]string{"unexpected": "true"}
	service.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "wrong",
			Protocol:   corev1.ProtocolTCP,
			Port:       9999,
			TargetPort: intstr.FromInt(9999),
			NodePort:   32000,
		},
	}
	service.Spec.ClusterIP = clusterIP
	service.Spec.ClusterIPs = []string{clusterIP}
	service.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv4Protocol}
	service.Spec.IPFamilyPolicy = &ipFamilyPolicy
	require.NoError(t, reconciler.Update(ctx, service))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	service = requirePrimaryService(t, ctx, reconciler, network)
	assert.Equal(t, foreignMetadataValue, service.Labels["example.com/foreign-label"])
	assert.Equal(t, "yacd", service.Labels[labelAppManagedBy])
	assert.Equal(t, foreignMetadataValue, service.Annotations["example.com/foreign-annotation"])
	assert.Equal(t, corev1.ServiceTypeClusterIP, service.Spec.Type)
	assert.Equal(t, primaryWorkloadSelectorLabels(network), service.Spec.Selector)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       cardanoNodePortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       network.Spec.Node.Port,
			TargetPort: intstr.FromString(cardanoNodePortName),
		},
	}, service.Spec.Ports)
	assert.Equal(t, clusterIP, service.Spec.ClusterIP)
	assert.Equal(t, []string{clusterIP}, service.Spec.ClusterIPs)
	assert.Equal(t, []corev1.IPFamily{corev1.IPv4Protocol}, service.Spec.IPFamilies)
	require.NotNil(t, service.Spec.IPFamilyPolicy)
	assert.Equal(t, corev1.IPFamilyPolicySingleStack, *service.Spec.IPFamilyPolicy)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcileCorrectsOgmiosServiceAndPreservesMetadata(t *testing.T) {
	const (
		clusterIP            = "10.0.0.43"
		foreignMetadataValue = "keep"
	)

	ctx := context.Background()
	network := localCardanoNetwork("corrects-ogmios-service")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	service := requirePrimaryOgmiosService(t, ctx, reconciler, network)
	ipFamilyPolicy := corev1.IPFamilyPolicySingleStack
	service.Labels["example.com/foreign-label"] = foreignMetadataValue
	service.Labels[labelAppManagedBy] = wrongManagedByLabelValue
	service.Annotations = map[string]string{"example.com/foreign-annotation": foreignMetadataValue}
	service.Spec.Type = corev1.ServiceTypeNodePort
	service.Spec.Selector = map[string]string{"unexpected": "true"}
	service.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "wrong",
			Protocol:   corev1.ProtocolTCP,
			Port:       9998,
			TargetPort: intstr.FromInt(9998),
			NodePort:   32001,
		},
	}
	service.Spec.ClusterIP = clusterIP
	service.Spec.ClusterIPs = []string{clusterIP}
	service.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv4Protocol}
	service.Spec.IPFamilyPolicy = &ipFamilyPolicy
	require.NoError(t, reconciler.Update(ctx, service))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	service = requirePrimaryOgmiosService(t, ctx, reconciler, network)
	assert.Equal(t, foreignMetadataValue, service.Labels["example.com/foreign-label"])
	assert.Equal(t, "yacd", service.Labels[labelAppManagedBy])
	assert.Equal(t, foreignMetadataValue, service.Annotations["example.com/foreign-annotation"])
	assert.Equal(t, corev1.ServiceTypeClusterIP, service.Spec.Type)
	assert.Equal(t, primaryWorkloadSelectorLabels(network), service.Spec.Selector)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       ogmiosPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultOgmiosPort,
			TargetPort: intstr.FromString(ogmiosPortName),
		},
	}, service.Spec.Ports)
	assert.Equal(t, clusterIP, service.Spec.ClusterIP)
	assert.Equal(t, []string{clusterIP}, service.Spec.ClusterIPs)
	assert.Equal(t, []corev1.IPFamily{corev1.IPv4Protocol}, service.Spec.IPFamilies)
	require.NotNil(t, service.Spec.IPFamilyPolicy)
	assert.Equal(t, corev1.IPFamilyPolicySingleStack, *service.Spec.IPFamilyPolicy)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcileCorrectsKupoServiceAndPreservesMetadata(t *testing.T) {
	const (
		clusterIP            = "10.0.0.44"
		foreignMetadataValue = "keep"
	)

	ctx := context.Background()
	network := localCardanoNetwork("corrects-kupo-service")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	service := requirePrimaryKupoService(t, ctx, reconciler, network)
	ipFamilyPolicy := corev1.IPFamilyPolicySingleStack
	service.Labels["example.com/foreign-label"] = foreignMetadataValue
	service.Labels[labelAppManagedBy] = wrongManagedByLabelValue
	service.Annotations = map[string]string{"example.com/foreign-annotation": foreignMetadataValue}
	service.Spec.Type = corev1.ServiceTypeNodePort
	service.Spec.Selector = map[string]string{"unexpected": "true"}
	service.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "wrong",
			Protocol:   corev1.ProtocolTCP,
			Port:       9997,
			TargetPort: intstr.FromInt(9997),
			NodePort:   32002,
		},
	}
	service.Spec.ClusterIP = clusterIP
	service.Spec.ClusterIPs = []string{clusterIP}
	service.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv4Protocol}
	service.Spec.IPFamilyPolicy = &ipFamilyPolicy
	require.NoError(t, reconciler.Update(ctx, service))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	service = requirePrimaryKupoService(t, ctx, reconciler, network)
	assert.Equal(t, foreignMetadataValue, service.Labels["example.com/foreign-label"])
	assert.Equal(t, "yacd", service.Labels[labelAppManagedBy])
	assert.Equal(t, foreignMetadataValue, service.Annotations["example.com/foreign-annotation"])
	assert.Equal(t, corev1.ServiceTypeClusterIP, service.Spec.Type)
	assert.Equal(t, primaryWorkloadSelectorLabels(network), service.Spec.Selector)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       kupoPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultKupoPort,
			TargetPort: intstr.FromString(kupoPortName),
		},
	}, service.Spec.Ports)
	assert.Equal(t, clusterIP, service.Spec.ClusterIP)
	assert.Equal(t, []string{clusterIP}, service.Spec.ClusterIPs)
	assert.Equal(t, []corev1.IPFamily{corev1.IPv4Protocol}, service.Spec.IPFamilies)
	require.NotNil(t, service.Spec.IPFamilyPolicy)
	assert.Equal(t, corev1.IPFamilyPolicySingleStack, *service.Spec.IPFamilyPolicy)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcileCorrectsFaucetServiceAndPreservesMetadata(t *testing.T) {
	const (
		clusterIP            = "10.0.0.45"
		foreignMetadataValue = "keep"
	)

	ctx := context.Background()
	network := localCardanoNetwork("corrects-faucet-service")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	service := requirePrimaryFaucetService(t, ctx, reconciler, network)
	ipFamilyPolicy := corev1.IPFamilyPolicySingleStack
	service.Labels["example.com/foreign-label"] = foreignMetadataValue
	service.Labels[labelAppManagedBy] = wrongManagedByLabelValue
	service.Annotations = map[string]string{"example.com/foreign-annotation": foreignMetadataValue}
	service.Spec.Type = corev1.ServiceTypeNodePort
	service.Spec.Selector = map[string]string{"unexpected": "true"}
	service.Spec.Ports = []corev1.ServicePort{
		{
			Name:       "wrong",
			Protocol:   corev1.ProtocolTCP,
			Port:       9996,
			TargetPort: intstr.FromInt(9996),
			NodePort:   32003,
		},
	}
	service.Spec.ClusterIP = clusterIP
	service.Spec.ClusterIPs = []string{clusterIP}
	service.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv4Protocol}
	service.Spec.IPFamilyPolicy = &ipFamilyPolicy
	require.NoError(t, reconciler.Update(ctx, service))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	service = requirePrimaryFaucetService(t, ctx, reconciler, network)
	assert.Equal(t, foreignMetadataValue, service.Labels["example.com/foreign-label"])
	assert.Equal(t, "yacd", service.Labels[labelAppManagedBy])
	assert.Equal(t, foreignMetadataValue, service.Annotations["example.com/foreign-annotation"])
	assert.Equal(t, corev1.ServiceTypeClusterIP, service.Spec.Type)
	assert.Equal(t, primaryWorkloadSelectorLabels(network), service.Spec.Selector)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       faucetPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultFaucetPort,
			TargetPort: intstr.FromString(faucetPortName),
		},
	}, service.Spec.Ports)
	assert.Equal(t, clusterIP, service.Spec.ClusterIP)
	assert.Equal(t, []string{clusterIP}, service.Spec.ClusterIPs)
	assert.Equal(t, []corev1.IPFamily{corev1.IPv4Protocol}, service.Spec.IPFamilies)
	require.NotNil(t, service.Spec.IPFamilyPolicy)
	assert.Equal(t, corev1.IPFamilyPolicySingleStack, *service.Spec.IPFamilyPolicy)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcilePreservesValidFaucetAuthToken(t *testing.T) {
	const (
		foreignMetadataValue = "keep"
		validToken           = "abcdefghijklmnopqrstuvwxyzABCDEF1234567890"
	)

	ctx := context.Background()
	network := localCardanoNetwork("preserves-faucet-token")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	secret := requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	secret.Labels["example.com/foreign-label"] = foreignMetadataValue
	secret.Labels[labelAppManagedBy] = wrongManagedByLabelValue
	secret.Annotations = map[string]string{"example.com/foreign-annotation": foreignMetadataValue}
	secret.Type = corev1.SecretTypeBasicAuth
	secret.Data[faucetAuthTokenKey] = []byte(validToken)
	require.NoError(t, reconciler.Update(ctx, secret))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	secret = requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	assert.Equal(t, foreignMetadataValue, secret.Labels["example.com/foreign-label"])
	assert.Equal(t, "yacd", secret.Labels[labelAppManagedBy])
	assert.Equal(t, foreignMetadataValue, secret.Annotations["example.com/foreign-annotation"])
	assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
	assert.Equal(t, validToken, string(secret.Data[faucetAuthTokenKey]))
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcileRegeneratesInvalidFaucetAuthToken(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("regenerates-faucet-token")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	secret := requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	secret.Data[faucetAuthTokenKey] = []byte("short")
	require.NoError(t, reconciler.Update(ctx, secret))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	secret = requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	token := string(secret.Data[faucetAuthTokenKey])
	assert.NotEqual(t, "short", token)
	assert.True(t, validFaucetAuthToken(token))
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcileRepairsMissingFaucetAuthSecret(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("repairs-faucet-token")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	secret := requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	require.NoError(t, reconciler.Delete(ctx, secret))
	assertNoPrimaryFaucetAuthSecret(t, ctx, reconciler, network)

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	secret = requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	assert.True(t, validFaucetAuthToken(string(secret.Data[faucetAuthTokenKey])))
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerApplyPrimaryDeploymentIgnoresAPIDefaults(t *testing.T) {
	const foreignMetadataValue = "keep"

	ctx := context.Background()
	network := localCardanoNetwork("ignores-api-defaults")
	reconciler := newTestReconciler(t, network)
	resources, err := (primaryWorkloadBuilder{scheme: reconciler.Scheme}).Build(network)
	require.NoError(t, err)

	result, err := reconciler.applyPrimaryDeployment(ctx, resources.Deployment)
	require.NoError(t, err)
	require.Equal(t, controllerutil.OperationResultCreated, result)

	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	applyDeploymentAPIDefaults(deployment)
	if deployment.Annotations == nil {
		deployment.Annotations = map[string]string{}
	}
	deployment.Labels["example.com/foreign-label"] = foreignMetadataValue
	deployment.Annotations["example.com/foreign-annotation"] = foreignMetadataValue
	deployment.Spec.Template.Labels["example.com/foreign-template-label"] = foreignMetadataValue
	deployment.Spec.Template.Annotations["example.com/foreign-template-annotation"] = foreignMetadataValue
	require.NoError(t, reconciler.Update(ctx, deployment))

	result, err = reconciler.applyPrimaryDeployment(ctx, resources.Deployment)
	require.NoError(t, err)
	assert.Equal(t, controllerutil.OperationResultNone, result)

	deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.Equal(t, corev1.RestartPolicyAlways, deployment.Spec.Template.Spec.RestartPolicy)
	assert.Equal(t, corev1.DNSClusterFirst, deployment.Spec.Template.Spec.DNSPolicy)
	assert.Equal(t, corev1.DefaultSchedulerName, deployment.Spec.Template.Spec.SchedulerName)
	require.NotNil(t, deployment.Spec.Template.Spec.TerminationGracePeriodSeconds)
	assert.Equal(t, int64(30), *deployment.Spec.Template.Spec.TerminationGracePeriodSeconds)
	assert.Equal(t, foreignMetadataValue, deployment.Labels["example.com/foreign-label"])
	assert.Equal(t, foreignMetadataValue, deployment.Annotations["example.com/foreign-annotation"])
	assert.Equal(t, foreignMetadataValue, deployment.Spec.Template.Labels["example.com/foreign-template-label"])
	assert.Equal(t, foreignMetadataValue, deployment.Spec.Template.Annotations["example.com/foreign-template-annotation"])
}

func TestCardanoNetworkReconcilerReconcileRejectsLocalnetInputChanges(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*yacdv1alpha1.CardanoNetwork)
	}{
		{
			name: "network-magic",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local.NetworkMagic = 43
			},
		},
		{
			name: "node-version",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Node.Version = "11.0.2"
			},
		},
		{
			name: "timing",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local.Timing.EpochLength = 600
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			network := localCardanoNetwork("rejects-localnet-" + tt.name)
			reconciler := newTestReconciler(t, network)

			_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
			require.NoError(t, err)

			deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
			originalFingerprint := deployment.Spec.Template.Annotations[localnetFingerprintAnno]
			pvc := requirePrimaryPVC(t, ctx, reconciler, network)
			require.Equal(t, originalFingerprint, pvc.Annotations[localnetFingerprintAnno])
			require.Equal(t, originalFingerprint, requireAcceptedLocalnetFingerprint(t, ctx, reconciler, network))

			current := requireNetwork(t, ctx, reconciler, network)
			tt.mutate(current)
			require.NoError(t, reconciler.Update(ctx, current))

			_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
			require.NoError(t, err)

			pvc = requirePrimaryPVC(t, ctx, reconciler, network)
			assert.Equal(t, originalFingerprint, pvc.Annotations[localnetFingerprintAnno])
			deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
			assert.Equal(t, originalFingerprint, deployment.Spec.Template.Annotations[localnetFingerprintAnno])
			assert.Equal(t, originalFingerprint, requireAcceptedLocalnetFingerprint(t, ctx, reconciler, network))
			assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedLocalnetChange)
			assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonUnsupportedLocalnetChange)
		})
	}
}

func TestCardanoNetworkReconcilerReconcileRejectsLocalnetInputChangeAfterPVCDeletion(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("rejects-localnet-after-pvc-delete")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	originalFingerprint := requireAcceptedLocalnetFingerprint(t, ctx, reconciler, network)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	require.NoError(t, reconciler.Delete(ctx, pvc))

	current := requireNetwork(t, ctx, reconciler, network)
	current.Spec.Local.NetworkMagic = 43
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	err = reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryNodeStatePVCName(network),
	}, &corev1.PersistentVolumeClaim{})
	assert.True(t, apierrors.IsNotFound(err), "expected PVC to remain absent, got %v", err)
	assert.Equal(t, originalFingerprint, requireAcceptedLocalnetFingerprint(t, ctx, reconciler, network))
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedLocalnetChange)
}

func TestCardanoNetworkReconcilerReconcileRejectsMissingLocalnetFingerprint(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("missing-localnet-fingerprint")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	delete(pvc.Annotations, localnetFingerprintAnno)
	require.NoError(t, reconciler.Update(ctx, pvc))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc = requirePrimaryPVC(t, ctx, reconciler, network)
	assert.Empty(t, pvc.Annotations[localnetFingerprintAnno])
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonMissingLocalnetFingerprint)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonMissingLocalnetFingerprint)
}

func TestCardanoNetworkReconcilerReconcileExpandsStorage(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("expands-storage")
	network.Spec.Node.Storage = &yacdv1alpha1.NodeStorageSpec{
		Size: resource.MustParse("10Gi"),
	}
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	current := requireNetwork(t, ctx, reconciler, network)
	current.Spec.Node.Storage.Size = resource.MustParse("20Gi")
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Zero(t, storage.Cmp(resource.MustParse("20Gi")))
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcilePreservesPVCForeignMetadata(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("preserves-pvc-metadata")
	network.Spec.Node.Storage = &yacdv1alpha1.NodeStorageSpec{
		Size: resource.MustParse("10Gi"),
	}
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	pvc.Labels["example.com/foreign-label"] = "keep"
	pvc.Labels[labelAppManagedBy] = wrongManagedByLabelValue
	pvc.Annotations["volume.kubernetes.io/selected-node"] = "kind-worker"
	require.NoError(t, reconciler.Update(ctx, pvc))

	current := requireNetwork(t, ctx, reconciler, network)
	current.Spec.Node.Storage.Size = resource.MustParse("20Gi")
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc = requirePrimaryPVC(t, ctx, reconciler, network)
	storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Zero(t, storage.Cmp(resource.MustParse("20Gi")))
	assert.Equal(t, "keep", pvc.Labels["example.com/foreign-label"])
	assert.Equal(t, "yacd", pvc.Labels[labelAppManagedBy])
	assert.Equal(t, "kind-worker", pvc.Annotations["volume.kubernetes.io/selected-node"])
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcileRejectsStorageShrink(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("rejects-storage-shrink")
	network.Spec.Node.Storage = &yacdv1alpha1.NodeStorageSpec{
		Size: resource.MustParse("20Gi"),
	}
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	current := requireNetwork(t, ctx, reconciler, network)
	current.Spec.Node.Storage.Size = resource.MustParse("10Gi")
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Zero(t, storage.Cmp(resource.MustParse("20Gi")))
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedStorageChange)
}

func TestCardanoNetworkReconcilerReconcileRejectsStorageClassDrift(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("rejects-storage-class-drift")
	storageClassName := testStorageClassName
	network.Spec.Node.Storage = &yacdv1alpha1.NodeStorageSpec{
		Size:             resource.MustParse("10Gi"),
		StorageClassName: &storageClassName,
	}
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	current := requireNetwork(t, ctx, reconciler, network)
	newStorageClassName := "slow"
	current.Spec.Node.Storage.StorageClassName = &newStorageClassName
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	require.NotNil(t, pvc.Spec.StorageClassName)
	assert.Equal(t, testStorageClassName, *pvc.Spec.StorageClassName)
	assert.Equal(t, testStorageClassName, pvc.Annotations[ctrlstorage.RequestedStorageClassAnnotation])
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedStorageChange)
}

func TestCardanoNetworkReconcilerReconcileRejectsStorageClassRemoval(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("rejects-storage-class-removal")
	storageClassName := testStorageClassName
	network.Spec.Node.Storage = &yacdv1alpha1.NodeStorageSpec{
		Size:             resource.MustParse("10Gi"),
		StorageClassName: &storageClassName,
	}
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	current := requireNetwork(t, ctx, reconciler, network)
	current.Spec.Node.Storage.StorageClassName = nil
	require.NoError(t, reconciler.Update(ctx, current))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	require.NotNil(t, pvc.Spec.StorageClassName)
	assert.Equal(t, testStorageClassName, *pvc.Spec.StorageClassName)
	assert.Equal(t, testStorageClassName, pvc.Annotations[ctrlstorage.RequestedStorageClassAnnotation])
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedStorageChange)
}

func TestCardanoNetworkReconcilerReconcileToleratesDefaultedStorageClass(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("tolerates-default-storage-class")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	defaultStorageClassName := "cluster-default"
	pvc.Spec.StorageClassName = &defaultStorageClassName
	require.NoError(t, reconciler.Update(ctx, pvc))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc = requirePrimaryPVC(t, ctx, reconciler, network)
	require.NotNil(t, pvc.Spec.StorageClassName)
	assert.Equal(t, defaultStorageClassName, *pvc.Spec.StorageClassName)
	assert.NotContains(t, pvc.Annotations, ctrlstorage.RequestedStorageClassAnnotation)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcileRejectsDeploymentSelectorDrift(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("rejects-selector-drift")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	deployment.Spec.Selector.MatchLabels[labelCardanoRole] = "unexpected"
	require.NoError(t, reconciler.Update(ctx, deployment))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedWorkloadChange)
}

func TestCardanoNetworkReconcilerReconcileRejectsChildResourceCollisions(t *testing.T) {
	tests := []struct {
		name  string
		child func(*yacdv1alpha1.CardanoNetwork) client.Object
	}{
		{
			name: "foreign-owned-network-artifacts-configmap",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:            networkArtifactsConfigMapName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-network-artifacts-configmap",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      networkArtifactsConfigMapName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
		{
			name: "foreign-owned-artifact-publisher-serviceaccount",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:            artifactPublisherServiceAccountName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-artifact-publisher-serviceaccount",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      artifactPublisherServiceAccountName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
		{
			name: "foreign-owned-artifact-publisher-role",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:            artifactPublisherRoleName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-artifact-publisher-role",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &rbacv1.Role{
					ObjectMeta: metav1.ObjectMeta{
						Name:      artifactPublisherRoleName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
		{
			name: "foreign-owned-artifact-publisher-rolebinding",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:            artifactPublisherRoleBindingName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-artifact-publisher-rolebinding",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						Name:      artifactPublisherRoleBindingName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
		{
			name: "foreign-owned-pvc",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:            primaryNodeStatePVCName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-pvc",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      primaryNodeStatePVCName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
		{
			name: "foreign-owned-deployment",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:            primaryWorkloadName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-deployment",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      primaryWorkloadName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
		{
			name: "foreign-owned-service",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            primaryWorkloadName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-service",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      primaryWorkloadName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
		{
			name: "foreign-owned-ogmios-service",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            primaryOgmiosServiceName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-ogmios-service",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      primaryOgmiosServiceName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
		{
			name: "foreign-owned-kupo-service",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            primaryKupoServiceName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-kupo-service",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      primaryKupoServiceName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
		{
			name: "foreign-owned-faucet-service",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:            primaryFaucetServiceName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-faucet-service",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      primaryFaucetServiceName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
		{
			name: "foreign-owned-faucet-secret",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:            primaryFaucetAuthSecretName(network),
						Namespace:       network.Namespace,
						OwnerReferences: []metav1.OwnerReference{foreignControllerOwnerReference()},
					},
				}
			},
		},
		{
			name: "unowned-faucet-secret",
			child: func(network *yacdv1alpha1.CardanoNetwork) client.Object {
				return &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      primaryFaucetAuthSecretName(network),
						Namespace: network.Namespace,
					},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			network := localCardanoNetwork("collision-" + tt.name)
			network.UID = types.UID("cardanonetwork-" + tt.name)
			reconciler := newTestReconciler(t, network, tt.child(network))

			result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{RequeueAfter: resourceConflictRequeueAfter}, result)

			assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonResourceConflict)
			assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonResourceConflict)
			assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonResourceConflict)
			assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionFalse, conditionReasonResourceConflict)
			assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonResourceConflict)
			assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonResourceConflict)
			assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonResourceConflict)
			assertCondition(t, ctx, reconciler, network, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonResourceConflict)
		})
	}
}

func TestCardanoNetworkReconcilerReconcileReturnsInternalBuildErrors(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("internal-build-error")
	reconciler := newTestReconciler(t, network)
	reconciler.Scheme = nil

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme is required")
	assert.Equal(t, ctrl.Result{}, result)
	current := requireNetwork(t, ctx, reconciler, network)
	assert.Empty(t, current.Status.Conditions)
}

// TestCardanoNetworkReconcilerReconcileMarksUnsupportedInput verifies adapter
// rejections are surfaced through status without creating children.
func TestCardanoNetworkReconcilerReconcileMarksUnsupportedInput(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("unsupported-input")
	network.Spec.Local.Era = yacdv1alpha1.CardanoEraBabbage
	reconciler := newTestReconciler(t, network)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	assertNoPrimaryChildren(t, ctx, reconciler, network)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	current := requireNetwork(t, ctx, reconciler, network)
	assert.Nil(t, current.Status.Endpoints)
}

func TestCardanoNetworkReconcilerReconcileRevokesFaucetOnUnsupportedSpec(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("revokes-unsupported-faucet")
	enableFaucet(network)
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	requirePrimaryFaucetService(t, ctx, reconciler, network)
	requirePrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	assertFaucetEndpoint(t, ctx, reconciler, network, primaryFaucetServiceName(network), defaultFaucetPort)
	assertFaucetStatus(t, ctx, reconciler, network, primaryFaucetAuthSecretName(network))

	current := requireNetwork(t, ctx, reconciler, network)
	current.Spec.ChainAPI.Faucet.DefaultSource = "../utxo1"
	require.NoError(t, reconciler.Update(ctx, current))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	assertNoPrimaryFaucetService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	assertNoContainerNamed(t, deployment.Spec.Template.Spec.InitContainers, faucetSourceAddressInitContainerName)
	assertNoContainerNamed(t, deployment.Spec.Template.Spec.Containers, faucetContainerName)
	assertNoVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, faucetAuthVolumeName)
	assertCondition(t, ctx, reconciler, network, conditionTypeFaucetReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
	current = requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Faucet)
	assert.Nil(t, current.Status.Faucet)
}

func TestCardanoNetworkReconcilerPrimaryNodeReadyConditionReportsMissingChildren(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("missing-children")
	reconciler := newTestReconciler(t, network)
	resources, err := (primaryWorkloadBuilder{scheme: reconciler.Scheme}).Build(network)
	require.NoError(t, err)

	got, err := reconciler.primaryNodeReadyCondition(ctx, network)
	require.NoError(t, err)
	assert.Equal(t, conditionTypeNodeReady, got.Type)
	assert.Equal(t, metav1.ConditionFalse, got.Status)
	assert.Equal(t, conditionReasonPrimaryWorkloadMissing, got.Reason)
	assert.Equal(t, "Primary node PVC is missing", got.Message)

	require.NoError(t, reconciler.Create(ctx, resources.PersistentVolumeClaim))
	got, err = reconciler.primaryNodeReadyCondition(ctx, network)
	require.NoError(t, err)
	assert.Equal(t, metav1.ConditionFalse, got.Status)
	assert.Equal(t, conditionReasonPrimaryWorkloadMissing, got.Reason)
	assert.Equal(t, "Primary node Service is missing", got.Message)

	require.NoError(t, reconciler.Create(ctx, resources.Service))
	got, err = reconciler.primaryNodeReadyCondition(ctx, network)
	require.NoError(t, err)
	assert.Equal(t, metav1.ConditionFalse, got.Status)
	assert.Equal(t, conditionReasonPrimaryWorkloadMissing, got.Reason)
	assert.Equal(t, "Primary node Deployment is missing", got.Message)
}

func TestCardanoNetworkReconcilerPrimaryNodeReadyConditionRequiresFreshAvailableDeployment(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("stale-deployment")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)

	deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
	deployment.Status.ObservedGeneration = deployment.Generation + 1
	require.NoError(t, reconciler.Status().Update(ctx, deployment))

	got, err := reconciler.primaryNodeReadyCondition(ctx, network)
	require.NoError(t, err)
	assert.Equal(t, metav1.ConditionFalse, got.Status)
	assert.Equal(t, conditionReasonDeploymentProgressing, got.Reason)
	assert.Equal(t, "Primary node Deployment has not observed the latest generation", got.Message)
}

// localCardanoNetwork returns a minimally supported local-mode CardanoNetwork.
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
					SlotLength:  metav1.Duration{Duration: defaultLocalSlotLength},
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

func enableFaucet(network *yacdv1alpha1.CardanoNetwork) {
	if network.Spec.ChainAPI == nil {
		network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{}
	}
	network.Spec.ChainAPI.Faucet = &yacdv1alpha1.FaucetSpec{
		Enabled:          true,
		Port:             defaultFaucetPort,
		DefaultSource:    defaultFaucetSource,
		MinTopUpLovelace: defaultFaucetMinLovelace,
		MaxTopUpLovelace: defaultFaucetMaxLovelace,
	}
}

// newTestReconciler returns a CardanoNetworkReconciler backed by a fake client.
func newTestReconciler(t *testing.T, objects ...client.Object) *CardanoNetworkReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&yacdv1alpha1.CardanoNetwork{}, &appsv1.Deployment{}, &corev1.Pod{})
	builder.WithObjects(objects...)
	fakeClient := builder.Build()

	return &CardanoNetworkReconciler{
		Client: fakeClient,
		Reader: fakeClient,
		Scheme: scheme,
	}
}

func requireNetwork(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *yacdv1alpha1.CardanoNetwork {
	t.Helper()

	current := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, reconciler.Get(ctx, reconcileRequestFor(network).NamespacedName, current))

	return current
}

func requireAcceptedLocalnetFingerprint(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) string {
	t.Helper()

	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Network)
	require.NotEmpty(t, current.Status.Network.LocalnetFingerprint)

	return current.Status.Network.LocalnetFingerprint
}

func requirePrimaryPVC(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.PersistentVolumeClaim {
	t.Helper()

	pvc := &corev1.PersistentVolumeClaim{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryNodeStatePVCName(network),
	}, pvc))

	return pvc
}

func requirePrimaryDeployment(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *appsv1.Deployment {
	t.Helper()

	deployment := &appsv1.Deployment{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryWorkloadName(network),
	}, deployment))

	return deployment
}

func requirePrimaryService(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.Service {
	t.Helper()

	service := &corev1.Service{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryWorkloadName(network),
	}, service))

	return service
}

func requirePrimaryOgmiosService(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.Service {
	t.Helper()

	service := &corev1.Service{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryOgmiosServiceName(network),
	}, service))

	return service
}

func requirePrimaryKupoService(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.Service {
	t.Helper()

	service := &corev1.Service{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryKupoServiceName(network),
	}, service))

	return service
}

func requirePrimaryFaucetService(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.Service {
	t.Helper()

	service := &corev1.Service{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryFaucetServiceName(network),
	}, service))

	return service
}

func requirePrimaryFaucetAuthSecret(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.Secret {
	t.Helper()

	secret := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryFaucetAuthSecretName(network),
	}, secret))

	return secret
}

func requireNetworkArtifactsConfigMap(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.ConfigMap {
	t.Helper()

	configMap := &corev1.ConfigMap{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      networkArtifactsConfigMapName(network),
	}, configMap))

	return configMap
}

func requireArtifactPublisherServiceAccount(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.ServiceAccount {
	t.Helper()

	serviceAccount := &corev1.ServiceAccount{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      artifactPublisherServiceAccountName(network),
	}, serviceAccount))

	return serviceAccount
}

func requireArtifactPublisherRole(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *rbacv1.Role {
	t.Helper()

	role := &rbacv1.Role{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      artifactPublisherRoleName(network),
	}, role))

	return role
}

func requireArtifactPublisherRoleBinding(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *rbacv1.RoleBinding {
	t.Helper()

	roleBinding := &rbacv1.RoleBinding{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      artifactPublisherRoleBindingName(network),
	}, roleBinding))

	return roleBinding
}

func publishNetworkArtifacts(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.ConfigMap {
	t.Helper()

	configMap := requireNetworkArtifactsConfigMap(t, ctx, reconciler, network)
	if configMap.Annotations == nil {
		configMap.Annotations = map[string]string{}
	}
	configMap.Annotations[ctrlartifacts.SchemaVersionAnnotation] = networkartifacts.SchemaVersion
	configMap.Annotations[ctrlartifacts.DataHashAnnotation] = testNetworkArtifactsDataHash
	if configMap.Data == nil {
		configMap.Data = map[string]string{}
	}
	maps.Copy(configMap.Data, testNetworkArtifactsData())
	require.NoError(t, reconciler.Update(ctx, configMap))

	return configMap
}

func testNetworkArtifactsData() map[string]string {
	return map[string]string{
		networkartifacts.ConfigurationKey:   "test configuration.yaml",
		networkartifacts.ByronGenesisKey:    "test byron-genesis.json",
		networkartifacts.ShelleyGenesisKey:  "test shelley-genesis.json",
		networkartifacts.AlonzoGenesisKey:   "test alonzo-genesis.json",
		networkartifacts.ConwayGenesisKey:   "test conway-genesis.json",
		networkartifacts.PrimaryTopologyKey: "test primary-topology.json",
		networkartifacts.PlanManifestKey:    "test yacd-localnet-plan.json",
		networkartifacts.ConnectionKey:      "test connection.json",
	}
}

func foreignControllerOwnerReference() metav1.OwnerReference {
	controller := true

	return metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "foreign-owner",
		UID:        types.UID("foreign-owner"),
		Controller: &controller,
	}
}

func applyDeploymentAPIDefaults(deployment *appsv1.Deployment) {
	terminationGracePeriodSeconds := int64(30)
	deployment.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyAlways
	deployment.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
	deployment.Spec.Template.Spec.SchedulerName = corev1.DefaultSchedulerName
	deployment.Spec.Template.Spec.TerminationGracePeriodSeconds = &terminationGracePeriodSeconds
}

func markPrimaryDeploymentAvailable(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	deployment *appsv1.Deployment,
) {
	t.Helper()

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
	require.NoError(t, reconciler.Status().Update(ctx, deployment))
}

func markPrimaryPodContainersReady(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
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
			Name:      primaryWorkloadName(network) + "-pod",
			Namespace: network.Namespace,
			Labels:    primaryWorkloadSelectorLabels(network),
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

func assertNoPrimaryChildren(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	err := reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryWorkloadName(network),
	}, &appsv1.Deployment{})
	assert.True(t, apierrors.IsNotFound(err), "expected primary Deployment to be absent, got %v", err)

	err = reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryNodeStatePVCName(network),
	}, &corev1.PersistentVolumeClaim{})
	assert.True(t, apierrors.IsNotFound(err), "expected primary PVC to be absent, got %v", err)

	err = reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryWorkloadName(network),
	}, &corev1.Service{})
	assert.True(t, apierrors.IsNotFound(err), "expected primary Service to be absent, got %v", err)

	assertNoPrimaryOgmiosService(t, ctx, reconciler, network)
	assertNoPrimaryKupoService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetService(t, ctx, reconciler, network)
	assertNoPrimaryFaucetAuthSecret(t, ctx, reconciler, network)
	assertNoNetworkArtifactChildren(t, ctx, reconciler, network)
}

func assertNoNetworkArtifactChildren(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	err := reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      networkArtifactsConfigMapName(network),
	}, &corev1.ConfigMap{})
	assert.True(t, apierrors.IsNotFound(err), "expected network artifacts ConfigMap to be absent, got %v", err)

	err = reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      artifactPublisherServiceAccountName(network),
	}, &corev1.ServiceAccount{})
	assert.True(t, apierrors.IsNotFound(err), "expected artifact publisher ServiceAccount to be absent, got %v", err)

	err = reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      artifactPublisherRoleName(network),
	}, &rbacv1.Role{})
	assert.True(t, apierrors.IsNotFound(err), "expected artifact publisher Role to be absent, got %v", err)

	err = reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      artifactPublisherRoleBindingName(network),
	}, &rbacv1.RoleBinding{})
	assert.True(t, apierrors.IsNotFound(err), "expected artifact publisher RoleBinding to be absent, got %v", err)
}

func assertNoPrimaryOgmiosService(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	err := reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryOgmiosServiceName(network),
	}, &corev1.Service{})
	assert.True(t, apierrors.IsNotFound(err), "expected Ogmios Service to be absent, got %v", err)
}

func assertNoPrimaryKupoService(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	err := reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryKupoServiceName(network),
	}, &corev1.Service{})
	assert.True(t, apierrors.IsNotFound(err), "expected Kupo Service to be absent, got %v", err)
}

func assertNoPrimaryFaucetService(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	err := reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryFaucetServiceName(network),
	}, &corev1.Service{})
	assert.True(t, apierrors.IsNotFound(err), "expected faucet Service to be absent, got %v", err)
}

func assertNoPrimaryFaucetAuthSecret(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	err := reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryFaucetAuthSecretName(network),
	}, &corev1.Secret{})
	assert.True(t, apierrors.IsNotFound(err), "expected faucet auth Secret to be absent, got %v", err)
}

func assertNoContainerNamed(t *testing.T, containers []corev1.Container, name string) {
	t.Helper()

	for _, container := range containers {
		assert.NotEqual(t, name, container.Name)
	}
}

func assertNoVolumeNamed(t *testing.T, volumes []corev1.Volume, name string) {
	t.Helper()

	for _, volume := range volumes {
		assert.NotEqual(t, name, volume.Name)
	}
}

func assertCondition(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
) {
	t.Helper()

	current := requireNetwork(t, ctx, reconciler, network)
	condition := apimeta.FindStatusCondition(current.Status.Conditions, conditionType)
	require.NotNil(t, condition)
	assert.Equal(t, status, condition.Status)
	assert.Equal(t, reason, condition.Reason)
	assert.Equal(t, current.Generation, condition.ObservedGeneration)
	assert.Equal(t, current.Generation, current.Status.ObservedGeneration)
}

func assertNodeToNodeEndpoint(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
	serviceName string,
	port int32,
) {
	t.Helper()

	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	require.NotNil(t, current.Status.Endpoints.NodeToNode)
	assert.Equal(t, serviceName, current.Status.Endpoints.NodeToNode.ServiceName)
	assert.Equal(t, port, current.Status.Endpoints.NodeToNode.Port)
	assert.Equal(t,
		fmt.Sprintf("tcp://%s.%s.svc.cluster.local:%d", serviceName, network.Namespace, port),
		current.Status.Endpoints.NodeToNode.URL,
	)
}

func assertOgmiosEndpoint(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
	serviceName string,
	port int32,
) {
	t.Helper()

	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	require.NotNil(t, current.Status.Endpoints.Ogmios)
	assert.Equal(t, serviceName, current.Status.Endpoints.Ogmios.ServiceName)
	assert.Equal(t, port, current.Status.Endpoints.Ogmios.Port)
	assert.Equal(t,
		fmt.Sprintf("ws://%s.%s.svc.cluster.local:%d", serviceName, network.Namespace, port),
		current.Status.Endpoints.Ogmios.URL,
	)
}

func assertKupoEndpoint(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
	serviceName string,
	port int32,
) {
	t.Helper()

	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	require.NotNil(t, current.Status.Endpoints.Kupo)
	assert.Equal(t, serviceName, current.Status.Endpoints.Kupo.ServiceName)
	assert.Equal(t, port, current.Status.Endpoints.Kupo.Port)
	assert.Equal(t,
		fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName, network.Namespace, port),
		current.Status.Endpoints.Kupo.URL,
	)
}

func assertFaucetEndpoint(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
	serviceName string,
	port int32,
) {
	t.Helper()

	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	require.NotNil(t, current.Status.Endpoints.Faucet)
	assert.Equal(t, serviceName, current.Status.Endpoints.Faucet.ServiceName)
	assert.Equal(t, port, current.Status.Endpoints.Faucet.Port)
	assert.Equal(t,
		fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName, network.Namespace, port),
		current.Status.Endpoints.Faucet.URL,
	)
}

func assertFaucetStatus(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
	authSecretName string,
) {
	t.Helper()

	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Faucet)
	assert.Equal(t, authSecretName, current.Status.Faucet.AuthSecretName)
}

// reconcileRequestFor returns a reconcile request targeting object.
func reconcileRequestFor(object *yacdv1alpha1.CardanoNetwork) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: object.Namespace,
			Name:      object.Name,
		},
	}
}
