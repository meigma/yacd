package cli

import (
	"bytes"
	"context"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/devconfig"
	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// devnetMocks bundles the lifecycle port mocks injected into the root command.
type devnetMocks struct {
	provisioner *mocks.Provisioner
	store       *mocks.Store
	installer   *mocks.Installer
	client      *mocks.Client
}

// newDevnetRoot builds a root command whose devnet factories return the given
// mocks. The kube client factory ignores its config and returns the mock.
func newDevnetRoot(t *testing.T, out, errOut *bytes.Buffer) (*devnetMocks, func(args ...string) error) {
	t.Helper()

	m := &devnetMocks{
		provisioner: mocks.NewProvisioner(t),
		store:       mocks.NewStore(t),
		installer:   mocks.NewInstaller(t),
		client:      mocks.NewClient(t),
	}
	root := NewRootCommand(Options{
		Out:                       out,
		Err:                       errOut,
		Viper:                     viper.New(),
		KubeClientFactory:         kubeClientFactory(m.client),
		ClusterProvisionerFactory: func() (cluster.Provisioner, error) { return m.provisioner, nil },
		ClusterStateFactory:       func() (clusterstate.Store, error) { return m.store, nil },
		OperatorInstallerFactory:  func(string, string) (operator.Installer, error) { return m.installer, nil },
	})

	run := func(args ...string) error {
		root.SetArgs(args)
		return root.ExecuteContext(context.Background())
	}

	return m, run
}

func (m *devnetMocks) expectLock() {
	m.store.EXPECT().Lock(mock.Anything).Return(func() error { return nil }, nil)
}

var devnetInfo = cluster.Info{
	Name:           cluster.ManagedName,
	Context:        cluster.ManagedContext,
	KubeconfigPath: "/home/dev/.kube/config",
	Running:        true,
}

func fundedNetwork() *yacdv1alpha1.CardanoNetwork {
	network := readyNetwork(devnetNetworkName)
	network.Status.Wallet = &yacdv1alpha1.WalletStatus{Address: "addr_test1funded", Funded: true}
	return network
}

func TestDevnetUp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	m, run := newDevnetRoot(t, &stdout, &stderr)

	m.expectLock()
	m.provisioner.EXPECT().EnsureCluster(mock.Anything, mock.Anything).Return(devnetInfo, nil)
	m.store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)
	m.store.EXPECT().Save(mock.Anything).Return(nil)
	m.installer.EXPECT().EnsureOperator(mock.Anything, operator.InstallSpec{Values: operator.Default()}).
		Return(operator.State{Installed: true, Ready: true, Version: "v0.1.1"}, nil)
	m.client.EXPECT().EnsureNamespace(mock.Anything, devnetNetworkName).Return(nil)
	m.client.EXPECT().ApplyCardanoNetwork(mock.Anything, mock.Anything).Return(nil)
	m.client.EXPECT().GetCardanoNetwork(mock.Anything, devnetNetworkName, devnetNetworkName).Return(fundedNetwork(), nil)

	require.NoError(t, run("devnet"))

	out := stdout.String()
	assert.Contains(t, out, "devnet is ready.")
	assert.Contains(t, out, "v0.1.1")
	assert.Contains(t, out, "addr_test1funded")
	assert.Contains(t, out, "ws://devnet-ogmios.devnet.svc.cluster.local:1337")
	assert.Contains(t, out, "cardano-cli query tip --testnet-magic 42")
}

func TestDevnetBareSkipsNetwork(t *testing.T) {
	var stdout, stderr bytes.Buffer
	m, run := newDevnetRoot(t, &stdout, &stderr)

	m.expectLock()
	m.provisioner.EXPECT().EnsureCluster(mock.Anything, mock.Anything).Return(devnetInfo, nil)
	m.store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)
	m.store.EXPECT().Save(mock.Anything).Return(nil)
	m.installer.EXPECT().EnsureOperator(mock.Anything, mock.Anything).
		Return(operator.State{Installed: true, Ready: true, Version: "v0.1.1"}, nil)

	require.NoError(t, run("devnet", "--bare"))

	assert.Contains(t, stdout.String(), "No network applied")
	m.client.AssertNotCalled(t, "ApplyCardanoNetwork", mock.Anything, mock.Anything)
}

