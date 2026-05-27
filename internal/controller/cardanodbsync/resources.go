package cardanodbsync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// configMap builds the dbsync config ConfigMap holding the rendered
// db-sync configuration and follower-node topology. The annotations carry
// the plan fingerprint, accepted database identity, and the upstream
// network artifact data hash so a hash change rolls the dbsync Pod
// through the standard Deployment hash-change path.
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

// persistentVolumeClaim builds the dbsync state PVC. The annotations carry
// the accepted database identity so identity drift fails fast at the
// callback level rather than after the Pod has tried to start.
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

// followerPersistentVolumeClaim builds the follower-node state PVC. Like
// the dbsync state PVC it carries the accepted database identity so a
// rebuild that changes identity drift fails before mounting.
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

// storagePersistentVolumeClaim builds a PVC of a given size and storage
// class name. The optional storage class is stamped onto an annotation so
// the callback can detect storage-class drift independently of the
// in-PVC field (which Kubernetes mutates after binding).
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
			ctrlannotations.RequestedStorageClass: *storageClassName,
		}
	}

	return pvc, nil
}

// pgPassSecret builds the pgpass Secret consumed by the pgPassInitContainer.
// The Secret carries the rendered libpq password file; the init container
// copies it into a writable EmptyDir with 0600 permissions before the
// db-sync container starts.
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
				dbSyncSecretVersionAnno:    pgPassMaterialFingerprint(pgPass),
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

func pgPassMaterialFingerprint(pgPass string) string {
	sum := sha256.Sum256([]byte(pgPass))

	return "sha256:" + hex.EncodeToString(sum[:])
}

// pgPassFile renders the libpq pgpass file content from the plan database
// inputs and the live database password Secret. Newline characters in any
// field are rejected because pgpass treats them as record separators.
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

// escapePGPassField escapes the two pgpass metacharacters (backslash and
// colon) so the rendered file parses safely.
func escapePGPassField(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, ":", `\:`)

	return value
}

