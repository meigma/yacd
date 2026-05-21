package cardanonetwork

import (
	"strings"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const defaultLocalSlotLength = 100 * time.Millisecond

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
// shapes fail before producing a partial StatefulSet.
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
			name: "public mode",
			mutate: func(network *yacdv1alpha1.CardanoNetwork) {
				network.Spec.Mode = yacdv1alpha1.CardanoNetworkModePublic
			},
			wantErr: `mode "public" is not supported`,
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

// TestPrimaryWorkloadBuilderBuildsStatefulSet verifies the initial primary
// node StatefulSet shape without the future Ogmios sidecar.
func TestPrimaryWorkloadBuilderBuildsStatefulSet(t *testing.T) {
	network := localCardanoNetwork("devnet")

	statefulSet, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	assert.Equal(t, "devnet-node", statefulSet.Name)
	assert.Equal(t, "default", statefulSet.Namespace)
	assert.Equal(t, "devnet-node", statefulSet.Spec.ServiceName)
	require.NotNil(t, statefulSet.Spec.Replicas)
	assert.Equal(t, int32(1), *statefulSet.Spec.Replicas)
	require.NotNil(t, statefulSet.Spec.PersistentVolumeClaimRetentionPolicy)
	assert.Equal(t, appsv1.DeletePersistentVolumeClaimRetentionPolicyType, statefulSet.Spec.PersistentVolumeClaimRetentionPolicy.WhenDeleted)
	assert.Equal(t, appsv1.RetainPersistentVolumeClaimRetentionPolicyType, statefulSet.Spec.PersistentVolumeClaimRetentionPolicy.WhenScaled)

	controller := metav1.GetControllerOf(statefulSet)
	require.NotNil(t, controller)
	assert.Equal(t, "devnet", controller.Name)
	assert.Equal(t, "CardanoNetwork", controller.Kind)

	expectedSelector := map[string]string{
		labelAppName:        labelPrimaryNodeName,
		labelAppInstance:    "devnet",
		labelAppComponent:   labelPrimaryRole,
		labelCardanoNetwork: "devnet",
		labelCardanoRole:    labelPrimaryRole,
	}
	assert.Equal(t, expectedSelector, statefulSet.Spec.Selector.MatchLabels)
	assert.Equal(t, expectedSelector, statefulSet.Spec.Template.Labels)
	assert.Equal(t, "yacd", statefulSet.Labels[labelAppManagedBy])
	assert.NotEmpty(t, statefulSet.Spec.Template.Annotations[localnetFingerprintAnno])
	require.NotNil(t, statefulSet.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.False(t, *statefulSet.Spec.Template.Spec.AutomountServiceAccountToken)

	require.Len(t, statefulSet.Spec.Template.Spec.InitContainers, 1)
	initContainer := statefulSet.Spec.Template.Spec.InitContainers[0]
	assert.Equal(t, localnetCreateEnvInitContainerName, initContainer.Name)
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state"},
	}, initContainer.VolumeMounts)

	require.Len(t, statefulSet.Spec.Template.Spec.Containers, 1)
	nodeContainer := statefulSet.Spec.Template.Spec.Containers[0]
	assert.Equal(t, cardanoNodeContainerName, nodeContainer.Name)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.1", nodeContainer.Image)
	assert.Equal(t, []string{"cardano-node"}, nodeContainer.Command)
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
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state"},
		{Name: nodeIPCVolumeName, MountPath: "/ipc"},
	}, nodeContainer.VolumeMounts)

	require.Len(t, statefulSet.Spec.Template.Spec.Volumes, 1)
	assert.Equal(t, nodeIPCVolumeName, statefulSet.Spec.Template.Spec.Volumes[0].Name)
	assert.NotNil(t, statefulSet.Spec.Template.Spec.Volumes[0].EmptyDir)

	require.Len(t, statefulSet.Spec.VolumeClaimTemplates, 1)
	claim := statefulSet.Spec.VolumeClaimTemplates[0]
	assert.Equal(t, localnetStateVolumeName, claim.Name)
	assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, claim.Spec.AccessModes)
	storage := claim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Zero(t, storage.Cmp(resource.MustParse("10Gi")))

	assertPodSecurityContext(t, statefulSet.Spec.Template.Spec.SecurityContext)
	assertRestrictedContainerSecurityContext(t, nodeContainer.SecurityContext)
}

func TestPrimaryWorkloadBuilderUsesSafeNamesAndLabels(t *testing.T) {
	network := localCardanoNetwork("devnet." + strings.Repeat("a", 80))

	statefulSet, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(statefulSet.Name), maxLabelValueLength)
	assert.True(t, strings.HasSuffix(statefulSet.Name, "-node"))
	assert.NotContains(t, statefulSet.Name, ".")
	assert.Equal(t, statefulSet.Name, statefulSet.Spec.ServiceName)

	selector := statefulSet.Spec.Selector.MatchLabels
	assert.LessOrEqual(t, len(selector[labelAppInstance]), maxLabelValueLength)
	assert.LessOrEqual(t, len(selector[labelCardanoNetwork]), maxLabelValueLength)
	assert.NotEqual(t, network.Name, selector[labelAppInstance])
	assert.Equal(t, selector, statefulSet.Spec.Template.Labels)
}

func TestPrimaryWorkloadBuilderAppliesNodeOverrides(t *testing.T) {
	network := localCardanoNetwork("custom-node")
	image := "example.com/cardano-node:test"
	storageClassName := "fast"
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

	statefulSet, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	nodeContainer := statefulSet.Spec.Template.Spec.Containers[0]
	assert.Equal(t, image, nodeContainer.Image)
	assert.Equal(t, *network.Spec.Node.Resources, nodeContainer.Resources)

	claim := statefulSet.Spec.VolumeClaimTemplates[0]
	storage := claim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Zero(t, storage.Cmp(resource.MustParse("20Gi")))
	require.NotNil(t, claim.Spec.StorageClassName)
	assert.Equal(t, storageClassName, *claim.Spec.StorageClassName)
}

func newTestPrimaryWorkloadBuilder(t *testing.T) primaryWorkloadBuilder {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))

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
