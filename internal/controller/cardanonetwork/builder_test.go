package cardanonetwork

import (
	"strings"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/meigma/yacd/internal/cardano/publicnet"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	ctrlartifacts "github.com/meigma/yacd/internal/ctrlkit/artifacts"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	defaultLocalSlotLength = 100 * time.Millisecond
	testStorageClassName   = "fast"
)

// TestPrimaryWorkloadBuilderLocalnetSpecMapsSupportedLocalInput verifies the
// CRD-to-localnet adapter shape now owned by the primary workload builder.
func TestPrimaryWorkloadBuilderLocalnetSpecMapsSupportedLocalInput(t *testing.T) {
	network := localCardanoNetwork("maps-supported-local-input")

	got, err := newTestPrimaryWorkloadBuilder(t).localnetSpec(network)
	require.NoError(t, err)

	assert.Equal(t, localnet.Spec{
		NetworkMagic: 42,
		PoolCount:    1,
		Timing: localnet.Timing{
			SlotLength:  defaultLocalSlotLength,
			EpochLength: 500,
		},
		Paths: localnet.Paths{
			StateDir: "/state",
			EnvDir:   "/state/env",
		},
		Tool: localnet.Tool{
			Version: "11.0.1",
		},
	}, got)
}

// TestPrimaryWorkloadBuilderRejectsUnsupportedInput verifies unsupported API
// shapes fail before producing partial primary workload resources.
func TestPrimaryWorkloadBuilderRejectsUnsupportedInput(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*yacdv1alpha1.CardanoNetwork)
		wantErr string
	}{
		{
			name: "blank node version",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Node.Version = " "
			},
			wantErr: "node version is required",
		},
		{
			name: "blank explicit node image",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				image := " "
				network.Spec.Node.Image = &image
			},
			wantErr: "node image override must not be blank",
		},
		{
			name: "invalid node port",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Node.Port = 0
			},
			wantErr: "node port must be between 1 and 65535",
		},
		{
			name: "blank ogmios image",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Ogmios: &yacdv1alpha1.OgmiosSpec{
						Enabled: true,
						Image:   " ",
						Port:    defaultOgmiosPort,
					},
				}
			},
			wantErr: "ogmios image is required",
		},
		{
			name: "invalid ogmios port",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Ogmios: &yacdv1alpha1.OgmiosSpec{
						Enabled: true,
						Image:   defaultOgmiosImage,
						Port:    65536,
					},
				}
			},
			wantErr: "ogmios port must be between 1 and 65535",
		},
		{
			name: "blank kupo image",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Kupo: &yacdv1alpha1.KupoSpec{
						Enabled: true,
						Image:   " ",
						Port:    defaultKupoPort,
					},
				}
			},
			wantErr: "kupo image is required",
		},
		{
			name: "invalid kupo port",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Kupo: &yacdv1alpha1.KupoSpec{
						Enabled: true,
						Image:   defaultKupoImage,
						Port:    65536,
					},
				}
			},
			wantErr: "kupo port must be between 1 and 65535",
		},
		{
			name: "explicit kupo with ogmios disabled",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Ogmios: &yacdv1alpha1.OgmiosSpec{
						Enabled: false,
					},
					Kupo: &yacdv1alpha1.KupoSpec{
						Enabled: true,
						Image:   defaultKupoImage,
						Port:    defaultKupoPort,
					},
				}
			},
			wantErr: "kupo requires ogmios to be enabled",
		},
		{
			name: "unsupported kupo image",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Kupo: &yacdv1alpha1.KupoSpec{
						Enabled: true,
						Image:   "cardanosolutions/kupo:v2.10.0",
						Port:    defaultKupoPort,
					},
				}
			},
			wantErr: `kupo image "cardanosolutions/kupo:v2.10.0" is not supported; supported image: cardanosolutions/kupo:v2.11.0`,
		},
		{
			name: "untagged kupo image",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Kupo: &yacdv1alpha1.KupoSpec{
						Enabled: true,
						Image:   "cardanosolutions/kupo",
						Port:    defaultKupoPort,
					},
				}
			},
			wantErr: `kupo image "cardanosolutions/kupo" is not supported; supported image: cardanosolutions/kupo:v2.11.0`,
		},
		{
			name: "ogmios port conflicts with node port",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Ogmios: &yacdv1alpha1.OgmiosSpec{
						Enabled: true,
						Image:   defaultOgmiosImage,
						Port:    network.Spec.Node.Port,
					},
				}
			},
			wantErr: "ogmios port 3001 conflicts with node-to-node port",
		},
		{
			name: "kupo port conflicts with node port",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Kupo: &yacdv1alpha1.KupoSpec{
						Enabled: true,
						Image:   defaultKupoImage,
						Port:    network.Spec.Node.Port,
					},
				}
			},
			wantErr: "kupo port 3001 conflicts with node-to-node port",
		},
		{
			name: "kupo port conflicts with ogmios port",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Kupo: &yacdv1alpha1.KupoSpec{
						Enabled: true,
						Image:   defaultKupoImage,
						Port:    defaultOgmiosPort,
					},
				}
			},
			wantErr: "kupo port 1337 conflicts with ogmios port",
		},
		{
			name: "blank faucet image",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				image := " "
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Faucet: &yacdv1alpha1.FaucetSpec{
						Enabled:          true,
						Image:            &image,
						Port:             defaultFaucetPort,
						DefaultSource:    defaultFaucetSource,
						MinTopUpLovelace: defaultFaucetMinLovelace,
						MaxTopUpLovelace: defaultFaucetMaxLovelace,
					},
				}
			},
			wantErr: "faucet image is required",
		},
		{
			name: "faucet image from different repository",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				image := "example.com/yacd-faucet:test"
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Faucet: &yacdv1alpha1.FaucetSpec{
						Enabled:          true,
						Image:            &image,
						Port:             defaultFaucetPort,
						DefaultSource:    defaultFaucetSource,
						MinTopUpLovelace: defaultFaucetMinLovelace,
						MaxTopUpLovelace: defaultFaucetMaxLovelace,
					},
				}
			},
			wantErr: `faucet image repository must match the configured default faucet image repository "ghcr.io/meigma/yacd/faucet"`,
		},
		{
			name: "invalid faucet port",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Faucet: &yacdv1alpha1.FaucetSpec{
						Enabled:          true,
						Port:             65536,
						DefaultSource:    defaultFaucetSource,
						MinTopUpLovelace: defaultFaucetMinLovelace,
						MaxTopUpLovelace: defaultFaucetMaxLovelace,
					},
				}
			},
			wantErr: "faucet port must be between 1 and 65535",
		},
		{
			name: "invalid faucet default source",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Faucet: &yacdv1alpha1.FaucetSpec{
						Enabled:          true,
						Port:             defaultFaucetPort,
						DefaultSource:    "../utxo1",
						MinTopUpLovelace: defaultFaucetMinLovelace,
						MaxTopUpLovelace: defaultFaucetMaxLovelace,
					},
				}
			},
			wantErr: "faucet defaultSource must use the utxoN source name format",
		},
		{
			name: "faucet min above max",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Faucet: &yacdv1alpha1.FaucetSpec{
						Enabled:          true,
						Port:             defaultFaucetPort,
						DefaultSource:    defaultFaucetSource,
						MinTopUpLovelace: 3_000_000,
						MaxTopUpLovelace: 2_000_000,
					},
				}
			},
			wantErr: "faucet minTopUpLovelace must not exceed maxTopUpLovelace",
		},
		{
			name: "explicit faucet with kupo disabled",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Kupo: &yacdv1alpha1.KupoSpec{
						Enabled: false,
					},
					Faucet: &yacdv1alpha1.FaucetSpec{
						Enabled:          true,
						Port:             defaultFaucetPort,
						DefaultSource:    defaultFaucetSource,
						MinTopUpLovelace: defaultFaucetMinLovelace,
						MaxTopUpLovelace: defaultFaucetMaxLovelace,
					},
				}
			},
			wantErr: "faucet requires kupo to be enabled",
		},
		{
			name: "faucet port conflicts with kupo port",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Faucet: &yacdv1alpha1.FaucetSpec{
						Enabled:          true,
						Port:             defaultKupoPort,
						DefaultSource:    defaultFaucetSource,
						MinTopUpLovelace: defaultFaucetMinLovelace,
						MaxTopUpLovelace: defaultFaucetMaxLovelace,
					},
				}
			},
			wantErr: "faucet port 1442 conflicts with kupo port",
		},
		{
			name: "unsupported ogmios image tag",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Ogmios: &yacdv1alpha1.OgmiosSpec{
						Enabled: true,
						Image:   "cardanosolutions/ogmios:latest",
						Port:    defaultOgmiosPort,
					},
				}
			},
			wantErr: `ogmios image tag "latest" is not a supported release tag`,
		},
		{
			name: "unsupported ogmios node compatibility",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Node.Version = "10.1.4"
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Ogmios: &yacdv1alpha1.OgmiosSpec{
						Enabled: true,
						Image:   defaultOgmiosImage,
						Port:    defaultOgmiosPort,
					},
				}
			},
			wantErr: "ogmios v6.14.* is not supported with cardano-node 10.1.4",
		},
		{
			name: "missing public spec",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Mode = yacdv1alpha1.CardanoNetworkModePublic
				network.Spec.Local = nil
			},
			wantErr: "public spec is required",
		},
		{
			name: "public mode with local spec",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Mode = yacdv1alpha1.CardanoNetworkModePublic
				network.Spec.Public = &yacdv1alpha1.PublicNetworkSpec{
					Profile: yacdv1alpha1.PublicNetworkProfilePreview,
				}
			},
			wantErr: "local spec is not supported with public mode",
		},
		{
			name: "public custom config source without resolved bundle",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Mode = yacdv1alpha1.CardanoNetworkModePublic
				network.Spec.Local = nil
				network.Spec.Public = &yacdv1alpha1.PublicNetworkSpec{
					Profile: yacdv1alpha1.PublicNetworkProfileCustom,
					ConfigSource: &yacdv1alpha1.NetworkConfigSource{
						ConfigMapRef: &corev1.LocalObjectReference{Name: "custom"},
					},
				}
			},
			wantErr: "public custom profile source has not been resolved",
		},
		{
			name: "public curated config source",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Mode = yacdv1alpha1.CardanoNetworkModePublic
				network.Spec.Local = nil
				network.Spec.Public = &yacdv1alpha1.PublicNetworkSpec{
					Profile: yacdv1alpha1.PublicNetworkProfilePreview,
					ConfigSource: &yacdv1alpha1.NetworkConfigSource{
						ConfigMapRef: &corev1.LocalObjectReference{Name: "custom"},
					},
				}
			},
			wantErr: "public configSource is supported only for custom profiles",
		},
		{
			name: "public mainnet without mithril bootstrap",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Mode = yacdv1alpha1.CardanoNetworkModePublic
				network.Spec.Local = nil
				network.Spec.Public = &yacdv1alpha1.PublicNetworkSpec{
					Profile: yacdv1alpha1.PublicNetworkProfileMainnet,
				}
			},
			wantErr: "public mainnet profile requires spec.public.bootstrap.mithril",
		},
		{
			name: "public preview with bootstrap",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Mode = yacdv1alpha1.CardanoNetworkModePublic
				network.Spec.Local = nil
				network.Spec.Public = &yacdv1alpha1.PublicNetworkSpec{
					Profile: yacdv1alpha1.PublicNetworkProfilePreview,
					Bootstrap: &yacdv1alpha1.PublicNetworkBootstrapSpec{
						Mithril: &yacdv1alpha1.MithrilBootstrapSpec{},
					},
				}
			},
			wantErr: "public bootstrap is supported only for mainnet",
		},
		{
			name: "public mainnet storage below minimum",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Mode = yacdv1alpha1.CardanoNetworkModePublic
				network.Spec.Local = nil
				network.Spec.Node.Storage = &yacdv1alpha1.NodeStorageSpec{
					Size: resource.MustParse("299Gi"),
				}
				network.Spec.Public = &yacdv1alpha1.PublicNetworkSpec{
					Profile: yacdv1alpha1.PublicNetworkProfileMainnet,
					Bootstrap: &yacdv1alpha1.PublicNetworkBootstrapSpec{
						Mithril: &yacdv1alpha1.MithrilBootstrapSpec{},
					},
				}
			},
			wantErr: "public mainnet node storage must be at least 300Gi",
		},
		{
			name: "public faucet",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				*network = *publicPreviewCardanoNetwork("public-faucet")
				enableFaucet(network)
			},
			wantErr: "faucet is not supported for public networks",
		},
		{
			name: "public kupo",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				*network = *publicPreviewCardanoNetwork("public-kupo")
				network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
					Kupo: &yacdv1alpha1.KupoSpec{
						Enabled: true,
						Image:   defaultKupoImage,
						Port:    defaultKupoPort,
					},
				}
			},
			wantErr: "kupo is not supported for public networks",
		},
		{
			name: "missing local spec",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local = nil
			},
			wantErr: "local spec is required",
		},
		{
			name: "public spec with local mode",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Public = &yacdv1alpha1.PublicNetworkSpec{
					Profile: yacdv1alpha1.PublicNetworkProfilePreview,
				}
			},
			wantErr: "public spec is not supported with local mode",
		},
		{
			name: "babbage era",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local.Era = yacdv1alpha1.CardanoEraBabbage
			},
			wantErr: `local era "babbage" is not supported`,
		},
		{
			name: "genesis tuning",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local.Genesis = &yacdv1alpha1.LocalGenesisSpec{
					Profile: yacdv1alpha1.GenesisProfileDefault,
				}
			},
			wantErr: "local genesis tuning is not supported",
		},
		{
			name: "pool count above one",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local.Topology.Pools.Count = 2
			},
			wantErr: "local pool count 2 is not supported",
		},
		{
			name: "pool defaults",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Local.Topology.Pools.Defaults = &yacdv1alpha1.LocalPoolDefaultsSpec{}
			},
			wantErr: "local pool defaults are not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network := localCardanoNetwork(tt.name)
			tt.mutate(network)

			_, err := newTestPrimaryWorkloadBuilder(t).Build(network)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestPrimaryWorkloadBuilderRejectsNilInputAndScheme(t *testing.T) {
	_, err := newTestPrimaryWorkloadBuilder(t).Build(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cardanonetwork is required")

	_, err = (primaryWorkloadBuilder{}).Build(localCardanoNetwork("missing-scheme"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme is required")
}

// TestPrimaryWorkloadBuilderBuildsPrimaryWorkload verifies the initial
// singleton primary node workload shape with the default Ogmios sidecar.
func TestPrimaryWorkloadBuilderBuildsPrimaryWorkload(t *testing.T) {
	network := localCardanoNetwork("devnet")
	enableFaucet(network)

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)
	deployment := resources.Deployment
	networkArtifactsConfigMap := resources.NetworkArtifactsConfigMap
	artifactPublisherServiceAccount := resources.ArtifactPublisherServiceAccount
	artifactPublisherRole := resources.ArtifactPublisherRole
	artifactPublisherRoleBinding := resources.ArtifactPublisherRoleBinding
	persistentVolumeClaim := resources.PersistentVolumeClaim
	service := resources.Service
	ogmiosService := resources.OgmiosService
	kupoService := resources.KupoService
	faucetService := resources.FaucetService
	artifactsService := resources.ArtifactsService
	faucetAuthSecret := resources.FaucetAuthSecret
	require.NotNil(t, ogmiosService)
	require.NotNil(t, kupoService)
	require.NotNil(t, faucetService)
	require.NotNil(t, artifactsService)
	require.NotNil(t, faucetAuthSecret)
	require.NotNil(t, networkArtifactsConfigMap)
	require.NotNil(t, artifactPublisherServiceAccount)
	require.NotNil(t, artifactPublisherRole)
	require.NotNil(t, artifactPublisherRoleBinding)

	assert.Equal(t, "devnet-node", deployment.Name)
	assert.Equal(t, "default", deployment.Namespace)
	require.NotNil(t, deployment.Spec.Replicas)
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)
	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type)

	controller := metav1.GetControllerOf(deployment)
	require.NotNil(t, controller)
	assert.Equal(t, "devnet", controller.Name)
	assert.Equal(t, "CardanoNetwork", controller.Kind)

	artifactsController := metav1.GetControllerOf(networkArtifactsConfigMap)
	require.NotNil(t, artifactsController)
	assert.Equal(t, "devnet", artifactsController.Name)
	assert.Equal(t, "CardanoNetwork", artifactsController.Kind)
	artifactServiceAccountController := metav1.GetControllerOf(artifactPublisherServiceAccount)
	require.NotNil(t, artifactServiceAccountController)
	assert.Equal(t, "devnet", artifactServiceAccountController.Name)
	assert.Equal(t, "CardanoNetwork", artifactServiceAccountController.Kind)
	artifactRoleController := metav1.GetControllerOf(artifactPublisherRole)
	require.NotNil(t, artifactRoleController)
	assert.Equal(t, "devnet", artifactRoleController.Name)
	assert.Equal(t, "CardanoNetwork", artifactRoleController.Kind)
	artifactRoleBindingController := metav1.GetControllerOf(artifactPublisherRoleBinding)
	require.NotNil(t, artifactRoleBindingController)
	assert.Equal(t, "devnet", artifactRoleBindingController.Name)
	assert.Equal(t, "CardanoNetwork", artifactRoleBindingController.Kind)

	pvcController := metav1.GetControllerOf(persistentVolumeClaim)
	require.NotNil(t, pvcController)
	assert.Equal(t, "devnet", pvcController.Name)
	assert.Equal(t, "CardanoNetwork", pvcController.Kind)

	serviceController := metav1.GetControllerOf(service)
	require.NotNil(t, serviceController)
	assert.Equal(t, "devnet", serviceController.Name)
	assert.Equal(t, "CardanoNetwork", serviceController.Kind)
	ogmiosServiceController := metav1.GetControllerOf(ogmiosService)
	require.NotNil(t, ogmiosServiceController)
	assert.Equal(t, "devnet", ogmiosServiceController.Name)
	assert.Equal(t, "CardanoNetwork", ogmiosServiceController.Kind)
	kupoServiceController := metav1.GetControllerOf(kupoService)
	require.NotNil(t, kupoServiceController)
	assert.Equal(t, "devnet", kupoServiceController.Name)
	assert.Equal(t, "CardanoNetwork", kupoServiceController.Kind)
	faucetServiceController := metav1.GetControllerOf(faucetService)
	require.NotNil(t, faucetServiceController)
	assert.Equal(t, "devnet", faucetServiceController.Name)
	assert.Equal(t, "CardanoNetwork", faucetServiceController.Kind)
	artifactsServiceController := metav1.GetControllerOf(artifactsService)
	require.NotNil(t, artifactsServiceController)
	assert.Equal(t, "devnet", artifactsServiceController.Name)
	assert.Equal(t, "CardanoNetwork", artifactsServiceController.Kind)
	faucetSecretController := metav1.GetControllerOf(faucetAuthSecret)
	require.NotNil(t, faucetSecretController)
	assert.Equal(t, "devnet", faucetSecretController.Name)
	assert.Equal(t, "CardanoNetwork", faucetSecretController.Kind)

	expectedSelector := map[string]string{
		labelAppName:        labelPrimaryNodeName,
		labelAppInstance:    "devnet",
		labelAppComponent:   labelPrimaryRole,
		labelCardanoNetwork: "devnet",
		labelCardanoRole:    labelPrimaryRole,
	}
	assert.Equal(t, expectedSelector, deployment.Spec.Selector.MatchLabels)
	assert.Equal(t, expectedSelector, deployment.Spec.Template.Labels)
	assert.Equal(t, "yacd", deployment.Labels[labelAppManagedBy])
	assert.NotEmpty(t, deployment.Spec.Template.Annotations[localnetFingerprintAnno])
	assert.NotEmpty(t, persistentVolumeClaim.Annotations[localnetFingerprintAnno])
	assert.Equal(t,
		deployment.Spec.Template.Annotations[localnetFingerprintAnno],
		persistentVolumeClaim.Annotations[localnetFingerprintAnno],
	)
	assert.NotContains(t, persistentVolumeClaim.Annotations, ctrlannotations.RequestedStorageClass)
	require.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.False(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.Equal(t, "devnet-artifact-publisher", deployment.Spec.Template.Spec.ServiceAccountName)

	require.Len(t, deployment.Spec.Template.Spec.InitContainers, 3)
	initContainer := deployment.Spec.Template.Spec.InitContainers[0]
	assert.Equal(t, localnetCreateEnvInitContainerName, initContainer.Name)
	assert.Equal(t, corev1.TerminationMessagePathDefault, initContainer.TerminationMessagePath)
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state"},
		{Name: artifactPublisherTokenVolumeName, MountPath: artifactPublisherServiceAccountMountDir, ReadOnly: true},
	}, initContainer.VolumeMounts)
	initEnv := envMap(initContainer)
	assert.Equal(t, "devnet-network-artifacts", initEnv[artifactConfigMapNameEnv])
	assert.Equal(t, "devnet", initEnv[artifactNetworkNameEnv])
	assert.Equal(t, "default", initEnv[artifactNetworkNamespaceEnv])
	assert.Equal(t, "local", initEnv[artifactNetworkModeEnv])
	assert.Equal(t, "conway", initEnv[artifactNetworkEraEnv])
	assert.Equal(t, "devnet-node.default.svc.cluster.local", initEnv[artifactNodeToNodeHostEnv])
	assert.Equal(t, "3001", initEnv[artifactNodeToNodePortEnv])
	assert.Equal(t, "tcp://devnet-node.default.svc.cluster.local:3001", initEnv[artifactNodeToNodeURLEnv])

	// The stage init container is ordered after create-env so it can flatten
	// the generated env dir onto the served-artifact PVC subdirectory.
	stageInitContainer := deployment.Spec.Template.Spec.InitContainers[1]
	assert.Equal(t, servedArtifactsInitContainerName, stageInitContainer.Name)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.0", stageInitContainer.Image)
	assert.Equal(t, []string{cardanoToolsCommand}, stageInitContainer.Command)
	assert.Equal(t, []string{
		"stage",
		"--state-dir", "/state/env",
		"--output-dir", "/state/artifacts",
		"--cardano-network-name", "devnet",
		"--cardano-network-namespace", "default",
		"--cardano-network-mode", "local",
		"--cardano-network-era", "conway",
		"--cardano-node-to-node-host", "devnet-node.default.svc.cluster.local",
		"--cardano-node-to-node-port", "3001",
	}, stageInitContainer.Args)
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state"},
	}, stageInitContainer.VolumeMounts)
	assertRestrictedContainerSecurityContext(t, stageInitContainer.SecurityContext)

	addressInitContainer := deployment.Spec.Template.Spec.InitContainers[2]
	assert.Equal(t, faucetSourceAddressInitContainerName, addressInitContainer.Name)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.4", addressInitContainer.Image)
	assert.Equal(t, []string{faucetSourceAddressCommand}, addressInitContainer.Command)
	addressInitArgs := strings.Join(addressInitContainer.Args, " ")
	assert.Contains(t, addressInitArgs, "cardano-cli address build")
	assert.Contains(t, addressInitArgs, "--testnet-magic 42")
	assert.Contains(t, addressInitArgs, "utxo.vkey")
	assert.Contains(t, addressInitArgs, "utxo.addr")
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state"},
	}, addressInitContainer.VolumeMounts)

	require.Len(t, deployment.Spec.Template.Spec.Containers, 5)
	nodeContainer := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, cardanoNodeContainerName, nodeContainer.Name)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.4", nodeContainer.Image)
	assert.Equal(t, []string{"cardano-node"}, nodeContainer.Command)
	assert.Equal(t, corev1.TerminationMessagePathDefault, nodeContainer.TerminationMessagePath)
	assert.Equal(t, []string{
		"run",
		"--config", "/state/env/configuration.yaml",
		"--topology", "/state/env/node-data/node1/topology.json",
		"--database-path", "/state/db",
		"--socket-path", "/ipc/node.socket",
		"--host-addr", "0.0.0.0",
		"--port", "3001",
		"--shelley-kes-key", "/state/env/pools-keys/pool1/kes.skey",
		"--shelley-vrf-key", "/state/env/pools-keys/pool1/vrf.skey",
		"--shelley-operational-certificate", "/state/env/pools-keys/pool1/opcert.cert",
	}, nodeContainer.Args)
	assert.Equal(t, []corev1.ContainerPort{
		{
			Name:          cardanoNodePortName,
			ContainerPort: 3001,
			Protocol:      corev1.ProtocolTCP,
		},
	}, nodeContainer.Ports)
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state"},
		{Name: nodeIPCVolumeName, MountPath: "/ipc"},
	}, nodeContainer.VolumeMounts)

	ogmiosContainer := deployment.Spec.Template.Spec.Containers[1]
	assert.Equal(t, ogmiosContainerName, ogmiosContainer.Name)
	assert.Equal(t, defaultOgmiosImage, ogmiosContainer.Image)
	assert.Equal(t, []string{ogmiosCommand}, ogmiosContainer.Command)
	assert.Equal(t, []string{
		"--node-socket", "/ipc/node.socket",
		"--node-config", "/state/env/configuration.yaml",
		"--host", "0.0.0.0",
		"--port", "1337",
	}, ogmiosContainer.Args)
	assert.Equal(t, []corev1.ContainerPort{
		{
			Name:          ogmiosPortName,
			ContainerPort: defaultOgmiosPort,
			Protocol:      corev1.ProtocolTCP,
		},
	}, ogmiosContainer.Ports)
	require.NotNil(t, ogmiosContainer.ReadinessProbe)
	require.NotNil(t, ogmiosContainer.StartupProbe)
	require.NotNil(t, ogmiosContainer.LivenessProbe)
	assert.Equal(t, []string{ogmiosCommand, "health-check", "--port", "1337"}, ogmiosContainer.ReadinessProbe.Exec.Command)
	assert.Equal(t, []string{ogmiosCommand, "health-check", "--port", "1337"}, ogmiosContainer.StartupProbe.Exec.Command)
	assert.Equal(t, []string{ogmiosCommand, "health-check", "--port", "1337"}, ogmiosContainer.LivenessProbe.Exec.Command)
	assert.Equal(t, int32(3), ogmiosContainer.ReadinessProbe.FailureThreshold)
	assert.Equal(t, int32(60), ogmiosContainer.StartupProbe.FailureThreshold)
	assert.Equal(t, int32(12), ogmiosContainer.LivenessProbe.FailureThreshold)
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state", ReadOnly: true},
		{Name: nodeIPCVolumeName, MountPath: "/ipc"},
	}, ogmiosContainer.VolumeMounts)

	kupoContainer := deployment.Spec.Template.Spec.Containers[2]
	assert.Equal(t, kupoContainerName, kupoContainer.Name)
	assert.Equal(t, defaultKupoImage, kupoContainer.Image)
	assert.Empty(t, kupoContainer.Command)
	assert.Equal(t, []string{
		"--ogmios-host", "127.0.0.1",
		"--ogmios-port", "1337",
		"--since", "origin",
		"--match", "*/*",
		"--prune-utxo",
		"--workdir", "/kupo",
		"--host", "0.0.0.0",
		"--port", "1442",
	}, kupoContainer.Args)
	assert.Equal(t, []corev1.ContainerPort{
		{
			Name:          kupoPortName,
			ContainerPort: defaultKupoPort,
			Protocol:      corev1.ProtocolTCP,
		},
	}, kupoContainer.Ports)
	require.NotNil(t, kupoContainer.ReadinessProbe)
	require.NotNil(t, kupoContainer.StartupProbe)
	require.NotNil(t, kupoContainer.LivenessProbe)
	assert.Equal(t, []string{kupoContainerName, "health-check", "--port", "1442"}, kupoContainer.ReadinessProbe.Exec.Command)
	assert.Equal(t, []string{kupoContainerName, "health-check", "--port", "1442"}, kupoContainer.StartupProbe.Exec.Command)
	assert.Equal(t, []string{kupoContainerName, "health-check", "--port", "1442"}, kupoContainer.LivenessProbe.Exec.Command)
	assert.Equal(t, int32(3), kupoContainer.ReadinessProbe.FailureThreshold)
	assert.Equal(t, int32(60), kupoContainer.StartupProbe.FailureThreshold)
	assert.Equal(t, int32(12), kupoContainer.LivenessProbe.FailureThreshold)
	assert.Equal(t, []corev1.VolumeMount{
		{Name: kupoDBVolumeName, MountPath: "/kupo"},
		{Name: kupoTmpVolumeName, MountPath: "/tmp"},
	}, kupoContainer.VolumeMounts)
	assert.Equal(t, defaultKupoResources(), kupoContainer.Resources)

	faucetContainer := deployment.Spec.Template.Spec.Containers[3]
	assert.Equal(t, faucetContainerName, faucetContainer.Name)
	assert.Equal(t, defaultFaucetImage, faucetContainer.Image)
	assert.Empty(t, faucetContainer.Command)
	assert.Equal(t, []string{
		"--listen-address", "0.0.0.0:8080",
		"--utxo-keys-dir", "/state/env/utxo-keys",
		"--default-source", "utxo1",
		"--ogmios-url", "ws://127.0.0.1:1337",
		"--kupo-url", "http://127.0.0.1:1442",
		"--auth-token-file", "/var/run/yacd-faucet/token",
		"--allow-remote-listen",
		"--min-topup-lovelace", "1000000",
		"--max-topup-lovelace", "10000000000",
	}, faucetContainer.Args)
	assert.Equal(t, []corev1.ContainerPort{
		{
			Name:          faucetPortName,
			ContainerPort: defaultFaucetPort,
			Protocol:      corev1.ProtocolTCP,
		},
	}, faucetContainer.Ports)
	require.NotNil(t, faucetContainer.ReadinessProbe)
	require.NotNil(t, faucetContainer.StartupProbe)
	require.NotNil(t, faucetContainer.LivenessProbe)
	assert.Equal(t, faucetReadinessPath, faucetContainer.ReadinessProbe.HTTPGet.Path)
	assert.Equal(t, faucetHealthPath, faucetContainer.StartupProbe.HTTPGet.Path)
	assert.Equal(t, faucetHealthPath, faucetContainer.LivenessProbe.HTTPGet.Path)
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state/env/utxo-keys", SubPath: "env/utxo-keys", ReadOnly: true},
		{Name: faucetAuthVolumeName, MountPath: "/var/run/yacd-faucet", ReadOnly: true},
	}, faucetContainer.VolumeMounts)

	// The always-on serve sidecar is appended last; it exposes the staged
	// served-artifact directory read-only over HTTP on port 8090.
	serveContainer := deployment.Spec.Template.Spec.Containers[4]
	assert.Equal(t, serveContainerName, serveContainer.Name)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.0", serveContainer.Image)
	assert.Equal(t, []string{cardanoToolsCommand}, serveContainer.Command)
	assert.Equal(t, []string{
		"serve",
		"--artifacts-dir", "/state/artifacts",
		"--listen", ":8090",
	}, serveContainer.Args)
	assert.Equal(t, []corev1.ContainerPort{
		{
			Name:          servePortName,
			ContainerPort: 8090,
			Protocol:      corev1.ProtocolTCP,
		},
	}, serveContainer.Ports)
	require.NotNil(t, serveContainer.ReadinessProbe)
	require.NotNil(t, serveContainer.ReadinessProbe.HTTPGet)
	assert.Equal(t, "/manifest.json", serveContainer.ReadinessProbe.HTTPGet.Path)
	assert.Equal(t, intstr.FromInt(8090), serveContainer.ReadinessProbe.HTTPGet.Port)
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state", ReadOnly: true},
	}, serveContainer.VolumeMounts)
	assertRestrictedContainerSecurityContext(t, serveContainer.SecurityContext)

	require.Len(t, deployment.Spec.Template.Spec.Volumes, 6)
	stateVolume := deployment.Spec.Template.Spec.Volumes[0]
	assert.Equal(t, localnetStateVolumeName, stateVolume.Name)
	require.NotNil(t, stateVolume.PersistentVolumeClaim)
	assert.Equal(t, "devnet-node-state", stateVolume.PersistentVolumeClaim.ClaimName)
	ipcVolume := deployment.Spec.Template.Spec.Volumes[1]
	assert.Equal(t, nodeIPCVolumeName, ipcVolume.Name)
	assert.NotNil(t, ipcVolume.EmptyDir)
	kupoVolume := deployment.Spec.Template.Spec.Volumes[2]
	assert.Equal(t, kupoDBVolumeName, kupoVolume.Name)
	require.NotNil(t, kupoVolume.EmptyDir)
	require.NotNil(t, kupoVolume.EmptyDir.SizeLimit)
	assert.Zero(t, kupoVolume.EmptyDir.SizeLimit.Cmp(resource.MustParse(defaultKupoDBSizeLimit)))
	kupoTmpVolume := deployment.Spec.Template.Spec.Volumes[3]
	assert.Equal(t, kupoTmpVolumeName, kupoTmpVolume.Name)
	require.NotNil(t, kupoTmpVolume.EmptyDir)
	require.NotNil(t, kupoTmpVolume.EmptyDir.SizeLimit)
	assert.Zero(t, kupoTmpVolume.EmptyDir.SizeLimit.Cmp(resource.MustParse(defaultKupoTmpSizeLimit)))
	faucetAuthVolume := deployment.Spec.Template.Spec.Volumes[4]
	assert.Equal(t, faucetAuthVolumeName, faucetAuthVolume.Name)
	require.NotNil(t, faucetAuthVolume.Secret)
	assert.Equal(t, "devnet-faucet-auth", faucetAuthVolume.Secret.SecretName)
	artifactPublisherTokenVolume := deployment.Spec.Template.Spec.Volumes[5]
	assert.Equal(t, artifactPublisherTokenVolumeName, artifactPublisherTokenVolume.Name)
	require.NotNil(t, artifactPublisherTokenVolume.Projected)
	require.Len(t, artifactPublisherTokenVolume.Projected.Sources, 3)
	require.NotNil(t, artifactPublisherTokenVolume.Projected.Sources[0].ServiceAccountToken)
	assert.Empty(t, artifactPublisherTokenVolume.Projected.Sources[0].ServiceAccountToken.Audience)

	assert.Equal(t, "devnet-network-artifacts", networkArtifactsConfigMap.Name)
	assert.Equal(t, "default", networkArtifactsConfigMap.Namespace)
	assert.Equal(t, "yacd", networkArtifactsConfigMap.Labels[labelAppManagedBy])
	assert.Equal(t, persistentVolumeClaim.Annotations[localnetFingerprintAnno], networkArtifactsConfigMap.Annotations[localnetFingerprintAnno])

	assert.Equal(t, "devnet-artifact-publisher", artifactPublisherServiceAccount.Name)
	assert.Equal(t, "default", artifactPublisherServiceAccount.Namespace)
	assert.Equal(t, "yacd", artifactPublisherServiceAccount.Labels[labelAppManagedBy])
	require.NotNil(t, artifactPublisherServiceAccount.AutomountServiceAccountToken)
	assert.False(t, *artifactPublisherServiceAccount.AutomountServiceAccountToken)

	assert.Equal(t, "devnet-artifact-publisher", artifactPublisherRole.Name)
	require.Len(t, artifactPublisherRole.Rules, 1)
	assert.Equal(t, []string{""}, artifactPublisherRole.Rules[0].APIGroups)
	assert.Equal(t, []string{"configmaps"}, artifactPublisherRole.Rules[0].Resources)
	assert.Equal(t, []string{"devnet-network-artifacts"}, artifactPublisherRole.Rules[0].ResourceNames)
	assert.Equal(t, []string{"get", "patch"}, artifactPublisherRole.Rules[0].Verbs)

	assert.Equal(t, "devnet-artifact-publisher", artifactPublisherRoleBinding.Name)
	assert.Equal(t, "Role", artifactPublisherRoleBinding.RoleRef.Kind)
	assert.Equal(t, "devnet-artifact-publisher", artifactPublisherRoleBinding.RoleRef.Name)
	require.Len(t, artifactPublisherRoleBinding.Subjects, 1)
	assert.Equal(t, rbacv1.ServiceAccountKind, artifactPublisherRoleBinding.Subjects[0].Kind)
	assert.Equal(t, "devnet-artifact-publisher", artifactPublisherRoleBinding.Subjects[0].Name)
	assert.Equal(t, "default", artifactPublisherRoleBinding.Subjects[0].Namespace)

	assert.Equal(t, "devnet-node-state", persistentVolumeClaim.Name)
	assert.Equal(t, "default", persistentVolumeClaim.Namespace)
	assert.Equal(t, "yacd", persistentVolumeClaim.Labels[labelAppManagedBy])
	assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, persistentVolumeClaim.Spec.AccessModes)
	storage := persistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Zero(t, storage.Cmp(resource.MustParse("10Gi")))

	assert.Equal(t, "devnet-node", service.Name)
	assert.Equal(t, "default", service.Namespace)
	assert.Equal(t, "yacd", service.Labels[labelAppManagedBy])
	assert.Equal(t, corev1.ServiceTypeClusterIP, service.Spec.Type)
	assert.Equal(t, expectedSelector, service.Spec.Selector)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       cardanoNodePortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       3001,
			TargetPort: intstr.FromString(cardanoNodePortName),
		},
	}, service.Spec.Ports)

	assert.Equal(t, "devnet-ogmios", ogmiosService.Name)
	assert.Equal(t, "default", ogmiosService.Namespace)
	assert.Equal(t, "yacd", ogmiosService.Labels[labelAppManagedBy])
	assert.Equal(t, corev1.ServiceTypeClusterIP, ogmiosService.Spec.Type)
	assert.Equal(t, expectedSelector, ogmiosService.Spec.Selector)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       ogmiosPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultOgmiosPort,
			TargetPort: intstr.FromString(ogmiosPortName),
		},
	}, ogmiosService.Spec.Ports)

	assert.Equal(t, "devnet-kupo", kupoService.Name)
	assert.Equal(t, "default", kupoService.Namespace)
	assert.Equal(t, "yacd", kupoService.Labels[labelAppManagedBy])
	assert.Equal(t, corev1.ServiceTypeClusterIP, kupoService.Spec.Type)
	assert.Equal(t, expectedSelector, kupoService.Spec.Selector)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       kupoPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultKupoPort,
			TargetPort: intstr.FromString(kupoPortName),
		},
	}, kupoService.Spec.Ports)

	assert.Equal(t, "devnet-faucet", faucetService.Name)
	assert.Equal(t, "default", faucetService.Namespace)
	assert.Equal(t, "yacd", faucetService.Labels[labelAppManagedBy])
	assert.Equal(t, corev1.ServiceTypeClusterIP, faucetService.Spec.Type)
	assert.Equal(t, expectedSelector, faucetService.Spec.Selector)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       faucetPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultFaucetPort,
			TargetPort: intstr.FromString(faucetPortName),
		},
	}, faucetService.Spec.Ports)

	assert.Equal(t, "devnet-artifacts", artifactsService.Name)
	assert.Equal(t, "default", artifactsService.Namespace)
	assert.Equal(t, "yacd", artifactsService.Labels[labelAppManagedBy])
	assert.Equal(t, corev1.ServiceTypeClusterIP, artifactsService.Spec.Type)
	assert.Equal(t, expectedSelector, artifactsService.Spec.Selector)
	assert.Equal(t, []corev1.ServicePort{
		{
			Name:       servePortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       defaultServePort,
			TargetPort: intstr.FromString(servePortName),
		},
	}, artifactsService.Spec.Ports)

	assert.Equal(t, "devnet-faucet-auth", faucetAuthSecret.Name)
	assert.Equal(t, "default", faucetAuthSecret.Namespace)
	assert.Equal(t, "yacd", faucetAuthSecret.Labels[labelAppManagedBy])
	assert.Equal(t, corev1.SecretTypeOpaque, faucetAuthSecret.Type)

	assertPodSecurityContext(t, deployment.Spec.Template.Spec.SecurityContext)
	assertRestrictedContainerSecurityContext(t, nodeContainer.SecurityContext)
	assertRestrictedContainerSecurityContext(t, ogmiosContainer.SecurityContext)
	assertRestrictedContainerSecurityContext(t, kupoContainer.SecurityContext)
	assertRestrictedContainerSecurityContext(t, faucetContainer.SecurityContext)
}

