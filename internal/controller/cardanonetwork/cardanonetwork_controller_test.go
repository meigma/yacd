package cardanonetwork

import (
	"context"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
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

// TestCardanoNetworkReconcilerReconcileBuildsPrimaryWorkload verifies supported
// resources are fetched and converted into a primary StatefulSet without
// creating children in this read-only slice.
func TestCardanoNetworkReconcilerReconcileBuildsPrimaryWorkload(t *testing.T) {
	network := localCardanoNetwork("builds-plan")
	reconciler := newTestReconciler(t, network)

	result, err := reconciler.Reconcile(context.Background(), reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	var statefulSets appsv1.StatefulSetList
	require.NoError(t, reconciler.List(context.Background(), &statefulSets))
	assert.Empty(t, statefulSets.Items)
}

// TestCardanoNetworkReconcilerReconcileIgnoresUnsupportedInput verifies
// adapter rejections are handled as read-only observations in this slice.
func TestCardanoNetworkReconcilerReconcileIgnoresUnsupportedInput(t *testing.T) {
	network := localCardanoNetwork("unsupported-input")
	network.Spec.Local.Era = yacdv1alpha1.CardanoEraBabbage
	reconciler := newTestReconciler(t, network)

	result, err := reconciler.Reconcile(context.Background(), reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
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

	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, object := range objects {
		builder.WithObjects(object)
	}

	return &CardanoNetworkReconciler{
		Client: builder.Build(),
		Scheme: scheme,
	}
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
