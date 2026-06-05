package cardanonetwork

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	ctrldbsync "github.com/meigma/yacd/internal/controller/cardanodbsync"
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
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	wrongManagedByLabelValue  = "wrong"
	forgedLocalnetFingerprint = "cafebabe-forged-localnet"
	testDBSyncSidecarRevision = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
)

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
	reconciler := newTestReconciler(t, network)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, result)
	requirePrimaryPVC(t, ctx, reconciler, network)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	service := requirePrimaryService(t, ctx, reconciler, network)
	ogmiosService := requirePrimaryOgmiosService(t, ctx, reconciler, network)
	kupoService := requirePrimaryKupoService(t, ctx, reconciler, network)
	artifactsService := requirePrimaryArtifactsService(t, ctx, reconciler, network)
	require.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.False(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.Empty(t, deployment.Spec.Template.Spec.ServiceAccountName)
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
			Name:       servePortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultServePort,
			TargetPort: intstr.FromString(servePortName),
		},
	}, artifactsService.Spec.Ports)
	assert.Equal(t, deployment.Spec.Template.Annotations[localnetFingerprintAnno], requireAcceptedLocalnetFingerprint(t, ctx, reconciler, network))
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertNodeToNodeEndpoint(t, ctx, reconciler, network, service.Name, network.Spec.Node.Port)
	assertOgmiosEndpoint(t, ctx, reconciler, network, ogmiosService.Name, defaultOgmiosPort)
	assertKupoEndpoint(t, ctx, reconciler, network, kupoService.Name, defaultKupoPort)
	assertArtifactsEndpoint(t, ctx, reconciler, network, artifactsService.Name, defaultServePort)
}

func TestCardanoNetworkReconcilerReconcileCreatesPublicPreviewWorkload(t *testing.T) {
	ctx := context.Background()
	network := publicPreviewCardanoNetwork("preview-workload")
	reconciler := newTestReconciler(t, network)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, result)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	// Preview is a curated public profile: it gets the cardano-tools fetch
	// init container and the always-on serve sidecar.
	require.Len(t, deployment.Spec.Template.Spec.InitContainers, 1)
	assert.Equal(t, servedArtifactsInitContainerName, deployment.Spec.Template.Spec.InitContainers[0].Name)
	assert.Equal(t, []string{
		"fetch",
		"--profile", "preview",
		"--output-dir", "/state/artifacts",
	}, deployment.Spec.Template.Spec.InitContainers[0].Args)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 3)
	assert.Equal(t, cardanoNodeContainerName, deployment.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, ogmiosContainerName, deployment.Spec.Template.Spec.Containers[1].Name)
	assert.Equal(t, serveContainerName, deployment.Spec.Template.Spec.Containers[2].Name)
	assertNoContainerNamed(t, deployment.Spec.Template.Spec.Containers, kupoContainerName)
	assert.Equal(t, "3eee469d6200db89fd64fbd032ccbb58a7ba557b920a07bc2f22523b6f009a29", deployment.Spec.Template.Annotations[networkFingerprintAnno])
	assert.NotContains(t, deployment.Spec.Template.Annotations, localnetFingerprintAnno)
	// The public node and ogmios read their config from the fetched
	// served-artifact directory on the node-state PVC, not a /profile ConfigMap.
	assertNoVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, "network-artifacts")
	assertNoPrimaryKupoService(t, ctx, reconciler, network)

	artifactsService := requirePrimaryArtifactsService(t, ctx, reconciler, network)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       servePortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultServePort,
			TargetPort: intstr.FromString(servePortName),
		},
	}, artifactsService.Spec.Ports)

	// The serve sidecar has no ready Pod yet, so ArtifactsReady is progressing.
	assertCondition(t, ctx, reconciler, network, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertNodeToNodeEndpoint(t, ctx, reconciler, network, primaryWorkloadName(network), network.Spec.Node.Port)
	assertOgmiosEndpoint(t, ctx, reconciler, network, primaryOgmiosServiceName(network), defaultOgmiosPort)
	assertArtifactsEndpoint(t, ctx, reconciler, network, primaryArtifactsServiceName(network), defaultServePort)
	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Network)
	assert.Equal(t, yacdv1alpha1.CardanoNetworkModePublic, current.Status.Network.Mode)
	assert.Equal(t, deployment.Spec.Template.Annotations[networkFingerprintAnno], current.Status.Network.NetworkFingerprint)
	assert.Empty(t, current.Status.Network.LocalnetFingerprint)
	require.NotNil(t, current.Status.Network.Profile)
	assert.Equal(t, yacdv1alpha1.PublicNetworkProfilePreview, *current.Status.Network.Profile)
	require.NotNil(t, current.Status.Network.NetworkMagic)
	assert.Equal(t, int64(2), *current.Status.Network.NetworkMagic)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Kupo)
}

func TestCardanoNetworkReconcilerReconcileRepairsForgedNetworkIdentityStatus(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("repairs-forged-network-status")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	baseline := requireAcceptedNetworkIdentityStatus(t, ctx, reconciler, network)
	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	require.Equal(t, baseline.NetworkFingerprint, pvc.Annotations[networkFingerprintAnno])
	require.Equal(t, baseline.LocalnetFingerprint, pvc.Annotations[localnetFingerprintAnno])

	current := requireNetwork(t, ctx, reconciler, network)
	current.Status.Network.NetworkFingerprint = "deadbeef-forged-network"
	current.Status.Network.LocalnetFingerprint = forgedLocalnetFingerprint
	storeNetworkStatus(t, ctx, reconciler, current)

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	repaired := requireAcceptedNetworkIdentityStatus(t, ctx, reconciler, network)
	assert.Equal(t, baseline, repaired)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
}

