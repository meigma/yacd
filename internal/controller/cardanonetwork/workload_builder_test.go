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

// TestPrimaryWorkloadBuilderBuildsDeploymentAndPVC verifies the initial
// singleton primary node workload shape without the future Ogmios sidecar.
func TestPrimaryWorkloadBuilderBuildsDeploymentAndPVC(t *testing.T) {
	network := localCardanoNetwork("devnet")

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)
	deployment := resources.Deployment
	persistentVolumeClaim := resources.PersistentVolumeClaim

	assert.Equal(t, "devnet-node", deployment.Name)
	assert.Equal(t, "default", deployment.Namespace)
	require.NotNil(t, deployment.Spec.Replicas)
	assert.Equal(t, int32(1), *deployment.Spec.Replicas)
	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type)

	controller := metav1.GetControllerOf(deployment)
	require.NotNil(t, controller)
	assert.Equal(t, "devnet", controller.Name)
	assert.Equal(t, "CardanoNetwork", controller.Kind)

	pvcController := metav1.GetControllerOf(persistentVolumeClaim)
	require.NotNil(t, pvcController)
	assert.Equal(t, "devnet", pvcController.Name)
	assert.Equal(t, "CardanoNetwork", pvcController.Kind)

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
	assert.NotContains(t, persistentVolumeClaim.Annotations, requestedStorageClassAnno)
	require.NotNil(t, deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	assert.False(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken)

	require.Len(t, deployment.Spec.Template.Spec.InitContainers, 1)
	initContainer := deployment.Spec.Template.Spec.InitContainers[0]
	assert.Equal(t, localnetCreateEnvInitContainerName, initContainer.Name)
	assert.Equal(t, corev1.TerminationMessagePathDefault, initContainer.TerminationMessagePath)
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state"},
	}, initContainer.VolumeMounts)

	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)
	nodeContainer := deployment.Spec.Template.Spec.Containers[0]
	assert.Equal(t, cardanoNodeContainerName, nodeContainer.Name)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.1", nodeContainer.Image)
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
	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state"},
		{Name: nodeIPCVolumeName, MountPath: "/ipc"},
	}, nodeContainer.VolumeMounts)

	require.Len(t, deployment.Spec.Template.Spec.Volumes, 2)
	stateVolume := deployment.Spec.Template.Spec.Volumes[0]
	assert.Equal(t, localnetStateVolumeName, stateVolume.Name)
	require.NotNil(t, stateVolume.PersistentVolumeClaim)
	assert.Equal(t, "devnet-node-state", stateVolume.PersistentVolumeClaim.ClaimName)
	ipcVolume := deployment.Spec.Template.Spec.Volumes[1]
	assert.Equal(t, nodeIPCVolumeName, ipcVolume.Name)
	assert.NotNil(t, ipcVolume.EmptyDir)

	assert.Equal(t, "devnet-node-state", persistentVolumeClaim.Name)
	assert.Equal(t, "default", persistentVolumeClaim.Namespace)
	assert.Equal(t, "yacd", persistentVolumeClaim.Labels[labelAppManagedBy])
	assert.Equal(t, []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, persistentVolumeClaim.Spec.AccessModes)
	storage := persistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Zero(t, storage.Cmp(resource.MustParse("10Gi")))

	assertPodSecurityContext(t, deployment.Spec.Template.Spec.SecurityContext)
	assertRestrictedContainerSecurityContext(t, nodeContainer.SecurityContext)
}

func TestPrimaryWorkloadBuilderUsesSafeNamesAndLabels(t *testing.T) {
	network := localCardanoNetwork("devnet." + strings.Repeat("a", 80))

	resources, err := newTestPrimaryWorkloadBuilder(t).Build(network)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(resources.Deployment.Name), maxLabelValueLength)
	assert.True(t, strings.HasSuffix(resources.Deployment.Name, "-node"))
	assert.NotContains(t, resources.Deployment.Name, ".")
	assert.LessOrEqual(t, len(resources.PersistentVolumeClaim.Name), maxLabelValueLength)
	assert.True(t, strings.HasSuffix(resources.PersistentVolumeClaim.Name, "-node-state"))
	assert.NotContains(t, resources.PersistentVolumeClaim.Name, ".")

	selector := resources.Deployment.Spec.Selector.MatchLabels
	assert.LessOrEqual(t, len(selector[labelAppInstance]), maxLabelValueLength)
	assert.LessOrEqual(t, len(selector[labelCardanoNetwork]), maxLabelValueLength)
	assert.NotEqual(t, network.Name, selector[labelAppInstance])
	assert.Equal(t, selector, resources.Deployment.Spec.Template.Labels)
}

func TestPrimaryWorkloadBuilderAvoidsSanitizedNameCollisions(t *testing.T) {
	dotted, err := newTestPrimaryWorkloadBuilder(t).Build(localCardanoNetwork("foo.bar"))
	require.NoError(t, err)
	dashed, err := newTestPrimaryWorkloadBuilder(t).Build(localCardanoNetwork("foo-bar"))
	require.NoError(t, err)

	assert.NotEqual(t, dotted.Deployment.Name, dashed.Deployment.Name)
	assert.NotEqual(t, dotted.PersistentVolumeClaim.Name, dashed.PersistentVolumeClaim.Name)
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
	assert.Equal(t, testStorageClassName, resources.PersistentVolumeClaim.Annotations[requestedStorageClassAnno])
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
