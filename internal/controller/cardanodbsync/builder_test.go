package cardanodbsync

import (
	"strings"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	expectedPGPass := "postgres.default.svc.cluster.local:5432:cexplorer:postgres:secret\n"

	resources, err := builder.Build(dbSync, network, artifactConfigMap, secret)

	require.NoError(t, err)
	require.NotNil(t, resources.Plan)
	assert.Equal(t, "dbsync-dbsync-config", resources.ConfigMap.Name)
	assert.Equal(t, "dbsync-dbsync-pgpass", resources.PGPassSecret.Name)
	assert.Equal(t, "dbsync-dbsync-state", resources.PersistentVolumeClaim.Name)
	assert.Equal(t, "dbsync-follower-state", resources.FollowerPersistentVolumeClaim.Name)
	assert.Equal(t, "dbsync-dbsync", resources.Deployment.Name)
	assert.Equal(t, "dbsync-dbsync-metrics", resources.MetricsService.Name)
	assert.Equal(t, resources.Plan.Fingerprint.Value, resources.ConfigMap.Annotations[dbSyncPlanFingerprintAnno])
	assert.Equal(t, resources.Plan.DatabaseIdentityFingerprint.Value, resources.ConfigMap.Annotations[dbSyncDatabaseIdentityAnno])
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower), resources.ConfigMap.Annotations[dbSyncPlacementModeAnno])
	assert.Equal(t, testNetworkArtifactDataHash, resources.ConfigMap.Annotations[dbSyncArtifactDataHashAnno])
	assert.Contains(t, resources.ConfigMap.Data[dbSyncConfigFileName], "NetworkName: ready-network")
	assert.Contains(t, resources.ConfigMap.Data[followerTopologyFileName], `"address": "ready-network-node.default.svc.cluster.local"`)

	storage := resources.PersistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "10Gi", storage.String())
	assert.Equal(t, resources.Plan.DatabaseIdentityFingerprint.Value, resources.PersistentVolumeClaim.Annotations[dbSyncDatabaseIdentityAnno])
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower), resources.PersistentVolumeClaim.Annotations[dbSyncPlacementModeAnno])
	followerStorage := resources.FollowerPersistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "10Gi", followerStorage.String())
	assert.Equal(t, resources.Plan.DatabaseIdentityFingerprint.Value, resources.FollowerPersistentVolumeClaim.Annotations[dbSyncDatabaseIdentityAnno])
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower), resources.FollowerPersistentVolumeClaim.Annotations[dbSyncPlacementModeAnno])
	assert.Equal(t, expectedPGPass, string(resources.PGPassSecret.Data[dbSyncPGPassFileName]))

	deployment := resources.Deployment
	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type)
	require.NotNil(t, deployment.Spec.Template.Spec.SecurityContext)
	assert.Equal(t, dbSyncRunAsID, *deployment.Spec.Template.Spec.SecurityContext.FSGroup)
	assert.Equal(t, dbSyncRunAsID, *deployment.Spec.Template.Spec.SecurityContext.RunAsGroup)
	assert.True(t, *deployment.Spec.Template.Spec.SecurityContext.RunAsNonRoot)
	assert.Equal(t, dbSyncRunAsID, *deployment.Spec.Template.Spec.SecurityContext.RunAsUser)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 2)
	require.Len(t, deployment.Spec.Template.Spec.InitContainers, 1)
	pgPassInit := requireInitContainer(t, deployment, dbSyncPGPassInitName)
	follower := requireContainer(t, deployment, followerNodeContainerName)
	dbSyncContainer := requireContainer(t, deployment, dbSyncContainerName)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.4", follower.Image)
	assert.Equal(t, "ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0", pgPassInit.Image)
	assert.Equal(t, "ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0", dbSyncContainer.Image)
	assert.Equal(t, []string{"/bin/sh", "-eu", "-c"}, pgPassInit.Command)
	assert.Contains(t, pgPassInit.Args[0], "chmod 0600 /configuration/pgpass")
	require.NotNil(t, follower.SecurityContext)
	require.NotNil(t, pgPassInit.SecurityContext)
	require.NotNil(t, dbSyncContainer.SecurityContext)
	assert.True(t, *follower.SecurityContext.RunAsNonRoot)
	assert.True(t, *pgPassInit.SecurityContext.RunAsNonRoot)
	assert.True(t, *dbSyncContainer.SecurityContext.RunAsNonRoot)
	assert.Equal(t, dbSyncRunAsID, *follower.SecurityContext.RunAsUser)
	assert.Equal(t, dbSyncRunAsID, *pgPassInit.SecurityContext.RunAsUser)
	assert.Equal(t, dbSyncRunAsID, *dbSyncContainer.SecurityContext.RunAsUser)
	assert.Equal(t, dbSyncRunAsID, *follower.SecurityContext.RunAsGroup)
	assert.Equal(t, dbSyncRunAsID, *pgPassInit.SecurityContext.RunAsGroup)
	assert.Equal(t, dbSyncRunAsID, *dbSyncContainer.SecurityContext.RunAsGroup)
	assert.Contains(t, follower.Args, "/network-artifacts/configuration.yaml")
	assert.Empty(t, dbSyncContainer.Command)
	assert.NotContains(t, dbSyncContainer.Args, "--schema-dir")
	assert.NotContains(t, dbSyncContainer.Args, "--state-dir")
	assert.Equal(t, resources.Plan.Fingerprint.Value, deployment.Spec.Template.Annotations[dbSyncPlanFingerprintAnno])
	assert.Equal(t, resources.Plan.DatabaseIdentityFingerprint.Value, deployment.Spec.Template.Annotations[dbSyncDatabaseIdentityAnno])
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower), deployment.Spec.Template.Annotations[dbSyncPlacementModeAnno])
	assert.Equal(t, testNetworkArtifactDataHash, deployment.Spec.Template.Annotations[dbSyncArtifactDataHashAnno])
	assert.Equal(t, pgPassMaterialFingerprint(expectedPGPass), deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])
	assert.Equal(t, pgPassMaterialFingerprint(expectedPGPass), resources.PGPassSecret.Annotations[dbSyncSecretVersionAnno])
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower), resources.PGPassSecret.Annotations[dbSyncPlacementModeAnno])

	assert.Equal(t, "/configuration/pgpass", requireEnvVar(t, dbSyncContainer, "PGPASSFILE").Value)
	assert.Empty(t, envVarValue(dbSyncContainer, "PGPASSWORD"))
	assert.Contains(t, dbSyncContainer.Args, "--pg-pass-env")
	assert.Contains(t, dbSyncContainer.Args, "PGPASSFILE")
	assert.Equal(t, dbSyncPGPassSecretName(dbSync), requireVolume(t, deployment, dbSyncPGPassSecretVolumeName).Secret.SecretName)
	assert.Equal(t, int32(0o440), *requireVolume(t, deployment, dbSyncPGPassSecretVolumeName).Secret.DefaultMode)
	require.NotNil(t, requireVolume(t, deployment, dbSyncPGPassVolumeName).EmptyDir)
	assert.Equal(t, dbSyncFollowerPVCName(dbSync), requireVolume(t, deployment, followerNodeStateVolumeName).PersistentVolumeClaim.ClaimName)
	assert.Equal(t, dbSyncNodeDatabaseDir, requireVolumeMount(t, follower, followerNodeStateVolumeName).MountPath)
	assert.Equal(t, dbSyncPGPassSecretMountDir, requireVolumeMount(t, pgPassInit, dbSyncPGPassSecretVolumeName).MountPath)
	assert.Equal(t, dbSyncPGPassMountDir, requireVolumeMount(t, pgPassInit, dbSyncPGPassVolumeName).MountPath)
	assert.Equal(t, dbSyncPGPassMountDir, requireVolumeMount(t, dbSyncContainer, dbSyncPGPassVolumeName).MountPath)
	assert.Equal(t, dbSyncStateMountDir, requireVolumeMount(t, dbSyncContainer, dbSyncStateVolumeName).MountPath)
	assert.Equal(t, int32(8080), resources.MetricsService.Spec.Ports[0].Port)
	assert.Equal(t, dbSyncWorkloadSelectorLabels(dbSync), resources.MetricsService.Spec.Selector)
}

