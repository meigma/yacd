package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

	path, err := writeEndpointsFile("devnet", "devnet", doc)
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

func TestWriteEndpointsFileUsesNamespaceQualifiedPath(t *testing.T) {
	t.Chdir(t.TempDir())

	magic := int64(42)
	doc := endpointsDocument{
		Network:      "devnet",
		Namespace:    "team-a",
		NetworkMagic: &magic,
		OgmiosURL:    "ws://127.0.0.1:40001",
	}

	path, err := writeEndpointsFile("team-a", "devnet", doc)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(".yacd", "team-a", "devnet", "endpoints.json"), path)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "endpoints file must be 0600")
	for _, dir := range []string{filepath.Join(".yacd", "team-a"), filepath.Join(".yacd", "team-a", "devnet")} {
		dirInfo, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), "state dir must be 0700")
	}
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

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"ogmiosUrl": "ws://127.0.0.1:40001"`)
	assert.NotContains(t, string(data), "faucet-token", "the endpoints file must never carry the token")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("runConnect did not return after the context was cancelled")
	}

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "connect should remove stale endpoint state on clean disconnect")
}

func TestNextBackoff(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 2*time.Second, nextBackoff(1*time.Second))
	assert.Equal(t, connectReconnectMaxBackoff, nextBackoff(connectReconnectMaxBackoff))
	assert.Equal(t, connectReconnectMaxBackoff, nextBackoff(10*time.Second))
}

// TestRunConnectReEstablishesAfterDrop drives the supervision loop's headline
// path: a connected forward drops, connect re-resolves the Pod and re-forwards,
// rewrites endpoints.json with the new local ports, then a cancel exits
// cleanly. The two Forward calls return sessions with different local ports so
// the rewrite is observable in the file.
func TestRunConnectReEstablishesAfterDrop(t *testing.T) {
	t.Chdir(t.TempDir())

	dropFirst := make(chan struct{})
	first := mocks.NewForwardSession(t)
	first.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	first.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	first.EXPECT().LocalPort(int32(8080)).Return(40003, true)
	first.EXPECT().Done().Return(dropFirst)
	first.EXPECT().Err().Return(errors.New("connection lost"))
	first.EXPECT().Close().Return(nil)

	second := mocks.NewForwardSession(t)
	second.EXPECT().LocalPort(int32(1337)).Return(50001, true)
	second.EXPECT().LocalPort(int32(1442)).Return(50002, true)
	second.EXPECT().LocalPort(int32(8080)).Return(50003, true)
	second.EXPECT().Done().Return(make(chan struct{})) // stays up until cancel
	second.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(first, nil).Once()
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(second, nil).Once()
	client.EXPECT().GetSecretValue(mock.Anything, "devnet", "devnet-faucet-auth", faucetAuthTokenKey).Return("faucet-token", nil)

	ctx, cancel := context.WithCancel(context.Background())
	var stderr bytes.Buffer
	commandContext := &commandContext{out: io.Discard, err: &stderr}

	done := make(chan error, 1)
	go func() { done <- runConnect(ctx, commandContext, client, "devnet", "devnet") }()

	path := filepath.Join(".yacd", "devnet", "endpoints.json")
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(path)

		return err == nil && strings.Contains(string(data), "40001")
	}, 2*time.Second, 10*time.Millisecond, "first connect did not write the endpoints file")

	close(dropFirst) // simulate the forward dropping

	require.Eventually(t, func() bool {
		data, err := os.ReadFile(path)

		return err == nil && strings.Contains(string(data), "50001")
	}, 2*time.Second, 10*time.Millisecond, "connect did not re-establish and rewrite the endpoints file")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("runConnect did not return after the context was cancelled")
	}

	// The drop is reported only after runConnect has returned, so reading the
	// buffer here is free of the goroutine's writes.
	assert.Contains(t, stderr.String(), "dropped")
	assert.Contains(t, stderr.String(), "re-establishing")
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "connect should remove stale endpoint state on clean disconnect")
}

// TestRunConnectBacksOffThenRecovers exercises the backoff branch: after a
// drop, the re-establish fails transiently (not NotFound), so connect logs the
// retry, backs off, and recovers on the next attempt.
func TestRunConnectBacksOffThenRecovers(t *testing.T) {
	t.Chdir(t.TempDir())

	dropFirst := make(chan struct{})
	first := mocks.NewForwardSession(t)
	first.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	first.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	first.EXPECT().LocalPort(int32(8080)).Return(40003, true)
	first.EXPECT().Done().Return(dropFirst)
	first.EXPECT().Err().Return(errors.New("connection lost"))
	first.EXPECT().Close().Return(nil)

	recovered := mocks.NewForwardSession(t)
	recovered.EXPECT().LocalPort(int32(1337)).Return(50001, true)
	recovered.EXPECT().LocalPort(int32(1442)).Return(50002, true)
	recovered.EXPECT().LocalPort(int32(8080)).Return(50003, true)
	recovered.EXPECT().Done().Return(make(chan struct{}))
	recovered.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	// First connect succeeds, the re-establish fails transiently (triggering the
	// backoff branch), and the third attempt recovers.
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(first, nil).Once()
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(nil, errors.New("dial tcp: connection refused")).Once()
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(recovered, nil).Once()
	client.EXPECT().GetSecretValue(mock.Anything, "devnet", "devnet-faucet-auth", faucetAuthTokenKey).Return("faucet-token", nil)

	ctx, cancel := context.WithCancel(context.Background())
	var stderr bytes.Buffer
	commandContext := &commandContext{out: io.Discard, err: &stderr}

	done := make(chan error, 1)
	go func() { done <- runConnect(ctx, commandContext, client, "devnet", "devnet") }()

	path := filepath.Join(".yacd", "devnet", "endpoints.json")
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(path)

		return err == nil && strings.Contains(string(data), "40001")
	}, 2*time.Second, 10*time.Millisecond, "first connect did not write the endpoints file")

	close(dropFirst)

	// The re-establish fails and backs off (1s initial) before the next attempt
	// recovers and rewrites the file with the new ports; allow for that wait.
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(path)

		return err == nil && strings.Contains(string(data), "50001")
	}, 4*time.Second, 20*time.Millisecond, "connect did not recover after backing off")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("runConnect did not return after the context was cancelled")
	}

	assert.Contains(t, stderr.String(), "Reconnect to devnet/devnet failed")
	assert.Contains(t, stderr.String(), "retrying in 1s")
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "connect should remove stale endpoint state on clean disconnect")
}

// TestRunConnectReturnsWhenNetworkDeletedDuringReconnect covers the fatal
// reconnect path: once a forward drops, a NotFound on the re-establish (the
// network was deleted) ends the loop with the error rather than retrying.
func TestRunConnectReturnsWhenNetworkDeletedDuringReconnect(t *testing.T) {
	t.Chdir(t.TempDir())

	dropFirst := make(chan struct{})
	first := mocks.NewForwardSession(t)
	first.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	first.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	first.EXPECT().LocalPort(int32(8080)).Return(40003, true)
	first.EXPECT().Done().Return(dropFirst)
	first.EXPECT().Err().Return(errors.New("connection lost"))
	first.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(readyNetwork("devnet"), nil).Once()
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").
		Return(nil, fmt.Errorf("cardanonetwork devnet/devnet %w", kube.ErrNotFound)).Once()
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil).Once()
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(first, nil).Once()
	client.EXPECT().GetSecretValue(mock.Anything, "devnet", "devnet-faucet-auth", faucetAuthTokenKey).Return("faucet-token", nil).Once()

	commandContext := &commandContext{out: io.Discard, err: io.Discard}

	done := make(chan error, 1)
	go func() { done <- runConnect(context.Background(), commandContext, client, "devnet", "devnet") }()

	path := filepath.Join(".yacd", "devnet", "endpoints.json")
	require.Eventually(t, func() bool {
		_, err := os.ReadFile(path)

		return err == nil
	}, 2*time.Second, 10*time.Millisecond, "first connect did not write the endpoints file")

	close(dropFirst) // the network is deleted while the forward is down

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	case <-time.After(2 * time.Second):
		t.Fatal("runConnect did not return after the network was deleted")
	}
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "connect should remove stale endpoint state after a dropped forward")
}
