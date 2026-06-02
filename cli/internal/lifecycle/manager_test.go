package lifecycle_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/devconfig"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/lifecycle"
	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// localEnvYAML is a valid local developer environment used to drive render in
// the Up tests. It mirrors examples/local/yacd.yaml.
const localEnvYAML = `apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: local
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 2Gi
    chainAPI:
      faucet:
        enabled: true
        port: 8080
        defaultSource: utxo1
        minTopUpLovelace: 1000000
        maxTopUpLovelace: 100000000000
      wallet:
        enabled: true
        fundingLovelace: 100000000000
    local:
      networkMagic: 42
      era: conway
      timing:
        slotLength: 100ms
        epochLength: 500
      topology:
        pools:
          count: 1
`

// managedInfo is the cluster.Info a healthy EnsureCluster returns.
var managedInfo = cluster.Info{
	Name:           cluster.ManagedName,
	Context:        cluster.ManagedContext,
	KubeconfigPath: "/home/dev/.kube/config",
	Running:        true,
}

// testContext bundles the mocks and the subject so the relationships stay
// explicit across cases.
type testContext struct {
	provisioner *mocks.Provisioner
	store       *mocks.Store
	installer   *mocks.Installer
	client      *mocks.Client
	captured    *bool
	manager     *lifecycle.Manager
}

// newTestContext builds a Manager wired to fresh mocks. captureOrder records
// whether CaptureContext ran before EnsureCluster.
func newTestContext(t *testing.T, prior string) *testContext {
	t.Helper()

	provisioner := mocks.NewProvisioner(t)
	store := mocks.NewStore(t)
	installer := mocks.NewInstaller(t)
	client := mocks.NewClient(t)
	captured := false

	tc := &testContext{
		provisioner: provisioner,
		store:       store,
		installer:   installer,
		client:      client,
		captured:    &captured,
	}
	tc.manager = &lifecycle.Manager{
		Provisioner:  provisioner,
		State:        store,
		NewInstaller: func(string, string) (operator.Installer, error) { return installer, nil },
		NewNetworks:  func(kube.Config) (kube.Client, error) { return client, nil },
		K3dVersion:   "v5.9.0",
		CaptureContext: func() (string, error) {
			captured = true
			return prior, nil
		},
		RestoreContext: func(string, string) error { return nil },
	}

	return tc
}

// expectLock sets up the lock acquire/release for a mutating flow.
func (tc *testContext) expectLock() {
	tc.store.EXPECT().Lock(mock.Anything).Return(func() error { return nil }, nil)
}

func loadEnv(t *testing.T) devconfig.Environment {
	t.Helper()
	env, err := devconfig.Load(strings.NewReader(localEnvYAML))
	require.NoError(t, err)
	return *env
}

// readyNetwork returns a CardanoNetwork that WaitReady accepts: a fresh Ready
// condition plus the wallet/endpoints/magic the command layer surfaces.
func readyNetwork() *yacdv1alpha1.CardanoNetwork {
	magic := int64(42)
	return &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "devnet", Namespace: "devnet", Generation: 1},
		Status: yacdv1alpha1.CardanoNetworkStatus{
			ObservedGeneration: 1,
			Network:            &yacdv1alpha1.CardanoNetworkIdentityStatus{NetworkMagic: &magic},
			Endpoints: &yacdv1alpha1.CardanoNetworkEndpointsStatus{
				Ogmios: &yacdv1alpha1.ServiceEndpointStatus{URL: "ws://ogmios.devnet.svc:1337"},
				Kupo:   &yacdv1alpha1.ServiceEndpointStatus{URL: "http://kupo.devnet.svc:1442"},
			},
			Wallet: &yacdv1alpha1.WalletStatus{Address: "addr_test1xyz", Funded: true},
			Conditions: []metav1.Condition{{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				Reason:             "AllReady",
			}},
		},
	}
}

func upOptions(t *testing.T, bare bool) lifecycle.UpOptions {
	return lifecycle.UpOptions{
		Bare:        bare,
		Env:         loadEnv(t),
		NetworkName: "devnet",
		Namespace:   "devnet",
		Timeout:     time.Minute,
	}
}