func TestDBSyncWorkloadBuilderBuildsPrimarySidecarMaterial(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := primarySidecarCardanoDBSync(localCardanoDBSync("dbsync", "ready-network"))
	network := readyCardanoNetwork("ready-network")
	secret := externalDatabaseSecretFor(dbSync)
	expectedPGPass := "postgres.default.svc.cluster.local:5432:cexplorer:postgres:secret\n"

	resources, err := builder.BuildPrimarySidecarForDatabase(dbSync, network, artifactConfigMapFor(network), secret, dbSyncDatabaseFromExternal(dbSync.Spec.Database.External))

	require.NoError(t, err)
	require.NotNil(t, resources.Plan)
	assert.Equal(t, "dbsync-dbsync-config", resources.ConfigMap.Name)
	assert.Equal(t, "dbsync-dbsync-pgpass", resources.PGPassSecret.Name)
	assert.Equal(t, "dbsync-dbsync-state", resources.PersistentVolumeClaim.Name)
	assert.Equal(t, "dbsync-dbsync-metrics", resources.MetricsService.Name)
	assert.Equal(t, resources.Plan.Fingerprint.Value, resources.ConfigMap.Annotations[dbSyncPlanFingerprintAnno])
	assert.Equal(t, resources.Plan.DatabaseIdentityFingerprint.Value, resources.ConfigMap.Annotations[dbSyncDatabaseIdentityAnno])
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar), resources.ConfigMap.Annotations[dbSyncPlacementModeAnno])
	assert.Equal(t, testNetworkArtifactDataHash, resources.ConfigMap.Annotations[dbSyncArtifactDataHashAnno])
	assert.Equal(t, resources.Plan.DatabaseIdentityFingerprint.Value, resources.PersistentVolumeClaim.Annotations[dbSyncDatabaseIdentityAnno])
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar), resources.PersistentVolumeClaim.Annotations[dbSyncPlacementModeAnno])
	assert.Equal(t, expectedPGPass, string(resources.PGPassSecret.Data[dbSyncPGPassFileName]))
	assert.Equal(t, pgPassMaterialFingerprint(expectedPGPass), resources.PGPassSecret.Annotations[dbSyncSecretVersionAnno])
	assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar), resources.PGPassSecret.Annotations[dbSyncPlacementModeAnno])
	assert.Equal(t, primarySidecarMetricsSelectorLabels(dbSync, network), resources.MetricsService.Spec.Selector)
}