func TestCardanoNetworkReconcilerReconcileCreatesPublicMainnetWorkload(t *testing.T) {
	ctx := context.Background()
	network := publicCardanoNetwork("mainnet-workload", yacdv1alpha1.PublicNetworkProfileMainnet)
	network.Spec.Public.Bootstrap = &yacdv1alpha1.PublicNetworkBootstrapSpec{
		Mithril: &yacdv1alpha1.MithrilBootstrapSpec{},
	}
	reconciler := newTestReconciler(t, network)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, result)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	// Mainnet is a curated public profile, so the fetch init container is
	// ordered before the Mithril bootstrap, and the serve sidecar runs.
	require.Len(t, deployment.Spec.Template.Spec.InitContainers, 2)
	assert.Equal(t, servedArtifactsInitContainerName, deployment.Spec.Template.Spec.InitContainers[0].Name)
	assert.Equal(t, "fetch", deployment.Spec.Template.Spec.InitContainers[0].Args[0])
	assert.Equal(t, mithrilBootstrapInitContainerName, deployment.Spec.Template.Spec.InitContainers[1].Name)
	assert.Equal(t, "ghcr.io/input-output-hk/mithril-client:main-2478748", deployment.Spec.Template.Spec.InitContainers[1].Image)
	require.NotNil(t, requireVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, mithrilTmpVolumeName).EmptyDir)
	requireContainerNamed(t, deployment.Spec.Template.Spec.Containers, serveContainerName)
	nodeContainer := requireContainerNamed(t, deployment.Spec.Template.Spec.Containers, cardanoNodeContainerName)
	assert.Equal(t, defaultMainnetNodeResources(), nodeContainer.Resources)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Zero(t, storage.Cmp(resource.MustParse(defaultMainnetNodeStorageSize)))

	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Network)
	require.NotNil(t, current.Status.Network.Profile)
	assert.Equal(t, yacdv1alpha1.PublicNetworkProfileMainnet, *current.Status.Network.Profile)
}

func TestCardanoNetworkReconcilerReconcileAttachesPrimarySidecarDBSync(t *testing.T) {
	ctx := context.Background()
	network := readyLocalCardanoNetwork()
	dbSync := readyPrimarySidecarDBSync("dbsync", network)
	reconciler := newTestReconciler(t, network, dbSync)
	storeNetworkStatus(t, ctx, reconciler, network)
	require.NotNil(t, requireNetwork(t, ctx, reconciler, network).Status.Endpoints.Artifacts)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.Equal(t, "dbsync", deployment.Spec.Template.Labels[labelDBSync])
	assert.Equal(t, testDBSyncSidecarRevision, deployment.Spec.Template.Annotations[dbSyncSidecarRevisionAnno])

	initContainer := requireContainerNamed(t, deployment.Spec.Template.Spec.InitContainers, "dbsync-pgpass-setup")
	assert.Equal(t, dbSync.Spec.Image, initContainer.Image)
	container := requireContainerNamed(t, deployment.Spec.Template.Spec.Containers, "cardano-db-sync")
	assert.Equal(t, dbSync.Spec.Image, container.Image)
	assert.Contains(t, container.Args, "--socket-path")
	assert.Contains(t, container.Args, "/ipc/node.socket")
	assert.Contains(t, container.VolumeMounts, corev1.VolumeMount{Name: nodeIPCVolumeName, MountPath: "/ipc"})
	configVolume := requireVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, "dbsync-config")
	require.NotNil(t, configVolume.ConfigMap)
	assert.Equal(t, "dbsync-config", configVolume.ConfigMap.Name)
	stateVolume := requireVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, "dbsync-state")
	require.NotNil(t, stateVolume.PersistentVolumeClaim)
	assert.Equal(t, "dbsync-state", stateVolume.PersistentVolumeClaim.ClaimName)
	pgpassVolume := requireVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, "dbsync-pgpass-secret")
	require.NotNil(t, pgpassVolume.Secret)
	assert.Equal(t, "dbsync-pgpass", pgpassVolume.Secret.SecretName)
	assertNoVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, "follower-state")

	// On the serve path the sidecar reads artifacts from the primary
	// node-state PVC at the staged subdirectory, so no network-artifacts
	// ConfigMap volume is appended; the db-sync container mounts the shared
	// localnet-state volume at the artifacts subdirectory read-only.
	assertNoVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, "network-artifacts")
	var artifactsMount corev1.VolumeMount
	for _, mount := range container.VolumeMounts {
		if mount.Name == localnetStateVolumeName {
			artifactsMount = mount
		}
	}
	assert.Equal(t, "/network-artifacts", artifactsMount.MountPath)
	assert.Equal(t, "artifacts", artifactsMount.SubPath)
	assert.True(t, artifactsMount.ReadOnly)

	attachDBSyncSidecar(t, ctx, reconciler, network)
	currentDBSync := &yacdv1alpha1.CardanoDBSync{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKeyFromObject(dbSync), currentDBSync))
	currentDBSync.Status.Placement.PrimarySidecar.Revision = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	require.NoError(t, reconciler.Status().Update(ctx, currentDBSync))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.Equal(t, "sha256:2222222222222222222222222222222222222222222222222222222222222222", deployment.Spec.Template.Annotations[dbSyncSidecarRevisionAnno])
}

func TestCardanoNetworkReconcilerReconcileAttachesPublicPrimarySidecarDBSync(t *testing.T) {
	ctx := context.Background()
	network := readyPublicPreviewCardanoNetwork()
	dbSync := readyPrimarySidecarDBSync("dbsync", network)
	reconciler := newTestReconciler(t, network, dbSync)
	storeNetworkStatus(t, ctx, reconciler, network)
	require.NotNil(t, requireNetwork(t, ctx, reconciler, network).Status.Endpoints.Artifacts)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.Equal(t, "dbsync", deployment.Spec.Template.Labels[labelDBSync])
	assert.Equal(t, testDBSyncSidecarRevision, deployment.Spec.Template.Annotations[dbSyncSidecarRevisionAnno])
	requireContainerNamed(t, deployment.Spec.Template.Spec.Containers, "cardano-db-sync")
	assertNoVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, "network-artifacts")
	assertNoVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, "follower-state")
}

