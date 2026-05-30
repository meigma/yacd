package cli

import (
	"context"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConnectNetworkForwardsAndBuildsEnv(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	network := readyNetwork("devnet")

	session := mocks.NewForwardSession(t)
	session.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	session.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	session.EXPECT().LocalPort(int32(8080)).Return(40003, true)
	session.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(session, nil)
	client.EXPECT().GetSecretValue(mock.Anything, "devnet", "devnet-faucet-auth", faucetAuthTokenKey).Return("faucet-token", nil)

	connected, err := connectNetwork(ctx, client, "devnet", "devnet")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"YACD_NETWORK=devnet",
		"YACD_NAMESPACE=devnet",
		"YACD_NETWORK_MAGIC=42",
		"YACD_OGMIOS_URL=ws://127.0.0.1:40001",
		"YACD_KUPO_URL=http://127.0.0.1:40002",
		"YACD_FAUCET_URL=http://127.0.0.1:40003",
		"YACD_FAUCET_TOKEN=faucet-token",
	}, connected.env)
	require.NoError(t, connected.Close())
}

func TestConnectNetworkOmitsTokenWhenFaucetNotReady(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	network := readyNetwork("devnet")
	// The faucet endpoint stays published, but FaucetReady is not True, so the
	// token must be omitted rather than read — and no Secret read is attempted.
	for i := range network.Status.Conditions {
		if network.Status.Conditions[i].Type == "FaucetReady" {
			network.Status.Conditions[i].Status = metav1.ConditionFalse
		}
	}

	session := mocks.NewForwardSession(t)
	session.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	session.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	session.EXPECT().LocalPort(int32(8080)).Return(40003, true)
	session.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(session, nil)
	// Deliberately no GetSecretValue expectation: reading the token here would
	// be an unexpected call and fail the mock's cleanup assertion.

	connected, err := connectNetwork(ctx, client, "devnet", "devnet")
	require.NoError(t, err)
	assert.Contains(t, connected.env, "YACD_FAUCET_URL=http://127.0.0.1:40003")
	for _, entry := range connected.env {
		assert.NotContains(t, entry, "YACD_FAUCET_TOKEN")
	}
	require.NoError(t, connected.Close())
}

func TestConnectNetworkRejectsNotReady(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	network := readyNetwork("devnet")
	network.Generation = 2 // status observed an older generation -> stale

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)

	// No PrimaryPodName/Forward expectations: the readiness gate must fail first.
	_, err := connectNetwork(ctx, client, "devnet", "devnet")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stale")
}

func TestConnectNetworkRejectsNoEndpoints(t *testing.T) {
	t.Parallel()

	// A non-default name/namespace also proves connectNetwork is identity-agnostic.
	ctx := context.Background()
	network := readyNetwork("ci")
	network.Name = "shard1"
	network.Status.Endpoints = nil // ready but nothing published to forward

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "ci", "shard1").Return(network, nil)

	_, err := connectNetwork(ctx, client, "ci", "shard1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no chain-API endpoints")
}

func TestForwardSpecsExcludesNodeToNode(t *testing.T) {
	t.Parallel()

	network := readyNetwork("devnet")
	network.Status.Endpoints.NodeToNode = &yacdv1alpha1.ServiceEndpointStatus{
		ServiceName: "devnet-node",
		Port:        3001,
		URL:         "tcp://devnet-node.devnet.svc.cluster.local:3001",
	}

	specs := forwardSpecs(network)
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
		assert.NotEqual(t, int32(3001), spec.Remote, "node-to-node must not be forwarded")
	}
	assert.ElementsMatch(t, []string{"ogmios", "kupo", "faucet"}, names)
}

func TestRequireReady(t *testing.T) {
	t.Parallel()

	t.Run("ready passes", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, requireReady(readyNetwork("devnet"), "devnet", "devnet"))
	})

	t.Run("degraded fails", func(t *testing.T) {
		t.Parallel()
		network := readyNetwork("devnet")
		network.Status.Conditions = append(network.Status.Conditions, metav1.Condition{
			Type:               "Degraded",
			Status:             metav1.ConditionTrue,
			Reason:             "UnsupportedSpec",
			Message:            "bad",
			ObservedGeneration: 1,
		})
		err := requireReady(network, "devnet", "devnet")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "degraded")
	})

	t.Run("missing ready fails", func(t *testing.T) {
		t.Parallel()
		network := readyNetwork("devnet")
		network.Status.Conditions = nil
		err := requireReady(network, "devnet", "devnet")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not ready")
	})
}