func TestDBSyncWorkloadBuilderBuildsManagedPostgresResources(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	dbSync.UID = types.UID("dbsync-uid")
	authSecret := &corev1.Secret{}
	authSecret.Name = managedPostgresAuthSecretName(dbSync)
	authSecret.Namespace = dbSync.Namespace
	authSecret.ResourceVersion = "11"
	authSecret.Data = map[string][]byte{
		managedPostgresPasswordKey: []byte("managed-secret"),
	}
	authSecret.Annotations = map[string]string{
		managedPostgresPasswordFingerprintAnno: managedPostgresPasswordFingerprint(authSecret.Data[managedPostgresPasswordKey]),
	}

	resources, err := builder.managedPostgresResources(dbSync, authSecret)

	require.NoError(t, err)
	assert.NotEmpty(t, resources.IdentityFingerprint)
	assert.Equal(t, "dbsync-postgres-state", resources.PersistentVolumeClaim.Name)
	assert.Equal(t, resources.IdentityFingerprint, resources.PersistentVolumeClaim.Annotations[managedPostgresIdentityAnno])
	storage := resources.PersistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "10Gi", storage.String())
	assert.Equal(t, "dbsync-postgres", resources.Service.Name)
	assert.Equal(t, corev1.ServiceTypeClusterIP, resources.Service.Spec.Type)
	assert.Equal(t, managedPostgresSelectorLabels(dbSync), resources.Service.Spec.Selector)
	assert.Equal(t, int32(5432), resources.Service.Spec.Ports[0].Port)
	assert.Equal(t, intstr.FromString(managedPostgresPortName), resources.Service.Spec.Ports[0].TargetPort)

	deployment := resources.Deployment
	assert.Equal(t, "dbsync-postgres", deployment.Name)
	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, deployment.Spec.Strategy.Type)
	require.NotNil(t, deployment.Spec.Template.Spec.SecurityContext)
	assert.Equal(t, managedPostgresRunAsID, *deployment.Spec.Template.Spec.SecurityContext.RunAsUser)
	assert.Equal(t, managedPostgresRunAsID, *deployment.Spec.Template.Spec.SecurityContext.RunAsGroup)
	assert.Equal(t, managedPostgresRunAsID, *deployment.Spec.Template.Spec.SecurityContext.FSGroup)
	assert.Equal(t, resources.IdentityFingerprint, deployment.Spec.Template.Annotations[managedPostgresIdentityAnno])
	assert.Equal(t, authSecret.Annotations[managedPostgresPasswordFingerprintAnno], deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])
	postgres := requireContainer(t, deployment, managedPostgresContainerName)
	assert.Equal(t, defaultManagedPostgresImage, postgres.Image)
	assert.Equal(t, managedPostgresRunAsID, *postgres.SecurityContext.RunAsUser)
	assert.Equal(t, managedPostgresRunAsID, *postgres.SecurityContext.RunAsGroup)
	assert.Equal(t, "cexplorer", requireEnvVar(t, postgres, "POSTGRES_DB").Value)
	assert.Equal(t, "postgres", requireEnvVar(t, postgres, "POSTGRES_USER").Value)
	assert.Equal(t, managedPostgresAuthSecretName(dbSync), requireEnvVar(t, postgres, "POSTGRES_PASSWORD").ValueFrom.SecretKeyRef.Name)
	assert.Equal(t, managedPostgresPasswordKey, requireEnvVar(t, postgres, "POSTGRES_PASSWORD").ValueFrom.SecretKeyRef.Key)
	assert.Equal(t, managedPostgresDataDir, requireEnvVar(t, postgres, "PGDATA").Value)
	require.NotNil(t, postgres.ReadinessProbe)
	assert.Contains(t, postgres.ReadinessProbe.Exec.Command, "pg_isready")
	require.NotNil(t, postgres.StartupProbe)
	assert.Equal(t, managedPostgresPVCName(dbSync), requireVolume(t, deployment, managedPostgresDataVolume).PersistentVolumeClaim.ClaimName)
	assert.Equal(t, managedPostgresDataMountDir, requireVolumeMount(t, postgres, managedPostgresDataVolume).MountPath)
}