func TestCardanoNetworkReconcilerReconcileReportsDBSyncAttachmentReadyWhenSidecarContainerReady(t *testing.T) {
	ctx := context.Background()
	network := readyLocalCardanoNetwork()
	dbSync := readyPrimarySidecarDBSync("dbsync", network)
	reconciler := newTestReconciler(t, network, dbSync)
	reconciler.timingProberOverride = cardanoNetworkTimingProberFunc(func(context.Context, string) (cardanoNetworkTiming, error) {
		return cardanoNetworkTiming{SystemStart: time.Now().Add(-time.Hour), SlotLengthSeconds: 1}, nil
	})
	reconciler.syncProberOverride = syncedNodeSyncProber()
	storeNetworkStatus(t, ctx, reconciler, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	attachDBSyncSidecar(t, ctx, reconciler, network)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName, ogmiosContainerName, kupoContainerName, serveContainerName, ctrldbsync.PrimarySidecarContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: nodeSyncProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, network, conditionTypeDBSyncAttachmentReady, metav1.ConditionTrue, conditionReasonDBSyncAttachmentReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady)
}

func TestCardanoNetworkReconcilerReconcileReportsDBSyncAttachmentBlockerBeforeNodeAvailability(t *testing.T) {
	ctx := context.Background()
	network := readyLocalCardanoNetwork()
	dbSync := readyPrimarySidecarDBSync("dbsync", network)
	reconciler := newTestReconciler(t, network, dbSync)
	storeNetworkStatus(t, ctx, reconciler, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	attachDBSyncSidecar(t, ctx, reconciler, network)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentObserved(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, network, conditionTypeDBSyncAttachmentReady, metav1.ConditionFalse, conditionReasonDBSyncAttachmentPending)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonDBSyncAttachmentPending)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionTrue, conditionReasonDBSyncAttachmentPending)
}

func TestCardanoNetworkReconcilerReconcileKeepsDBSyncAttachmentReadySeparateFromNode(t *testing.T) {
	ctx := context.Background()
	network := readyLocalCardanoNetwork()
	dbSync := readyPrimarySidecarDBSync("dbsync", network)
	reconciler := newTestReconciler(t, network, dbSync)
	storeNetworkStatus(t, ctx, reconciler, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	attachDBSyncSidecar(t, ctx, reconciler, network)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentObserved(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, ctrldbsync.PrimarySidecarContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, network, conditionTypeDBSyncAttachmentReady, metav1.ConditionTrue, conditionReasonDBSyncAttachmentReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
}

func TestCardanoNetworkReconcilerReconcileAttachesPrimarySidecarDBSyncIncumbentWhenMultipleClaimsExist(t *testing.T) {
	ctx := context.Background()
	network := readyLocalCardanoNetwork()
	first := readyPrimarySidecarDBSync("first", network)
	second := readyPrimarySidecarDBSync("second", network)
	reconciler := newTestReconciler(t, network, first, second)
	storeNetworkStatus(t, ctx, reconciler, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.Equal(t, "first", deployment.Spec.Template.Labels[labelDBSync])
	requireContainerNamed(t, deployment.Spec.Template.Spec.Containers, "cardano-db-sync")

	currentFirst := &yacdv1alpha1.CardanoDBSync{}
	require.NoError(t, reconciler.Get(ctx, client.ObjectKeyFromObject(first), currentFirst))
	require.NoError(t, reconciler.Delete(ctx, currentFirst))
	storeNetworkStatus(t, ctx, reconciler, network)

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
	assert.Equal(t, "second", deployment.Spec.Template.Labels[labelDBSync])
	requireContainerNamed(t, deployment.Spec.Template.Spec.Containers, "cardano-db-sync")
}

func TestCardanoNetworkReconcilerReconcileSkipsPrimarySidecarDBSyncWhenStatusContractIsNotAttachable(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*yacdv1alpha1.CardanoDBSync)
	}{
		{
			name: "stale observed generation",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync) {
				dbSync.Status.ObservedGeneration = 0
			},
		},
		{
			name: "missing material condition",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync) {
				dbSync.Status.Conditions = nil
			},
		},
		{
			name: "material condition false",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync) {
				dbSync.Status.Conditions[0].Status = metav1.ConditionFalse
			},
		},
		{
			name: "missing placement",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync) {
				dbSync.Status.Placement = nil
			},
		},
		{
			name: "missing revision",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync) {
				dbSync.Status.Placement.PrimarySidecar.Revision = ""
			},
		},
		{
			name: "missing configmap name",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync) {
				dbSync.Status.Placement.PrimarySidecar.Resources.ConfigMapName = ""
			},
		},
		{
			name: "missing pgpass secret name",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync) {
				dbSync.Status.Placement.PrimarySidecar.Resources.PGPassSecretName = ""
			},
		},
		{
			name: "missing state pvc name",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync) {
				dbSync.Status.Placement.PrimarySidecar.Resources.StatePVCName = ""
			},
		},
		{
			name: "missing metrics service name",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync) {
				dbSync.Status.Placement.PrimarySidecar.Resources.MetricsServiceName = ""
			},
		},
		{
			name: "wrong network",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync) {
				dbSync.Status.Placement.PrimarySidecar.NetworkName = "other-network"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			network := readyLocalCardanoNetwork()
			dbSync := readyPrimarySidecarDBSync("dbsync", network)
			tt.mutate(dbSync)
			reconciler := newTestReconciler(t, network, dbSync)
			storeNetworkStatus(t, ctx, reconciler, network)

			_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

			require.NoError(t, err)
			deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
			assertNoContainerNamed(t, deployment.Spec.Template.Spec.Containers, "cardano-db-sync")
			assert.NotContains(t, deployment.Spec.Template.Labels, labelDBSync)
			assertCondition(t, ctx, reconciler, network, conditionTypeDBSyncAttachmentReady, metav1.ConditionFalse, conditionReasonDBSyncAttachmentPending)
		})
	}
}

