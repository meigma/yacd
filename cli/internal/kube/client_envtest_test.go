package kube

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestRuntimeClientAppliesAndGetsCardanoNetwork(t *testing.T) {
	ctx := context.Background()
	kubeClient, apiClient := newEnvtestClient(t)
	namespace := createNamespace(t, ctx, apiClient, "cli-apply")

	network := localCardanoNetwork(namespace, "devnet")
	require.NoError(t, kubeClient.ApplyCardanoNetwork(ctx, network))

	got, err := kubeClient.GetCardanoNetwork(ctx, namespace, "devnet")
	require.NoError(t, err)
	require.NotNil(t, got.Spec.Local)
	assert.Equal(t, int64(42), got.Spec.Local.NetworkMagic)
}

func TestWaitReadyReturnsReadyNetwork(t *testing.T) {
	ctx := context.Background()
	kubeClient, apiClient := newEnvtestClient(t)
	namespace := createNamespace(t, ctx, apiClient, "cli-wait")

	network := localCardanoNetwork(namespace, "ready")
	require.NoError(t, kubeClient.ApplyCardanoNetwork(ctx, network))

	current := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, apiClient.Get(ctx, crclient.ObjectKey{Namespace: namespace, Name: "ready"}, current))
	current.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "ready",
			ObservedGeneration: current.Generation,
			LastTransitionTime: metav1.Now(),
		},
	}
	require.NoError(t, apiClient.Status().Update(ctx, current))

	got, err := WaitReady(ctx, kubeClient, namespace, "ready", 5*time.Second)
	require.NoError(t, err)
	assert.Equal(t, "ready", got.Name)
}

func TestRuntimeClientGetsSecretValue(t *testing.T) {
	ctx := context.Background()
	kubeClient, apiClient := newEnvtestClient(t)
	namespace := createNamespace(t, ctx, apiClient, "cli-secret")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devnet-faucet-auth",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"token": []byte("super-secret-token-which-is-long-enough"),
		},
	}
	require.NoError(t, apiClient.Create(ctx, secret))

	got, err := kubeClient.GetSecretValue(ctx, namespace, "devnet-faucet-auth", "token")
	require.NoError(t, err)
	assert.Equal(t, "super-secret-token-which-is-long-enough", got)
}

func TestWaitReadyFailsOnDegradedNetwork(t *testing.T) {
	ctx := context.Background()
	kubeClient, apiClient := newEnvtestClient(t)
	namespace := createNamespace(t, ctx, apiClient, "cli-degraded")

	network := localCardanoNetwork(namespace, "degraded")
	require.NoError(t, kubeClient.ApplyCardanoNetwork(ctx, network))

	current := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, apiClient.Get(ctx, crclient.ObjectKey{Namespace: namespace, Name: "degraded"}, current))
	current.Status.Conditions = []metav1.Condition{
		{
			Type:               "Degraded",
			Status:             metav1.ConditionTrue,
			Reason:             "UnsupportedSpec",
			Message:            "unsupported",
			ObservedGeneration: current.Generation,
			LastTransitionTime: metav1.Now(),
		},
	}
	require.NoError(t, apiClient.Status().Update(ctx, current))

	_, err := WaitReady(ctx, kubeClient, namespace, "degraded", 5*time.Second)
	require.Error(t, err)
}

func TestWaitReadyIgnoresStaleReadyGeneration(t *testing.T) {
	t.Parallel()

	network := localCardanoNetwork("cli-stale", "ready")
	network.Generation = 2
	network.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "previous spec was ready",
			ObservedGeneration: 1,
			LastTransitionTime: metav1.Now(),
		},
	}

	_, err := WaitReady(context.Background(), &staticClient{network: network}, "cli-stale", "ready", 10*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "observed generation 1")
	assert.Contains(t, err.Error(), "current generation 2")
}

func TestWaitReadyTimesOutCleanly(t *testing.T) {
	t.Parallel()

	network := localCardanoNetwork("cli-timeout", "pending")
	network.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "Reconciling",
			Message:            "still reconciling",
			ObservedGeneration: network.Generation,
			LastTransitionTime: metav1.Now(),
		},
	}

	_, err := WaitReady(context.Background(), &staticClient{network: network}, "cli-timeout", "pending", 10*time.Millisecond)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not become ready")
	assert.Contains(t, err.Error(), "Reconciling")
}

func newEnvtestClient(t *testing.T) (*Adapter, crclient.Client) {
	t.Helper()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "charts", "yacd", "crds")},
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err, "start envtest")
	t.Cleanup(func() {
		assert.NoError(t, testEnv.Stop(), "stop envtest")
	})

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "add client-go scheme")
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme), "add yacd scheme")

	apiClient, err := crclient.New(cfg, crclient.Options{Scheme: scheme})
	require.NoError(t, err, "create client")

	return &Adapter{client: apiClient, namespace: "default"}, apiClient
}

func createNamespace(t *testing.T, ctx context.Context, apiClient crclient.Client, name string) string {
	t.Helper()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	require.NoError(t, apiClient.Create(ctx, namespace), "create namespace")

	return name
}

func localCardanoNetwork(namespace string, name string) *yacdv1alpha1.CardanoNetwork {
	return &yacdv1alpha1.CardanoNetwork{
		TypeMeta: metav1.TypeMeta{
			APIVersion: yacdv1alpha1.GroupVersion.String(),
			Kind:       "CardanoNetwork",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: yacdv1alpha1.CardanoNetworkSpec{
			Mode: yacdv1alpha1.CardanoNetworkModeLocal,
			Node: yacdv1alpha1.CardanoNodeSpec{
				Version: "11.0.1",
				Port:    3001,
				Storage: &yacdv1alpha1.NodeStorageSpec{
					Size: resource.MustParse("2Gi"),
				},
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

// staticClient is a hand-rolled Client used by the pure WaitReady tests in
// this file. It cannot be replaced by a generated mock because the tests
// exercise the polling loop directly without setting up per-call
// expectations; the mock would error on the duplicate get.
type staticClient struct {
	network *yacdv1alpha1.CardanoNetwork
}

func (s *staticClient) DefaultNamespace() string {
	return "default"
}

func (s *staticClient) ApplyCardanoNetwork(context.Context, *yacdv1alpha1.CardanoNetwork) error {
	return nil
}

func (s *staticClient) GetCardanoNetwork(context.Context, string, string) (*yacdv1alpha1.CardanoNetwork, error) {
	return s.network.DeepCopy(), nil
}

func (s *staticClient) GetSecretValue(context.Context, string, string, string) (string, error) {
	return "", nil
}
