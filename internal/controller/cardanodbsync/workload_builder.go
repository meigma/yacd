package cardanodbsync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	dbSyncNameSuffix = "dbsync"

	dbSyncContainerName       = "cardano-db-sync"
	followerNodeContainerName = "follower-node"
	dbSyncMetricsPortName     = "metrics"
	followerNodePortName      = "node-to-node"

	dbSyncConfigMapSuffix    = "dbsync-config"
	dbSyncStatePVCSuffix     = "dbsync-state"
	dbSyncFollowerPVCSuffix  = "follower-state"
	dbSyncPGPassSecretSuffix = "dbsync-pgpass"
	dbSyncMetricsSuffix      = "dbsync-metrics"

	dbSyncConfigMapVolumeName         = "dbsync-config"
	networkArtifactsVolumeName        = "network-artifacts"
	dbSyncStateVolumeName             = "dbsync-state"
	followerNodeStateVolumeName       = "follower-state"
	nodeIPCVolumeName                 = "node-ipc"
	dbSyncTmpVolumeName               = "dbsync-tmp"
	dbSyncPGPassVolumeName            = "dbsync-pgpass"
	dbSyncConfigMountDir              = "/config"
	networkArtifactsMountDir          = "/network-artifacts"
	dbSyncStateMountDir               = "/var/lib/cexplorer"
	dbSyncTmpMountDir                 = "/tmp"
	dbSyncPGPassMountDir              = "/configuration"
	dbSyncNodeDatabaseDir             = "/state/node-db"
	dbSyncNodeSocketDir               = "/ipc"
	dbSyncNodeSocketPath              = "/ipc/node.socket"
	dbSyncNodeHostAddress             = "0.0.0.0"
	dbSyncNodePort              int32 = 3001

	dbSyncConfigFileName       = "db-sync-config.yaml"
	followerTopologyFileName   = "follower-topology.json"
	dbSyncPGPassFileName       = "pgpass"
	dbSyncPlanFingerprintAnno  = "yacd.meigma.io/dbsync-plan-fingerprint"
	dbSyncDatabaseIdentityAnno = "yacd.meigma.io/dbsync-database-identity"
	dbSyncSecretVersionAnno    = "yacd.meigma.io/external-database-secret-resource-version"
	dbSyncArtifactDataHashAnno = "yacd.meigma.io/network-artifact-data-hash"

	labelAppName       = "app.kubernetes.io/name"
	labelAppInstance   = "app.kubernetes.io/instance"
	labelAppComponent  = "app.kubernetes.io/component"
	labelAppManagedBy  = "app.kubernetes.io/managed-by"
	labelDBSync        = "yacd.meigma.io/cardanodbsync"
	labelCardanoRole   = "yacd.meigma.io/role"
	labelDBSyncAppName = "cardano-db-sync"
	labelDBSyncRole    = "dbsync"

	defaultFollowerNodeImageRepository = "ghcr.io/meigma/yacd/cardano-testnet"
	defaultFollowerNodeImageRevision   = "yacd.3"
	defaultFollowerNodeStorageSize     = "10Gi"
	maxLabelValueLength                = 63
	safeNameHashLength                 = 10
)

type dbSyncWorkloadResources struct {
	ConfigMap                     *corev1.ConfigMap
	PGPassSecret                  *corev1.Secret
	PersistentVolumeClaim         *corev1.PersistentVolumeClaim
	FollowerPersistentVolumeClaim *corev1.PersistentVolumeClaim
	Deployment                    *appsv1.Deployment
	MetricsService                *corev1.Service
	Plan                          dbsync.Plan
}

type dbSyncWorkloadBuilder struct {
	scheme *runtime.Scheme
}

func (b dbSyncWorkloadBuilder) Build(
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	networkArtifacts *corev1.ConfigMap,
	externalDatabaseSecret *corev1.Secret,
) (*dbSyncWorkloadResources, error) {
	if dbSync == nil {
		return nil, fmt.Errorf("cardanodbsync is required")
	}
	external := dbSync.Spec.Database.External
	if external == nil {
		return nil, unsupportedSpec("external database spec is required")
	}

	return b.BuildForDatabase(dbSync, network, networkArtifacts, externalDatabaseSecret, dbSyncDatabaseFromExternal(external))
}