// deployment builds the two-container dbsync workload Deployment. The
// Recreate strategy is required because the follower-node and db-sync
// containers both write to PVCs that cannot be mounted ReadWriteOnce by
// two Pods at once.
func (b dbSyncWorkloadBuilder) deployment(
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	networkArtifacts *corev1.ConfigMap,
	databaseSecret *corev1.Secret,
	plan dbsync.Plan,
) (*appsv1.Deployment, error) {
	selectorLabels := dbSyncWorkloadSelectorLabels(dbSync)
	labels := dbSyncWorkloadLabels(dbSync)
	pgPass, err := pgPassFile(plan, databaseSecret)
	if err != nil {
		return nil, err
	}
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
						dbSyncSecretVersionAnno:    pgPassMaterialFingerprint(pgPass),
					},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: new(false),
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:      new(dbSyncRunAsID),
						RunAsGroup:   new(dbSyncRunAsID),
						RunAsNonRoot: new(true),
						RunAsUser:    new(dbSyncRunAsID),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					InitContainers: []corev1.Container{
						b.pgPassInitContainer(dbSync),
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
							Name: dbSyncPGPassSecretVolumeName,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  dbSyncPGPassSecretName(dbSync),
									DefaultMode: new(int32(0o440)),
								},
							},
						},
						{
							Name: dbSyncPGPassVolumeName,
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

// metricsService builds the ClusterIP Service that fronts the db-sync
// container's Prometheus metrics endpoint.
func (b dbSyncWorkloadBuilder) metricsService(dbSync *yacdv1alpha1.CardanoDBSync, plan dbsync.Plan) (*corev1.Service, error) {
	return b.metricsServiceForSelector(dbSync, plan, dbSyncWorkloadSelectorLabels(dbSync))
}

func (b dbSyncWorkloadBuilder) metricsServiceForSelector(
	dbSync *yacdv1alpha1.CardanoDBSync,
	plan dbsync.Plan,
	selector map[string]string,
) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbSyncMetricsServiceName(dbSync),
			Namespace: dbSync.Namespace,
			Labels:    dbSyncWorkloadLabels(dbSync),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selector,
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

// managedPostgresPersistentVolumeClaim builds the managed Postgres state
// PVC. The annotations carry the accepted managed-Postgres bootstrap
// identity so identity drift fails fast at the callback level.
func (b dbSyncWorkloadBuilder) managedPostgresPersistentVolumeClaim(dbSync *yacdv1alpha1.CardanoDBSync, identityFingerprint string) (*corev1.PersistentVolumeClaim, error) {
	managed := dbSync.Spec.Database.Managed
	quantity, err := resource.ParseQuantity(storageSizeFrom(managed.Storage, defaultManagedPostgresStorageSize))
	if err != nil {
		return nil, unsupportedSpec("parse managed Postgres PVC storage size: %v", err)
	}
	annotations := map[string]string{
		managedPostgresIdentityAnno: identityFingerprint,
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        managedPostgresPVCName(dbSync),
			Namespace:   dbSync.Namespace,
			Labels:      managedPostgresLabels(dbSync),
			Annotations: annotations,
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
	if managed.Storage != nil && managed.Storage.StorageClassName != nil {
		pvc.Spec.StorageClassName = managed.Storage.StorageClassName
		pvc.Annotations[ctrlannotations.RequestedStorageClass] = *managed.Storage.StorageClassName
	}
	if err := controllerutil.SetControllerReference(dbSync, pvc, b.scheme); err != nil {
		return nil, fmt.Errorf("set managed Postgres state PVC owner reference: %w", err)
	}

	return pvc, nil
}

// managedPostgresService builds the ClusterIP Service that fronts the
// managed Postgres workload.
func (b dbSyncWorkloadBuilder) managedPostgresService(dbSync *yacdv1alpha1.CardanoDBSync) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedPostgresServiceName(dbSync),
			Namespace: dbSync.Namespace,
			Labels:    managedPostgresLabels(dbSync),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: managedPostgresSelectorLabels(dbSync),
			Ports: []corev1.ServicePort{{
				Name:       managedPostgresPortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       managedPostgresPort,
				TargetPort: intstr.FromString(managedPostgresPortName),
			}},
		},
	}
	if err := controllerutil.SetControllerReference(dbSync, service, b.scheme); err != nil {
		return nil, fmt.Errorf("set managed Postgres Service owner reference: %w", err)
	}

	return service, nil
}

// managedPostgresDeployment builds the managed Postgres Deployment. Like
// the dbsync workload it uses the Recreate strategy because the PVC is
// ReadWriteOnce.
func (b dbSyncWorkloadBuilder) managedPostgresDeployment(
	dbSync *yacdv1alpha1.CardanoDBSync,
	authSecret *corev1.Secret,
	identityFingerprint string,
) (*appsv1.Deployment, error) {
	managed := dbSync.Spec.Database.Managed
	selectorLabels := managedPostgresSelectorLabels(dbSync)
	secretVersion, err := managedPostgresCredentialVersion(dbSync, authSecret)
	if err != nil {
		return nil, err
	}
	labels := managedPostgresLabels(dbSync)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedPostgresDeploymentName(dbSync),
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
						dbSyncSecretVersionAnno:     secretVersion,
						managedPostgresIdentityAnno: identityFingerprint,
					},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: new(false),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: new(true),
						RunAsUser:    new(managedPostgresRunAsID),
						RunAsGroup:   new(managedPostgresRunAsID),
						FSGroup:      new(managedPostgresRunAsID),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						managedPostgresContainer(managed, authSecret.Name),
					},
					Volumes: []corev1.Volume{{
						Name: managedPostgresDataVolume,
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
								ClaimName: managedPostgresPVCName(dbSync),
							},
						},
					}},
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(dbSync, deployment, b.scheme); err != nil {
		return nil, fmt.Errorf("set managed Postgres Deployment owner reference: %w", err)
	}

	return deployment, nil
}