func TestDBSyncWorkloadBuilderBuildsDBSyncWorkloadForManagedPostgres(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := managedCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	authSecret := &corev1.Secret{}
	authSecret.Name = managedPostgresAuthSecretName(dbSync)
	authSecret.Namespace = dbSync.Namespace
	authSecret.ResourceVersion = "12"
	authSecret.Data = map[string][]byte{
		managedPostgresPasswordKey: []byte("managed-secret"),
	}
	expectedPGPass := "dbsync-postgres.default.svc.cluster.local:5432:cexplorer:postgres:managed-secret\n"

	resources, err := builder.BuildForDatabase(dbSync, network, artifactConfigMapFor(network), authSecret, dbSyncDatabaseFromManaged(dbSync, authSecret.Name))

	require.NoError(t, err)
	assert.Equal(t, expectedPGPass, string(resources.PGPassSecret.Data[dbSyncPGPassFileName]))
	assert.Equal(t, "dbsync-postgres.default.svc.cluster.local", requireEnvVar(t, requireContainer(t, resources.Deployment, dbSyncContainerName), "PGHOST").Value)
	assert.Equal(t, "disable", requireEnvVar(t, requireContainer(t, resources.Deployment, dbSyncContainerName), "PGSSLMODE").Value)
	assert.Equal(t, pgPassMaterialFingerprint(expectedPGPass), resources.Deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])
}