func (b dbSyncWorkloadBuilder) BuildForDatabase(
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	networkArtifacts *corev1.ConfigMap,
	databaseSecret *corev1.Secret,
	database dbsync.Database,
) (*dbSyncWorkloadResources, error) {
	if dbSync == nil {
		return nil, fmt.Errorf("cardanodbsync is required")
	}
	if network == nil {
		return nil, fmt.Errorf("cardanonetwork is required")
	}
	if networkArtifacts == nil {
		return nil, fmt.Errorf("network artifacts ConfigMap is required")
	}
	if databaseSecret == nil {
		return nil, fmt.Errorf("database credential Secret is required")
	}
	if b.scheme == nil {
		return nil, fmt.Errorf("scheme is required")
	}

	spec, err := b.planSpec(dbSync, network, database)
	if err != nil {
		return nil, err
	}
	plan, err := dbsync.BuildPlan(spec)
	if err != nil {
		return nil, unsupportedSpec("build db-sync plan: %v", err)
	}

	configMap, err := b.configMap(dbSync, network, plan)
	if err != nil {
		return nil, err
	}
	persistentVolumeClaim, err := b.persistentVolumeClaim(dbSync, plan)
	if err != nil {
		return nil, err
	}
	followerPersistentVolumeClaim, err := b.followerPersistentVolumeClaim(dbSync, plan)
	if err != nil {
		return nil, err
	}
	pgPassSecret, err := b.pgPassSecret(dbSync, databaseSecret, plan)
	if err != nil {
		return nil, err
	}
	deployment, err := b.deployment(dbSync, network, networkArtifacts, databaseSecret, plan)
	if err != nil {
		return nil, err
	}
	service, err := b.metricsService(dbSync, plan)
	if err != nil {
		return nil, err
	}

	return &dbSyncWorkloadResources{
		ConfigMap:                     configMap,
		PGPassSecret:                  pgPassSecret,
		PersistentVolumeClaim:         persistentVolumeClaim,
		FollowerPersistentVolumeClaim: followerPersistentVolumeClaim,
		Deployment:                    deployment,
		MetricsService:                service,
		Plan:                          plan,
	}, nil
}

func (b dbSyncWorkloadBuilder) planSpec(dbSync *yacdv1alpha1.CardanoDBSync, network *yacdv1alpha1.CardanoNetwork, database dbsync.Database) (dbsync.Spec, error) {
	if network.Status.Endpoints == nil || network.Status.Endpoints.NodeToNode == nil {
		return dbsync.Spec{}, unsupportedSpec("node-to-node endpoint is required")
	}
	if strings.TrimSpace(dbSync.Spec.Image) == "" {
		return dbsync.Spec{}, unsupportedSpec("db-sync image is required")
	}
	if dbSync.Spec.FollowerNode != nil && dbSync.Spec.FollowerNode.Image != nil && strings.TrimSpace(*dbSync.Spec.FollowerNode.Image) == "" {
		return dbsync.Spec{}, unsupportedSpec("follower node image override must not be blank")
	}
	nodeVersion := strings.TrimSpace(network.Spec.Node.Version)
	if nodeVersion == "" && (dbSync.Spec.FollowerNode == nil || dbSync.Spec.FollowerNode.Image == nil) {
		return dbsync.Spec{}, unsupportedSpec("network node version is required to derive follower node image")
	}

	return dbsync.Spec{
		NetworkName:          network.Name,
		RequiresNetworkMagic: true,
		NetworkArtifactHash:  network.Status.Artifacts.DataHash,
		Image:                strings.TrimSpace(dbSync.Spec.Image),
		NodeToNode: dbsync.NodeToNode{
			Host: fmt.Sprintf("%s.%s.svc.cluster.local", network.Status.Endpoints.NodeToNode.ServiceName, network.Namespace),
			Port: network.Status.Endpoints.NodeToNode.Port,
		},
		Database:     database,
		Runtime:      runtimeSettings(dbSync),
		Storage:      storageSettings(dbSync),
		Insert:       insertOptions(dbSync),
		IPFSGateways: slicesClone(dbSync.Spec.Config.IPFSGateways),
		Paths: dbsync.Paths{
			ConfigFile:   dbSyncConfigFilePath(),
			TopologyFile: followerTopologyFilePath(),
			NodeConfig:   networkArtifactFilePath("configuration.yaml"),
			SocketPath:   dbSyncNodeSocketPath,
			StateDir:     dbSyncStateMountDir,
			PGPassFile:   dbSyncPGPassFilePath(),
		},
	}, nil
}