func TestDevnetDown(t *testing.T) {
	var stdout, stderr bytes.Buffer
	m, run := newDevnetRoot(t, &stdout, &stderr)

	m.expectLock()
	m.store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)
	m.provisioner.EXPECT().DeleteCluster(mock.Anything, cluster.ManagedName).Return(nil)
	m.store.EXPECT().Clear().Return(nil)

	require.NoError(t, run("devnet", "down"))
	assert.Contains(t, stdout.String(), "devnet cluster removed.")
}

func TestDevnetStatus(t *testing.T) {
	t.Run("cluster present", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		m, run := newDevnetRoot(t, &stdout, &stderr)

		m.provisioner.EXPECT().Status(mock.Anything, cluster.ManagedName, mock.Anything).
			Return(cluster.Status{Exists: true, Running: true, Healthy: true, Context: cluster.ManagedContext}, nil)
		m.store.EXPECT().Load().Return(clusterstate.Record{Context: cluster.ManagedContext}, true, nil)
		m.installer.EXPECT().OperatorState(mock.Anything, mock.Anything).
			Return(operator.State{Installed: true, Ready: true, Version: "v0.1.1"}, nil)
		m.client.EXPECT().ListCardanoNetworks(mock.Anything, "").
			Return([]yacdv1alpha1.CardanoNetwork{*fundedNetwork()}, nil)

		require.NoError(t, run("devnet", "status"))
		out := stdout.String()
		assert.Contains(t, out, cluster.ManagedContext)
		assert.Contains(t, out, "v0.1.1")
		assert.Contains(t, out, "devnet/devnet")
	})

	t.Run("no record prints a hint without probing the runtime", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		m, run := newDevnetRoot(t, &stdout, &stderr)

		m.store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)

		require.NoError(t, run("devnet", "status"))
		assert.Contains(t, stdout.String(), "Run `yacd devnet`")
		m.provisioner.AssertNotCalled(t, "Status", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("cluster absent but record present clears the stale record", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		m, run := newDevnetRoot(t, &stdout, &stderr)

		m.provisioner.EXPECT().Status(mock.Anything, cluster.ManagedName, mock.Anything).Return(cluster.Status{Exists: false}, nil)
		m.store.EXPECT().Load().Return(clusterstate.Record{Context: cluster.ManagedContext}, true, nil)
		m.store.EXPECT().Clear().Return(nil)

		require.NoError(t, run("devnet", "status"))
		assert.Contains(t, stderr.String(), "cleared stale state")
		assert.Contains(t, stdout.String(), "Run `yacd devnet`")
	})
}

func TestDevnetRejectsExplicitTarget(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
	}{
		{"kubeconfig", "YACD_KUBECONFIG"},
		{"context", "YACD_KUBE_CONTEXT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, "explicit-value")
			var stdout, stderr bytes.Buffer
			// The guard rejects before any cluster/store/operator call.
			_, run := newDevnetRoot(t, &stdout, &stderr)

			err := run("devnet")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "manages its own cluster")
		})
	}
}

func TestDevnetRejectsNonPositiveTimeout(t *testing.T) {
	for _, args := range [][]string{
		{"devnet", "--timeout", "0s"},
		{"devnet", "down", "--timeout", "0s"},
	} {
		var stdout, stderr bytes.Buffer
		// No port mocks are exercised: the timeout guard rejects before any
		// cluster, store, or operator call.
		_, run := newDevnetRoot(t, &stdout, &stderr)

		err := run(args...)
		require.Error(t, err, "args %v", args)
		assert.Contains(t, err.Error(), "--timeout must be greater than 0")
	}
}

// TestDefaultDevnetEnvIsValid guards the embedded default environment against
// drift from examples/local/yacd.yaml: it must parse, be a local network, and
// carry the funded-wallet defaults the devnet UX promises.
func TestDefaultDevnetEnvIsValid(t *testing.T) {
	env, err := devconfig.Load(bytes.NewReader(defaultDevnetEnvYAML))
	require.NoError(t, err)

	assert.Equal(t, yacdv1alpha1.CardanoNetworkModeLocal, env.Spec.Network.Mode)
	require.NotNil(t, env.Spec.Network.Local)
	assert.Equal(t, int64(42), env.Spec.Network.Local.NetworkMagic)
	require.NotNil(t, env.Spec.Network.ChainAPI)
	require.NotNil(t, env.Spec.Network.ChainAPI.Wallet)
	assert.True(t, env.Spec.Network.ChainAPI.Wallet.Enabled)
	assert.Equal(t, int64(100000000000), env.Spec.Network.ChainAPI.Wallet.FundingLovelace)
}