func TestManagerUp(t *testing.T) {
	t.Run("provisions cluster, installs operator, applies network", func(t *testing.T) {
		tc := newTestContext(t, "minikube")
		tc.expectLock()
		tc.provisioner.EXPECT().EnsureCluster(mock.Anything, cluster.DefaultSpec(time.Minute)).Return(managedInfo, nil)
		tc.store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)
		var saved clusterstate.Record
		tc.store.EXPECT().Save(mock.Anything).Run(func(r clusterstate.Record) { saved = r }).Return(nil)
		tc.installer.EXPECT().EnsureOperator(mock.Anything, operator.InstallSpec{}).
			Return(operator.State{Installed: true, Ready: true, Version: "v0.1.1"}, nil)
		tc.client.EXPECT().EnsureNamespace(mock.Anything, "devnet").Return(nil)
		tc.client.EXPECT().ApplyCardanoNetwork(mock.Anything, mock.Anything).Return(nil)
		tc.client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork(), nil)

		result, err := tc.manager.Up(context.Background(), upOptions(t, false))
		require.NoError(t, err)

		assert.True(t, *tc.captured, "CaptureContext must run before EnsureCluster")
		assert.Equal(t, cluster.ManagedContext, result.Target.Context)
		assert.Equal(t, "v0.1.1", result.Operator.Version)
		require.NotNil(t, result.Network)
		assert.Equal(t, "addr_test1xyz", result.Network.Status.Wallet.Address)
		// The captured prior is a real context, so it is recorded for restore.
		assert.Equal(t, "minikube", saved.PriorContext)
		assert.Equal(t, cluster.ManagedContext, saved.Context)
		assert.Equal(t, "v5.9.0", saved.K3dVersion)
	})

	t.Run("bare stops after operator and applies no network", func(t *testing.T) {
		tc := newTestContext(t, "minikube")
		tc.expectLock()
		tc.provisioner.EXPECT().EnsureCluster(mock.Anything, mock.Anything).Return(managedInfo, nil)
		tc.store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)
		tc.store.EXPECT().Save(mock.Anything).Return(nil)
		tc.installer.EXPECT().EnsureOperator(mock.Anything, mock.Anything).
			Return(operator.State{Installed: true, Ready: true, Version: "v0.1.1"}, nil)

		result, err := tc.manager.Up(context.Background(), upOptions(t, true))
		require.NoError(t, err)
		assert.Nil(t, result.Network)
		tc.client.AssertNotCalled(t, "ApplyCardanoNetwork", mock.Anything, mock.Anything)
	})

	t.Run("warm re-run preserves the recorded prior context", func(t *testing.T) {
		// Captured context is already the managed one (we own it); the recorded
		// real prior must survive instead of being overwritten with the managed
		// context.
		tc := newTestContext(t, cluster.ManagedContext)
		tc.expectLock()
		tc.provisioner.EXPECT().EnsureCluster(mock.Anything, mock.Anything).Return(managedInfo, nil)
		tc.store.EXPECT().Load().Return(clusterstate.Record{PriorContext: "minikube"}, true, nil)
		var saved clusterstate.Record
		tc.store.EXPECT().Save(mock.Anything).Run(func(r clusterstate.Record) { saved = r }).Return(nil)
		tc.installer.EXPECT().EnsureOperator(mock.Anything, mock.Anything).
			Return(operator.State{Installed: true, Ready: true, Version: "v0.1.1"}, nil)
		tc.client.EXPECT().EnsureNamespace(mock.Anything, "devnet").Return(nil)
		tc.client.EXPECT().ApplyCardanoNetwork(mock.Anything, mock.Anything).Return(nil)
		tc.client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork(), nil)

		_, err := tc.manager.Up(context.Background(), upOptions(t, false))
		require.NoError(t, err)
		assert.Equal(t, "minikube", saved.PriorContext)
	})

	t.Run("rebuilds a missing record against a live cluster", func(t *testing.T) {
		tc := newTestContext(t, "")
		tc.expectLock()
		tc.provisioner.EXPECT().EnsureCluster(mock.Anything, mock.Anything).Return(managedInfo, nil)
		tc.store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)
		var saved clusterstate.Record
		tc.store.EXPECT().Save(mock.Anything).Run(func(r clusterstate.Record) { saved = r }).Return(nil)
		tc.installer.EXPECT().EnsureOperator(mock.Anything, mock.Anything).
			Return(operator.State{Installed: true, Ready: true, Version: "v0.1.1"}, nil)
		tc.client.EXPECT().EnsureNamespace(mock.Anything, "devnet").Return(nil)
		tc.client.EXPECT().ApplyCardanoNetwork(mock.Anything, mock.Anything).Return(nil)
		tc.client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork(), nil)

		_, err := tc.manager.Up(context.Background(), upOptions(t, false))
		require.NoError(t, err)
		assert.Equal(t, cluster.ManagedName, saved.ClusterName)
		assert.Equal(t, managedInfo.KubeconfigPath, saved.KubeconfigPath)
	})

	t.Run("propagates an operator refusal without applying a network", func(t *testing.T) {
		tc := newTestContext(t, "minikube")
		tc.expectLock()
		tc.provisioner.EXPECT().EnsureCluster(mock.Anything, mock.Anything).Return(managedInfo, nil)
		tc.store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)
		tc.store.EXPECT().Save(mock.Anything).Return(nil)
		tc.installer.EXPECT().EnsureOperator(mock.Anything, mock.Anything).
			Return(operator.State{}, errors.New("in-cluster operator is newer than this CLI"))

		_, err := tc.manager.Up(context.Background(), upOptions(t, false))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "newer")
		tc.client.AssertNotCalled(t, "ApplyCardanoNetwork", mock.Anything, mock.Anything)
	})
}