func dbSyncDatabaseFromExternal(external *yacdv1alpha1.CardanoDBSyncExternalDatabaseSpec) dbsync.Database {
	if external == nil {
		return dbsync.Database{}
	}

	return dbsync.Database{
		Host:               external.Host,
		Port:               external.Port,
		Name:               external.Database,
		User:               external.User,
		PasswordSecretName: external.PasswordSecretRef.Name,
		PasswordSecretKey:  externalDatabasePasswordKey(external),
		SSLMode:            string(external.SSLMode),
	}
}

func runtimeSettings(dbSync *yacdv1alpha1.CardanoDBSync) dbsync.Runtime {
	settings := dbsync.Runtime{
		Cache:       true,
		EpochTable:  true,
		MetricsPort: 8080,
	}
	if dbSync.Spec.Config.Runtime == nil {
		return settings
	}

	settings.Cache = dbSync.Spec.Config.Runtime.Cache
	settings.EpochTable = dbSync.Spec.Config.Runtime.EpochTable
	settings.ForceIndexes = dbSync.Spec.Config.Runtime.ForceIndexes
	if dbSync.Spec.Config.Runtime.MetricsPort != 0 {
		settings.MetricsPort = dbSync.Spec.Config.Runtime.MetricsPort
	}
	return settings
}

func storageSettings(dbSync *yacdv1alpha1.CardanoDBSync) dbsync.Storage {
	settings := dbsync.Storage{
		LedgerBackend: "lsm",
		NearTipEpoch:  580,
	}
	if dbSync.Spec.Config.LedgerBackend != "" {
		settings.LedgerBackend = string(dbSync.Spec.Config.LedgerBackend)
	}
	if dbSync.Spec.Config.Snapshot != nil {
		settings.NearTipEpoch = dbSync.Spec.Config.Snapshot.NearTipEpoch
	}
	settings.StateStorageSize = storageSizeFrom(dbSync.Spec.StateStorage, "")
	return settings
}

func insertOptions(dbSync *yacdv1alpha1.CardanoDBSync) dbsync.InsertOptions {
	insert := dbSync.Spec.Config.Insert
	if insert == nil {
		return dbsync.InsertOptions{}
	}

	options := insertOptionsForPreset(insert.Preset)
	if insert.TxCBOR != nil {
		options.TxCBOR = enableDisable(*insert.TxCBOR)
	}
	if insert.TxOut != nil {
		if insert.TxOut.Mode != nil {
			options.TxOut.Mode = string(*insert.TxOut.Mode)
		}
		if insert.TxOut.ForceTxIn != nil {
			options.TxOut.ForceTxIn = *insert.TxOut.ForceTxIn
		}
		if insert.TxOut.UseAddressTable != nil {
			options.TxOut.UseAddressTable = *insert.TxOut.UseAddressTable
		}
	}
	if insert.Ledger != nil {
		options.Ledger = string(*insert.Ledger)
	}
	if insert.Shelley != nil {
		options.Shelley = featureSelection(options.Shelley, insert.Shelley.Enabled, insert.Shelley.StakeAddresses, nil, nil, nil)
	}
	if insert.MultiAsset != nil {
		options.MultiAsset = featureSelection(options.MultiAsset, insert.MultiAsset.Enabled, nil, insert.MultiAsset.Policies, nil, nil)
	}
	if insert.Metadata != nil {
		options.Metadata = featureSelection(options.Metadata, insert.Metadata.Enabled, nil, nil, insert.Metadata.Keys, nil)
	}
	if insert.Plutus != nil {
		options.Plutus = featureSelection(options.Plutus, insert.Plutus.Enabled, nil, nil, nil, insert.Plutus.ScriptHashes)
	}
	if insert.Governance != nil {
		options.Governance = enableDisable(*insert.Governance)
	}
	if insert.OffchainPoolData != nil {
		options.OffchainPoolData = enableDisable(*insert.OffchainPoolData)
	}
	if insert.OffchainVoteData != nil {
		options.OffchainVoteData = enableDisable(*insert.OffchainVoteData)
	}
	if insert.PoolStats != nil {
		options.PoolStats = enableDisable(*insert.PoolStats)
	}
	if insert.JSONType != nil {
		options.JSONType = string(*insert.JSONType)
	}
	if insert.RemoveJSONBFromSchema != nil {
		options.RemoveJSONBFromSchema = enableDisable(*insert.RemoveJSONBFromSchema)
	}

	return options
}

