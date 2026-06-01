package cardanodbsync

import (
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// withServedArtifacts publishes a serve endpoint on a ready network so the
// db-sync controller selects the HTTP transport (local + curated-public).
func withServedArtifacts(network *yacdv1alpha1.CardanoNetwork) *yacdv1alpha1.CardanoNetwork {
	network.Status.Endpoints.Artifacts = &yacdv1alpha1.ServiceEndpointStatus{
		ServiceName: network.Name + "-artifacts",
		Port:        8090,
		URL:         "http://" + network.Name + "-artifacts." + network.Namespace + ".svc.cluster.local:8090",
	}
	return network
}

// TestDedicatedFollowerServePathFetchesOverHTTP verifies the serve path renders
// the network-artifacts volume as an emptyDir filled by a sync init container,
// with no ConfigMap and the workload mounts unchanged.
func TestDedicatedFollowerServePathFetchesOverHTTP(t *testing.T) {
	const toolsOverride = "ghcr.io/meigma/yacd/cardano-tools:tilt"

	builder := newDBSyncWorkloadBuilder(t)
	network := withServedArtifacts(readyCardanoNetwork("ready-network"))
	builder.servedArtifactsURL = network.Status.Endpoints.Artifacts.URL
	builder.defaultCardanoToolsImage = toolsOverride
	dbSync := localCardanoDBSync("dbsync", network.Name)
	secret := externalDatabaseSecretFor(dbSync)

	// The serve path passes a nil ConfigMap; the bundle is fetched in-pod.
	resources, err := builder.BuildForDatabase(dbSync, network, nil, secret, dbSyncDatabaseFromExternal(dbSync.Spec.Database.External))
	require.NoError(t, err)

	deployment := resources.Deployment

	// The network-artifacts volume is an emptyDir, not a ConfigMap projection.
	artifactsVolume := requireVolume(t, deployment, networkArtifactsVolumeName)
	assert.NotNil(t, artifactsVolume.EmptyDir, "serve path uses an emptyDir the sync init fills")
	assert.Nil(t, artifactsVolume.ConfigMap, "serve path mounts no ConfigMap")

	// The sync init container runs first (before pgpass), pulls from the serve
	// endpoint, and writes the shared mount.
	require.Len(t, deployment.Spec.Template.Spec.InitContainers, 2)
	assert.Equal(t, dbSyncArtifactsSyncInitName, deployment.Spec.Template.Spec.InitContainers[0].Name, "sync init runs before pgpass init")
	syncInit := requireInitContainer(t, deployment, dbSyncArtifactsSyncInitName)
	assert.Equal(t, toolsOverride, syncInit.Image)
	assert.Equal(t, []string{cardanoToolsBinaryPath}, syncInit.Command)
	assert.Equal(t, []string{
		"sync",
		"--serve-url", network.Status.Endpoints.Artifacts.URL,
		"--output-dir", networkArtifactsMountDir,
	}, syncInit.Args)
	syncMount := requireVolumeMount(t, syncInit, networkArtifactsVolumeName)
	assert.False(t, syncMount.ReadOnly, "sync init writes the fetched bundle")
	assert.Empty(t, syncMount.SubPath)

	// The workload containers read the same path with no subPath, so nothing
	// downstream of the mount changes between transports.
	for _, name := range []string{dbSyncContainerName, followerNodeContainerName} {
		container := requireContainer(t, deployment, name)
		mount := requireVolumeMount(t, container, networkArtifactsVolumeName)
		assert.Equal(t, networkArtifactsMountDir, mount.MountPath)
		assert.Empty(t, mount.SubPath, "%s mounts the artifacts volume root", name)
		assert.True(t, mount.ReadOnly)
	}

	// Identity still comes from the published content hash, unchanged by the
	// transport swap.
	assert.Equal(t, network.Status.Artifacts.DataHash, deployment.Spec.Template.Annotations[dbSyncArtifactDataHashAnno])
}

// TestDedicatedFollowerConfigMapPathHasNoSyncInit verifies the custom-public
// (no serve endpoint) path keeps the ConfigMap projection and adds no sync init.
func TestDedicatedFollowerConfigMapPathHasNoSyncInit(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	network := readyCardanoNetwork("ready-network") // no Artifacts endpoint
	dbSync := localCardanoDBSync("dbsync", network.Name)
	secret := externalDatabaseSecretFor(dbSync)

	resources, err := builder.Build(dbSync, network, artifactConfigMapFor(network), secret)
	require.NoError(t, err)

	deployment := resources.Deployment
	artifactsVolume := requireVolume(t, deployment, networkArtifactsVolumeName)
	require.NotNil(t, artifactsVolume.ConfigMap, "ConfigMap path projects the network-artifacts ConfigMap")
	assert.Equal(t, network.Name+"-network-artifacts", artifactsVolume.ConfigMap.Name)
	assert.Nil(t, artifactsVolume.EmptyDir)

	require.Len(t, deployment.Spec.Template.Spec.InitContainers, 1)
	assert.Equal(t, dbSyncPGPassInitName, deployment.Spec.Template.Spec.InitContainers[0].Name, "no sync init on the ConfigMap path")
}

// TestPrimarySidecarServePathMountsStagedPVC verifies a serve-path attachment
// mounts the primary node-state PVC subdirectory and appends no artifacts
// ConfigMap volume.
func TestPrimarySidecarServePathMountsStagedPVC(t *testing.T) {
	network := withServedArtifacts(readyCardanoNetwork("ready-network"))
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", network.Name))

	resources := PrimarySidecarAttachmentResources{
		ArtifactsStateVolumeName: "localnet-state",
		ArtifactsSubPath:         "artifacts",
		ConfigMapName:            "dbsync-dbsync-config",
		PGPassSecretName:         "dbsync-dbsync-pgpass",
		StatePVCName:             "dbsync-dbsync-state",
		Revision:                 "rev-1",
	}
	attachment, err := BuildPrimarySidecarAttachment(dbSync, network, dbSyncDatabaseFromExternal(dbSync.Spec.Database.External), resources)
	require.NoError(t, err)

	mount := requireVolumeMount(t, attachment.Container, "localnet-state")
	assert.Equal(t, networkArtifactsMountDir, mount.MountPath)
	assert.Equal(t, "artifacts", mount.SubPath, "the sidecar reads the staged artifacts subdirectory")
	assert.True(t, mount.ReadOnly)

	for _, volume := range attachment.Volumes {
		assert.NotEqual(t, networkArtifactsVolumeName, volume.Name, "serve path appends no network-artifacts volume; it reuses the primary state volume")
	}
}

// TestPrimarySidecarConfigMapPathMountsConfigMap verifies the custom-public
// attachment keeps the ConfigMap volume and the default mount.
func TestPrimarySidecarConfigMapPathMountsConfigMap(t *testing.T) {
	network := readyCardanoNetwork("ready-network") // no Artifacts endpoint
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", network.Name))

	resources := PrimarySidecarAttachmentResources{
		NetworkArtifactsConfigMapName: network.Name + "-network-artifacts",
		ConfigMapName:                 "dbsync-dbsync-config",
		PGPassSecretName:              "dbsync-dbsync-pgpass",
		StatePVCName:                  "dbsync-dbsync-state",
		Revision:                      "rev-1",
	}
	attachment, err := BuildPrimarySidecarAttachment(dbSync, network, dbSyncDatabaseFromExternal(dbSync.Spec.Database.External), resources)
	require.NoError(t, err)

	var artifactsVolume *corev1.Volume
	for i := range attachment.Volumes {
		if attachment.Volumes[i].Name == networkArtifactsVolumeName {
			artifactsVolume = &attachment.Volumes[i]
		}
	}
	require.NotNil(t, artifactsVolume, "ConfigMap path appends a network-artifacts volume")
	require.NotNil(t, artifactsVolume.ConfigMap)
	assert.Equal(t, network.Name+"-network-artifacts", artifactsVolume.ConfigMap.Name)

	mount := requireVolumeMount(t, attachment.Container, networkArtifactsVolumeName)
	assert.Equal(t, networkArtifactsMountDir, mount.MountPath)
	assert.Empty(t, mount.SubPath)
}
