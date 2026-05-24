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

	dbSyncConfigMapSuffix = "dbsync-config"
	dbSyncStatePVCSuffix  = "dbsync-state"
	dbSyncMetricsSuffix   = "dbsync-metrics"

	dbSyncConfigMapVolumeName        = "dbsync-config"
	networkArtifactsVolumeName       = "network-artifacts"
	dbSyncStateVolumeName            = "dbsync-state"
	nodeIPCVolumeName                = "node-ipc"
	dbSyncTmpVolumeName              = "dbsync-tmp"
	dbSyncConfigMountDir             = "/config"
	networkArtifactsMountDir         = "/network-artifacts"
	dbSyncStateMountDir              = "/state"
	dbSyncTmpMountDir                = "/tmp"
	dbSyncNodeDatabaseDir            = "/state/node-db"
	dbSyncNodeSocketDir              = "/ipc"
	dbSyncNodeSocketPath             = "/ipc/node.socket"
	dbSyncNodeHostAddress            = "0.0.0.0"
	dbSyncNodePort             int32 = 3001

	dbSyncConfigFileName       = "db-sync-config.yaml"
	followerTopologyFileName   = "follower-topology.json"
	dbSyncPlanFingerprintAnno  = "yacd.meigma.io/dbsync-plan-fingerprint"
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
	maxLabelValueLength                = 63
	safeNameHashLength                 = 10
)

type dbSyncWorkloadResources struct {
	ConfigMap             *corev1.ConfigMap
	PersistentVolumeClaim *corev1.PersistentVolumeClaim
	Deployment            *appsv1.Deployment
	MetricsService        *corev1.Service
	Plan                  dbsync.Plan
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
	if network == nil {
		return nil, fmt.Errorf("cardanonetwork is required")
	}
	if networkArtifacts == nil {
		return nil, fmt.Errorf("network artifacts ConfigMap is required")
	}
	if externalDatabaseSecret == nil {
		return nil, fmt.Errorf("external database Secret is required")
	}
	if b.scheme == nil {
		return nil, fmt.Errorf("scheme is required")
	}

	spec, err := b.planSpec(dbSync, network)
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
	deployment, err := b.deployment(dbSync, network, networkArtifacts, externalDatabaseSecret, plan)
	if err != nil {
		return nil, err
	}
	service, err := b.metricsService(dbSync, plan)
	if err != nil {
		return nil, err
	}

	return &dbSyncWorkloadResources{
		ConfigMap:             configMap,
		PersistentVolumeClaim: persistentVolumeClaim,
		Deployment:            deployment,
		MetricsService:        service,
		Plan:                  plan,
	}, nil
}

func (b dbSyncWorkloadBuilder) planSpec(dbSync *yacdv1alpha1.CardanoDBSync, network *yacdv1alpha1.CardanoNetwork) (dbsync.Spec, error) {
	external := dbSync.Spec.Database.External
	if external == nil {
		return dbsync.Spec{}, unsupportedSpec("external database spec is required")
	}
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
		NodeToNode: dbsync.NodeToNode{
			Host: fmt.Sprintf("%s.%s.svc.cluster.local", network.Status.Endpoints.NodeToNode.ServiceName, network.Namespace),
			Port: network.Status.Endpoints.NodeToNode.Port,
		},
		Database: dbsync.Database{
			Host:               external.Host,
			Port:               external.Port,
			Name:               external.Database,
			User:               external.User,
			PasswordSecretName: external.PasswordSecretRef.Name,
			PasswordSecretKey:  externalDatabasePasswordKey(external),
			SSLMode:            string(external.SSLMode),
		},
		Runtime: runtimeSettings(dbSync),
		Storage: storageSettings(dbSync),
		Insert:  insertOptions(dbSync),
		Paths: dbsync.Paths{
			ConfigFile:   dbSyncConfigFilePath(),
			TopologyFile: followerTopologyFilePath(),
			NodeConfig:   networkArtifactFilePath("configuration.yaml"),
			SocketPath:   dbSyncNodeSocketPath,
			StateDir:     "/state/db-sync-ledger",
			SchemaDir:    "/opt/cardano-db-sync/schema",
		},
	}, nil
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
	if dbSync.Spec.StateStorage != nil {
		settings.StateStorageSize = dbSync.Spec.StateStorage.Size.String()
	}
	return settings
}