func insertOptionsForPreset(preset yacdv1alpha1.CardanoDBSyncInsertPreset) dbsync.InsertOptions {
	switch preset {
	case yacdv1alpha1.CardanoDBSyncInsertPresetOnlyUTxO:
		return dbsync.InsertOptions{
			TxCBOR:           "disable",
			TxOut:            dbsync.TxOutOption{Mode: "bootstrap"},
			Ledger:           "ignore",
			Shelley:          dbsync.FeatureSelection{Enabled: false},
			MultiAsset:       dbsync.FeatureSelection{Enabled: true},
			Metadata:         dbsync.FeatureSelection{Enabled: false},
			Plutus:           dbsync.FeatureSelection{Enabled: false},
			Governance:       "disable",
			OffchainPoolData: "disable",
			OffchainVoteData: "disable",
			PoolStats:        "disable",
			JSONType:         "text",
		}
	case yacdv1alpha1.CardanoDBSyncInsertPresetOnlyGovernance:
		return dbsync.InsertOptions{
			TxCBOR:           "disable",
			TxOut:            dbsync.TxOutOption{Mode: "disable"},
			Ledger:           "enable",
			Shelley:          dbsync.FeatureSelection{Enabled: false},
			MultiAsset:       dbsync.FeatureSelection{Enabled: false},
			Metadata:         dbsync.FeatureSelection{Enabled: false},
			Plutus:           dbsync.FeatureSelection{Enabled: false},
			Governance:       "enable",
			OffchainPoolData: "disable",
			OffchainVoteData: "disable",
			PoolStats:        "enable",
			JSONType:         "text",
		}
	case yacdv1alpha1.CardanoDBSyncInsertPresetDisableAll:
		return dbsync.InsertOptions{
			TxCBOR:           "disable",
			TxOut:            dbsync.TxOutOption{Mode: "disable"},
			Ledger:           "disable",
			Shelley:          dbsync.FeatureSelection{Enabled: false},
			MultiAsset:       dbsync.FeatureSelection{Enabled: false},
			Metadata:         dbsync.FeatureSelection{Enabled: false},
			Plutus:           dbsync.FeatureSelection{Enabled: false},
			Governance:       "disable",
			OffchainPoolData: "disable",
			OffchainVoteData: "disable",
			PoolStats:        "disable",
			JSONType:         "text",
		}
	default:
		return dbsync.InsertOptions{
			TxCBOR:           "disable",
			TxOut:            dbsync.TxOutOption{Mode: "enable"},
			Ledger:           "enable",
			Shelley:          dbsync.FeatureSelection{Enabled: true},
			MultiAsset:       dbsync.FeatureSelection{Enabled: true},
			Metadata:         dbsync.FeatureSelection{Enabled: true},
			Plutus:           dbsync.FeatureSelection{Enabled: true},
			Governance:       "enable",
			OffchainPoolData: "disable",
			OffchainVoteData: "disable",
			PoolStats:        "enable",
			JSONType:         "text",
		}
	}
}

func featureSelection(base dbsync.FeatureSelection, enabled *bool, stakeAddresses []string, policies []string, keys []int64, scriptHashes []string) dbsync.FeatureSelection {
	selection := dbsync.FeatureSelection{
		Enabled:        base.Enabled,
		StakeAddresses: slicesClone(stakeAddresses),
		Policies:       slicesClone(policies),
		Keys:           slicesClone(keys),
		ScriptHashes:   slicesClone(scriptHashes),
	}
	if enabled != nil {
		selection.Enabled = *enabled
	}
	return selection
}

func slicesClone[T any](values []T) []T {
	if values == nil {
		return nil
	}
	copied := make([]T, len(values))
	copy(copied, values)
	return copied
}

func enableDisable(enabled bool) string {
	if enabled {
		return "enable"
	}
	return "disable"
}