// TestCardanoNetworkReconcilerReconcileAttachesPrimarySidecarDBSyncWithoutConfigMap
// proves the serve-path attachment does not depend on the network-artifacts
// ConfigMap: a local network stages artifacts onto the node-state PVC, so the
// sidecar attaches (reading the staged subdirectory) even when no artifact
// ConfigMap name is published. This is what lets PR-B delete the ConfigMap
// without wedging primary-sidecar db-sync.
func TestCardanoNetworkReconcilerReconcileAttachesPrimarySidecarDBSyncWithoutConfigMap(t *testing.T) {
	ctx := context.Background()
	network := readyLocalCardanoNetwork()
	dbSync := readyPrimarySidecarDBSync("dbsync", network)
	reconciler := newTestReconciler(t, network, dbSync)
	storeNetworkStatus(t, ctx, reconciler, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	requireContainerNamed(t, deployment.Spec.Template.Spec.Containers, "cardano-db-sync")
	assert.Equal(t, "dbsync", deployment.Spec.Template.Labels[labelDBSync])
	assertNoVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, "network-artifacts")
}

func TestCardanoNetworkReconcilerReconcileSkipsPrimarySidecarDBSyncOnPortConflict(t *testing.T) {
	ctx := context.Background()
	network := readyLocalCardanoNetwork()
	dbSync := readyPrimarySidecarDBSync("dbsync", network)
	// Force the db-sync metrics port to collide with the ogmios port already
	// owned by the primary Pod so the attachment is rejected as unsupported.
	dbSync.Spec.Config.Runtime = &yacdv1alpha1.CardanoDBSyncRuntimeSpec{MetricsPort: defaultOgmiosPort}
	reconciler := newTestReconciler(t, network, dbSync)
	storeNetworkStatus(t, ctx, reconciler, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	assertNoContainerNamed(t, deployment.Spec.Template.Spec.Containers, "cardano-db-sync")
	assert.NotContains(t, deployment.Spec.Template.Labels, labelDBSync)
	assertCondition(t, ctx, reconciler, network, conditionTypeDBSyncAttachmentReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec)
}

func TestCardanoNetworkReconcilerReconcileReportsNodeReadyWhenDeploymentAvailable(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("node-ready")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName, ogmiosContainerName, kupoContainerName, serveContainerName)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: nodeSyncProbeRequeueAfter}, result)

	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeDBSyncAttachmentReady, metav1.ConditionFalse, conditionReasonDBSyncAttachmentNotRequested)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionTrue, conditionReasonNodeReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionTrue, conditionReasonOgmiosReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionTrue, conditionReasonKupoReady)
}

func TestCardanoNetworkReconcilerReconcileKeepsNodeReadySeparateFromOgmios(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("node-ready-ogmios-waiting")
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
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonDeploymentProgressing)
}

func TestCardanoNetworkReconcilerReconcileRequiresKupoReadyWhenEnabled(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("ogmios-ready-kupo-waiting")
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
	require.Len(t, deployment.Spec.Template.Spec.Containers, 2)
	assert.Equal(t, cardanoNodeContainerName, deployment.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, serveContainerName, deployment.Spec.Template.Spec.Containers[1].Name)
	assertNoPrimaryOgmiosService(t, ctx, reconciler, network)
	assertNoPrimaryKupoService(t, ctx, reconciler, network)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonOgmiosDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Ogmios)
	assert.Nil(t, current.Status.Endpoints.Kupo)

	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName)
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionTrue, conditionReasonNodeReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonOgmiosDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonOgmiosDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonOgmiosDisabled)
}

func TestCardanoNetworkReconcilerReconcileDeletesOwnedOgmiosServiceWhenDisabled(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("deletes-ogmios-service")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	requirePrimaryOgmiosService(t, ctx, reconciler, network)
	requirePrimaryKupoService(t, ctx, reconciler, network)

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
	current = requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Ogmios)
	assert.Nil(t, current.Status.Endpoints.Kupo)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionFalse, conditionReasonOgmiosDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
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
	require.Len(t, deployment.Spec.Template.Spec.Containers, 3)
	assert.Equal(t, cardanoNodeContainerName, deployment.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, ogmiosContainerName, deployment.Spec.Template.Spec.Containers[1].Name)
	assert.Equal(t, serveContainerName, deployment.Spec.Template.Spec.Containers[2].Name)
	requirePrimaryOgmiosService(t, ctx, reconciler, network)
	assertNoPrimaryKupoService(t, ctx, reconciler, network)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Kupo)

	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName, ogmiosContainerName, serveContainerName)
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionTrue, conditionReasonNodeReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeOgmiosReady, metav1.ConditionTrue, conditionReasonOgmiosReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonReady)
}

func TestCardanoNetworkReconcilerReconcileDeletesOwnedKupoServiceWhenDisabled(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("deletes-kupo-service")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	requirePrimaryKupoService(t, ctx, reconciler, network)

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
	current = requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Endpoints)
	assert.Nil(t, current.Status.Endpoints.Kupo)
	assertCondition(t, ctx, reconciler, network, conditionTypeKupoReady, metav1.ConditionFalse, conditionReasonKupoDisabled)
}

