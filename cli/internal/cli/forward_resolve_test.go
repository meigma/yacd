package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// reachableProber accepts every URL; unreachableProber rejects every URL. They
// make the externalURL probe verdict deterministic without a real dial.
func reachableProber(context.Context, string) error   { return nil }
func unreachableProber(context.Context, string) error { return errors.New("unreachable") }

// proberForHosts accepts only URLs containing one of the given substrings,
// rejecting the rest — so a test can make one endpoint reachable and another not.
func proberForHosts(reachable ...string) EndpointProber {
	return func(_ context.Context, rawURL string) error {
		for _, marker := range reachable {
			if strings.Contains(rawURL, marker) {
				return nil
			}
		}

		return errors.New("unreachable")
	}
}

// networkWithExternalURLs returns a ready devnet network whose Ogmios/Kupo
// endpoints advertise the standard localhost externalURLs a devnet publishes.
func networkWithExternalURLs() *yacdv1alpha1.CardanoNetwork {
	network := readyNetwork("devnet")
	network.Status.Endpoints.Ogmios.ExternalURL = "ws://localhost:1337"
	network.Status.Endpoints.Kupo.ExternalURL = "http://localhost:1442"

	return network
}

func TestResolveChainAccessUsesReachableExternalURLWithoutForwarding(t *testing.T) {
	t.Parallel()

	network := networkWithExternalURLs()
	// No PrimaryPodName/Forward expectations: a reachable externalURL must not
	// forward. An unexpected call fails the auto-asserted mock.
	client := newKubeMock(t)
	cc := &commandContext{endpointProber: reachableProber}

	access, err := cc.resolveChainAccess(context.Background(), client, network, "devnet", "devnet", chainOverrides{})
	require.NoError(t, err)
	defer func() { require.NoError(t, access.Close()) }()

	assert.Nil(t, access.session, "no forward should be opened")
	assert.Equal(t, "ws://localhost:1337", access.ogmiosURL)
	assert.Equal(t, "http://localhost:1442", access.kupoURL)
	assert.Equal(t, []string{
		"YACD_NETWORK=devnet",
		"YACD_NAMESPACE=devnet",
		"YACD_NETWORK_MAGIC=42",
		"YACD_OGMIOS_URL=ws://localhost:1337",
		"YACD_KUPO_URL=http://localhost:1442",
	}, access.env)
}

func TestResolveChainAccessFallsBackToForwardWhenProbeFails(t *testing.T) {
	t.Parallel()

	network := networkWithExternalURLs()

	session := mocks.NewForwardSession(t)
	session.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	session.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	session.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(session, nil)

	cc := &commandContext{endpointProber: unreachableProber}

	access, err := cc.resolveChainAccess(context.Background(), client, network, "devnet", "devnet", chainOverrides{})
	require.NoError(t, err)
	defer func() { require.NoError(t, access.Close()) }()

	assert.Equal(t, "ws://127.0.0.1:40001", access.ogmiosURL)
	assert.Equal(t, "http://127.0.0.1:40002", access.kupoURL)
}

func TestResolveChainAccessForwardsOnlyUnreachableEndpoint(t *testing.T) {
	t.Parallel()

	network := networkWithExternalURLs()

	// Only Kupo is forwarded, so the session is asked for only its local port.
	session := mocks.NewForwardSession(t)
	session.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	session.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde",
		mock.MatchedBy(func(specs []kube.PortForwardSpec) bool {
			return len(specs) == 1 && specs[0].Name == "kupo" && specs[0].Remote == int32(1442)
		}),
	).Return(session, nil)

	// Ogmios's externalURL is reachable; Kupo's is not.
	cc := &commandContext{endpointProber: proberForHosts("1337")}

	access, err := cc.resolveChainAccess(context.Background(), client, network, "devnet", "devnet", chainOverrides{})
	require.NoError(t, err)
	defer func() { require.NoError(t, access.Close()) }()

	assert.Equal(t, "ws://localhost:1337", access.ogmiosURL, "reachable externalURL is used directly")
	assert.Equal(t, "http://127.0.0.1:40002", access.kupoURL, "unreachable endpoint falls back to a forward")
}

func TestResolveChainAccessPrefersOverride(t *testing.T) {
	t.Parallel()

	network := networkWithExternalURLs()
	// The override short-circuits ahead of the (reachable) externalURL; with both
	// endpoints resolved directly, nothing is forwarded.
	client := newKubeMock(t)
	cc := &commandContext{endpointProber: reachableProber}

	access, err := cc.resolveChainAccess(context.Background(), client, network, "devnet", "devnet", chainOverrides{
		OgmiosURL: "ws://custom-ogmios:9999",
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, access.Close()) }()

	assert.Equal(t, "ws://custom-ogmios:9999", access.ogmiosURL)
	assert.Equal(t, "http://localhost:1442", access.kupoURL)
}

func TestResolveChainAccessPrefersAmbientEnv(t *testing.T) {
	// No t.Parallel: t.Setenv is incompatible with parallel tests.
	t.Setenv(envOgmiosURL, "ws://ambient-ogmios:8888")

	network := networkWithExternalURLs()
	client := newKubeMock(t)
	cc := &commandContext{endpointProber: reachableProber}

	access, err := cc.resolveChainAccess(context.Background(), client, network, "devnet", "devnet", chainOverrides{})
	require.NoError(t, err)
	defer func() { require.NoError(t, access.Close()) }()

	assert.Equal(t, "ws://ambient-ogmios:8888", access.ogmiosURL, "ambient YACD_OGMIOS_URL outranks the externalURL")
	assert.Equal(t, "http://localhost:1442", access.kupoURL)
}

func TestResolveChainAccessForwardsWhenNoExternalURL(t *testing.T) {
	t.Parallel()

	// A network that advertises no externalURL must forward without ever probing.
	network := readyNetwork("devnet")

	session := mocks.NewForwardSession(t)
	session.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	session.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	session.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(session, nil)

	cc := &commandContext{endpointProber: func(context.Context, string) error {
		t.Error("prober must not run when no externalURL is published")

		return nil
	}}

	access, err := cc.resolveChainAccess(context.Background(), client, network, "devnet", "devnet", chainOverrides{})
	require.NoError(t, err)
	defer func() { require.NoError(t, access.Close()) }()

	assert.Equal(t, "ws://127.0.0.1:40001", access.ogmiosURL)
	assert.Equal(t, "http://127.0.0.1:40002", access.kupoURL)
}

func TestProbeEndpointReachableRejectsUnparseableURL(t *testing.T) {
	t.Parallel()

	err := probeEndpointReachable(context.Background(), "://no-scheme")
	require.Error(t, err)
}

func TestDialAddressInfersDefaultPortFromScheme(t *testing.T) {
	t.Parallel()

	address, err := dialAddress("wss://ogmios.example.com")
	require.NoError(t, err)
	assert.Equal(t, "ogmios.example.com:443", address)

	address, err = dialAddress("ws://localhost:1337")
	require.NoError(t, err)
	assert.Equal(t, "localhost:1337", address)
}