func TestDBSyncWorkloadBuilderCredentialFingerprintTracksPGPassMaterial(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	artifactConfigMap := artifactConfigMapFor(network)
	secret := externalDatabaseSecretFor(dbSync)
	secret.ResourceVersion = "7"

	base, err := builder.Build(dbSync, network, artifactConfigMap, secret)
	require.NoError(t, err)
	baseFingerprint := base.Deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno]
	require.NotEmpty(t, baseFingerprint)

	secret.ResourceVersion = "8"
	metadataOnly, err := builder.Build(dbSync, network, artifactConfigMap, secret)
	require.NoError(t, err)
	assert.Equal(t, baseFingerprint, metadataOnly.Deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])
	assert.Equal(t, baseFingerprint, metadataOnly.PGPassSecret.Annotations[dbSyncSecretVersionAnno])

	secret.Data[externalDatabasePasswordKey(dbSync.Spec.Database.External)] = []byte("rotated-secret")
	rotated, err := builder.Build(dbSync, network, artifactConfigMap, secret)
	require.NoError(t, err)
	assert.NotEqual(t, baseFingerprint, rotated.Deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno])
	assert.Equal(t, rotated.Deployment.Spec.Template.Annotations[dbSyncSecretVersionAnno], rotated.PGPassSecret.Annotations[dbSyncSecretVersionAnno])
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
	assert.Equal(t, base.Plan.DatabaseIdentityFingerprint, changed.Plan.DatabaseIdentityFingerprint)
	assert.NotEqual(t, base.Deployment.Spec.Template.Annotations[dbSyncPlanFingerprintAnno], changed.Deployment.Spec.Template.Annotations[dbSyncPlanFingerprintAnno])
	assert.Equal(t, base.Deployment.Spec.Template.Annotations[dbSyncDatabaseIdentityAnno], changed.Deployment.Spec.Template.Annotations[dbSyncDatabaseIdentityAnno])
	assert.Equal(t, changed.Plan.Fingerprint.Value, changed.Deployment.Spec.Template.Annotations[dbSyncPlanFingerprintAnno])
	assert.Contains(t, requireContainer(t, changed.Deployment, dbSyncContainerName).Args, "--force-indexes")
}

func TestDBSyncWorkloadBuilderDatabaseIdentityIncludesDBSyncImage(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")
	artifactConfigMap := artifactConfigMapFor(network)
	secret := externalDatabaseSecretFor(dbSync)

	base, err := builder.Build(dbSync, network, artifactConfigMap, secret)
	require.NoError(t, err)

	dbSync.Spec.Image = "ghcr.io/intersectmbo/cardano-db-sync:13.8.0.0"
	changed, err := builder.Build(dbSync, network, artifactConfigMap, secret)

	require.NoError(t, err)
	assert.NotEqual(t, base.Plan.Fingerprint, changed.Plan.Fingerprint)
	assert.NotEqual(t, base.Plan.DatabaseIdentityFingerprint, changed.Plan.DatabaseIdentityFingerprint)
	assert.NotEqual(t, base.Deployment.Spec.Template.Annotations[dbSyncDatabaseIdentityAnno], changed.Deployment.Spec.Template.Annotations[dbSyncDatabaseIdentityAnno])
	assert.Equal(t, "ghcr.io/intersectmbo/cardano-db-sync:13.8.0.0", requireContainer(t, changed.Deployment, dbSyncContainerName).Image)
}