func TestCardanoNetworkReconcilerReconcileIsIdempotent(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("idempotent")
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
	// node-to-node, ogmios, kupo, and the always-on artifacts Service.
	assert.Len(t, services.Items, 4)
	var secrets corev1.SecretList
	require.NoError(t, reconciler.List(ctx, &secrets))
	// The genesis-funded faucet wallet Secret.
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

func TestCardanoNetworkReconcilerApplyPrimaryDeploymentIgnoresAPIDefaults(t *testing.T) {
	const foreignMetadataValue = "keep"

	ctx := context.Background()
	network := localCardanoNetwork("ignores-api-defaults")
	reconciler := newTestReconciler(t, network)
	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
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

func TestCardanoNetworkReconcilerReconcileRecoversAfterForgedStatusAndPVCFingerprintRestore(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("recovers-forged-status-pvc")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	baseline := requireAcceptedNetworkIdentityStatus(t, ctx, reconciler, network)

	current := requireNetwork(t, ctx, reconciler, network)
	current.Status.Network.NetworkFingerprint = "deadbeef-forged-both"
	current.Status.Network.LocalnetFingerprint = forgedLocalnetFingerprint
	storeNetworkStatus(t, ctx, reconciler, current)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	pvc.Annotations[localnetFingerprintAnno] = "cafebabe-forged-pvc-annotation"
	require.NoError(t, reconciler.Update(ctx, pvc))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	degraded := requireAcceptedNetworkIdentityStatus(t, ctx, reconciler, network)
	assert.Equal(t, baseline.NetworkFingerprint, degraded.NetworkFingerprint)
	assert.Equal(t, "cafebabe-forged-pvc-annotation", degraded.LocalnetFingerprint)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedLocalnetChange)

	pvc = requirePrimaryPVC(t, ctx, reconciler, network)
	pvc.Annotations[localnetFingerprintAnno] = baseline.LocalnetFingerprint
	require.NoError(t, reconciler.Update(ctx, pvc))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	recovered := requireAcceptedNetworkIdentityStatus(t, ctx, reconciler, network)
	assert.Equal(t, baseline, recovered)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
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
	current.Status.Network.NetworkFingerprint = ""
	current.Status.Network.LocalnetFingerprint = ""
	storeNetworkStatus(t, ctx, reconciler, current)

	current = requireNetwork(t, ctx, reconciler, network)
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

func TestCardanoNetworkReconcilerReconcileRendersOgmiosNodePortService(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("ogmios-nodeport")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Ogmios: &yacdv1alpha1.OgmiosSpec{
			Enabled: true,
			Image:   defaultOgmiosImage,
			Port:    defaultOgmiosPort,
			Service: &yacdv1alpha1.ServiceExposureSpec{
				Type:     yacdv1alpha1.ChainAPIServiceTypeNodePort,
				NodePort: 30500,
			},
		},
	}
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	service := requirePrimaryOgmiosService(t, ctx, reconciler, network)
	assert.Equal(t, corev1.ServiceTypeNodePort, service.Spec.Type)
	require.Len(t, service.Spec.Ports, 1)
	assert.Equal(t, int32(30500), service.Spec.Ports[0].NodePort)
}

func TestCardanoNetworkReconcilerReconcilePreservesAutoAssignedOgmiosNodePort(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("ogmios-nodeport-auto")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Ogmios: &yacdv1alpha1.OgmiosSpec{
			Enabled: true,
			Image:   defaultOgmiosImage,
			Port:    defaultOgmiosPort,
			Service: &yacdv1alpha1.ServiceExposureSpec{
				Type: yacdv1alpha1.ChainAPIServiceTypeNodePort,
			},
		},
	}
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	// The fake client has no NodePort allocator, so simulate the
	// Kubernetes-assigned node port, then prove a subsequent reconcile preserves
	// it rather than wiping it to 0 (the thrash this feature must avoid).
	service := requirePrimaryOgmiosService(t, ctx, reconciler, network)
	require.Len(t, service.Spec.Ports, 1)
	service.Spec.Ports[0].NodePort = 31900
	require.NoError(t, reconciler.Update(ctx, service))

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	service = requirePrimaryOgmiosService(t, ctx, reconciler, network)
	require.Len(t, service.Spec.Ports, 1)
	assert.Equal(t, int32(31900), service.Spec.Ports[0].NodePort)
}

func TestCardanoNetworkReconcilerReconcileDegradesWhenPrimaryPVCIsDeleting(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("primary-pvc-deleting")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	pvc.Finalizers = []string{"test.example.io/never-removed"}
	require.NoError(t, reconciler.Update(ctx, pvc))
	require.NoError(t, reconciler.Delete(ctx, pvc))
	pvc = requirePrimaryPVC(t, ctx, reconciler, network)
	require.False(t, pvc.DeletionTimestamp.IsZero())

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	assert.Empty(t, result)

	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonChildBeingDeleted)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonChildBeingDeleted)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionFalse, conditionReasonChildBeingDeleted)
	current := requireNetwork(t, ctx, reconciler, network)
	degraded := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeDegraded))
	require.NotNil(t, degraded)
	assert.Contains(t, degraded.Message, primaryNodeStatePVCName(network))
	assert.Contains(t, degraded.Message, "test.example.io/never-removed")
}

func TestCardanoNetworkReconcilerReconcileRefusesPrimaryPVCRecreationAfterAcceptance(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("primary-state-lost")
	reconciler := newTestReconciler(t, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	requirePrimaryPVC(t, ctx, reconciler, network)
	require.NotEmpty(t, requireAcceptedLocalnetFingerprint(t, ctx, reconciler, network))

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	require.NoError(t, reconciler.Delete(ctx, pvc))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	assert.Empty(t, result)

	err = reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryNodeStatePVCName(network),
	}, &corev1.PersistentVolumeClaim{})
	assert.True(t, apierrors.IsNotFound(err), "expected PVC to remain absent, got %v", err)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonPrimaryStateLost)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonPrimaryStateLost)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeReady, metav1.ConditionFalse, conditionReasonPrimaryStateLost)
	current := requireNetwork(t, ctx, reconciler, network)
	degraded := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeDegraded))
	require.NotNil(t, degraded)
	assert.Contains(t, degraded.Message, primaryNodeStatePVCName(network))
	assert.Contains(t, degraded.Message, "Delete and recreate the CardanoNetwork")
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