func TestPrimaryWorkloadBuilderBuildsPublicWorkload(t *testing.T) {
	tests := []struct {
		name                 string
		profile              yacdv1alpha1.PublicNetworkProfile
		customBundle         *publicnet.CustomBundle
		wantNetworkMagic     int64
		wantRequiresMagic    bool
		wantFingerprint      string
		wantMissingArtifacts []string
		mithrilBootstrap     bool
		// curated is true for the curated public profiles (preview, preprod,
		// mainnet) that get the fetch init container + serve sidecar, and
		// false for the custom profile that gets neither in this additive PR.
		curated bool
	}{
		{
			name:              "preview",
			profile:           yacdv1alpha1.PublicNetworkProfilePreview,
			wantNetworkMagic:  2,
			wantRequiresMagic: true,
			wantFingerprint:   "3eee469d6200db89fd64fbd032ccbb58a7ba557b920a07bc2f22523b6f009a29",
			curated:           true,
		},
		{
			name:                 "preprod",
			profile:              yacdv1alpha1.PublicNetworkProfilePreprod,
			wantNetworkMagic:     1,
			wantRequiresMagic:    true,
			wantMissingArtifacts: []string{networkartifacts.CheckpointsKey},
			curated:              true,
		},
		{
			name:              "mainnet",
			profile:           yacdv1alpha1.PublicNetworkProfileMainnet,
			wantNetworkMagic:  764824073,
			wantRequiresMagic: false,
			mithrilBootstrap:  true,
			curated:           true,
		},
		{
			name:              "custom",
			profile:           yacdv1alpha1.PublicNetworkProfileCustom,
			customBundle:      customPublicProfileBundle(t),
			wantNetworkMagic:  2,
			wantRequiresMagic: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			network := publicCardanoNetwork(tc.name, tc.profile)
			builder := newTestPrimaryWorkloadBuilder(t)
			if tc.profile == yacdv1alpha1.PublicNetworkProfileCustom {
				network.Spec.Public.ConfigSource = &yacdv1alpha1.NetworkConfigSource{
					ConfigMapRef: &corev1.LocalObjectReference{Name: "custom-profile"},
				}
				builder.publicCustomBundle = tc.customBundle
			}
			if tc.mithrilBootstrap {
				network.Spec.Public.Bootstrap = &yacdv1alpha1.PublicNetworkBootstrapSpec{
					Mithril: &yacdv1alpha1.MithrilBootstrapSpec{},
				}
			}

			resources, err := builder.Build(network)
			require.NoError(t, err)

			assert.Equal(t, yacdv1alpha1.CardanoNetworkModePublic, resources.NetworkPlan.Mode)
			require.NotNil(t, resources.NetworkPlan.Profile)
			assert.Equal(t, tc.profile, *resources.NetworkPlan.Profile)
			assert.Equal(t, tc.wantNetworkMagic, resources.NetworkPlan.NetworkMagic)
			assert.Equal(t, tc.wantRequiresMagic, resources.NetworkPlan.RequiresNetworkMagic)
			assert.NotEmpty(t, resources.NetworkPlan.Fingerprint)
			if tc.wantFingerprint != "" {
				assert.Equal(t, tc.wantFingerprint, resources.NetworkPlan.Fingerprint)
			}
			assert.Nil(t, resources.ArtifactPublisherServiceAccount)
			assert.Nil(t, resources.ArtifactPublisherRole)
			assert.Nil(t, resources.ArtifactPublisherRoleBinding)
			assert.NotNil(t, resources.OgmiosService)
			assert.Nil(t, resources.KupoService)
			assert.Nil(t, resources.FaucetService)
			assert.Nil(t, resources.FaucetAuthSecret)
			// The artifacts Service fronts the serve sidecar: present for the
			// curated public profiles, absent for custom-public.
			if tc.curated {
				require.NotNil(t, resources.ArtifactsService)
				assert.Equal(t, tc.name+"-artifacts", resources.ArtifactsService.Name)
				assert.Equal(t, "default", resources.ArtifactsService.Namespace)
				assert.Equal(t, corev1.ServiceTypeClusterIP, resources.ArtifactsService.Spec.Type)
				assert.Equal(t, primaryWorkloadSelectorLabels(network), resources.ArtifactsService.Spec.Selector)
				assert.Equal(t, []corev1.ServicePort{
					{
						Name:       servePortName,
						Protocol:   corev1.ProtocolTCP,
						Port:       defaultServePort,
						TargetPort: intstr.FromString(servePortName),
					},
				}, resources.ArtifactsService.Spec.Ports)
			} else {
				assert.Nil(t, resources.ArtifactsService)
			}

			deployment := resources.Deployment
			assert.Equal(t, tc.name+"-node", deployment.Name)
			assert.Empty(t, deployment.Spec.Template.Spec.ServiceAccountName)
			require.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken)
			assert.False(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken)
			assert.Equal(t, resources.NetworkPlan.Fingerprint, deployment.Spec.Template.Annotations[networkFingerprintAnno])
			assert.NotContains(t, deployment.Spec.Template.Annotations, localnetFingerprintAnno)
			if tc.curated {
				// Curated public profiles get a fetch init container first;
				// mainnet additionally gets the Mithril bootstrap ordered
				// after fetch.
				wantInitCount := 1
				if tc.mithrilBootstrap {
					wantInitCount = 2
				}
				require.Len(t, deployment.Spec.Template.Spec.InitContainers, wantInitCount)

				fetchInit := deployment.Spec.Template.Spec.InitContainers[0]
				assert.Equal(t, servedArtifactsInitContainerName, fetchInit.Name)
				assert.Equal(t, "ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.0", fetchInit.Image)
				assert.Equal(t, []string{cardanoToolsCommand}, fetchInit.Command)
				assert.Equal(t, []string{
					"fetch",
					"--profile", string(tc.profile),
					"--output-dir", "/state/artifacts",
				}, fetchInit.Args)
				assert.Equal(t, []corev1.VolumeMount{
					{Name: localnetStateVolumeName, MountPath: "/state"},
				}, fetchInit.VolumeMounts)
				assertRestrictedContainerSecurityContext(t, fetchInit.SecurityContext)
			}
			if tc.mithrilBootstrap {
				mithril := deployment.Spec.Template.Spec.InitContainers[1]
				assert.Equal(t, mithrilBootstrapInitContainerName, mithril.Name)
				assert.Equal(t, "ghcr.io/input-output-hk/mithril-client:main-2478748", mithril.Image)
				assert.Equal(t, []string{mithrilBootstrapCommand}, mithril.Command)
				assert.Contains(t, strings.Join(mithril.Args, " "), "mithril-client cardano-db download")
				assert.Contains(t, strings.Join(mithril.Args, " "), "--include-ancillary")
				assert.Contains(t, strings.Join(mithril.Args, " "), `--download-dir "${download_dir}"`)
				assert.Contains(t, strings.Join(mithril.Args, " "), `mv "${candidate_db}" "${target_db}"`)
				assert.Equal(t, []corev1.VolumeMount{
					{Name: localnetStateVolumeName, MountPath: "/state"},
					{Name: mithrilTmpVolumeName, MountPath: "/tmp"},
				}, mithril.VolumeMounts)
				mithrilEnv := envMap(mithril)
				assert.Equal(t, "https://aggregator.release-mainnet.api.mithril.network/aggregator", mithrilEnv[mithrilAggregatorEndpointEnvName])
				assert.Equal(t, "latest", mithrilEnv[mithrilSnapshotEnvName])
				assert.NotEmpty(t, mithrilEnv[mithrilGenesisVerificationKeyEnvName])
				assert.NotEmpty(t, mithrilEnv[mithrilAncillaryVerificationKeyEnvName])
			}
			if !tc.curated {
				assert.Empty(t, deployment.Spec.Template.Spec.InitContainers)
			}

			// Curated public profiles add the always-on serve sidecar (node,
			// ogmios, serve); custom adds neither (node, ogmios).
			if tc.curated {
				require.Len(t, deployment.Spec.Template.Spec.Containers, 3)
				serveContainer := deployment.Spec.Template.Spec.Containers[2]
				assert.Equal(t, serveContainerName, serveContainer.Name)
				assert.Equal(t, "ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.0", serveContainer.Image)
				assert.Equal(t, []string{
					"serve",
					"--artifacts-dir", "/state/artifacts",
					"--listen", ":8090",
				}, serveContainer.Args)
				require.Len(t, serveContainer.Ports, 1)
				assert.Equal(t, int32(8090), serveContainer.Ports[0].ContainerPort)
				assert.Equal(t, servePortName, serveContainer.Ports[0].Name)
				require.NotNil(t, serveContainer.ReadinessProbe)
				require.NotNil(t, serveContainer.ReadinessProbe.HTTPGet)
				assert.Equal(t, "/manifest.json", serveContainer.ReadinessProbe.HTTPGet.Path)
				assert.Equal(t, []corev1.VolumeMount{
					{Name: localnetStateVolumeName, MountPath: "/state", ReadOnly: true},
				}, serveContainer.VolumeMounts)
				assertRestrictedContainerSecurityContext(t, serveContainer.SecurityContext)
			} else {
				require.Len(t, deployment.Spec.Template.Spec.Containers, 2)
				assertNoContainerNamed(t, deployment.Spec.Template.Spec.Containers, serveContainerName)
			}

			nodeContainer := deployment.Spec.Template.Spec.Containers[0]
			assert.Equal(t, cardanoNodeContainerName, nodeContainer.Name)
			assert.Equal(t, "/profile", nodeContainer.WorkingDir)
			assert.Equal(t, []string{
				"run",
				"--config", "/profile/configuration.yaml",
				"--topology", "/profile/primary-topology.json",
				"--database-path", "/state/db",
				"--socket-path", "/ipc/node.socket",
				"--host-addr", "0.0.0.0",
				"--port", "3001",
			}, nodeContainer.Args)
			assert.NotContains(t, nodeContainer.Args, "--shelley-kes-key")
			assert.Equal(t, []corev1.VolumeMount{
				{Name: localnetStateVolumeName, MountPath: "/state"},
				{Name: nodeIPCVolumeName, MountPath: "/ipc"},
				{Name: publicProfileVolumeName, MountPath: "/profile", ReadOnly: true},
			}, nodeContainer.VolumeMounts)
			if tc.mithrilBootstrap {
				assert.Equal(t, defaultMainnetNodeResources(), nodeContainer.Resources)
			} else {
				assert.Empty(t, nodeContainer.Resources)
			}

			ogmiosContainer := deployment.Spec.Template.Spec.Containers[1]
			assert.Equal(t, ogmiosContainerName, ogmiosContainer.Name)
			assert.Equal(t, "/profile", ogmiosContainer.WorkingDir)
			assert.Equal(t, []string{
				"--node-socket", "/ipc/node.socket",
				"--node-config", "/profile/configuration.yaml",
				"--host", "0.0.0.0",
				"--port", "1337",
			}, ogmiosContainer.Args)
			assert.Equal(t, []corev1.VolumeMount{
				{Name: nodeIPCVolumeName, MountPath: "/ipc"},
				{Name: publicProfileVolumeName, MountPath: "/profile", ReadOnly: true},
			}, ogmiosContainer.VolumeMounts)

			if tc.mithrilBootstrap {
				require.Len(t, deployment.Spec.Template.Spec.Volumes, 4)
				require.NotNil(t, requireVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, mithrilTmpVolumeName).EmptyDir)
			} else {
				require.Len(t, deployment.Spec.Template.Spec.Volumes, 3)
				assertNoVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, mithrilTmpVolumeName)
			}
			publicProfileVolume := requireVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, publicProfileVolumeName)
			require.NotNil(t, publicProfileVolume.ConfigMap)
			assert.Equal(t, tc.name+"-network-artifacts", publicProfileVolume.ConfigMap.Name)
			assertNoVolumeNamed(t, deployment.Spec.Template.Spec.Volumes, artifactPublisherTokenVolumeName)

			configMap := resources.NetworkArtifactsConfigMap
			assert.Equal(t, resources.NetworkPlan.Fingerprint, configMap.Annotations[networkFingerprintAnno])
			assert.NotContains(t, configMap.Annotations, localnetFingerprintAnno)
			assert.Equal(t, networkartifacts.SchemaVersion, configMap.Annotations[ctrlannotations.ArtifactSchemaVersion])
			assert.Equal(t, ctrlartifacts.ComputeDataHash(configMap.Data), configMap.Annotations[ctrlannotations.ArtifactDataHash])
			for _, key := range []string{
				networkartifacts.ConfigurationKey,
				networkartifacts.ByronGenesisKey,
				networkartifacts.ShelleyGenesisKey,
				networkartifacts.AlonzoGenesisKey,
				networkartifacts.ConwayGenesisKey,
				networkartifacts.PrimaryTopologyKey,
				networkartifacts.PeerSnapshotKey,
				networkartifacts.PublicProfileManifestKey,
				networkartifacts.ConnectionKey,
			} {
				assert.NotEmpty(t, configMap.Data[key], "artifact %s", key)
			}
			if len(tc.wantMissingArtifacts) == 0 {
				assert.NotEmpty(t, configMap.Data[networkartifacts.CheckpointsKey], "artifact %s", networkartifacts.CheckpointsKey)
			}
			for _, key := range tc.wantMissingArtifacts {
				assert.Empty(t, configMap.Data[key], "artifact %s", key)
			}
			if tc.mithrilBootstrap {
				assert.NotEmpty(t, configMap.Data[networkartifacts.MithrilGenesisKey])
				assert.NotEmpty(t, configMap.Data[networkartifacts.MithrilAncillaryKey])
			}

			assert.Equal(t, resources.NetworkPlan.Fingerprint, resources.PersistentVolumeClaim.Annotations[networkFingerprintAnno])
			assert.NotContains(t, resources.PersistentVolumeClaim.Annotations, localnetFingerprintAnno)
			storage := resources.PersistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
			if tc.mithrilBootstrap {
				assert.Zero(t, storage.Cmp(resource.MustParse(defaultMainnetNodeStorageSize)))
			} else {
				assert.Zero(t, storage.Cmp(resource.MustParse(defaultNodeStorageSize)))
			}
		})
	}
}

