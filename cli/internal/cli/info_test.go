package cli

import (
	"bytes"
	"context"
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
	root.SetArgs([]string{"info", "devnet", "--json"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, "dev-context", capturedConfig.Context)
	assert.Equal(t, "/tmp/yacd-kubeconfig", capturedConfig.Kubeconfig)
	for _, want := range []string{
		`"name": "devnet"`,
		`"namespace": "env-ns"`,
		`"type": "Ready"`,
		`"url": "ws://devnet-ogmios.env-ns.svc.cluster.local:1337"`,
		`"url": "http://devnet-kupo.env-ns.svc.cluster.local:1442"`,
		`"url": "http://devnet-faucet.env-ns.svc.cluster.local:8080"`,
		`"authSecretName": "devnet-faucet-auth"`,
	} {
		assert.Contains(t, stdout.String(), want)
	}
}