func TestCardanoNetworkReconcilerReconcileSurfacesStorageExpansionRejection(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("rejects-storage-expansion")
	network.Spec.Node.Storage = &yacdv1alpha1.NodeStorageSpec{
		Size: resource.MustParse("2Gi"),
	}
	rejection := apierrors.NewForbidden(
		corev1.Resource("persistentvolumeclaims"),
		primaryNodeStatePVCName(network),
		errors.New("only dynamically provisioned pvc can be resized and the storageclass that provisions the pvc must support resize"),
	)
	reconciler := newTestReconcilerWithInterceptor(t, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if _, ok := obj.(*corev1.PersistentVolumeClaim); ok && obj.GetName() == primaryNodeStatePVCName(network) {
				return rejection
			}

			return c.Update(ctx, obj, opts...)
		},
	}, network)

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)

	current := requireNetwork(t, ctx, reconciler, network)
	current.Spec.Node.Storage.Size = resource.MustParse("5Gi")
	current.Generation = 2
	require.NoError(t, reconciler.Update(ctx, current))

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	assert.Empty(t, result)

	pvc := requirePrimaryPVC(t, ctx, reconciler, network)
	storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Zero(t, storage.Cmp(resource.MustParse("2Gi")))
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonStorageExpansionRejected)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionFalse, conditionReasonStorageExpansionRejected)
	current = requireNetwork(t, ctx, reconciler, network)
	degraded := apimeta.FindStatusCondition(current.Status.Conditions, string(conditionTypeDegraded))
	require.NotNil(t, degraded)
	assert.Contains(t, degraded.Message, "storage expansion from 2Gi to 5Gi was rejected by Kubernetes")
	assert.Contains(t, degraded.Message, "only dynamically provisioned pvc can be resized")
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
	assert.Equal(t, testStorageClassName, pvc.Annotations[ctrlannotations.RequestedStorageClass])
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
	assert.Equal(t, testStorageClassName, pvc.Annotations[ctrlannotations.RequestedStorageClass])
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
	assert.NotContains(t, pvc.Annotations, ctrlannotations.RequestedStorageClass)
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
			assertCondition(t, ctx, reconciler, network, conditionTypeArtifactsReady, metav1.ConditionFalse, conditionReasonResourceConflict)
		})
	}
}

func TestCardanoNetworkReconcilerReconcileReturnsInternalBuildErrors(t *testing.T) {
	ctx := context.Background()
	// A public network is used so the build reaches its scheme guard without
	// first tripping the local-only faucet wallet ensure step (which generates
	// the wallet Secret before Build and needs a non-nil scheme of its own).
	network := publicPreviewCardanoNetwork("internal-build-error")
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
	tests := []struct {
		name    string
		network *yacdv1alpha1.CardanoNetwork
	}{
		{
			name: "local babbage era",
			network: func() *yacdv1alpha1.CardanoNetwork {
				network := localCardanoNetwork("unsupported-local-era")
				network.Spec.Local.Era = yacdv1alpha1.CardanoEraBabbage
				return network
			}(),
		},
		{
			name: "public kupo",
			network: func() *yacdv1alpha1.CardanoNetwork {
				network := publicPreviewCardanoNetwork("unsupported-public-kupo")
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Kupo: &yacdv1alpha1.KupoSpec{
						Enabled: true,
						Image:   defaultKupoImage,
						Port:    defaultKupoPort,
					},
				}
				return network
			}(),
		},
		{
			name:    "public mainnet without mithril bootstrap",
			network: publicCardanoNetwork("unsupported-public-mainnet", yacdv1alpha1.PublicNetworkProfileMainnet),
		},
		{
			name: "public mainnet storage below minimum",
			network: func() *yacdv1alpha1.CardanoNetwork {
				network := publicCardanoNetwork("unsupported-mainnet-storage", yacdv1alpha1.PublicNetworkProfileMainnet)
				network.Spec.Public.Bootstrap = &yacdv1alpha1.PublicNetworkBootstrapSpec{
					Mithril: &yacdv1alpha1.MithrilBootstrapSpec{},
				}
				network.Spec.Node.Storage = &yacdv1alpha1.NodeStorageSpec{
					Size: resource.MustParse("299Gi"),
				}
				return network
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			network := tt.network
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
			current := requireNetwork(t, ctx, reconciler, network)
			assert.Nil(t, current.Status.Endpoints)
		})
	}
}

func TestCardanoNetworkReconcilerPrimaryNodeReadyConditionReportsMissingChildren(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("missing-children")
	reconciler := newTestReconciler(t, network)
	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	got, err := reconciler.primaryNodeReadyCondition(ctx, network)
	require.NoError(t, err)
	assert.Equal(t, conditionTypeNodeReady, conditionType(got.Type))
	assert.Equal(t, metav1.ConditionFalse, got.Status)
	assert.Equal(t, conditionReasonPrimaryWorkloadMissing, conditionReason(got.Reason))
	assert.Equal(t, "Primary node PVC is missing", got.Message)

	require.NoError(t, reconciler.Create(ctx, resources.PersistentVolumeClaim))
	got, err = reconciler.primaryNodeReadyCondition(ctx, network)
	require.NoError(t, err)
	assert.Equal(t, metav1.ConditionFalse, got.Status)
	assert.Equal(t, conditionReasonPrimaryWorkloadMissing, conditionReason(got.Reason))
	assert.Equal(t, "Primary node Service is missing", got.Message)

	require.NoError(t, reconciler.Create(ctx, resources.Service))
	got, err = reconciler.primaryNodeReadyCondition(ctx, network)
	require.NoError(t, err)
	assert.Equal(t, metav1.ConditionFalse, got.Status)
	assert.Equal(t, conditionReasonPrimaryWorkloadMissing, conditionReason(got.Reason))
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
	assert.Equal(t, conditionReasonDeploymentProgressing, conditionReason(got.Reason))
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

// publicPreviewCardanoNetwork returns a minimally supported public preview
// CardanoNetwork.
func publicPreviewCardanoNetwork(name string) *yacdv1alpha1.CardanoNetwork {
	return publicCardanoNetwork(name, yacdv1alpha1.PublicNetworkProfilePreview)
}

func publicCardanoNetwork(name string, profile yacdv1alpha1.PublicNetworkProfile) *yacdv1alpha1.CardanoNetwork {
	return &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: yacdv1alpha1.CardanoNetworkSpec{
			Mode: yacdv1alpha1.CardanoNetworkModePublic,
			Node: yacdv1alpha1.CardanoNodeSpec{
				Version: "11.0.1",
				Port:    3001,
			},
			Public: &yacdv1alpha1.PublicNetworkSpec{
				Profile: profile,
			},
		},
	}
}

