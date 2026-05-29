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

func TestUpDryRunPrintsManifestWithoutKubeClient(t *testing.T) {
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
	root.SetArgs([]string{"up", "devnet", "-f", configPath, "--namespace", "flag-ns", "--dry-run"})

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

func TestUpDryRunDefaultsNamespaceToName(t *testing.T) {
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
	root.SetArgs([]string{"up", "devnet", "-f", configPath, "--dry-run"})

	require.NoError(t, root.ExecuteContext(context.Background()))

	assert.Contains(t, stdout.String(), "name: devnet")
	assert.Contains(t, stdout.String(), "namespace: devnet")
	assert.Contains(t, stderr.String(), "Dry run: rendered CardanoNetwork devnet/devnet; no resources applied.")
}

func TestUpDryRunAllowsMainnetWithWarning(t *testing.T) {
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
	root.SetArgs([]string{"up", "mainnet", "-f", configPath, "--dry-run"})

	require.NoError(t, root.ExecuteContext(context.Background()))

	assert.Contains(t, stdout.String(), "profile: mainnet")
	assert.Contains(t, stdout.String(), "bootstrap:")
	assert.Contains(t, stderr.String(), "Warning: rendering mainnet CardanoNetwork mainnet/mainnet without --allow-mainnet")
	assert.Contains(t, stderr.String(), "Dry run: rendered CardanoNetwork mainnet/mainnet; no resources applied.")
}

func TestUpDryRunDoesNotWarnForNonMainnetPublicProfiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
	}{
		{name: "preview", config: testPublicPreviewDevConfig},
		{
			name:   "preprod",
			config: strings.Replace(testPublicPreviewDevConfig, "profile: preview", "profile: preprod", 1),
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
			root.SetArgs([]string{"up", tc.name, "-f", configPath, "--dry-run"})

			require.NoError(t, root.ExecuteContext(context.Background()))
			assert.NotContains(t, stderr.String(), "Warning: rendering mainnet")
		})
	}
}

func TestUpRequiresExactlyOneName(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed with invalid args")
		},
	})
	root.SetArgs([]string{"up", "-f", configPath, "--dry-run"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

func TestUpRejectsInvalidName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  string
	}{
		{name: "uppercase", arg: "Devnet"},
		{name: "underscore", arg: "dev_net"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			configPath := writeTempConfig(t, testDevConfig)
			root := NewRootCommand(Options{
				Viper: viper.New(),
				KubeClientFactory: func(kube.Config) (kube.Client, error) {
					return nil, fmt.Errorf("kube client should not be constructed with invalid NAME")
				},
			})
			root.SetArgs([]string{"up", tc.arg, "-f", configPath, "--dry-run"})

			err := root.ExecuteContext(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid NAME")
		})
	}
}

func TestUpRejectsInvalidWaitTimeoutBeforeApply(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed with invalid timeout")
		},
	})
	root.SetArgs([]string{"up", "devnet", "-f", configPath, "--wait", "--timeout", "0s"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--timeout must be greater than 0 when --wait is set")
}

func TestUpEnsuresNamespaceAppliesAndWaits(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	var stderr bytes.Buffer

	client := newKubeMock(t)
	var applied *yacdv1alpha1.CardanoNetwork
	client.EXPECT().EnsureNamespace(mock.Anything, "devnet").Return(nil)
	client.EXPECT().
		ApplyCardanoNetwork(mock.Anything, mock.AnythingOfType("*v1alpha1.CardanoNetwork")).
		Run(func(_ context.Context, network *yacdv1alpha1.CardanoNetwork) {
			applied = network.DeepCopy()
		}).
		Return(nil)
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "devnet", "devnet").
		Return(readyNetwork("devnet"), nil)

	root := NewRootCommand(Options{
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"up", "devnet", "-f", configPath})

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NotNil(t, applied, "CardanoNetwork was not applied")
	assert.Equal(t, "devnet", applied.Name)
	assert.Equal(t, "devnet", applied.Namespace)
	assert.Contains(t, stderr.String(), "CardanoNetwork devnet/devnet is ready.")
}

