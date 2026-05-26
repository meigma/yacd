package cardanodbsync

import (
	"fmt"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// dbSyncContainer builds the cardano-db-sync container. The container reads
// the rendered config from the ConfigMap volume, the libpq pgpass file from
// the initContainer-prepared EmptyDir, and writes its durable state to the
// dbsync-state PVC.
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

// followerNodeContainer builds the follower cardano-node container that
// peers with the referenced CardanoNetwork's primary node and exposes its
// IPC socket to the cardano-db-sync sibling through a shared EmptyDir.
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

// pgPassInitContainer copies the rendered pgpass file from the
// controller-mounted Secret into a writable EmptyDir and chmods it to 0600
// so libpq accepts it. The db-sync container then mounts the EmptyDir
// read-only.
func (b dbSyncWorkloadBuilder) pgPassInitContainer(dbSync *yacdv1alpha1.CardanoDBSync) corev1.Container {
	source := dbSyncPGPassSecretMountDir + "/" + dbSyncPGPassFileName
	target := dbSyncPGPassFilePath()
	script := fmt.Sprintf("cp %s %s\nchmod 0600 %s\n", source, target, target)

	return corev1.Container{
		Name:            dbSyncPGPassInitName,
		Image:           strings.TrimSpace(dbSync.Spec.Image),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-eu", "-c"},
		Args:            []string{script},
		VolumeMounts: []corev1.VolumeMount{
			{Name: dbSyncPGPassSecretVolumeName, MountPath: dbSyncPGPassSecretMountDir, ReadOnly: true},
			{Name: dbSyncPGPassVolumeName, MountPath: dbSyncPGPassMountDir},
		},
		SecurityContext:          restrictedSecurityContext(true),
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
}

// managedPostgresContainer builds the managed Postgres container. The
// container reads bootstrap credentials from the controller-owned auth
// Secret and writes its data directory to the managed-postgres-state
// PVC.
func managedPostgresContainer(managed *yacdv1alpha1.CardanoDBSyncManagedDatabaseSpec, authSecretName string) corev1.Container {
	database := managedPostgresDatabaseName(managed)
	user := managedPostgresUser(managed)
	container := corev1.Container{
		Name:            managedPostgresContainerName,
		Image:           managedPostgresImage(managed),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args:            managedPostgresArgs(managed),
		Env: []corev1.EnvVar{
			{Name: "POSTGRES_DB", Value: database},
			{Name: "POSTGRES_USER", Value: user},
			{
				Name: "POSTGRES_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: authSecretName},
						Key:                  managedPostgresPasswordKey,
					},
				},
			},
			{Name: "PGDATA", Value: managedPostgresDataDir},
		},
		Ports: []corev1.ContainerPort{{
			Name:          managedPostgresPortName,
			ContainerPort: managedPostgresPort,
			Protocol:      corev1.ProtocolTCP,
		}},
		StartupProbe:   pgIsReadyProbe(database, user, 60),
		ReadinessProbe: pgIsReadyProbe(database, user, 6),
		VolumeMounts: []corev1.VolumeMount{{
			Name:      managedPostgresDataVolume,
			MountPath: managedPostgresDataMountDir,
		}},
		SecurityContext:          managedPostgresSecurityContext(),
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	if managed.Resources != nil {
		container.Resources = *managed.Resources.DeepCopy()
	}

	return container
}

// managedPostgresArgs renders the postgres CLI args for tunable parameters.
// Returns nil when the spec does not request any tuning so the container
// uses the upstream defaults.
func managedPostgresArgs(managed *yacdv1alpha1.CardanoDBSyncManagedDatabaseSpec) []string {
	if managed == nil || managed.Parameters == nil {
		return nil
	}

	args := []string{}
	if managed.Parameters.MaintenanceWorkMem != nil {
		args = append(args, "-c", "maintenance_work_mem="+postgresMemoryQuantity(*managed.Parameters.MaintenanceWorkMem))
	}
	if managed.Parameters.MaxParallelMaintenanceWorkers != nil {
		args = append(args, "-c", "max_parallel_maintenance_workers="+strconv.Itoa(int(*managed.Parameters.MaxParallelMaintenanceWorkers)))
	}
	if len(args) == 0 {
		return nil
	}

	return args
}

// postgresMemoryQuantity converts a Kubernetes resource.Quantity into the
// kB-unit string Postgres expects for its memory tuning parameters.
func postgresMemoryQuantity(value resource.Quantity) string {
	bytes := value.Value()
	if bytes <= 0 {
		return "0"
	}
	kib := (bytes + 1023) / 1024

	return fmt.Sprintf("%dkB", kib)
}

// pgIsReadyProbe builds an exec-based pg_isready probe. failureThreshold
// is the only parameter that varies between the startup probe (long, 60)
// and the readiness probe (short, 6).
func pgIsReadyProbe(database string, user string, failureThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"pg_isready",
					"-U", user,
					"-d", database,
					"-h", "127.0.0.1",
					"-p", strconv.Itoa(int(managedPostgresPort)),
				},
			},
		},
		PeriodSeconds:    5,
		TimeoutSeconds:   3,
		FailureThreshold: failureThreshold,
	}
}

// restrictedSecurityContext builds the SecurityContext shared by dbsync
// workload containers (follower-node, db-sync, pgpass init). The
// readOnlyRoot flag flips for the db-sync container, which writes to its
// shared volumes through bind-mounted EmptyDirs and cannot have a strictly
// read-only root.
func restrictedSecurityContext(readOnlyRoot bool) *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: new(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		ReadOnlyRootFilesystem: &readOnlyRoot,
		RunAsNonRoot:           new(true),
		RunAsUser:              new(dbSyncRunAsID),
		RunAsGroup:             new(dbSyncRunAsID),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// managedPostgresSecurityContext builds the SecurityContext for the
// managed Postgres container. The container runs as the upstream
// "postgres" UID/GID and drops every capability; it cannot use a read-only
// root because Postgres writes to /var/lib/postgresql/data on startup.
func managedPostgresSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: new(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		RunAsNonRoot: new(true),
		RunAsUser:    new(managedPostgresRunAsID),
		RunAsGroup:   new(managedPostgresRunAsID),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// dbSyncConfigFilePath is the in-container path of the rendered db-sync
// configuration file mounted from the dbsync config ConfigMap.
func dbSyncConfigFilePath() string {
	return dbSyncConfigMountDir + "/" + dbSyncConfigFileName
}

// followerTopologyFilePath is the in-container path of the rendered
// follower-node topology file mounted from the dbsync config ConfigMap.
func followerTopologyFilePath() string {
	return dbSyncConfigMountDir + "/" + followerTopologyFileName
}

// networkArtifactFilePath is the in-container path of a network artifact
// file mounted from the referenced CardanoNetwork's artifact ConfigMap.
func networkArtifactFilePath(name string) string {
	return networkArtifactsMountDir + "/" + name
}

// dbSyncPGPassFilePath is the in-container path of the libpq pgpass file
// the pgPassInitContainer renders into a writable EmptyDir.
func dbSyncPGPassFilePath() string {
	return dbSyncPGPassMountDir + "/" + dbSyncPGPassFileName
}