func readyLocalCardanoNetwork() *yacdv1alpha1.CardanoNetwork {
	network := localCardanoNetwork("primary-dbsync")
	network.Status.ObservedGeneration = network.Generation
	network.Status.Endpoints = &yacdv1alpha1.CardanoNetworkEndpointsStatus{
		NodeToNode: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: primaryWorkloadName(network),
			Port:        network.Spec.Node.Port,
			URL:         fmt.Sprintf("tcp://%s.%s.svc.cluster.local:%d", primaryWorkloadName(network), network.Namespace, network.Spec.Node.Port),
		},
		Ogmios: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: primaryOgmiosServiceName(network),
			Port:        defaultOgmiosPort,
			URL:         fmt.Sprintf("ws://%s.%s.svc.cluster.local:%d", primaryOgmiosServiceName(network), network.Namespace, defaultOgmiosPort),
		},
		Artifacts: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: primaryArtifactsServiceName(network),
			Port:        defaultServePort,
			URL:         fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", primaryArtifactsServiceName(network), network.Namespace, defaultServePort),
		},
	}
	network.Status.Conditions = []metav1.Condition{{
		Type:               string(conditionTypeArtifactsReady),
		Status:             metav1.ConditionTrue,
		Reason:             string(conditionReasonArtifactsReady),
		Message:            "Network artifacts are ready",
		ObservedGeneration: network.Generation,
		LastTransitionTime: metav1.Now(),
	}}

	return network
}

func readyPublicPreviewCardanoNetwork() *yacdv1alpha1.CardanoNetwork {
	network := publicPreviewCardanoNetwork("public-primary-dbsync")
	network.Generation = 1
	networkMagic := int64(2)
	profile := yacdv1alpha1.PublicNetworkProfilePreview
	era := yacdv1alpha1.CardanoEraConway
	network.Status.ObservedGeneration = network.Generation
	network.Status.Network = &yacdv1alpha1.CardanoNetworkIdentityStatus{
		Mode:               yacdv1alpha1.CardanoNetworkModePublic,
		Profile:            &profile,
		NetworkMagic:       &networkMagic,
		NetworkFingerprint: "3eee469d6200db89fd64fbd032ccbb58a7ba557b920a07bc2f22523b6f009a29",
		Era:                &era,
	}
	network.Status.Endpoints = &yacdv1alpha1.CardanoNetworkEndpointsStatus{
		NodeToNode: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: primaryWorkloadName(network),
			Port:        network.Spec.Node.Port,
			URL:         fmt.Sprintf("tcp://%s.%s.svc.cluster.local:%d", primaryWorkloadName(network), network.Namespace, network.Spec.Node.Port),
		},
		Ogmios: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: primaryOgmiosServiceName(network),
			Port:        defaultOgmiosPort,
			URL:         fmt.Sprintf("ws://%s.%s.svc.cluster.local:%d", primaryOgmiosServiceName(network), network.Namespace, defaultOgmiosPort),
		},
		Artifacts: &yacdv1alpha1.ServiceEndpointStatus{
			ServiceName: primaryArtifactsServiceName(network),
			Port:        defaultServePort,
			URL:         fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", primaryArtifactsServiceName(network), network.Namespace, defaultServePort),
		},
	}
	network.Status.Conditions = []metav1.Condition{{
		Type:               string(conditionTypeArtifactsReady),
		Status:             metav1.ConditionTrue,
		Reason:             string(conditionReasonArtifactsReady),
		Message:            "Network artifacts are ready",
		ObservedGeneration: network.Generation,
		LastTransitionTime: metav1.Now(),
	}}

	return network
}

func readyPrimarySidecarDBSync(name string, network *yacdv1alpha1.CardanoNetwork) *yacdv1alpha1.CardanoDBSync {
	return &yacdv1alpha1.CardanoDBSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  network.Namespace,
			UID:        types.UID(name + "-uid"),
			Generation: 1,
		},
		Spec: yacdv1alpha1.CardanoDBSyncSpec{
			NetworkRef: yacdv1alpha1.CardanoDBSyncNetworkReference{Name: network.Name},
			Image:      "ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0",
			Placement: &yacdv1alpha1.CardanoDBSyncPlacementSpec{
				Mode: yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar,
			},
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
		Status: yacdv1alpha1.CardanoDBSyncStatus{
			ObservedGeneration: 1,
			Placement: &yacdv1alpha1.CardanoDBSyncPlacementStatus{
				Mode: yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar,
				PrimarySidecar: &yacdv1alpha1.CardanoDBSyncPrimarySidecarStatus{
					NetworkName: network.Name,
					Revision:    testDBSyncSidecarRevision,
					Resources: yacdv1alpha1.CardanoDBSyncPrimarySidecarResourcesStatus{
						ConfigMapName:      name + "-config",
						PGPassSecretName:   name + "-pgpass",
						StatePVCName:       name + "-state",
						MetricsServiceName: name + "-metrics",
					},
				},
			},
			Conditions: []metav1.Condition{{
				Type:               "SidecarMaterialReady",
				Status:             metav1.ConditionTrue,
				Reason:             "ReconcileSucceeded",
				Message:            "Primary-sidecar material is ready",
				ObservedGeneration: 1,
				LastTransitionTime: metav1.Now(),
			}},
		},
	}
}

// newTestReconciler returns a CardanoNetworkReconciler backed by a fake client.
func newTestReconciler(t *testing.T, objects ...client.Object) *CardanoNetworkReconciler {
	t.Helper()

	return newTestReconcilerWithInterceptor(t, interceptor.Funcs{}, objects...)
}