func TestUpUsesNamespaceFlagOverride(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	var stderr bytes.Buffer

	client := newKubeMock(t)
	var applied *yacdv1alpha1.CardanoNetwork
	client.EXPECT().EnsureNamespace(mock.Anything, "flag-ns").Return(nil)
	client.EXPECT().
		ApplyCardanoNetwork(mock.Anything, mock.AnythingOfType("*v1alpha1.CardanoNetwork")).
		Run(func(_ context.Context, network *yacdv1alpha1.CardanoNetwork) {
			applied = network.DeepCopy()
		}).
		Return(nil)
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "flag-ns", "devnet").
		Return(readyNetwork("flag-ns"), nil)

	root := NewRootCommand(Options{
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"up", "devnet", "-f", configPath, "-n", "flag-ns"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NotNil(t, applied, "CardanoNetwork was not applied")
	assert.Equal(t, "devnet", applied.Name)
	assert.Equal(t, "flag-ns", applied.Namespace)
	assert.Contains(t, stderr.String(), "CardanoNetwork flag-ns/devnet is ready.")
}

func TestUpWithoutWaitAppliesOnly(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testDevConfig)
	var stderr bytes.Buffer

	// No GetCardanoNetwork expectation: with --wait=false the apply path must
	// not poll for readiness. mockery fails the test if it is invoked.
	client := newKubeMock(t)
	client.EXPECT().EnsureNamespace(mock.Anything, "devnet").Return(nil)
	client.EXPECT().
		ApplyCardanoNetwork(mock.Anything, mock.AnythingOfType("*v1alpha1.CardanoNetwork")).
		Return(nil)

	root := NewRootCommand(Options{
		Err:               &stderr,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"up", "devnet", "-f", configPath, "--wait=false"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Contains(t, stderr.String(), "Applied CardanoNetwork devnet/devnet.")
	assert.NotContains(t, stderr.String(), "is ready.")
}

func TestUpRejectsMainnetApplyWithoutAllowFlag(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testPublicMainnetDevConfig)
	root := NewRootCommand(Options{
		Viper: viper.New(),
		KubeClientFactory: func(kube.Config) (kube.Client, error) {
			return nil, fmt.Errorf("kube client should not be constructed when --allow-mainnet is missing")
		},
	})
	root.SetArgs([]string{"up", "mainnet", "-f", configPath})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mainnet deployments require --allow-mainnet")
}

func TestUpAppliesMainnetWithAllowFlag(t *testing.T) {
	t.Parallel()

	configPath := writeTempConfig(t, testPublicMainnetDevConfig)
	client := newKubeMock(t)
	var applied *yacdv1alpha1.CardanoNetwork
	client.EXPECT().EnsureNamespace(mock.Anything, "mainnet").Return(nil)
	client.EXPECT().
		ApplyCardanoNetwork(mock.Anything, mock.AnythingOfType("*v1alpha1.CardanoNetwork")).
		Run(func(_ context.Context, network *yacdv1alpha1.CardanoNetwork) {
			applied = network.DeepCopy()
		}).
		Return(nil)
	client.EXPECT().
		GetCardanoNetwork(mock.Anything, "mainnet", "mainnet").
		Return(readyNetwork("mainnet"), nil)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"up", "mainnet", "-f", configPath, "--allow-mainnet"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NotNil(t, applied)
	require.NotNil(t, applied.Spec.Public)
	assert.Equal(t, yacdv1alpha1.PublicNetworkProfileMainnet, applied.Spec.Public.Profile)
	require.NotNil(t, applied.Spec.Public.Bootstrap)
	require.NotNil(t, applied.Spec.Public.Bootstrap.Mithril)
}
