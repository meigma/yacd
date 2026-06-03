package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// orphanTestRoot wires a root command with the three lifecycle factories plus a
// kube client, for exercising the managed-target orphan reconcile through a verb.
func orphanTestRoot(t *testing.T, stdout, stderr *bytes.Buffer) (*mocks.Store, *mocks.Provisioner, *mocks.Client, func(...string) error) {
	t.Helper()
	store := mocks.NewStore(t)
	provisioner := mocks.NewProvisioner(t)
	client := mocks.NewClient(t)

	root := NewRootCommand(Options{
		Out: stdout, Err: stderr, Viper: viper.New(),
		KubeClientFactory:         kubeClientFactory(client),
		ClusterStateFactory:       func() (clusterstate.Store, error) { return store, nil },
		ClusterProvisionerFactory: func() (cluster.Provisioner, error) { return provisioner, nil },
	})
	run := func(args ...string) error {
		root.SetArgs(args)
		return root.ExecuteContext(context.Background())
	}

	return store, provisioner, client, run
}

func TestManagedTargetOrphanReconcile(t *testing.T) {
	managedRecord := clusterstate.Record{Context: cluster.ManagedContext, KubeconfigPath: "/home/dev/.kube/config"}

	t.Run("clears the record when the managed cluster is gone", func(t *testing.T) {
		t.Setenv("YACD_KUBECONFIG", "")
		t.Setenv("YACD_KUBE_CONTEXT", "")
		t.Setenv("YACD_NAMESPACE", "")

		var stdout, stderr bytes.Buffer
		store, provisioner, client, run := orphanTestRoot(t, &stdout, &stderr)

		store.EXPECT().Load().Return(managedRecord, true, nil)
		client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").
			Return(nil, errors.New("dial tcp: connection refused"))
		provisioner.EXPECT().Status(mock.Anything, cluster.ManagedName, mock.Anything).Return(cluster.Status{Exists: false}, nil)
		store.EXPECT().Clear().Return(nil)

		require.Error(t, run("info", "devnet"))
		assert.Contains(t, stderr.String(), "cleared stale state")
	})

	t.Run("keeps the record when the managed cluster still exists", func(t *testing.T) {
		t.Setenv("YACD_KUBECONFIG", "")
		t.Setenv("YACD_KUBE_CONTEXT", "")
		t.Setenv("YACD_NAMESPACE", "")

		var stdout, stderr bytes.Buffer
		store, provisioner, client, run := orphanTestRoot(t, &stdout, &stderr)

		store.EXPECT().Load().Return(managedRecord, true, nil)
		client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").
			Return(nil, errors.New("transient API error"))
		provisioner.EXPECT().Status(mock.Anything, cluster.ManagedName, mock.Anything).
			Return(cluster.Status{Exists: true, Running: true}, nil)

		require.Error(t, run("info", "devnet"))
		assert.NotContains(t, stderr.String(), "cleared stale state")
		store.AssertNotCalled(t, "Clear")
	})

	t.Run("does not probe when an explicit target was used", func(t *testing.T) {
		t.Setenv("YACD_KUBE_CONTEXT", "explicit")
		t.Setenv("YACD_NAMESPACE", "")

		var stdout, stderr bytes.Buffer
		store, provisioner, client, run := orphanTestRoot(t, &stdout, &stderr)

		client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").
			Return(nil, errors.New("connection refused"))

		require.Error(t, run("info", "devnet"))
		provisioner.AssertNotCalled(t, "Status", mock.Anything, mock.Anything, mock.Anything)
		store.AssertNotCalled(t, "Clear")
	})
}