func (b dbSyncWorkloadBuilder) configMap(dbSync *yacdv1alpha1.CardanoDBSync, network *yacdv1alpha1.CardanoNetwork, plan dbsync.Plan) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncConfigMapName(dbSync),
			Namespace: dbSync.Namespace,
			Labels:    dbSyncWorkloadLabels(dbSync),
			Annotations: map[string]string{
				dbSyncPlanFingerprintAnno:  plan.Fingerprint.Value,
				dbSyncDatabaseIdentityAnno: plan.DatabaseIdentityFingerprint.Value,
				dbSyncArtifactDataHashAnno: network.Status.Artifacts.DataHash,
			},
		},
		Data: map[string]string{
			dbSyncConfigFileName:     plan.ConfigYAML,
			followerTopologyFileName: plan.TopologyJSON,
		},
	}
	if err := controllerutil.SetControllerReference(dbSync, configMap, b.scheme); err != nil {
		return nil, fmt.Errorf("set db-sync config ConfigMap owner reference: %w", err)
	}

	return configMap, nil
}

func (b dbSyncWorkloadBuilder) persistentVolumeClaim(dbSync *yacdv1alpha1.CardanoDBSync, plan dbsync.Plan) (*corev1.PersistentVolumeClaim, error) {
	pvc, err := b.storagePersistentVolumeClaim(
		dbSync,
		dbSyncStatePVCName(dbSync),
		plan.Spec.Storage.StateStorageSize,
		storageClassNameFrom(dbSync.Spec.StateStorage),
	)
	if err != nil {
		return nil, err
	}
	if err := controllerutil.SetControllerReference(dbSync, pvc, b.scheme); err != nil {
		return nil, fmt.Errorf("set db-sync state PVC owner reference: %w", err)
	}
	if pvc.Annotations == nil {
		pvc.Annotations = map[string]string{}
	}
	pvc.Annotations[dbSyncDatabaseIdentityAnno] = plan.DatabaseIdentityFingerprint.Value

	return pvc, nil
}

func (b dbSyncWorkloadBuilder) followerPersistentVolumeClaim(dbSync *yacdv1alpha1.CardanoDBSync, plan dbsync.Plan) (*corev1.PersistentVolumeClaim, error) {
	size := defaultFollowerNodeStorageSize
	var storageClassName *string
	if dbSync.Spec.FollowerNode != nil && dbSync.Spec.FollowerNode.Storage != nil {
		size = storageSizeFrom(dbSync.Spec.FollowerNode.Storage, defaultFollowerNodeStorageSize)
		storageClassName = dbSync.Spec.FollowerNode.Storage.StorageClassName
	}
	pvc, err := b.storagePersistentVolumeClaim(
		dbSync,
		dbSyncFollowerPVCName(dbSync),
		size,
		storageClassName,
	)
	if err != nil {
		return nil, err
	}
	if err := controllerutil.SetControllerReference(dbSync, pvc, b.scheme); err != nil {
		return nil, fmt.Errorf("set follower node state PVC owner reference: %w", err)
	}
	if pvc.Annotations == nil {
		pvc.Annotations = map[string]string{}
	}
	pvc.Annotations[dbSyncDatabaseIdentityAnno] = plan.DatabaseIdentityFingerprint.Value

	return pvc, nil
}

func (b dbSyncWorkloadBuilder) storagePersistentVolumeClaim(
	dbSync *yacdv1alpha1.CardanoDBSync,
	name string,
	size string,
	storageClassName *string,
) (*corev1.PersistentVolumeClaim, error) {
	quantity, err := resource.ParseQuantity(size)
	if err != nil {
		return nil, unsupportedSpec("parse PVC storage size %q: %v", size, err)
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dbSync.Namespace,
			Labels:    dbSyncWorkloadLabels(dbSync),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: quantity,
				},
			},
		},
	}
	if storageClassName != nil {
		pvc.Spec.StorageClassName = storageClassName
		pvc.Annotations = map[string]string{
			requestedStorageClassAnno: *storageClassName,
		}
	}

	return pvc, nil
}

func storageClassNameFrom(storage *yacdv1alpha1.CardanoDBSyncStorageSpec) *string {
	if storage == nil {
		return nil
	}

	return storage.StorageClassName
}

func storageSizeFrom(storage *yacdv1alpha1.CardanoDBSyncStorageSpec, fallback string) string {
	if storage == nil || storage.Size == nil {
		return fallback
	}

	return storage.Size.String()
}

