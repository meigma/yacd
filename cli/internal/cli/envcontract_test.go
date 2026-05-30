package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoopbackURLPreservesScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		published string
		localPort int
		want      string
	}{
		{
			name:      "websocket scheme is preserved",
			published: "ws://devnet-ogmios.devnet.svc.cluster.local:1337",
			localPort: 40001,
			want:      "ws://127.0.0.1:40001",
		},
		{
			name:      "http scheme is preserved",
			published: "http://devnet-kupo.devnet.svc.cluster.local:1442",
			localPort: 40002,
			want:      "http://127.0.0.1:40002",
		},
		{
			name:      "path is preserved",
			published: "http://devnet-kupo.devnet.svc.cluster.local:1442/health",
			localPort: 40002,
			want:      "http://127.0.0.1:40002/health",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := loopbackURL(tt.published, tt.localPort)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoopbackURLRejectsSchemelessURL(t *testing.T) {
	t.Parallel()

	// A scheme-relative URL parses cleanly but has no scheme to preserve.
	_, err := loopbackURL("//devnet-ogmios.devnet.svc.cluster.local:1337", 40001)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no scheme")
}

func TestHostEnvBuildsLoopbackContract(t *testing.T) {
	t.Parallel()

	network := readyNetwork("devnet")
	local := map[int32]int{1337: 40001, 1442: 40002, 8080: 40003}
	lookup := func(remote int32) (int, bool) {
		port, ok := local[remote]

		return port, ok
	}

	env, err := hostEnv(network, lookup, "faucet-token")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"YACD_NETWORK=devnet",
		"YACD_NAMESPACE=devnet",
		"YACD_NETWORK_MAGIC=42",
		"YACD_OGMIOS_URL=ws://127.0.0.1:40001",
		"YACD_KUPO_URL=http://127.0.0.1:40002",
		"YACD_FAUCET_URL=http://127.0.0.1:40003",
		"YACD_FAUCET_TOKEN=faucet-token",
	}, env)
}

func TestHostEnvSkipsUnforwardedEndpointsAndEmptyToken(t *testing.T) {
	t.Parallel()

	network := readyNetwork("devnet")
	// Only Ogmios was forwarded; Kupo/faucet have no local port.
	lookup := func(remote int32) (int, bool) {
		if remote == 1337 {
			return 40001, true
		}

		return 0, false
	}

	env, err := hostEnv(network, lookup, "")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"YACD_NETWORK=devnet",
		"YACD_NAMESPACE=devnet",
		"YACD_NETWORK_MAGIC=42",
		"YACD_OGMIOS_URL=ws://127.0.0.1:40001",
	}, env)
	assert.NotContains(t, env, "YACD_FAUCET_TOKEN=")
}

func TestPodEnvUsesClusterURLsAndOmitsToken(t *testing.T) {
	t.Parallel()

	network := readyNetwork("devnet")

	env := podEnv(network, "/ipc/node.socket")
	assert.Equal(t, []string{
		"YACD_NETWORK=devnet",
		"YACD_NAMESPACE=devnet",
		"YACD_NETWORK_MAGIC=42",
		"YACD_OGMIOS_URL=ws://devnet-ogmios.devnet.svc.cluster.local:1337",
		"YACD_KUPO_URL=http://devnet-kupo.devnet.svc.cluster.local:1442",
		"YACD_FAUCET_URL=http://devnet-faucet.devnet.svc.cluster.local:8080",
		"CARDANO_NODE_SOCKET_PATH=/ipc/node.socket",
	}, env)
	for _, entry := range env {
		assert.NotContains(t, entry, "YACD_FAUCET_TOKEN", "the in-pod contract must never carry the faucet token")
	}
}