func TestPrimaryWorkloadBuilderLeavesFaucetDisabledByDefault(t *testing.T) {
	network := localCardanoNetwork("faucet-default-disabled")

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	require.Len(t, resources.Deployment.Spec.Template.Spec.Containers, 4)
	assert.Equal(t, cardanoNodeContainerName, resources.Deployment.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, ogmiosContainerName, resources.Deployment.Spec.Template.Spec.Containers[1].Name)
	assert.Equal(t, kupoContainerName, resources.Deployment.Spec.Template.Spec.Containers[2].Name)
	assert.Equal(t, serveContainerName, resources.Deployment.Spec.Template.Spec.Containers[3].Name)
	require.Len(t, resources.Deployment.Spec.Template.Spec.InitContainers, 2)
	assert.Equal(t, localnetCreateEnvInitContainerName, resources.Deployment.Spec.Template.Spec.InitContainers[0].Name)
	assert.Equal(t, servedArtifactsInitContainerName, resources.Deployment.Spec.Template.Spec.InitContainers[1].Name)
	require.Len(t, resources.Deployment.Spec.Template.Spec.Volumes, 5)
	assert.NotNil(t, resources.NetworkArtifactsConfigMap)
	assert.NotNil(t, resources.ArtifactPublisherServiceAccount)
	assert.NotNil(t, resources.ArtifactPublisherRole)
	assert.NotNil(t, resources.ArtifactPublisherRoleBinding)
	assert.NotNil(t, resources.OgmiosService)
	assert.NotNil(t, resources.KupoService)
	assert.Nil(t, resources.FaucetService)
	assert.Nil(t, resources.FaucetAuthSecret)
}

