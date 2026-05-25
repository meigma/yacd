package cardanodbsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
	ctrlstorage "github.com/meigma/yacd/internal/ctrlkit/storage"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	managedPostgresAuthSecretSuffix = "postgres-auth"
	managedPostgresStatePVCSuffix   = "postgres-state"
	managedPostgresSuffix           = "postgres"

	managedPostgresContainerName = "postgres"
	managedPostgresPortName      = "postgres"
	managedPostgresRole          = "postgres"
	managedPostgresDataVolume    = "postgres-state"
	managedPostgresDataMountDir  = "/var/lib/postgresql/data"
	managedPostgresDataDir       = "/var/lib/postgresql/data/pgdata"
	managedPostgresPasswordKey   = "password"

	managedPostgresIdentityAnno            = "yacd.meigma.io/managed-postgres-identity"
	managedPostgresPasswordFingerprintAnno = "yacd.meigma.io/managed-postgres-password-fingerprint"

	defaultManagedPostgresImage             = "postgres:17.2-alpine"
	defaultManagedPostgresDatabase          = "cexplorer"
	defaultManagedPostgresUser              = "postgres"
	defaultManagedPostgresStorageSize       = "10Gi"
	defaultManagedPostgresSSLMode           = "disable"
	managedPostgresPort               int32 = 5432
	managedPostgresRunAsID            int64 = 70
)

type managedPostgresResources struct {
	PersistentVolumeClaim *corev1.PersistentVolumeClaim
	Service               *corev1.Service
	Deployment            *appsv1.Deployment
	IdentityFingerprint   string
}

func (b dbSyncWorkloadBuilder) managedPostgresResources(
	dbSync *yacdv1alpha1.CardanoDBSync,
	authSecret *corev1.Secret,
) (*managedPostgresResources, error) {
	if dbSync == nil {
		return nil, fmt.Errorf("cardanodbsync is required")
	}
	if dbSync.Spec.Database.Managed == nil {
		return nil, unsupportedSpec("managed database spec is required")
	}
	if authSecret == nil {
		return nil, fmt.Errorf("managed Postgres auth Secret is required")
	}
	if b.scheme == nil {
		return nil, fmt.Errorf("scheme is required")
	}

	identityFingerprint, err := managedPostgresIdentityFingerprint(dbSync, authSecret)
	if err != nil {
		return nil, err
	}
	pvc, err := b.managedPostgresPersistentVolumeClaim(dbSync, identityFingerprint)
	if err != nil {
		return nil, err
	}
	service, err := b.managedPostgresService(dbSync)
	if err != nil {
		return nil, err
	}
	deployment, err := b.managedPostgresDeployment(dbSync, authSecret, identityFingerprint)
	if err != nil {
		return nil, err
	}

	return &managedPostgresResources{
		PersistentVolumeClaim: pvc,
		Service:               service,
		Deployment:            deployment,
		IdentityFingerprint:   identityFingerprint,
	}, nil
}

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
		pvc.Annotations[ctrlstorage.RequestedStorageClassAnnotation] = *managed.Storage.StorageClassName
	}
	if err := controllerutil.SetControllerReference(dbSync, pvc, b.scheme); err != nil {
		return nil, fmt.Errorf("set managed Postgres state PVC owner reference: %w", err)
	}

	return pvc, nil
}

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

func postgresMemoryQuantity(value resource.Quantity) string {
	bytes := value.Value()
	if bytes <= 0 {
		return "0"
	}
	kib := (bytes + 1023) / 1024

	return fmt.Sprintf("%dkB", kib)
}

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

func dbSyncDatabaseFromManaged(dbSync *yacdv1alpha1.CardanoDBSync, authSecretName string) dbsync.Database {
	managed := dbSync.Spec.Database.Managed

	return dbsync.Database{
		Host:               fmt.Sprintf("%s.%s.svc.cluster.local", managedPostgresServiceName(dbSync), dbSync.Namespace),
		Port:               managedPostgresPort,
		Name:               managedPostgresDatabaseName(managed),
		User:               managedPostgresUser(managed),
		PasswordSecretName: authSecretName,
		PasswordSecretKey:  managedPostgresPasswordKey,
		SSLMode:            defaultManagedPostgresSSLMode,
	}
}

func managedPostgresImage(managed *yacdv1alpha1.CardanoDBSyncManagedDatabaseSpec) string {
	if managed == nil || strings.TrimSpace(managed.Image) == "" {
		return defaultManagedPostgresImage
	}

	return strings.TrimSpace(managed.Image)
}