func newTestReconcilerWithInterceptor(t *testing.T, funcs interceptor.Funcs, objects ...client.Object) *CardanoNetworkReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(funcs).
		WithStatusSubresource(&yacdv1alpha1.CardanoNetwork{}, &yacdv1alpha1.CardanoDBSync{}, &appsv1.Deployment{}, &corev1.Pod{})
	objectCopies := make([]client.Object, 0, len(objects))
	for _, object := range objects {
		objectCopies = append(objectCopies, object.DeepCopyObject().(client.Object))
	}
	builder.WithObjects(objectCopies...)
	fakeClient := builder.Build()

	return &CardanoNetworkReconciler{
		Client:             fakeClient,
		Reader:             fakeClient,
		Scheme:             scheme,
		syncProberOverride: syncedNodeSyncProber(),
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

func storeNetworkStatus(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	current := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, reconciler.Get(ctx, reconcileRequestFor(network).NamespacedName, current))
	current.Status = network.Status
	require.NoError(t, reconciler.Status().Update(ctx, current))
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

func requireAcceptedNetworkIdentityStatus(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) acceptedNetworkIdentity {
	t.Helper()

	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Network)
	require.NotEmpty(t, current.Status.Network.NetworkFingerprint)
	require.NotEmpty(t, current.Status.Network.LocalnetFingerprint)

	return acceptedNetworkIdentity{
		NetworkFingerprint:  current.Status.Network.NetworkFingerprint,
		LocalnetFingerprint: current.Status.Network.LocalnetFingerprint,
	}
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

func requirePrimaryArtifactsService(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.Service {
	t.Helper()

	service := &corev1.Service{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryArtifactsServiceName(network),
	}, service))

	return service
}

// TestCardanoNetworkReconcilerReconcileGatesFaucetWalletOnLocalMode proves the
// P4 re-gate at the controller level: with the faucet service (and its
// spec.chainAPI.faucet toggle) gone, the genesis-funded faucet wallet Secret is
// created for a local network and is absent for a non-local one, gated on mode
// alone. This is the reconcile-driven companion to the unit-level
// TestFaucetWalletEnabledPredicate, and the load-bearing guard against a fresh
// devnet booting with no funding source.
func TestCardanoNetworkReconcilerReconcileGatesFaucetWalletOnLocalMode(t *testing.T) {
	ctx := context.Background()

	t.Run("local mode creates the genesis-funded faucet wallet Secret", func(t *testing.T) {
		network := localCardanoNetwork("faucet-wallet-local")
		reconciler := newTestReconciler(t, network)

		_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
		require.NoError(t, err)

		secret := requirePrimaryFaucetWalletSecret(t, ctx, reconciler, network)
		assert.Equal(t, faucetWalletName, secret.Labels[walletNameLabel])
		assert.NotEmpty(t, secret.Data[walletAddressKey])
	})

	t.Run("non-local mode leaves no faucet wallet Secret", func(t *testing.T) {
		network := publicPreviewCardanoNetwork("faucet-wallet-public")
		reconciler := newTestReconciler(t, network)

		_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
		require.NoError(t, err)

		assertNoPrimaryFaucetWalletSecret(t, ctx, reconciler, network)
	})
}

func requirePrimaryFaucetWalletSecret(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) *corev1.Secret {
	t.Helper()

	secret := &corev1.Secret{}
	require.NoError(t, reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryFaucetWalletSecretName(network),
	}, secret))

	return secret
}

// attachDBSyncSidecar reconciles the primary node twice so the db-sync primary
// sidecar attachment settles. Since the network-artifacts ConfigMap removal (F0
// PR-B1) there is no producer ConfigMap to publish; the referenced CardanoDBSync
// claim alone drives attachment.
func attachDBSyncSidecar(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
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

func markPrimaryDeploymentObserved(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	deployment *appsv1.Deployment,
) {
	t.Helper()

	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas = 1
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 0
	deployment.Status.AvailableReplicas = 0
	deployment.Status.Conditions = []appsv1.DeploymentCondition{
		{
			Type:               appsv1.DeploymentAvailable,
			Status:             corev1.ConditionFalse,
			Reason:             "MinimumReplicasUnavailable",
			Message:            "Deployment does not have minimum availability.",
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
	assertNoPrimaryArtifactsService(t, ctx, reconciler, network)
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

func assertNoPrimaryArtifactsService(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	err := reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryArtifactsServiceName(network),
	}, &corev1.Service{})
	assert.True(t, apierrors.IsNotFound(err), "expected artifacts Service to be absent, got %v", err)
}

func assertNoPrimaryFaucetWalletSecret(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	err := reconciler.Get(ctx, types.NamespacedName{
		Namespace: network.Namespace,
		Name:      primaryFaucetWalletSecretName(network),
	}, &corev1.Secret{})
	assert.True(t, apierrors.IsNotFound(err), "expected faucet wallet Secret to be absent, got %v", err)
}

func assertNoContainerNamed(t *testing.T, containers []corev1.Container, name string) {
	t.Helper()

	for _, container := range containers {
		assert.NotEqual(t, name, container.Name)
	}
}

func requireContainerNamed(t *testing.T, containers []corev1.Container, name string) corev1.Container {
	t.Helper()

	for _, container := range containers {
		if container.Name == name {
			return container
		}
	}
	require.Failf(t, "missing container", "expected container %s", name)
	return corev1.Container{}
}

func requireVolumeNamed(t *testing.T, volumes []corev1.Volume, name string) corev1.Volume {
	t.Helper()

	for _, volume := range volumes {
		if volume.Name == name {
			return volume
		}
	}
	require.Failf(t, "missing volume", "expected volume %s", name)
	return corev1.Volume{}
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
	ct conditionType,
	status metav1.ConditionStatus,
	reason conditionReason,
) {
	t.Helper()

	current := requireNetwork(t, ctx, reconciler, network)
	condition := apimeta.FindStatusCondition(current.Status.Conditions, string(ct))
	require.NotNil(t, condition)
	assert.Equal(t, status, condition.Status)
	assert.Equal(t, string(reason), condition.Reason)
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

func assertArtifactsEndpoint(
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
	require.NotNil(t, current.Status.Endpoints.Artifacts)
	assert.Equal(t, serviceName, current.Status.Endpoints.Artifacts.ServiceName)
	assert.Equal(t, port, current.Status.Endpoints.Artifacts.Port)
	assert.Equal(t,
		fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName, network.Namespace, port),
		current.Status.Endpoints.Artifacts.URL,
	)
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