func (b dbSyncWorkloadBuilder) pgPassSecret(
	dbSync *yacdv1alpha1.CardanoDBSync,
	databaseSecret *corev1.Secret,
	plan dbsync.Plan,
) (*corev1.Secret, error) {
	pgPass, err := pgPassFile(plan, databaseSecret)
	if err != nil {
		return nil, err
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncPGPassSecretName(dbSync),
			Namespace: dbSync.Namespace,
			Labels:    dbSyncWorkloadLabels(dbSync),
			Annotations: map[string]string{
				dbSyncPlanFingerprintAnno:  plan.Fingerprint.Value,
				dbSyncDatabaseIdentityAnno: plan.DatabaseIdentityFingerprint.Value,
				dbSyncSecretVersionAnno:    databaseSecret.ResourceVersion,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			dbSyncPGPassFileName: []byte(pgPass),
		},
	}
	if err := controllerutil.SetControllerReference(dbSync, secret, b.scheme); err != nil {
		return nil, fmt.Errorf("set db-sync pgpass Secret owner reference: %w", err)
	}

	return secret, nil
}

func pgPassFile(plan dbsync.Plan, databaseSecret *corev1.Secret) (string, error) {
	passwordBytes := databaseSecret.Data[plan.Spec.Database.PasswordSecretKey]
	if len(passwordBytes) == 0 {
		return "", unsupportedSpec("database credential Secret does not contain key %q", plan.Spec.Database.PasswordSecretKey)
	}
	password := string(passwordBytes)

	fields := []struct {
		name  string
		value string
	}{
		{name: "host", value: plan.Spec.Database.Host},
		{name: "port", value: strconv.Itoa(int(plan.Spec.Database.Port))},
		{name: "database", value: plan.Spec.Database.Name},
		{name: "user", value: plan.Spec.Database.User},
		{name: "password", value: password},
	}
	rendered := make([]string, 0, len(fields))
	for _, field := range fields {
		if strings.ContainsAny(field.value, "\r\n") {
			return "", unsupportedSpec("external database %s cannot contain newlines when rendered as pgpass", field.name)
		}
		rendered = append(rendered, escapePGPassField(field.value))
	}

	return strings.Join(rendered, ":") + "\n", nil
}

func escapePGPassField(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, ":", `\:`)
	return value
}

func (b dbSyncWorkloadBuilder) deployment(
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	networkArtifacts *corev1.ConfigMap,
	databaseSecret *corev1.Secret,
	plan dbsync.Plan,
) (*appsv1.Deployment, error) {
	selectorLabels := dbSyncWorkloadSelectorLabels(dbSync)
	labels := dbSyncWorkloadLabels(dbSync)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncWorkloadName(dbSync),
			Namespace: dbSync.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(1)),
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selectorLabels,
					Annotations: map[string]string{
						dbSyncPlanFingerprintAnno:  plan.Fingerprint.Value,
						dbSyncDatabaseIdentityAnno: plan.DatabaseIdentityFingerprint.Value,
						dbSyncArtifactDataHashAnno: network.Status.Artifacts.DataHash,
						dbSyncSecretVersionAnno:    databaseSecret.ResourceVersion,
					},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: new(false),
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: new(int64(10001)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						b.followerNodeContainer(dbSync, network, plan),
						b.dbSyncContainer(dbSync, plan),
					},
					Volumes: []corev1.Volume{
						{
							Name: networkArtifactsVolumeName,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: networkArtifacts.Name},
								},
							},
						},
						{
							Name: dbSyncConfigMapVolumeName,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: dbSyncConfigMapName(dbSync)},
								},
							},
						},
						{
							Name: dbSyncStateVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: dbSyncStatePVCName(dbSync),
								},
							},
						},
						{
							Name: followerNodeStateVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: dbSyncFollowerPVCName(dbSync),
								},
							},
						},
						{
							Name: nodeIPCVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: dbSyncPGPassVolumeName,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  dbSyncPGPassSecretName(dbSync),
									DefaultMode: new(int32(0o600)),
								},
							},
						},
						{
							Name: dbSyncTmpVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(dbSync, deployment, b.scheme); err != nil {
		return nil, fmt.Errorf("set db-sync Deployment owner reference: %w", err)
	}

	return deployment, nil
}