func insertOptions(dbSync *yacdv1alpha1.CardanoDBSync) dbsync.InsertOptions {
	insert := dbSync.Spec.Config.Insert
	if insert == nil {
		return dbsync.InsertOptions{}
	}

	options := insertOptionsForPreset(insert.Preset)
	if insert.TxCBOR {
		options.TxCBOR = "enable"
	}
	if insert.TxOut != nil {
		options.TxOut = dbsync.TxOutOption{
			Mode:            string(insert.TxOut.Mode),
			ForceTxIn:       insert.TxOut.ForceTxIn,
			UseAddressTable: insert.TxOut.UseAddressTable,
		}
	}
	if insert.Ledger != "" {
		options.Ledger = string(insert.Ledger)
	}
	if insert.Shelley != nil {
		options.Shelley = featureSelection(insert.Shelley.Enabled, insert.Shelley.StakeAddresses, nil, nil, nil)
	}
	if insert.MultiAsset != nil {
		options.MultiAsset = featureSelection(insert.MultiAsset.Enabled, nil, insert.MultiAsset.Policies, nil, nil)
	}
	if insert.Metadata != nil {
		options.Metadata = featureSelection(insert.Metadata.Enabled, nil, nil, insert.Metadata.Keys, nil)
	}
	if insert.Plutus != nil {
		options.Plutus = featureSelection(insert.Plutus.Enabled, nil, nil, nil, insert.Plutus.ScriptHashes)
	}
	options.Governance = enableDisable(insert.Governance)
	options.OffchainPoolData = enableDisable(insert.OffchainPoolData)
	options.OffchainVoteData = enableDisable(insert.OffchainVoteData)
	options.PoolStats = enableDisable(insert.PoolStats)
	if insert.JSONType != "" {
		options.JSONType = string(insert.JSONType)
	}
	options.RemoveJSONBFromSchema = insert.RemoveJSONBFromSchema

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
			PoolStats:        "disable",
			JSONType:         "text",
		}
	}
}

func featureSelection(enabled bool, stakeAddresses []string, policies []string, keys []int64, scriptHashes []string) dbsync.FeatureSelection {
	return dbsync.FeatureSelection{
		Enabled:        enabled,
		StakeAddresses: slicesClone(stakeAddresses),
		Policies:       slicesClone(policies),
		Keys:           slicesClone(keys),
		ScriptHashes:   slicesClone(scriptHashes),
	}
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
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncStatePVCName(dbSync),
			Namespace: dbSync.Namespace,
			Labels:    dbSyncWorkloadLabels(dbSync),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(plan.Spec.Storage.StateStorageSize),
				},
			},
		},
	}
	if dbSync.Spec.StateStorage != nil && dbSync.Spec.StateStorage.StorageClassName != nil {
		pvc.Spec.StorageClassName = dbSync.Spec.StateStorage.StorageClassName
		pvc.Annotations = map[string]string{
			requestedStorageClassAnno: *dbSync.Spec.StateStorage.StorageClassName,
		}
	}
	if err := controllerutil.SetControllerReference(dbSync, pvc, b.scheme); err != nil {
		return nil, fmt.Errorf("set db-sync state PVC owner reference: %w", err)
	}

	return pvc, nil
}

func (b dbSyncWorkloadBuilder) deployment(
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	networkArtifacts *corev1.ConfigMap,
	externalDatabaseSecret *corev1.Secret,
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
						dbSyncArtifactDataHashAnno: network.Status.Artifacts.DataHash,
						dbSyncSecretVersionAnno:    externalDatabaseSecret.ResourceVersion,
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
							Name: nodeIPCVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
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
			{Name: dbSyncStateVolumeName, MountPath: dbSyncStateMountDir},
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
	env := make([]corev1.EnvVar, 0, len(plan.Environment())+1)
	for _, planEnv := range plan.Environment() {
		env = append(env, corev1.EnvVar{Name: planEnv.Name, Value: planEnv.Value})
	}
	env = append(env, corev1.EnvVar{
		Name: "PGPASSWORD",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: plan.Spec.Database.PasswordSecretName},
				Key:                  plan.Spec.Database.PasswordSecretKey,
			},
		},
	})

	container := corev1.Container{
		Name:            dbSyncContainerName,
		Image:           strings.TrimSpace(dbSync.Spec.Image),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{plan.Run.Command},
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
			{Name: dbSyncTmpVolumeName, MountPath: dbSyncTmpMountDir},
		},
		SecurityContext:          restrictedSecurityContext(false),
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	if dbSync.Spec.Resources != nil {
		container.Resources = *dbSync.Spec.Resources.DeepCopy()
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

func dbSyncWorkloadName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return safeDNSLabelWithSuffix(dbSync.Name, dbSyncNameSuffix)
}

func dbSyncConfigMapName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return safeDNSLabelWithSuffix(dbSync.Name, dbSyncConfigMapSuffix)
}

func dbSyncStatePVCName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return safeDNSLabelWithSuffix(dbSync.Name, dbSyncStatePVCSuffix)
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