func TestManagerDown(t *testing.T) {
	t.Run("deletes the cluster, restores the prior context, clears the record", func(t *testing.T) {
		tc := newTestContext(t, "")
		tc.expectLock()
		tc.store.EXPECT().Load().Return(clusterstate.Record{
			ClusterName: cluster.ManagedName, Context: cluster.ManagedContext,
			PriorContext: "minikube", KubeconfigPath: "/home/dev/.kube/config",
		}, true, nil)
		tc.provisioner.EXPECT().DeleteCluster(mock.Anything, cluster.ManagedName).Return(nil)
		var restoredPath, restoredContext string
		tc.manager.RestoreContext = func(path, context string) error {
			restoredPath, restoredContext = path, context
			return nil
		}
		tc.store.EXPECT().Clear().Return(nil)

		require.NoError(t, tc.manager.Down(context.Background(), lifecycle.DownOptions{}))
		assert.Equal(t, "/home/dev/.kube/config", restoredPath)
		assert.Equal(t, "minikube", restoredContext)
	})

	t.Run("no record: deletes and clears without restoring", func(t *testing.T) {
		tc := newTestContext(t, "")
		tc.expectLock()
		tc.store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)
		tc.provisioner.EXPECT().DeleteCluster(mock.Anything, cluster.ManagedName).Return(nil)
		restoreCalled := false
		tc.manager.RestoreContext = func(string, string) error { restoreCalled = true; return nil }
		tc.store.EXPECT().Clear().Return(nil)

		require.NoError(t, tc.manager.Down(context.Background(), lifecycle.DownOptions{}))
		assert.False(t, restoreCalled, "no record means no context to restore")
	})
}

func TestManagerStatus(t *testing.T) {
	t.Run("cluster present: reports operator and networks", func(t *testing.T) {
		tc := newTestContext(t, "")
		tc.provisioner.EXPECT().Status(mock.Anything, cluster.ManagedName).
			Return(cluster.Status{Exists: true, Running: true, Healthy: true, Context: cluster.ManagedContext}, nil)
		tc.store.EXPECT().Load().Return(clusterstate.Record{Context: cluster.ManagedContext}, true, nil)
		tc.installer.EXPECT().OperatorState(mock.Anything).
			Return(operator.State{Installed: true, Ready: true, Version: "v0.1.1"}, nil)
		tc.client.EXPECT().ListCardanoNetworks(mock.Anything, "").
			Return([]yacdv1alpha1.CardanoNetwork{*readyNetwork()}, nil)

		report, err := tc.manager.Status(context.Background())
		require.NoError(t, err)
		assert.True(t, report.Cluster.Healthy)
		assert.Equal(t, "v0.1.1", report.Operator.Version)
		assert.Len(t, report.Networks, 1)
	})

	t.Run("cluster absent: returns early without operator or network calls", func(t *testing.T) {
		tc := newTestContext(t, "")
		tc.provisioner.EXPECT().Status(mock.Anything, cluster.ManagedName).Return(cluster.Status{Exists: false}, nil)
		tc.store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)

		report, err := tc.manager.Status(context.Background())
		require.NoError(t, err)
		assert.False(t, report.Cluster.Exists)
		assert.Empty(t, report.Networks)
		tc.installer.AssertNotCalled(t, "OperatorState", mock.Anything)
	})
}