func TestPrimaryWorkloadBuilderUsesSafeNamesAndLabels(t *testing.T) {
	network := localCardanoNetwork("devnet." + strings.Repeat("a", 80))
	enableFaucet(network)

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(resources.Deployment.Name), ctrlnames.MaxLabelValueLength)
	assert.True(t, strings.HasSuffix(resources.Deployment.Name, "-node"))
	assert.NotContains(t, resources.Deployment.Name, ".")
	assert.Equal(t, resources.Deployment.Name, resources.Service.Name)
	assert.LessOrEqual(t, len(resources.OgmiosService.Name), ctrlnames.MaxLabelValueLength)
	assert.True(t, strings.HasSuffix(resources.OgmiosService.Name, "-ogmios"))
	assert.NotContains(t, resources.OgmiosService.Name, ".")
	assert.LessOrEqual(t, len(resources.KupoService.Name), ctrlnames.MaxLabelValueLength)
	assert.True(t, strings.HasSuffix(resources.KupoService.Name, "-kupo"))
	assert.NotContains(t, resources.KupoService.Name, ".")
	assert.LessOrEqual(t, len(resources.FaucetService.Name), ctrlnames.MaxLabelValueLength)
	assert.True(t, strings.HasSuffix(resources.FaucetService.Name, "-faucet"))
	assert.NotContains(t, resources.FaucetService.Name, ".")
	assert.LessOrEqual(t, len(resources.FaucetAuthSecret.Name), ctrlnames.MaxLabelValueLength)
	assert.True(t, strings.HasSuffix(resources.FaucetAuthSecret.Name, "-faucet-auth"))
	assert.NotContains(t, resources.FaucetAuthSecret.Name, ".")
	assert.LessOrEqual(t, len(resources.PersistentVolumeClaim.Name), ctrlnames.MaxLabelValueLength)
	assert.True(t, strings.HasSuffix(resources.PersistentVolumeClaim.Name, "-node-state"))
	assert.NotContains(t, resources.PersistentVolumeClaim.Name, ".")

	selector := resources.Deployment.Spec.Selector.MatchLabels
	assert.LessOrEqual(t, len(selector[labelAppInstance]), ctrlnames.MaxLabelValueLength)
	assert.LessOrEqual(t, len(selector[labelCardanoNetwork]), ctrlnames.MaxLabelValueLength)
	assert.NotEqual(t, network.Name, selector[labelAppInstance])
	assert.Equal(t, selector, resources.Deployment.Spec.Template.Labels)
}

