package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestDeployDryRunPrintsManifestWithoutKubeClient(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{
		Out:   &stdout,
		Err:   &stderr,
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed for dry run")
		},
	})
	root.SetArgs([]string{"deploy", "-f", configPath, "--namespace", "flag-ns", "--dry-run"})

	require.NoError(t, root.ExecuteContext(context.Background()))

	for _, want := range []string{
		"apiVersion: yacd.meigma.io/v1alpha1",
		"kind: CardanoNetwork",
		"name: devnet",
		"namespace: flag-ns",
	} {
		assert.Contains(t, stdout.String(), want)
	}
	assert.Contains(t, stderr.String(), "Dry run: rendered CardanoNetwork flag-ns/devnet; no resources applied.")
}

func TestDeployDryRunAllowsMainnetWithWarning(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testPublicMainnetDevConfig)
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(Options{
		Out:   &stdout,
		Err:   &stderr,
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed for dry run")
		},
	})
	root.SetArgs([]string{"deploy", "-f", configPath, "--dry-run"})

	require.NoError(t, root.ExecuteContext(context.Background()))

	assert.Contains(t, stdout.String(), "profile: mainnet")
	assert.Contains(t, stdout.String(), "bootstrap:")
	assert.Contains(t, stderr.String(), "Warning: rendering mainnet CardanoNetwork config-ns/mainnet without --allow-mainnet")
	assert.Contains(t, stderr.String(), "Dry run: rendered CardanoNetwork config-ns/mainnet; no resources applied.")
}

func TestDeployDryRunDoesNotWarnForNonMainnetPublicProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
	}{
		{name: "preview", config: testPublicPreviewDevConfig},
		{
			name: "preprod",
			config: strings.Replace(
				strings.Replace(testPublicPreviewDevConfig, "profile: preview", "profile: preprod", 1),
				"name: preview", "name: preprod", 1,
			),
		},
		{name: "custom", config: testPublicCustomDevConfig},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			configPath := writeTempConfig(t, tc.config)
			var stderr bytes.Buffer
			root := NewRootCommand(Options{
				Err:   &stderr,
				Viper: viper.New(),
				KubeClientFactory: func(kube.Config) (kube.Client, error) {
					return nil, fmt.Errorf("kube client should not be constructed for dry run")
				},
			})
			root.SetArgs([]string{"deploy", "-f", configPath, "--dry-run"})

			require.NoError(t, root.ExecuteContext(context.Background()))
			assert.NotContains(t, stderr.String(), "Warning: rendering mainnet")
		})
	}
}

func TestDeployRejectsUnexpectedArgs(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed with invalid args")
		},
	})
	root.SetArgs([]string{"deploy", "unexpected", "-f", configPath, "--dry-run"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown command "unexpected"`)
}

func TestDeployDryRunUsesKubeDefaultNamespace(t *testing.T) {
	t.Setenv("YACD_NAMESPACE", "")

	configPath := writeTempConfig(t, strings.Replace(testDevConfig, "  namespace: config-ns\n", "", 1))
	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:   &stdout,
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed for dry run")
		},
		KubeNamespaceResolver: func(kube.Config) (string, error) {
			return "kube-ns", nil
		},
	})
	root.SetArgs([]string{"deploy", "-f", configPath, "--dry-run"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Contains(t, stdout.String(), "namespace: kube-ns")
}

func TestDeployRejectsInvalidWaitTimeoutBeforeApply(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed with invalid timeout")
		},
	})
	root.SetArgs([]string{"deploy", "-f", configPath, "--wait", "--timeout", "0s"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--timeout must be greater than 0 when --wait is set")
}

func TestDeployAppliesAndWaits(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	var stderr bytes.Buffer

	client := newKubeMock(t)
	var applied *yacdv1alpha1.CardanoNetwork
	client.EXPECT().DefaultNamespace().Return("default-ns").Maybe()
	client.EXPECT().
		ApplyCardanoNetwork(mock.Anything, mock.AnythingOfType("*v1alpha1.CardanoNetwork")).
		Run(func(_ context.Context, network *yacdv1alpha1.CardanoNetwork) {
			applied = network.DeepCopy()
		}).
		Return(nil)
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "config-ns", "devnet").
		Return(readyNetwork("config-ns"), nil)

	root := NewRootCommand(Options{
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"deploy", "-f", configPath, "--wait"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NotNil(t, applied, "CardanoNetwork was not applied")
	assert.Equal(t, "config-ns", applied.Namespace)
	assert.Contains(t, stderr.String(), "CardanoNetwork config-ns/devnet is ready.")
}

func TestDeployRejectsMainnetApplyWithoutAllowFlag(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testPublicMainnetDevConfig)
	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed when --allow-mainnet is missing")
		},
	})
	root.SetArgs([]string{"deploy", "-f", configPath})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mainnet deployments require --allow-mainnet")
}

func TestDeployAppliesMainnetWithAllowFlag(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testPublicMainnetDevConfig)
	client := newKubeMock(t)
	var applied *yacdv1alpha1.CardanoNetwork
	client.EXPECT().
		ApplyCardanoNetwork(mock.Anything, mock.AnythingOfType("*v1alpha1.CardanoNetwork")).
		Run(func(_ context.Context, network *yacdv1alpha1.CardanoNetwork) {
			applied = network.DeepCopy()
		}).
		Return(nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"deploy", "-f", configPath, "--allow-mainnet"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NotNil(t, applied)
	require.NotNil(t, applied.Spec.Public)
	assert.Equal(t, yacdv1alpha1.PublicNetworkProfileMainnet, applied.Spec.Public.Profile)
	require.NotNil(t, applied.Spec.Public.Bootstrap)
	require.NotNil(t, applied.Spec.Public.Bootstrap.Mithril)
}
