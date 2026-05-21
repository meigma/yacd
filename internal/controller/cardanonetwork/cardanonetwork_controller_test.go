package cardanonetwork

import (
	"context"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestLocalnetSpecFromCardanoNetworkMapsSupportedLocalInput verifies the
// adapter shape for currently supported local-mode cardano-testnet fields.
func TestLocalnetSpecFromCardanoNetworkMapsSupportedLocalInput(t *testing.T) {
	network := localCardanoNetwork("maps-supported-local-input")

	got, err := localnetSpecFromCardanoNetwork(network)
	require.NoError(t, err)

	assert.Equal(t, localnet.Spec{
		NetworkMagic: 42,
		PoolCount:    1,
		Timing: localnet.Timing{
			SlotLength:  100 * time.Millisecond,
			EpochLength: 500,
		},
		Paths: localnet.Paths{
			StateDir: "/state",
			EnvDir:   "/state/env",
		},
		Tool: localnet.Tool{
			Version: "11.0.1",
		},
	}, got)
}

// TestLocalnetSpecFromCardanoNetworkRejectsUnsupportedInput verifies the
// adapter fails fast when CRD fields cannot be represented by localnet yet.
func TestLocalnetSpecFromCardanoNetworkRejectsUnsupportedInput(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*yacdv1alpha1.CardanoNetwork)
		wantErr string
	}{
		{
			name: "public mode",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Mode = yacdv1alpha1.CardanoNetworkModePublic
			},
			wantErr: `mode "public" is not supported`,
		},
		{
			name: "missing local spec",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local = nil
			},
			wantErr: "local spec is required",
		},
		{
			name: "public spec with local mode",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Public = &yacdv1alpha1.PublicNetworkSpec{
					Profile: yacdv1alpha1.PublicNetworkProfilePreview,
				}
			},
			wantErr: "public spec is not supported with local mode",
		},
		{
			name: "babbage era",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local.Era = yacdv1alpha1.CardanoEraBabbage
			},
			wantErr: `local era "babbage" is not supported`,
		},
		{
			name: "genesis tuning",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local.Genesis = &yacdv1alpha1.LocalGenesisSpec{
					Profile: yacdv1alpha1.GenesisProfileDefault,
				}
			},
			wantErr: "local genesis tuning is not supported",
		},
		{
			name: "pool defaults",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local.Topology.Pools.Defaults = &yacdv1alpha1.LocalPoolDefaultsSpec{}
			},
			wantErr: "local pool defaults are not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network := localCardanoNetwork(tt.name)
			tt.mutate(network)

			_, err := localnetSpecFromCardanoNetwork(network)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

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

// TestCardanoNetworkReconcilerReconcileBuildsPlan verifies supported resources
// are fetched and converted into a localnet plan without creating children.
func TestCardanoNetworkReconcilerReconcileBuildsPlan(t *testing.T) {
	network := localCardanoNetwork("builds-plan")
	reconciler := newTestReconciler(t, network)

	result, err := reconciler.Reconcile(context.Background(), reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
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

// newTestReconciler returns a CardanoNetworkReconciler backed by a fake client.
func newTestReconciler(t *testing.T, objects ...*yacdv1alpha1.CardanoNetwork) *CardanoNetworkReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))

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