func TestPrimaryWorkloadBuilderAvoidsSanitizedNameCollisions(t *testing.T) {
	dottedNetwork := localCardanoNetwork("foo.bar")
	enableFaucet(dottedNetwork)
	dotted, err := newTestPrimaryWorkloadBuilder(t).Build(dottedNetwork)
	require.NoError(t, err)
	dashedNetwork := localCardanoNetwork("foo-bar")
	enableFaucet(dashedNetwork)
	dashed, err := newTestPrimaryWorkloadBuilder(t).Build(dashedNetwork)
	require.NoError(t, err)

	assert.NotEqual(t, dotted.Deployment.Name, dashed.Deployment.Name)
	assert.NotEqual(t, dotted.PersistentVolumeClaim.Name, dashed.PersistentVolumeClaim.Name)
	assert.NotEqual(t, dotted.Service.Name, dashed.Service.Name)
	assert.NotEqual(t, dotted.OgmiosService.Name, dashed.OgmiosService.Name)
	assert.NotEqual(t, dotted.KupoService.Name, dashed.KupoService.Name)
	assert.NotEqual(t, dotted.FaucetService.Name, dashed.FaucetService.Name)
	assert.NotEqual(t, dotted.FaucetAuthSecret.Name, dashed.FaucetAuthSecret.Name)
}