func TestDBSyncWorkloadBuilderUsesFollowerStorageAndIPFSGateways(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	storageClassName := "fast"
	storageSize := resource.MustParse("25Gi")
	dbSync.Spec.FollowerNode = &yacdv1alpha1.CardanoDBSyncFollowerNodeSpec{
		Storage: &yacdv1alpha1.CardanoDBSyncStorageSpec{
			Size:             &storageSize,
			StorageClassName: &storageClassName,
		},
	}
	dbSync.Spec.Config.IPFSGateways = []string{"https://ipfs.example.test"}
	network := readyCardanoNetwork("ready-network")

	resources, err := builder.Build(dbSync, network, artifactConfigMapFor(network), externalDatabaseSecretFor(dbSync))

	require.NoError(t, err)
	storage := resources.FollowerPersistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "25Gi", storage.String())
	require.NotNil(t, resources.FollowerPersistentVolumeClaim.Spec.StorageClassName)
	assert.Equal(t, storageClassName, *resources.FollowerPersistentVolumeClaim.Spec.StorageClassName)
	assert.Contains(t, resources.ConfigMap.Data[dbSyncConfigFileName], "ipfs_gateway:")
	assert.Contains(t, resources.ConfigMap.Data[dbSyncConfigFileName], "- https://ipfs.example.test")
}

func TestDBSyncWorkloadBuilderDefaultsStorageSizeWithStorageClassOnly(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	stateStorageClassName := "fast-state"
	followerStorageClassName := "fast-follower"
	dbSync.Spec.StateStorage = &yacdv1alpha1.CardanoDBSyncStorageSpec{
		StorageClassName: &stateStorageClassName,
	}
	dbSync.Spec.FollowerNode = &yacdv1alpha1.CardanoDBSyncFollowerNodeSpec{
		Storage: &yacdv1alpha1.CardanoDBSyncStorageSpec{
			StorageClassName: &followerStorageClassName,
		},
	}
	network := readyCardanoNetwork("ready-network")

	resources, err := builder.Build(dbSync, network, artifactConfigMapFor(network), externalDatabaseSecretFor(dbSync))

	require.NoError(t, err)
	stateStorage := resources.PersistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "10Gi", stateStorage.String())
	require.NotNil(t, resources.PersistentVolumeClaim.Spec.StorageClassName)
	assert.Equal(t, stateStorageClassName, *resources.PersistentVolumeClaim.Spec.StorageClassName)
	followerStorage := resources.FollowerPersistentVolumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "10Gi", followerStorage.String())
	require.NotNil(t, resources.FollowerPersistentVolumeClaim.Spec.StorageClassName)
	assert.Equal(t, followerStorageClassName, *resources.FollowerPersistentVolumeClaim.Spec.StorageClassName)
}

func TestDBSyncWorkloadBuilderInsertPresetsDoNotUseDefaultedOverrides(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	network := readyCardanoNetwork("ready-network")

	dbSync := localCardanoDBSync("disable-all", "ready-network")
	dbSync.Spec.Config.Insert = &yacdv1alpha1.CardanoDBSyncInsertSpec{
		Preset: yacdv1alpha1.CardanoDBSyncInsertPresetDisableAll,
	}
	resources, err := builder.Build(dbSync, network, artifactConfigMapFor(network), externalDatabaseSecretFor(dbSync))

	require.NoError(t, err)
	config := resources.ConfigMap.Data[dbSyncConfigFileName]
	assert.Contains(t, config, "ledger: disable")
	assert.Contains(t, config, "governance: disable")
	assert.Contains(t, config, "pool_stat: disable")

	dbSync = localCardanoDBSync("full", "ready-network")
	dbSync.Spec.Config.Insert = &yacdv1alpha1.CardanoDBSyncInsertSpec{
		Preset: yacdv1alpha1.CardanoDBSyncInsertPresetFull,
	}
	resources, err = builder.Build(dbSync, network, artifactConfigMapFor(network), externalDatabaseSecretFor(dbSync))

	require.NoError(t, err)
	assert.Contains(t, resources.ConfigMap.Data[dbSyncConfigFileName], "pool_stat: enable")
}

