package cli

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"testing"

	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRunCommandLine(t *testing.T) {
	// No t.Parallel: t.Setenv below is incompatible with parallel tests.
	assert.Equal(t, []string{"go", "test", "./..."}, runCommandLine([]string{"go", "test", "./..."}))

	t.Setenv("SHELL", "/usr/bin/fish")
	assert.Equal(t, []string{"/usr/bin/fish"}, runCommandLine(nil))

	t.Setenv("SHELL", "")
	assert.Equal(t, []string{"/bin/sh"}, runCommandLine(nil))
}

// runMock wires a mock kube.Client + ForwardSession for a ready network with
// the three chain endpoints forwarded to fixed local ports.
func runMock(t *testing.T, done <-chan struct{}) *mocks.Client {
	t.Helper()

	network := readyNetwork("devnet")
	session := mocks.NewForwardSession(t)
	session.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	session.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	session.EXPECT().LocalPort(int32(8080)).Return(40003, true)
	session.EXPECT().Done().Return(done)
	session.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(session, nil)
	client.EXPECT().GetSecretValue(mock.Anything, "devnet", "devnet-faucet-auth", faucetAuthTokenKey).Return("faucet-token", nil)

	return client
}

func runRoot(t *testing.T, client *mocks.Client, args ...string) (string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{
		In:                bytes.NewReader(nil),
		Out:               &stdout,
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs(args)

	err := root.ExecuteContext(context.Background())

	return stdout.String(), err
}

func TestRunInjectsYacdEnvironment(t *testing.T) {
	t.Parallel()

	never := make(chan struct{}) // forwards never drop during the command
	client := runMock(t, never)

	stdout, err := runRoot(t, client, "run", "devnet", "--",
		"sh", "-c", `printf '%s|%s' "$YACD_NETWORK" "$YACD_OGMIOS_URL"`)
	require.NoError(t, err)
	assert.Equal(t, "devnet|ws://127.0.0.1:40001", stdout)
}

func TestRunPropagatesChildExitCode(t *testing.T) {
	t.Parallel()

	never := make(chan struct{})
	client := runMock(t, never)

	_, err := runRoot(t, client, "run", "devnet", "--", "sh", "-c", "exit 7")
	require.Error(t, err)

	code, printErr := ResolveExit(err)
	assert.Equal(t, 7, code)
	assert.False(t, printErr, "a child's own exit is silent; it already wrote its output")
}

func TestRunReportsDroppedForward(t *testing.T) {
	t.Parallel()

	dropped := make(chan struct{})
	close(dropped) // the forwards are already gone

	network := readyNetwork("devnet")
	session := mocks.NewForwardSession(t)
	session.EXPECT().LocalPort(int32(1337)).Return(40001, true)
	session.EXPECT().LocalPort(int32(1442)).Return(40002, true)
	session.EXPECT().LocalPort(int32(8080)).Return(40003, true)
	session.EXPECT().Done().Return(dropped)
	session.EXPECT().Err().Return(assert.AnError)
	session.EXPECT().Close().Return(nil)

	client := newKubeMock(t)
	client.EXPECT().GetCardanoNetwork(mock.Anything, "devnet", "devnet").Return(network, nil)
	client.EXPECT().PrimaryPodName(mock.Anything, "devnet", "devnet").Return("devnet-node-abcde", nil)
	client.EXPECT().Forward(mock.Anything, "devnet", "devnet-node-abcde", mock.Anything).Return(session, nil)
	client.EXPECT().GetSecretValue(mock.Anything, "devnet", "devnet-faucet-auth", faucetAuthTokenKey).Return("faucet-token", nil)

	// A long sleep would hang without the drop-driven cancellation.
	_, err := runRoot(t, client, "run", "devnet", "--", "sleep", "30")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lost connection to devnet/devnet")
}

func TestProcessExitCodeForExitedProcess(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "exit 5")
	_ = cmd.Run()
	assert.Equal(t, 5, processExitCode(cmd.ProcessState))
}

func TestProcessExitCodeForSignaledProcess(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	_ = cmd.Wait()

	// A signal-killed child maps to the shell convention 128+signal (130 for SIGINT).
	assert.Equal(t, 128+int(syscall.SIGINT), processExitCode(cmd.ProcessState))
}
