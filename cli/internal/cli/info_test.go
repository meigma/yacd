package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestInfoReadsGlobalKubeEnvironment(t *testing.T) {
	t.Setenv("YACD_KUBECONFIG", "/tmp/yacd-kubeconfig")
	t.Setenv("YACD_KUBE_CONTEXT", "dev-context")
	t.Setenv("YACD_NAMESPACE", "env-ns")

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("default-ns").Maybe()
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "env-ns", "devnet").
		Return(readyNetwork("env-ns"), nil)
	client.EXPECT().
		GetSecret(mock.Anything, "env-ns", "devnet-wallet-faucet").
		Return(faucetWalletSecret("addr_test1info"), nil)

	var capturedConfig kube.Config
	factory := func(config kube.Config) (kube.Client, error) {
		capturedConfig = config
		return client, nil
	}

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: factory,
	})
	root.SetArgs([]string{"info", "devnet", "-o", "json"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, "dev-context", capturedConfig.Context)
	assert.Equal(t, "/tmp/yacd-kubeconfig", capturedConfig.Kubeconfig)
	for _, want := range []string{
		`"name": "devnet"`,
		`"namespace": "env-ns"`,
		`"type": "Ready"`,
		`"url": "ws://devnet-ogmios.env-ns.svc.cluster.local:1337"`,
		`"url": "http://devnet-kupo.env-ns.svc.cluster.local:1442"`,
	} {
		assert.Contains(t, stdout.String(), want)
	}
}

func TestInfoDefaultsNamespaceToName(t *testing.T) {
	t.Setenv("YACD_NAMESPACE", "")

	// resolveIdentity defaults the namespace to NAME, so info looks the
	// network up in the devnet namespace rather than the kubeconfig default.
	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("default-ns").Maybe()
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "devnet", "devnet").
		Return(readyNetwork("devnet"), nil)
	client.EXPECT().
		GetSecret(mock.Anything, "devnet", "devnet-wallet-faucet").
		Return(faucetWalletSecret("addr_test1info"), nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"info", "devnet", "-o", "json"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Contains(t, stdout.String(), `"namespace": "devnet"`)
}

func TestInfoOmitsWalletWhenFaucetWalletAbsent(t *testing.T) {
	t.Setenv("YACD_NAMESPACE", "")

	// An operator that predates the genesis-funded faucet wallet has no such
	// Secret; info must still render, simply without a wallet section.
	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("default-ns").Maybe()
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "devnet", "devnet").
		Return(readyNetwork("devnet"), nil)
	client.EXPECT().
		GetSecret(mock.Anything, "devnet", "devnet-wallet-faucet").
		Return(nil, errors.New(`secrets "devnet-wallet-faucet" not found`))

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"info", "devnet", "-o", "json"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.NotContains(t, stdout.String(), `"wallet"`)
}
