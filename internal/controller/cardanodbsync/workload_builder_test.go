package cardanodbsync

import (
	"strings"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func TestDBSyncWorkloadBuilderBuildsExternalDatabaseWorkload(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	dbSync.UID = types.UID("dbsync-uid")
	network := readyCardanoNetwork("ready-network")
	artifactConfigMap := artifactConfigMapFor(network)
	artifactConfigMap.UID = types.UID("artifact-uid")
	secret := externalDatabaseSecretFor(dbSync)
	secret.ResourceVersion = "7"

	resources, err := builder.Build(dbSync, network, artifactConfigMap, secret)

	require.NoError(t, err)
	require.NotNil(t, resources.Plan)
	assert.Equal(t, "dbsync-dbsync-config", resources.ConfigMap.Name)
	assert.Equal(t, "dbsync-dbsync-state", resources.PersistentVolumeClaim.Name)
	assert.Equal(t, "dbsync-dbsync", resources.Deployment.Name)
	assert.Equal(t, "dbsync-dbsync-metrics", resources.MetricsService.Name)
	assert.Equal(t, resources.Plan.Fingerprint.Value, resources.ConfigMap.Annotations[dbSyncPlanFingerprintAnno])
	assert.Equal(t, testNetworkArtifactDataHash, resources.ConfigMap.Annotations[dbSyncArtifactDataHashAnno])
	assert.Contains(t, resources.ConfigMap.Data[dbSyncConfigFileName], "NetworkName: ready-network")
	assert.Contains(t, resources.ConfigMap.Data[followerTopologyFileName], `"address": "ready-network-node.default.svc.cluster.local"`)

	storage := resources.PersistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "10Gi", storage.String())

	deployment := resources.Deployment
	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 2)
	follower := requireContainer(t, deployment, followerNodeContainerName)
	dbSyncContainer := requireContainer(t, deployment, dbSyncContainerName)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.3", follower.Image)
	assert.Equal(t, "ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0", dbSyncContainer.Image)
	assert.Contains(t, follower.Args, "/network-artifacts/configuration.yaml")
	assert.Contains(t, dbSyncContainer.Args, "--schema-dir")
	assert.Contains(t, dbSyncContainer.Args, "/opt/cardano-db-sync/schema")
	assert.Equal(t, resources.Plan.Fingerprint.Value, deployment.Spec.Template.Annotations[dbSyncPlanFingerprintAnno])
	assert.Equal(t, testNetworkArtifactDataHash, deployment.Spec.Template.Annotations[dbSyncArtifactDataHashAnno])
	assert.Equal(t, "7", deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])

	passwordEnv := requireEnvVar(t, dbSyncContainer, "PGPASSWORD")
	require.NotNil(t, passwordEnv.ValueFrom)
	require.NotNil(t, passwordEnv.ValueFrom.SecretKeyRef)
	assert.Equal(t, "dbsync-postgres", passwordEnv.ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, "password", passwordEnv.ValueFrom.SecretKeyRef.Key)
	assert.Equal(t, int32(8080), resources.MetricsService.Spec.Ports[0].Port)
	assert.Equal(t, dbSyncWorkloadSelectorLabels(dbSync), resources.MetricsService.Spec.Selector)
}

func TestDBSyncWorkloadBuilderFingerprintChangesWithRuntimeConfig(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	artifactConfigMap := artifactConfigMapFor(network)
	secret := externalDatabaseSecretFor(dbSync)

	base, err := builder.Build(dbSync, network, artifactConfigMap, secret)
	require.NoError(t, err)

	dbSync.Spec.Config.Runtime = &yacdv1alpha1.CardanoDBSyncRuntimeSpec{
		Cache:        true,
		EpochTable:   true,
		ForceIndexes: true,
		MetricsPort:  8080,
	}
	changed, err := builder.Build(dbSync, network, artifactConfigMap, secret)

	require.NoError(t, err)
	assert.NotEqual(t, base.Plan.Fingerprint, changed.Plan.Fingerprint)
	assert.NotEqual(t, base.Deployment.Spec.Template.Annotations[dbSyncPlanFingerprintAnno], changed.Deployment.Spec.Template.Annotations[dbSyncPlanFingerprintAnno])
	assert.Equal(t, changed.Plan.Fingerprint.Value, changed.Deployment.Spec.Template.Annotations[dbSyncPlanFingerprintAnno])
	assert.Contains(t, requireContainer(t, changed.Deployment, dbSyncContainerName).Args, "--force-indexes")
}

func TestDBSyncWorkloadBuilderUsesSafeResourceAndLabelNames(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("db.sync."+strings.Repeat("x", 80), "ready-network")
	network := readyCardanoNetwork("ready-network")

	resources, err := builder.Build(dbSync, network, artifactConfigMapFor(network), externalDatabaseSecretFor(dbSync))

	require.NoError(t, err)
	for _, name := range []string{
		resources.ConfigMap.Name,
		resources.PersistentVolumeClaim.Name,
		resources.Deployment.Name,
		resources.MetricsService.Name,
	} {
		assert.LessOrEqual(t, len(name), 63)
		assert.NotContains(t, name, ".")
	}
	for _, value := range resources.Deployment.Spec.Selector.MatchLabels {
		assert.LessOrEqual(t, len(value), 63)
	}
}

func newDBSyncWorkloadBuilder(t *testing.T) dbSyncWorkloadBuilder {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	return dbSyncWorkloadBuilder{scheme: scheme}
}

func requireContainer(t *testing.T, deployment *appsv1.Deployment, name string) corev1.Container {
	t.Helper()

	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == name {
			return container
		}
	}
	require.Failf(t, "missing container", "expected container %s", name)
	return corev1.Container{}
}

func requireEnvVar(t *testing.T, container corev1.Container, name string) corev1.EnvVar {
	t.Helper()

	for _, env := range container.Env {
		if env.Name == name {
			return env
		}
	}
	require.Failf(t, "missing env var", "expected env var %s", name)
	return corev1.EnvVar{}
}
