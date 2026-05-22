package kube

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
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
	if err := kubeClient.ApplyCardanoNetwork(ctx, network); err != nil {
		t.Fatalf("ApplyCardanoNetwork returned an error: %v", err)
	}

	got, err := kubeClient.GetCardanoNetwork(ctx, namespace, "devnet")
	if err != nil {
		t.Fatalf("GetCardanoNetwork returned an error: %v", err)
	}
	if got.Spec.Local == nil {
		t.Fatal("got nil local spec")
	}
	if got.Spec.Local.NetworkMagic != 42 {
		t.Fatalf("network magic = %d, want 42", got.Spec.Local.NetworkMagic)
	}
}

func TestWaitReadyReturnsReadyNetwork(t *testing.T) {
	ctx := context.Background()
	kubeClient, apiClient := newEnvtestClient(t)
	namespace := createNamespace(t, ctx, apiClient, "cli-wait")

	network := localCardanoNetwork(namespace, "ready")
	if err := kubeClient.ApplyCardanoNetwork(ctx, network); err != nil {
		t.Fatalf("ApplyCardanoNetwork returned an error: %v", err)
	}

	current := &yacdv1alpha1.CardanoNetwork{}
	if err := apiClient.Get(ctx, crclient.ObjectKey{Namespace: namespace, Name: "ready"}, current); err != nil {
		t.Fatalf("get applied network: %v", err)
	}
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
	if err := apiClient.Status().Update(ctx, current); err != nil {
		t.Fatalf("update status: %v", err)
	}

	got, err := WaitReady(ctx, kubeClient, namespace, "ready", 5*time.Second)
	if err != nil {
		t.Fatalf("WaitReady returned an error: %v", err)
	}
	if got.Name != "ready" {
		t.Fatalf("ready network name = %q, want ready", got.Name)
	}
}

func TestWaitReadyFailsOnDegradedNetwork(t *testing.T) {
	ctx := context.Background()
	kubeClient, apiClient := newEnvtestClient(t)
	namespace := createNamespace(t, ctx, apiClient, "cli-degraded")

	network := localCardanoNetwork(namespace, "degraded")
	if err := kubeClient.ApplyCardanoNetwork(ctx, network); err != nil {
		t.Fatalf("ApplyCardanoNetwork returned an error: %v", err)
	}

	current := &yacdv1alpha1.CardanoNetwork{}
	if err := apiClient.Get(ctx, crclient.ObjectKey{Namespace: namespace, Name: "degraded"}, current); err != nil {
		t.Fatalf("get applied network: %v", err)
	}
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
	if err := apiClient.Status().Update(ctx, current); err != nil {
		t.Fatalf("update status: %v", err)
	}

	_, err := WaitReady(ctx, kubeClient, namespace, "degraded", 5*time.Second)
	if err == nil {
		t.Fatal("WaitReady succeeded, want degraded error")
	}
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
	if err == nil {
		t.Fatal("WaitReady succeeded, want stale generation error")
	}
	if got := err.Error(); !strings.Contains(got, "observed generation 1") || !strings.Contains(got, "current generation 2") {
		t.Fatalf("error = %q, want stale generation details", got)
	}
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
	if err == nil {
		t.Fatal("WaitReady succeeded, want timeout error")
	}
	if got := err.Error(); !strings.Contains(got, "did not become ready") || !strings.Contains(got, "Reconciling") {
		t.Fatalf("error = %q, want clean timeout status", got)
	}
}

func newEnvtestClient(t *testing.T) (*runtimeClient, crclient.Client) {
	t.Helper()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "charts", "yacd", "crds")},
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Fatalf("stop envtest: %v", err)
		}
	})

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := yacdv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add yacd scheme: %v", err)
	}

	apiClient, err := crclient.New(cfg, crclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return &runtimeClient{client: apiClient, namespace: "default"}, apiClient
}

func createNamespace(t *testing.T, ctx context.Context, apiClient crclient.Client, name string) string {
	t.Helper()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	if err := apiClient.Create(ctx, namespace); err != nil {
		t.Fatalf("create namespace: %v", err)
	}

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