func (b dbSyncWorkloadBuilder) followerNodeContainer(dbSync *yacdv1alpha1.CardanoDBSync, network *yacdv1alpha1.CardanoNetwork, plan dbsync.Plan) corev1.Container {
	container := corev1.Container{
		Name:            followerNodeContainerName,
		Image:           b.followerNodeImage(dbSync, network),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"cardano-node"},
		Args: []string{
			"run",
			"--config", plan.Spec.Paths.NodeConfig,
			"--topology", plan.Spec.Paths.TopologyFile,
			"--database-path", dbSyncNodeDatabaseDir,
			"--socket-path", dbSyncNodeSocketPath,
			"--host-addr", dbSyncNodeHostAddress,
			"--port", strconv.Itoa(int(dbSyncNodePort)),
		},
		Ports: []corev1.ContainerPort{{
			Name:          followerNodePortName,
			ContainerPort: dbSyncNodePort,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts: []corev1.VolumeMount{
			{Name: networkArtifactsVolumeName, MountPath: networkArtifactsMountDir, ReadOnly: true},
			{Name: dbSyncConfigMapVolumeName, MountPath: dbSyncConfigMountDir, ReadOnly: true},
			{Name: followerNodeStateVolumeName, MountPath: dbSyncNodeDatabaseDir},
			{Name: nodeIPCVolumeName, MountPath: dbSyncNodeSocketDir},
		},
		SecurityContext:          restrictedSecurityContext(true),
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	if dbSync.Spec.FollowerNode != nil && dbSync.Spec.FollowerNode.Resources != nil {
		container.Resources = *dbSync.Spec.FollowerNode.Resources.DeepCopy()
	}
	return container
}

func (b dbSyncWorkloadBuilder) dbSyncContainer(dbSync *yacdv1alpha1.CardanoDBSync, plan dbsync.Plan) corev1.Container {
	env := make([]corev1.EnvVar, 0, len(plan.Environment()))
	for _, planEnv := range plan.Environment() {
		env = append(env, corev1.EnvVar{Name: planEnv.Name, Value: planEnv.Value})
	}

	container := corev1.Container{
		Name:            dbSyncContainerName,
		Image:           strings.TrimSpace(dbSync.Spec.Image),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            plan.Run.Args,
		WorkingDir:      networkArtifactsMountDir,
		Env:             env,
		Ports: []corev1.ContainerPort{{
			Name:          dbSyncMetricsPortName,
			ContainerPort: plan.Spec.Runtime.MetricsPort,
			Protocol:      corev1.ProtocolTCP,
		}},
		VolumeMounts: []corev1.VolumeMount{
			{Name: networkArtifactsVolumeName, MountPath: networkArtifactsMountDir, ReadOnly: true},
			{Name: dbSyncConfigMapVolumeName, MountPath: dbSyncConfigMountDir, ReadOnly: true},
			{Name: dbSyncStateVolumeName, MountPath: dbSyncStateMountDir},
			{Name: nodeIPCVolumeName, MountPath: dbSyncNodeSocketDir},
			{Name: dbSyncPGPassVolumeName, MountPath: dbSyncPGPassMountDir, ReadOnly: true},
			{Name: dbSyncTmpVolumeName, MountPath: dbSyncTmpMountDir},
		},
		SecurityContext:          restrictedSecurityContext(false),
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	if dbSync.Spec.Resources != nil {
		container.Resources = *dbSync.Spec.Resources.DeepCopy()
	}
	if plan.Run.Command != "" {
		container.Command = []string{plan.Run.Command}
	}
	return container
}

func restrictedSecurityContext(readOnlyRoot bool) *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: new(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		ReadOnlyRootFilesystem: &readOnlyRoot,
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

func (b dbSyncWorkloadBuilder) metricsService(dbSync *yacdv1alpha1.CardanoDBSync, plan dbsync.Plan) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncMetricsServiceName(dbSync),
			Namespace: dbSync.Namespace,
			Labels:    dbSyncWorkloadLabels(dbSync),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: dbSyncWorkloadSelectorLabels(dbSync),
			Ports: []corev1.ServicePort{{
				Name:       dbSyncMetricsPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       plan.Spec.Runtime.MetricsPort,
				TargetPort: intstr.FromString(dbSyncMetricsPortName),
			}},
		},
	}
	if err := controllerutil.SetControllerReference(dbSync, service, b.scheme); err != nil {
		return nil, fmt.Errorf("set db-sync metrics Service owner reference: %w", err)
	}

	return service, nil
}

func (b dbSyncWorkloadBuilder) followerNodeImage(dbSync *yacdv1alpha1.CardanoDBSync, network *yacdv1alpha1.CardanoNetwork) string {
	if dbSync.Spec.FollowerNode != nil && dbSync.Spec.FollowerNode.Image != nil {
		return strings.TrimSpace(*dbSync.Spec.FollowerNode.Image)
	}
	return fmt.Sprintf("%s:%s-%s", defaultFollowerNodeImageRepository, strings.TrimSpace(network.Spec.Node.Version), defaultFollowerNodeImageRevision)
}

func dbSyncConfigFilePath() string {
	return dbSyncConfigMountDir + "/" + dbSyncConfigFileName
}

func followerTopologyFilePath() string {
	return dbSyncConfigMountDir + "/" + followerTopologyFileName
}

func networkArtifactFilePath(name string) string {
	return networkArtifactsMountDir + "/" + name
}

func dbSyncPGPassFilePath() string {
	return dbSyncPGPassMountDir + "/" + dbSyncPGPassFileName
}

func dbSyncWorkloadName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return safeDNSLabelWithSuffix(dbSync.Name, dbSyncNameSuffix)
}

