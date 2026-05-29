package cli

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestDownDeletesAndWaitsUntilGone(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	client := newKubeMock(t)
	client.EXPECT().DeleteCardanoNetwork(mock.Anything, "devnet", "devnet").Return(nil)
	// WaitGone polls GetCardanoNetwork; a wrapped ErrNotFound signals the
	// object (and its garbage-collected children) is gone.
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "devnet", "devnet").
		Return(nil, fmt.Errorf("cardanonetwork devnet/devnet %w", kube.ErrNotFound))

	root := NewRootCommand(Options{
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"down", "devnet"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Contains(t, stderr.String(), "Deleting CardanoNetwork devnet/devnet...")
	assert.Contains(t, stderr.String(), "CardanoNetwork devnet/devnet is gone.")
}

func TestDownIsIdempotentWhenAlreadyAbsent(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	// DeleteCardanoNetwork is idempotent and returns nil when the object is
	// already gone; WaitGone then sees the wrapped not-found immediately.
	client := newKubeMock(t)
	client.EXPECT().DeleteCardanoNetwork(mock.Anything, "devnet", "devnet").Return(nil)
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "devnet", "devnet").
		Return(nil, fmt.Errorf("cardanonetwork devnet/devnet %w", kube.ErrNotFound))

	root := NewRootCommand(Options{
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"down", "devnet"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Contains(t, stderr.String(), "CardanoNetwork devnet/devnet is gone.")
}

func TestDownUsesNamespaceFlagOverride(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().DeleteCardanoNetwork(mock.Anything, "flag-ns", "devnet").Return(nil)
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "flag-ns", "devnet").
		Return(nil, fmt.Errorf("cardanonetwork flag-ns/devnet %w", kube.ErrNotFound))

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"down", "devnet", "-n", "flag-ns"})

	require.NoError(t, root.ExecuteContext(context.Background()))
}

func TestDownWithoutWaitDeletesOnly(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	// No GetCardanoNetwork expectation: with --wait=false down must not poll;
	// mockery fails the test if WaitGone is reached.
	client := newKubeMock(t)
	client.EXPECT().DeleteCardanoNetwork(mock.Anything, "devnet", "devnet").Return(nil)

	root := NewRootCommand(Options{
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"down", "devnet", "--wait=false"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Contains(t, stderr.String(), "Deleting CardanoNetwork devnet/devnet...")
	assert.NotContains(t, stderr.String(), "is gone.")
}

func TestDownRequiresExactlyOneName(t *testing.T) {
	t.Parallel()

	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed with invalid args")
		},
	})
	root.SetArgs([]string{"down"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

func TestDownRejectsInvalidName(t *testing.T) {
	t.Parallel()

	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed with invalid NAME")
		},
	})
	root.SetArgs([]string{"down", "Devnet"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid NAME")
}