func managedPostgresDatabaseName(managed *yacdv1alpha1.CardanoDBSyncManagedDatabaseSpec) string {
	if managed == nil || strings.TrimSpace(managed.Database) == "" {
		return defaultManagedPostgresDatabase
	}

	return strings.TrimSpace(managed.Database)
}

func managedPostgresUser(managed *yacdv1alpha1.CardanoDBSyncManagedDatabaseSpec) string {
	if managed == nil || strings.TrimSpace(managed.User) == "" {
		return defaultManagedPostgresUser
	}

	return strings.TrimSpace(managed.User)
}

type managedPostgresIdentityInput struct {
	Kind         string                           `json:"kind"`
	Image        string                           `json:"image"`
	Database     string                           `json:"database"`
	User         string                           `json:"user"`
	Port         int32                            `json:"port"`
	PasswordKey  string                           `json:"passwordKey"`
	AuthSecret   managedPostgresAuthIdentityInput `json:"authSecret"`
	AuthProvided bool                             `json:"authProvided"`
}

type managedPostgresAuthIdentityInput struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func managedPostgresIdentityFingerprint(dbSync *yacdv1alpha1.CardanoDBSync, authSecret *corev1.Secret) (string, error) {
	managed := dbSync.Spec.Database.Managed
	credentialVersion, err := managedPostgresCredentialVersion(dbSync, authSecret)
	if err != nil {
		return "", err
	}

	input, err := json.Marshal(managedPostgresIdentityInput{
		Kind:         "managed-postgres/v1",
		Image:        managedPostgresImage(managed),
		Database:     managedPostgresDatabaseName(managed),
		User:         managedPostgresUser(managed),
		Port:         managedPostgresPort,
		PasswordKey:  managedPostgresPasswordKey,
		AuthProvided: managed.AuthSecretRef != nil,
		AuthSecret: managedPostgresAuthIdentityInput{
			Name:    authSecret.Name,
			Version: credentialVersion,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal managed Postgres identity input: %w", err)
	}
	sum := sha256.Sum256(input)

	return hex.EncodeToString(sum[:]), nil
}

func managedPostgresCredentialVersion(dbSync *yacdv1alpha1.CardanoDBSync, authSecret *corev1.Secret) (string, error) {
	if dbSync == nil || dbSync.Spec.Database.Managed == nil {
		return "", unsupportedSpec("managed database spec is required")
	}
	if authSecret == nil {
		return "", fmt.Errorf("managed Postgres auth Secret is required")
	}
	if dbSync.Spec.Database.Managed.AuthSecretRef == nil {
		fingerprint := authSecret.Annotations[managedPostgresPasswordFingerprintAnno]
		if fingerprint == "" {
			return "", unsupportedSpec("managed Postgres generated auth Secret is missing password fingerprint")
		}

		return fingerprint, nil
	}

	password := authSecret.Data[managedPostgresPasswordKey]
	if len(password) == 0 {
		return "", unsupportedSpec("managed Postgres auth Secret does not contain key password")
	}

	return managedPostgresPasswordFingerprint(password), nil
}

func managedPostgresPasswordFingerprint(password []byte) string {
	sum := sha256.Sum256(password)

	return hex.EncodeToString(sum[:])
}

func managedPostgresAuthSecretName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, managedPostgresAuthSecretSuffix)
}

func managedPostgresPVCName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, managedPostgresStatePVCSuffix)
}

func managedPostgresServiceName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, managedPostgresSuffix)
}

func managedPostgresDeploymentName(dbSync *yacdv1alpha1.CardanoDBSync) string {
	return ctrlnames.DNSLabelWithSuffix(dbSync.Name, managedPostgresSuffix)
}

func managedPostgresSelectorLabels(dbSync *yacdv1alpha1.CardanoDBSync) map[string]string {
	instance := ctrlnames.LabelValue(dbSync.Name)
	return map[string]string{
		labelAppName:      labelDBSyncAppName,
		labelAppInstance:  instance,
		labelAppComponent: managedPostgresRole,
		labelDBSync:       instance,
		labelCardanoRole:  managedPostgresRole,
	}
}

func managedPostgresLabels(dbSync *yacdv1alpha1.CardanoDBSync) map[string]string {
	labels := managedPostgresSelectorLabels(dbSync)
	labels[labelAppManagedBy] = "yacd"
	return labels
}