func TestPrimaryWorkloadBuilderAppliesNodeOverrides(t *testing.T) {
	network := localCardanoNetwork("custom-node")
	image := "example.com/cardano-node:test"
	storageClassName := testStorageClassName
	network.Spec.Node.Image = &image
	network.Spec.Node.Storage = &yacdv1alpha1.NodeStorageSpec{
		Size:             resource.MustParse("20Gi"),
		StorageClassName: &storageClassName,
	}
	network.Spec.Node.Resources = &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("1Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
	}

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	nodeContainer := resources.Deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, image, nodeContainer.Image)
	assert.Equal(t, *network.Spec.Node.Resources, nodeContainer.Resources)

	storage := resources.PersistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Zero(t, storage.Cmp(resource.MustParse("20Gi")))
	require.NotNil(t, resources.PersistentVolumeClaim.Spec.StorageClassName)
	assert.Equal(t, testStorageClassName, *resources.PersistentVolumeClaim.Spec.StorageClassName)
	assert.Equal(t, testStorageClassName, resources.PersistentVolumeClaim.Annotations[ctrlannotations.RequestedStorageClass])
}

func TestPrimaryWorkloadBuilderAppliesOgmiosOverrides(t *testing.T) {
	network := localCardanoNetwork("custom-ogmios")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Ogmios: &yacdv1alpha1.OgmiosSpec{
			Enabled: true,
			Image:   "example.com/ogmios:v6.14.0",
			Port:    1444,
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
		},
	}
	enableFaucet(network)

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	require.NotNil(t, resources.OgmiosService)
	require.Len(t, resources.Deployment.Spec.Template.Spec.Containers, 5)
	ogmiosContainer := resources.Deployment.Spec.Template.Spec.Containers[1]
	assert.Equal(t, "example.com/ogmios:v6.14.0", ogmiosContainer.Image)
	assert.Contains(t, ogmiosContainer.Args, "1444")
	assert.Equal(t, []string{ogmiosCommand, "health-check", "--port", "1444"}, ogmiosContainer.ReadinessProbe.Exec.Command)
	assert.Equal(t, *network.Spec.ChainAPI.Ogmios.Resources, ogmiosContainer.Resources)
	assert.Equal(t, int32(1444), resources.OgmiosService.Spec.Ports[0].Port)
	assert.Equal(t, intstr.FromString(ogmiosPortName), resources.OgmiosService.Spec.Ports[0].TargetPort)
	kupoContainer := resources.Deployment.Spec.Template.Spec.Containers[2]
	assert.Contains(t, kupoContainer.Args, "1444")
}