func TestDBSyncWorkloadBuilderPreservesNestedPresetValuesUnlessOverridden(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("utxo", "ready-network")
	dbSync.Spec.Config.LedgerBackend = yacdv1alpha1.CardanoDBSyncLedgerBackendInMemory
	forceTxIn := true
	dbSync.Spec.Config.Insert = &yacdv1alpha1.CardanoDBSyncInsertSpec{
		Preset: yacdv1alpha1.CardanoDBSyncInsertPresetOnlyUTxO,
		TxOut: &yacdv1alpha1.CardanoDBSyncTxOutSpec{
			ForceTxIn: &forceTxIn,
		},
	}
	network := readyCardanoNetwork("ready-network")

	resources, err := builder.Build(dbSync, network, artifactConfigMapFor(network), externalDatabaseSecretFor(dbSync))

	require.NoError(t, err)
	config := resources.ConfigMap.Data[dbSyncConfigFileName]
	assert.Contains(t, config, "value: bootstrap")
	assert.Contains(t, config, "force_tx_in: true")
}

func TestDBSyncWorkloadBuilderAppliesExplicitInsertOverrides(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	txCBOR := true
	governance := false
	poolStats := false
	removeJSONBFromSchema := true
	ledger := yacdv1alpha1.CardanoDBSyncLedgerModeIgnore
	jsonType := yacdv1alpha1.CardanoDBSyncJSONTypeJSONB
	dbSync.Spec.Config.Insert = &yacdv1alpha1.CardanoDBSyncInsertSpec{
		Preset:                yacdv1alpha1.CardanoDBSyncInsertPresetFull,
		TxCBOR:                &txCBOR,
		Ledger:                &ledger,
		Governance:            &governance,
		PoolStats:             &poolStats,
		JSONType:              &jsonType,
		RemoveJSONBFromSchema: &removeJSONBFromSchema,
	}
	network := readyCardanoNetwork("ready-network")

	resources, err := builder.Build(dbSync, network, artifactConfigMapFor(network), externalDatabaseSecretFor(dbSync))

	require.NoError(t, err)
	config := resources.ConfigMap.Data[dbSyncConfigFileName]
	assert.Contains(t, config, "tx_cbor: enable")
	assert.Contains(t, config, "ledger: ignore")
	assert.Contains(t, config, "governance: disable")
	assert.Contains(t, config, "pool_stat: disable")
	assert.Contains(t, config, "json_type: jsonb")
	assert.Contains(t, config, "remove_jsonb_from_schema: enable")
}

func TestDBSyncWorkloadBuilderEscapesPGPassFields(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("dbsync", "ready-network")
	dbSync.Spec.Database.External.Host = "postgres:rw.default.svc.cluster.local"
	dbSync.Spec.Database.External.User = `post\gres`
	network := readyCardanoNetwork("ready-network")
	secret := externalDatabaseSecretFor(dbSync)
	secret.Data["password"] = []byte(`sec:ret\value`)

	resources, err := builder.Build(dbSync, network, artifactConfigMapFor(network), secret)

	require.NoError(t, err)
	assert.Equal(t, `postgres\:rw.default.svc.cluster.local:5432:cexplorer:post\\gres:sec\:ret\\value`+"\n", string(resources.PGPassSecret.Data[dbSyncPGPassFileName]))
}