func dbSyncConfigMapName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return safeDNSLabelWithSuffix(dbSync.Name, dbSyncConfigMapSuffix)
}

func dbSyncStatePVCName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return safeDNSLabelWithSuffix(dbSync.Name, dbSyncStatePVCSuffix)
}

func dbSyncFollowerPVCName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return safeDNSLabelWithSuffix(dbSync.Name, dbSyncFollowerPVCSuffix)
}

func dbSyncPGPassSecretName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return safeDNSLabelWithSuffix(dbSync.Name, dbSyncPGPassSecretSuffix)
}

func dbSyncMetricsServiceName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return safeDNSLabelWithSuffix(dbSync.Name, dbSyncMetricsSuffix)
}

func dbSyncWorkloadSelectorLabels(dbSync *yacdv1alpha1.CardanoDBSync) map[string]string {
	instance := safeLabelValue(dbSync.Name)
	return map[string]string{
		labelAppName:      labelDBSyncAppName,
		labelAppInstance:  instance,
		labelAppComponent: labelDBSyncRole,
		labelDBSync:       instance,
		labelCardanoRole:  labelDBSyncRole,
	}
}

func dbSyncWorkloadLabels(dbSync *yacdv1alpha1.CardanoDBSync) map[string]string {
	labels := dbSyncWorkloadSelectorLabels(dbSync)
	labels[labelAppManagedBy] = "yacd"
	return labels
}

func safeDNSLabelWithSuffix(value string, suffix string) string {
	base := sanitizeDNSLabel(value)
	needsHash := base != value
	if base == "" {
		base = "x"
		needsHash = true
	}

	candidateSuffix := "-" + suffix
	if needsHash {
		candidateSuffix = fmt.Sprintf("-%s-%s", shortNameHash(value), suffix)
	}
	candidate := base + candidateSuffix
	if len(candidate) <= maxLabelValueLength {
		return candidate
	}

	hash := shortNameHash(value)
	hashSuffix := fmt.Sprintf("-%s-%s", hash, suffix)
	prefixLength := maxLabelValueLength - len(hashSuffix)
	prefix := strings.Trim(base[:prefixLength], "-")
	if prefix == "" {
		prefix = "x"
	}

	return prefix + hashSuffix
}

func safeLabelValue(value string) string {
	base := sanitizeLabelValue(value)
	if base == "" {
		base = shortNameHash(value)
	}
	if len(base) <= maxLabelValueLength {
		return base
	}

	hash := shortNameHash(value)
	hashSuffix := "-" + hash
	prefixLength := maxLabelValueLength - len(hashSuffix)
	prefix := strings.TrimRight(base[:prefixLength], "-_.")
	if prefix == "" {
		prefix = "x"
	}

	return prefix + hashSuffix
}

func sanitizeDNSLabel(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range strings.ToLower(value) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			builder.WriteRune(char)
			continue
		}
		builder.WriteByte('-')
	}

	return strings.Trim(builder.String(), "-")
}

func sanitizeLabelValue(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' ||
			char == '_' ||
			char == '.' {
			builder.WriteRune(char)
			continue
		}
		builder.WriteByte('-')
	}

	return strings.Trim(builder.String(), "-_.")
}

func shortNameHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:safeNameHashLength]
}
