package cardanonetwork

import (
	"context"
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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

// TestCardanoNetworkReconcilerReconcileCreatesPrimaryWorkload verifies a
// supported resource creates the singleton primary node PVC and Deployment.
func TestCardanoNetworkReconcilerReconcileCreatesPrimaryWorkload(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("creates-workload")
	reconciler := newTestReconciler(t, network)

	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	requirePrimaryPVC(t, ctx, reconciler, network)
	requirePrimaryDeployment(t, ctx, reconciler, network)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionFalse, conditionReasonReconcileSucceeded)
	assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonWorkloadApplied)
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
	cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
	assert.Zero(t, cpuRequest.Cmp(resource.MustParse("250m")))
	assert.Equal(t, originalFingerprint, deployment.Spec.Template.Annotations[localnetFingerprintAnno])
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

			current := requireNetwork(t, ctx, reconciler, network)
			tt.mutate(current)
			require.NoError(t, reconciler.Update(ctx, current))

			_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))
			require.NoError(t, err)

			pvc = requirePrimaryPVC(t, ctx, reconciler, network)
			assert.Equal(t, originalFingerprint, pvc.Annotations[localnetFingerprintAnno])
			deployment = requirePrimaryDeployment(t, ctx, reconciler, network)
			assert.Equal(t, originalFingerprint, deployment.Spec.Template.Annotations[localnetFingerprintAnno])
			assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedLocalnetChange)
			assertCondition(t, ctx, reconciler, network, conditionTypeProgressing, metav1.ConditionFalse, conditionReasonUnsupportedLocalnetChange)
		})
	}
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
	storageClassName := "fast"
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
	assert.Equal(t, storageClassName, *pvc.Spec.StorageClassName)
	assertCondition(t, ctx, reconciler, network, conditionTypeDegraded, metav1.ConditionTrue, conditionReasonUnsupportedStorageChange)
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

// newTestReconciler returns a CardanoNetworkReconciler backed by a fake client.
func newTestReconciler(t *testing.T, objects ...*yacdv1alpha1.CardanoNetwork) *CardanoNetworkReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&yacdv1alpha1.CardanoNetwork{})
	for _, object := range objects {
		builder.WithObjects(object)
	}

	return &CardanoNetworkReconciler{
		Client: builder.Build(),
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

// reconcileRequestFor returns a reconcile request targeting object.
func reconcileRequestFor(object *yacdv1alpha1.CardanoNetwork) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: object.Namespace,
			Name:      object.Name,
		},
	}
}