func TestPrimaryWorkloadBuilderAppliesKupoPortAndResourceOverrides(t *testing.T) {
	network := localCardanoNetwork("custom-kupo")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Kupo: &yacdv1alpha1.KupoSpec{
			Enabled: true,
			Image:   defaultKupoImage,
			Port:    2442,
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
		},
	}
	enableFaucet(network)

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	require.NotNil(t, resources.KupoService)
	require.Len(t, resources.Deployment.Spec.Template.Spec.Containers, 5)
	kupoContainer := resources.Deployment.Spec.Template.Spec.Containers[2]
	assert.Equal(t, defaultKupoImage, kupoContainer.Image)
	assert.Contains(t, kupoContainer.Args, "2442")
	assert.Equal(t, []string{kupoContainerName, "health-check", "--port", "2442"}, kupoContainer.ReadinessProbe.Exec.Command)
	assert.Equal(t, *network.Spec.ChainAPI.Kupo.Resources, kupoContainer.Resources)
	assert.Equal(t, int32(2442), resources.KupoService.Spec.Ports[0].Port)
	assert.Equal(t, intstr.FromString(kupoPortName), resources.KupoService.Spec.Ports[0].TargetPort)
}

func TestPrimaryWorkloadBuilderAppliesFaucetOverrides(t *testing.T) {
	network := localCardanoNetwork("custom-faucet")
	image := "ghcr.io/meigma/yacd/faucet:test"
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Faucet: &yacdv1alpha1.FaucetSpec{
			Enabled:          true,
			Image:            &image,
			Port:             18080,
			DefaultSource:    "utxo2",
			MinTopUpLovelace: 2_000_000,
			MaxTopUpLovelace: 5_000_000,
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
		},
	}

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	require.NotNil(t, resources.FaucetService)
	require.NotNil(t, resources.FaucetAuthSecret)
	require.Len(t, resources.Deployment.Spec.Template.Spec.Containers, 5)
	faucetContainer := resources.Deployment.Spec.Template.Spec.Containers[3]
	assert.Equal(t, image, faucetContainer.Image)
	assert.Contains(t, faucetContainer.Args, "0.0.0.0:18080")
	assert.Contains(t, faucetContainer.Args, "utxo2")
	assert.Contains(t, faucetContainer.Args, "2000000")
	assert.Contains(t, faucetContainer.Args, "5000000")
	assert.Equal(t, *network.Spec.ChainAPI.Faucet.Resources, faucetContainer.Resources)
	assert.Equal(t, int32(18080), resources.FaucetService.Spec.Ports[0].Port)
	assert.Equal(t, intstr.FromString(faucetPortName), resources.FaucetService.Spec.Ports[0].TargetPort)
}

