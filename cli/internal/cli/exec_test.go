package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	utilexec "k8s.io/client-go/util/exec"
)

func TestWrapExecCommand(t *testing.T) {
	t.Parallel()

	got := wrapExecCommand([]string{"A=1", "B=2"}, []string{"cardano-cli", "query", "tip"})
	assert.Equal(t, []string{"env", "A=1", "B=2", "cardano-cli", "query", "tip"}, got)
}

func TestExecRunsInPodWithArgvOnlyEnv(t *testing.T) {
	t.Parallel()

	network := readyNetwork("devnet")
	var captured kube.ExecRequest

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Exec(mock.Anything, mock.Anything).
		Run(func(_ context.Context, req kube.ExecRequest) { captured = req }).
		Return(nil)

	_, err := runRoot(t, client, "exec", "devnet", "--", "cardano-cli", "query", "tip")
	require.NoError(t, err)

	assert.Equal(t, "devnet", captured.Namespace)
	assert.Equal(t, "devnet-node-abcde", captured.PodName)
	assert.Equal(t, "cardano-node", captured.Container)
	assert.False(t, captured.TTY, "non-terminal stdin must not request a TTY")
	assert.Equal(t, []string{
		"env",
		"YACD_NETWORK=devnet",
		"YACD_NAMESPACE=devnet",
		"YACD_NETWORK_MAGIC=42",
		"YACD_OGMIOS_URL=ws://devnet-ogmios.devnet.svc.cluster.local:1337",
		"YACD_KUPO_URL=http://devnet-kupo.devnet.svc.cluster.local:1442",
		"YACD_FAUCET_URL=http://devnet-faucet.devnet.svc.cluster.local:8080",
		"CARDANO_NODE_SOCKET_PATH=/ipc/node.socket",
		"cardano-cli", "query", "tip",
	}, captured.Command)
	for _, arg := range captured.Command {
		assert.NotContains(t, arg, "YACD_FAUCET_TOKEN", "the in-pod argv must never carry the faucet token")
	}
}

func TestExecPropagatesRemoteExitCode(t *testing.T) {
	t.Parallel()

	network := readyNetwork("devnet")
	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Exec(mock.Anything, mock.Anything).Return(utilexec.CodeExitError{Err: errors.New("query failed"), Code: 5})

	_, err := runRoot(t, client, "exec", "devnet", "--", "cardano-cli", "query", "tip")
	require.Error(t, err)

	code, printErr := ResolveExit(err)
	assert.Equal(t, 5, code)
	assert.False(t, printErr, "a remote command's own exit is silent")
}

func TestExecRequiresCommand(t *testing.T) {
	t.Parallel()

	// No kube expectations: the command-required check precedes any client use.
	client := newKubeMock(t)
	_, err := runRoot(t, client, "exec", "devnet")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a command is required")
}

func TestExecExitError(t *testing.T) {
	t.Parallel()

	code, printErr := ResolveExit(execExitError(utilexec.CodeExitError{Err: errors.New("boom"), Code: 3}))
	assert.Equal(t, 3, code)
	assert.False(t, printErr)

	// A non-exit transport error passes through and is printed as a generic failure.
	code, printErr = ResolveExit(execExitError(errors.New("connection reset")))
	assert.Equal(t, 1, code)
	assert.True(t, printErr)
}
