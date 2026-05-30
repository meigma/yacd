package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestWriteEndpointsFile(t *testing.T) {
	t.Chdir(t.TempDir())

	magic := int64(42)
	doc := endpointsDocument{
		Network:      "devnet",
		Namespace:    "devnet",
		NetworkMagic: &magic,
		OgmiosURL:    "ws://127.0.0.1:40001",
		KupoURL:      "http://127.0.0.1:40002",
		FaucetURL:    "http://127.0.0.1:40003",
	}

	path, err := writeEndpointsFile("devnet", doc)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(".yacd", "devnet", "endpoints.json"), path)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "endpoints file must be 0600")
	dirInfo, err := os.Stat(filepath.Join(".yacd", "devnet"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "state dir must be 0700")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"ogmiosUrl": "ws://127.0.0.1:40001"`)
	assert.Contains(t, string(data), `"networkMagic": 42`)
	assert.NotContains(t, string(data), "oken", "the endpoints file must never carry the faucet token")
}

func TestPrintConnectStatus(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	doc := endpointsDocument{
		Network:   "devnet",
		Namespace: "devnet",
		OgmiosURL: "ws://127.0.0.1:40001",
		KupoURL:   "http://127.0.0.1:40002",
	}
	require.NoError(t, printConnectStatus(&out, doc, ".yacd/devnet/endpoints.json"))

	text := out.String()
	assert.Contains(t, text, "Forwarding devnet (namespace devnet)")
	assert.Contains(t, text, "YACD_OGMIOS_URL=ws://127.0.0.1:40001")
	assert.Contains(t, text, "YACD_KUPO_URL=http://127.0.0.1:40002")
	assert.NotContains(t, text, "YACD_FAUCET_URL", "an unpublished faucet URL must be omitted")
	assert.Contains(t, text, ".yacd/devnet/endpoints.json")
}

func TestRunConnectInitialFailureIsFatal(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "ghost", "ghost").
		Return(nil, fmt.Errorf("cardanonetwork ghost/ghost %w", kube.ErrNotFound))

	commandContext := &commandContext{out: io.Discard, err: io.Discard}
	err := runConnect(context.Background(), commandContext, client, "ghost", "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunConnectWritesFileAndExitsOnCancel(t *testing.T) {
	t.Chdir(t.TempDir())

	network := readyNetwork("devnet")
	session := mocks.NewForwardSession(t)
	session.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	session.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	session.EXPECT().LocalPort(int32(8080)).Return(40003, true)
	session.EXPECT().Done().Return(make(chan struct{})) // never drops on its own
	session.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(session, nil)
	client.EXPECT().GetSecretValue(mock.Anything, "devnet", "devnet-faucet-auth", faucetAuthTokenKey).Return("faucet-token", nil)

	ctx, cancel := context.WithCancel(context.Background())
	commandContext := &commandContext{out: io.Discard, err: io.Discard}

	done := make(chan error, 1)
	go func() { done <- runConnect(ctx, commandContext, client, "devnet", "devnet") }()

	path := filepath.Join(".yacd", "devnet", "endpoints.json")
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)

		return err == nil
	}, 2*time.Second, 10*time.Millisecond, "connect did not write the endpoints file")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("runConnect did not return after the context was cancelled")
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"ogmiosUrl": "ws://127.0.0.1:40001"`)
	assert.NotContains(t, string(data), "faucet-token", "the endpoints file must never carry the token")
}

func TestNextBackoff(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 2*time.Second, nextBackoff(1*time.Second))
	assert.Equal(t, connectReconnectMaxBackoff, nextBackoff(connectReconnectMaxBackoff))
	assert.Equal(t, connectReconnectMaxBackoff, nextBackoff(10*time.Second))
}