func TestPrimaryWorkloadBuilderDisablesOgmios(t *testing.T) {
	network := localCardanoNetwork("ogmios-disabled")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Ogmios: &yacdv1alpha1.OgmiosSpec{
			Enabled: false,
		},
	}

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	require.Len(t, resources.Deployment.Spec.Template.Spec.Containers, 2)
	assert.Equal(t, cardanoNodeContainerName, resources.Deployment.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, serveContainerName, resources.Deployment.Spec.Template.Spec.Containers[1].Name)
	assert.Nil(t, resources.OgmiosService)
	assert.Nil(t, resources.KupoService)
	assert.Nil(t, resources.FaucetService)
	assert.Nil(t, resources.FaucetAuthSecret)
	require.Len(t, resources.Deployment.Spec.Template.Spec.Volumes, 3)
}

func TestPrimaryWorkloadBuilderDisablesKupo(t *testing.T) {
	network := localCardanoNetwork("kupo-disabled")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Kupo: &yacdv1alpha1.KupoSpec{
			Enabled: false,
		},
	}

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	require.Len(t, resources.Deployment.Spec.Template.Spec.Containers, 3)
	assert.Equal(t, cardanoNodeContainerName, resources.Deployment.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, ogmiosContainerName, resources.Deployment.Spec.Template.Spec.Containers[1].Name)
	assert.Equal(t, serveContainerName, resources.Deployment.Spec.Template.Spec.Containers[2].Name)
	assert.NotNil(t, resources.OgmiosService)
	assert.Nil(t, resources.KupoService)
	assert.Nil(t, resources.FaucetService)
	assert.Nil(t, resources.FaucetAuthSecret)
	require.Len(t, resources.Deployment.Spec.Template.Spec.InitContainers, 2)
	require.Len(t, resources.Deployment.Spec.Template.Spec.Volumes, 3)
}

func TestPrimaryWorkloadBuilderDisablesFaucet(t *testing.T) {
	network := localCardanoNetwork("faucet-disabled")
	network.Spec.ChainAPI = &yacdv1alpha1.ChainAPISpec{
		Faucet: &yacdv1alpha1.FaucetSpec{
			Enabled: false,
		},
	}

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	require.Len(t, resources.Deployment.Spec.Template.Spec.Containers, 4)
	assert.Equal(t, cardanoNodeContainerName, resources.Deployment.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, ogmiosContainerName, resources.Deployment.Spec.Template.Spec.Containers[1].Name)
	assert.Equal(t, kupoContainerName, resources.Deployment.Spec.Template.Spec.Containers[2].Name)
	assert.Equal(t, serveContainerName, resources.Deployment.Spec.Template.Spec.Containers[3].Name)
	assert.NotNil(t, resources.OgmiosService)
	assert.NotNil(t, resources.KupoService)
	assert.Nil(t, resources.FaucetService)
	assert.Nil(t, resources.FaucetAuthSecret)
	require.Len(t, resources.Deployment.Spec.Template.Spec.InitContainers, 2)
	require.Len(t, resources.Deployment.Spec.Template.Spec.Volumes, 5)
}

func newTestPrimaryWorkloadBuilder(t *testing.T) primaryWorkloadBuilder {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, rbacv1.AddToScheme(scheme))

	return primaryWorkloadBuilder{scheme: scheme}
}

func assertPodSecurityContext(t *testing.T, securityContext *corev1.PodSecurityContext) {
	t.Helper()

	require.NotNil(t, securityContext)
	assert.Equal(t, int64(10001), *securityContext.FSGroup)
	assert.Equal(t, int64(10001), *securityContext.RunAsGroup)
	assert.True(t, *securityContext.RunAsNonRoot)
	assert.Equal(t, int64(10001), *securityContext.RunAsUser)
	require.NotNil(t, securityContext.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, securityContext.SeccompProfile.Type)
}

func assertRestrictedContainerSecurityContext(t *testing.T, securityContext *corev1.SecurityContext) {
	t.Helper()

	require.NotNil(t, securityContext)
	assert.False(t, *securityContext.AllowPrivilegeEscalation)
	assert.Equal(t, []corev1.Capability{"ALL"}, securityContext.Capabilities.Drop)
	assert.True(t, *securityContext.ReadOnlyRootFilesystem)
	assert.True(t, *securityContext.RunAsNonRoot)
	assert.Equal(t, int64(10001), *securityContext.RunAsUser)
	assert.Equal(t, int64(10001), *securityContext.RunAsGroup)
	require.NotNil(t, securityContext.SeccompProfile)
	assert.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, securityContext.SeccompProfile.Type)
}

func customPublicProfileBundle(t testing.TB) *publicnet.CustomBundle {
	t.Helper()

	plan, err := publicnet.BuildPlan(publicnet.Spec{Profile: "preview"})
	require.NoError(t, err)
	return &publicnet.CustomBundle{Files: map[string]string{
		"config.json":          plan.Artifacts[networkartifacts.ConfigurationKey],
		"byron-genesis.json":   plan.Artifacts[networkartifacts.ByronGenesisKey],
		"shelley-genesis.json": plan.Artifacts[networkartifacts.ShelleyGenesisKey],
		"alonzo-genesis.json":  plan.Artifacts[networkartifacts.AlonzoGenesisKey],
		"conway-genesis.json":  plan.Artifacts[networkartifacts.ConwayGenesisKey],
		"topology.json":        plan.Artifacts[networkartifacts.PrimaryTopologyKey],
		"checkpoints.json":     plan.Artifacts[networkartifacts.CheckpointsKey],
		"peer-snapshot.json":   plan.Artifacts[networkartifacts.PeerSnapshotKey],
	}}
}
