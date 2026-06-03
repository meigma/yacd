package cli

import (
	"testing"

	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTarget(t *testing.T) {
	t.Run("explicit target wins and the store is never consulted", func(t *testing.T) {
		store := mocks.NewStore(t) // no Load expectation: a consult would panic
		cfg := RuntimeConfig{Kubeconfig: "/tmp/kc", KubeContext: "explicit"}

		got, err := ResolveTarget(cfg, store)
		require.NoError(t, err)
		assert.Equal(t, "/tmp/kc", got.Kubeconfig)
		assert.Equal(t, "explicit", got.Context)
		store.AssertNotCalled(t, "Load")
	})

	t.Run("explicit context alone wins over a present record", func(t *testing.T) {
		store := mocks.NewStore(t)
		cfg := RuntimeConfig{KubeContext: "explicit"}

		got, err := ResolveTarget(cfg, store)
		require.NoError(t, err)
		assert.Equal(t, "explicit", got.Context)
		assert.Empty(t, got.Kubeconfig)
	})

	t.Run("managed record is used when no explicit target is set", func(t *testing.T) {
		store := mocks.NewStore(t)
		store.EXPECT().Load().Return(clusterstate.Record{
			Context:        "k3d-yacd",
			KubeconfigPath: "/home/dev/.kube/config",
		}, true, nil)

		got, err := ResolveTarget(RuntimeConfig{}, store)
		require.NoError(t, err)
		assert.Equal(t, "k3d-yacd", got.Context)
		assert.Equal(t, "/home/dev/.kube/config", got.Kubeconfig)
	})

	t.Run("no explicit target and no record resolves to the empty ambient config", func(t *testing.T) {
		store := mocks.NewStore(t)
		store.EXPECT().Load().Return(clusterstate.Record{}, false, nil)

		got, err := ResolveTarget(RuntimeConfig{}, store)
		require.NoError(t, err)
		assert.Empty(t, got.Context)
		assert.Empty(t, got.Kubeconfig)
	})

	t.Run("a store load error propagates", func(t *testing.T) {
		store := mocks.NewStore(t)
		store.EXPECT().Load().Return(clusterstate.Record{}, false, assert.AnError)

		_, err := ResolveTarget(RuntimeConfig{}, store)
		require.Error(t, err)
	})
}
