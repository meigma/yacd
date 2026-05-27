package cardanodbsync

import (
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// In-container layout shared across the dbsync workload builders. The
// volume names, mount points, and file names appear together because each
// constant is consumed by at least two of builder.go, containers.go, and
// resources.go; keeping them centralized prevents drift between the
// rendered Pod spec and the file paths embedded in plan args.
const (
	dbSyncContainerName       = "cardano-db-sync"
	followerNodeContainerName = "follower-node"
	dbSyncMetricsPortName     = "metrics"
	followerNodePortName      = "node-to-node"

	dbSyncConfigMapVolumeName    = "dbsync-config"
	networkArtifactsVolumeName   = "network-artifacts"
	dbSyncStateVolumeName        = "dbsync-state"
	followerNodeStateVolumeName  = "follower-state"
	nodeIPCVolumeName            = "node-ipc"
	dbSyncTmpVolumeName          = "dbsync-tmp"
	dbSyncPGPassVolumeName       = "dbsync-pgpass"
	dbSyncPGPassSecretVolumeName = "dbsync-pgpass-secret"

	dbSyncConfigMountDir             = "/config"
	networkArtifactsMountDir         = "/network-artifacts"
	dbSyncStateMountDir              = "/var/lib/cexplorer"
	dbSyncTmpMountDir                = "/tmp"
	dbSyncPGPassMountDir             = "/configuration"
	dbSyncPGPassSecretMountDir       = "/pgpass-source"
	dbSyncNodeDatabaseDir            = "/state/node-db"
	dbSyncNodeSocketDir              = "/ipc"
	dbSyncNodeSocketPath             = "/ipc/node.socket"
	dbSyncNodeHostAddress            = "0.0.0.0"
	dbSyncNodePort             int32 = 3001

	dbSyncConfigFileName     = "db-sync-config.yaml"
	followerTopologyFileName = "follower-topology.json"
	dbSyncPGPassFileName     = "pgpass"
	dbSyncPGPassInitName     = "dbsync-pgpass-setup"
)

// dbSyncWorkloadResources is the desired-state bundle the builder produces
// for the dbsync workload. The bundle covers the two-container Deployment
// and every dependent K8s object it mounts.
type dbSyncWorkloadResources struct {
	// ConfigMap holds the rendered db-sync configuration and follower-node
	// topology mounted into the dbsync workload Pod.
	ConfigMap *corev1.ConfigMap
	// PGPassSecret holds the rendered libpq pgpass file consumed by the
	// pgPassInitContainer.
	PGPassSecret *corev1.Secret
	// PersistentVolumeClaim is the durable state PVC for the db-sync
	// container.
	PersistentVolumeClaim *corev1.PersistentVolumeClaim
	// FollowerPersistentVolumeClaim is the durable state PVC for the
	// follower-node container.
	FollowerPersistentVolumeClaim *corev1.PersistentVolumeClaim
	// Deployment is the two-container dbsync workload Deployment.
	Deployment *appsv1.Deployment
	// MetricsService fronts the db-sync container's Prometheus metrics
	// endpoint.
	MetricsService *corev1.Service
	// Plan is the dbsync planner output the builder used to render the
	// rest of the bundle. Carried in the bundle so the reconciler can
	// observe the plan fingerprint and database identity without
	// recomputing the plan.
	Plan dbsync.Plan
}

// dbSyncWorkloadBuilder converts a CardanoDBSync spec, the referenced
// CardanoNetwork, the network artifacts ConfigMap, and the database
// password Secret into the desired dbsync workload resources. The builder
// is pure: it produces in-memory Kubernetes objects and never touches the
// API server, the file system, time, or randomness.
type dbSyncWorkloadBuilder struct {
	// scheme is required to set controller references on owned children.
	scheme *runtime.Scheme

	// defaultCardanoTestnetImage is the Reconciler-injected override for
	// the follower-node container image. When non-empty it replaces the
	// computed "<repo>:<networkNodeVersion>-<revision>" reference. The
	// local dev stack's docker-built image flows in through here so
	// CardanoDBSync picks up publisher changes the published
	// cardano-testnet tag does not yet contain.
	defaultCardanoTestnetImage string
}

// Build renders the dbsync workload resources for the external-database
// path. It is a convenience wrapper around BuildForDatabase that derives
// the dbsync.Database planner input from spec.database.external and
// rejects a missing external block up front.
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

// BuildForDatabase renders the dbsync workload resources for an arbitrary
// dbsync.Database (used by the managed-Postgres path where the caller
// already resolved the in-cluster Postgres Service address). The order of
// operations is:
//
//  1. translate the CardanoDBSync spec into a dbsync.Spec the planner
//     accepts (planSpec)
//  2. compute the dbsync plan (config YAML, topology, fingerprints)
//  3. assemble the ConfigMap, PVCs, pgpass Secret, Deployment, and
//     metrics Service
//
// BuildForDatabase returns an unsupportedSpecError when the spec is not
// satisfiable; the reconciler surfaces that as a Degraded condition
// rather than retrying.
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

// planSpec translates the CardanoDBSync spec, the referenced CardanoNetwork
// status, and the resolved dbsync.Database into the dbsync planner input.
// It rejects empty image references and missing network endpoints up front
// so the planner does not have to recheck them.
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
			NodeConfig:   networkArtifactFilePath(networkartifacts.ConfigurationKey),
			SocketPath:   dbSyncNodeSocketPath,
			StateDir:     dbSyncStateMountDir,
			PGPassFile:   dbSyncPGPassFilePath(),
		},
	}, nil
}