func TestDBSyncWorkloadBuilderRejectsNewlinePGPassFields(t *testing.T) {
	testCases := []struct {
		name    string
		mutate  func(*yacdv1alpha1.CardanoDBSync, *corev1.Secret)
		wantErr string
	}{
		{
			name: "host",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync, secret *corev1.Secret) {
				dbSync.Spec.Database.External.Host = "postgres\nrw.default.svc.cluster.local"
			},
			wantErr: "host cannot contain newlines",
		},
		{
			name: "database",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync, secret *corev1.Secret) {
				dbSync.Spec.Database.External.Database = "cexplorer\nother"
			},
			wantErr: "database cannot contain newlines",
		},
		{
			name: "user",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync, secret *corev1.Secret) {
				dbSync.Spec.Database.External.User = "postgres\nother"
			},
			wantErr: "user cannot contain newlines",
		},
		{
			name: "password",
			mutate: func(dbSync *yacdv1alpha1.CardanoDBSync, secret *corev1.Secret) {
				secret.Data["password"] = []byte("line-one\nline-two")
			},
			wantErr: "password cannot contain newlines",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			builder := newDBSyncWorkloadBuilder(t)
			dbSync := localCardanoDBSync("dbsync", "ready-network")
			network := readyCardanoNetwork("ready-network")
			secret := externalDatabaseSecretFor(dbSync)
			testCase.mutate(dbSync, secret)

			_, err := builder.Build(dbSync, network, artifactConfigMapFor(network), secret)

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

func TestDBSyncWorkloadBuilderUsesSafeResourceAndLabelNames(t *testing.T) {
	builder := newDBSyncWorkloadBuilder(t)
	dbSync := localCardanoDBSync("db.sync."+strings.Repeat("x", 80), "ready-network")
	network := readyCardanoNetwork("ready-network")

	resources, err := builder.Build(dbSync, network, artifactConfigMapFor(network), externalDatabaseSecretFor(dbSync))

	require.NoError(t, err)
	authSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: managedPostgresAuthSecretName(dbSync), Namespace: dbSync.Namespace},
		Data: map[string][]byte{
			managedPostgresPasswordKey: []byte("managed-secret"),
		},
	}
	authSecret.Annotations = map[string]string{
		managedPostgresPasswordFingerprintAnno: managedPostgresPasswordFingerprint(authSecret.Data[managedPostgresPasswordKey]),
	}
	managedResources, err := builder.managedPostgresResources(managedCardanoDBSync(dbSync.Name, "ready-network"), authSecret)
	require.NoError(t, err)
	for _, name := range []string{
		resources.ConfigMap.Name,
		resources.PGPassSecret.Name,
		resources.PersistentVolumeClaim.Name,
		resources.FollowerPersistentVolumeClaim.Name,
		resources.Deployment.Name,
		resources.MetricsService.Name,
		managedPostgresAuthSecretName(dbSync),
		managedResources.PersistentVolumeClaim.Name,
		managedResources.Service.Name,
		managedResources.Deployment.Name,
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

// TestFollowerNodeImageHonorsInjectedOverride verifies the
// Reconciler-injected defaultCardanoTestnetImage replaces the legacy
// "<repo>:<networkNodeVersion>-<revision>" reference on the follower-node
// container so the local dev stack picks up post-release publisher
// changes that the published cardano-testnet tag does not yet contain.
func TestFollowerNodeImageHonorsInjectedOverride(t *testing.T) {
	const override = "ghcr.io/meigma/yacd/cardano-testnet:tilt"

	builder := newDBSyncWorkloadBuilder(t)
	builder.defaultCardanoTestnetImage = override

	dbSync := localCardanoDBSync("dbsync", "ready-network")
	network := readyCardanoNetwork("ready-network")

	assert.Equal(t, override, builder.followerNodeImage(dbSync, network))
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

func requireInitContainer(t *testing.T, deployment *appsv1.Deployment, name string) corev1.Container {
	t.Helper()

	for _, container := range deployment.Spec.Template.Spec.InitContainers {
		if container.Name == name {
			return container
		}
	}
	require.Failf(t, "missing init container", "expected init container %s", name)
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

func envVarValue(container corev1.Container, name string) string {
	for _, env := range container.Env {
		if env.Name == name {
			return env.Value
		}
	}
	return ""
}

func requireVolume(t *testing.T, deployment *appsv1.Deployment, name string) corev1.Volume {
	t.Helper()

	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == name {
			return volume
		}
	}
	require.Failf(t, "missing volume", "expected volume %s", name)
	return corev1.Volume{}
}

func requireVolumeMount(t *testing.T, container corev1.Container, name string) corev1.VolumeMount {
	t.Helper()

	for _, mount := range container.VolumeMounts {
		if mount.Name == name {
			return mount
		}
	}
	require.Failf(t, "missing volume mount", "expected volume mount %s", name)
	return corev1.VolumeMount{}
}
