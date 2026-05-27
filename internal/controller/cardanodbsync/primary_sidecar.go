package cardanodbsync

import (
	"fmt"
	"maps"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
	"github.com/meigma/yacd/internal/cardano/primarypod"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// PrimarySidecarContainerName is the db-sync container name CardanoNetwork
	// checks when attributing primary-sidecar readiness.
	PrimarySidecarContainerName = dbSyncContainerName

	conditionMessagePrimarySidecarResourcesApplied = "CardanoDBSync primary-sidecar resources are applied"
	conditionMessageDedicatedFollowerNotUsed       = "Dedicated follower node is not used by primarySidecar placement"
	conditionMessageNodeSocketReady                = "Primary node socket is ready"
	conditionMessageNodeSocketNotReady             = "Primary node socket is not ready"
	conditionMessagePrimaryDeploymentMissing       = "CardanoNetwork primary Deployment is missing"
	conditionMessagePrimaryDeploymentStale         = "CardanoNetwork primary Deployment has not observed the latest generation"
	conditionMessagePrimaryDeploymentBusy          = "CardanoNetwork primary Deployment is not available"
)

// PrimarySidecarAttachment is the pod-template fragment CardanoNetwork
// appends to the primary node Deployment for exactly one eligible
// primarySidecar claim.
type PrimarySidecarAttachment struct {
	// InitContainer renders pgpass material inside the shared Pod.
	InitContainer corev1.Container
	// Container is the long-running cardano-db-sync sidecar.
	Container corev1.Container
	// Volumes are the DB Sync and network artifacts volumes mounted by the
	// init container and sidecar.
	Volumes []corev1.Volume
	// PodLabels are merged into the primary Pod template without changing the
	// Deployment selector.
	PodLabels map[string]string
	// PodAnnotations are merged into the primary Pod template to trigger rolls
	// when sidecar-mounted material changes.
	PodAnnotations map[string]string
}

// PrimarySidecarAttachmentResources are the status-published resource names
// CardanoNetwork uses to render a db-sync sidecar.
type PrimarySidecarAttachmentResources struct {
	// NetworkArtifactsConfigMapName is the CardanoNetwork-owned artifacts
	// ConfigMap mounted by db-sync.
	NetworkArtifactsConfigMapName string
	// ConfigMapName is the CardanoDBSync-owned db-sync config ConfigMap.
	ConfigMapName string
	// PGPassSecretName is the CardanoDBSync-owned pgpass Secret.
	PGPassSecretName string
	// StatePVCName is the CardanoDBSync-owned db-sync state PVC.
	StatePVCName string
	// Revision is the opaque rollout revision CardanoDBSync published in
	// status.
	Revision string
}

// PrimarySidecarPodLabels returns the non-conflicting labels CardanoNetwork
// should add to the primary Pod template so the CardanoDBSync-owned metrics
// Service can select the sidecar without changing the Deployment selector.
func PrimarySidecarPodLabels(dbSync *yacdv1alpha1.CardanoDBSync) map[string]string {
	return primarySidecarPodSelectorLabels(dbSync)
}

// PrimarySidecarClaimReadyForAttachment returns the claim's attachable
// primarySidecar status when it is current, material-ready, and valid for the
// supplied CardanoNetwork. CardanoNetwork uses this to avoid reading
// CardanoDBSync-owned child resources.
func PrimarySidecarClaimReadyForAttachment(
	dbSync *yacdv1alpha1.CardanoDBSync,
	networkName string,
) (*yacdv1alpha1.CardanoDBSyncPrimarySidecarStatus, bool) {
	if dbSync == nil || dbSync.Status.ObservedGeneration < dbSync.Generation {
		return nil, false
	}
	condition := apimeta.FindStatusCondition(dbSync.Status.Conditions, string(conditionTypeSidecarMaterialReady))
	if condition == nil ||
		condition.Status != metav1.ConditionTrue ||
		condition.ObservedGeneration < dbSync.Generation {
		return nil, false
	}
	if dbSync.Status.Placement == nil ||
		dbSync.Status.Placement.Mode != yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar ||
		dbSync.Status.Placement.PrimarySidecar == nil {
		return nil, false
	}
	sidecar := dbSync.Status.Placement.PrimarySidecar
	if sidecar.NetworkName != networkName ||
		sidecar.Revision == "" ||
		sidecar.Resources.ConfigMapName == "" ||
		sidecar.Resources.PGPassSecretName == "" ||
		sidecar.Resources.StatePVCName == "" ||
		sidecar.Resources.MetricsServiceName == "" {
		return nil, false
	}

	return sidecar, true
}

// PrimarySidecarDatabase returns the database planner input CardanoNetwork
// needs to render a db-sync sidecar. The CardanoDBSync controller still owns
// validation and Secret materialization; this exposes only non-secret
// connection metadata used in the container environment.
func PrimarySidecarDatabase(dbSync *yacdv1alpha1.CardanoDBSync) (dbsync.Database, bool) {
	switch {
	case dbSync == nil:
		return dbsync.Database{}, false
	case dbSync.Spec.Database.External != nil && dbSync.Spec.Database.Managed == nil:
		return dbSyncDatabaseFromExternal(dbSync.Spec.Database.External), true
	case dbSync.Spec.Database.Managed != nil && dbSync.Spec.Database.External == nil:
		secretName := managedPostgresAuthSecretName(dbSync)
		if dbSync.Spec.Database.Managed.AuthSecretRef != nil {
			secretName = dbSync.Spec.Database.Managed.AuthSecretRef.Name
		}
		return dbSyncDatabaseFromManaged(dbSync, secretName), true
	default:
		return dbsync.Database{}, false
	}
}

// BuildPrimarySidecarAttachment builds the pod-template fragment
// CardanoNetwork appends to its primary Deployment. It is pure and does not
// require a runtime.Scheme because CardanoDBSync already owns the mounted
// resources.
func BuildPrimarySidecarAttachment(
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	database dbsync.Database,
	resources PrimarySidecarAttachmentResources,
) (*PrimarySidecarAttachment, error) {
	if err := ValidatePrimarySidecarLocalNetwork(dbSync, network); err != nil {
		return nil, err
	}
	if resources.ConfigMapName == "" {
		return nil, fmt.Errorf("db-sync ConfigMap name is required")
	}
	if resources.PGPassSecretName == "" {
		return nil, fmt.Errorf("db-sync pgpass Secret name is required")
	}
	if resources.StatePVCName == "" {
		return nil, fmt.Errorf("db-sync state PVC name is required")
	}
	if resources.Revision == "" {
		return nil, fmt.Errorf("db-sync sidecar revision is required")
	}
	if resources.NetworkArtifactsConfigMapName == "" {
		return nil, fmt.Errorf("network artifacts ConfigMap name is required")
	}

	spec, err := dbSyncPlanSpec(dbSync, network, database)
	if err != nil {
		return nil, err
	}
	plan, err := dbsync.BuildPlan(spec)
	if err != nil {
		return nil, unsupportedSpec("build db-sync plan: %v", err)
	}

	// The reused container helpers are pure and do not depend on scheme or the
	// follower-node image override.
	builder := dbSyncWorkloadBuilder{}
	return &PrimarySidecarAttachment{
		InitContainer: builder.pgPassInitContainer(dbSync),
		Container:     builder.dbSyncContainer(dbSync, plan),
		Volumes:       primarySidecarVolumes(resources),
		PodLabels:     primarySidecarPodSelectorLabels(dbSync),
		PodAnnotations: map[string]string{
			dbSyncSidecarRevisionAnno: resources.Revision,
		},
	}, nil
}

// ValidatePrimarySidecarLocalNetwork applies the primary-sidecar runtime
// constraints that CardanoDBSync owns as the db-sync status surface.
func ValidatePrimarySidecarLocalNetwork(dbSync *yacdv1alpha1.CardanoDBSync, network *yacdv1alpha1.CardanoNetwork) error {
	if dbSync == nil {
		return fmt.Errorf("cardanodbsync is required")
	}
	if network == nil {
		return fmt.Errorf("cardanonetwork is required")
	}
	if network.Spec.Mode != yacdv1alpha1.CardanoNetworkModeLocal {
		return unsupportedSpec("primarySidecar placement is supported only for local CardanoNetwork resources")
	}

	dbSyncPort := int32(0)
	if dbSync.Spec.Config.Runtime != nil {
		dbSyncPort = dbSync.Spec.Config.Runtime.MetricsPort
	}
	if dbSyncPort == 0 {
		dbSyncPort = runtimeSettings(dbSync).MetricsPort
	}

	if owner, ok := primarypod.PortOwners(network)[dbSyncPort]; ok {
		return unsupportedSpec("db-sync metrics port %d conflicts with %s port in the primary Pod", dbSyncPort, owner)
	}

	return nil
}

// primarySidecarPodSelectorLabels returns the DB Sync discriminator labels
// added only to the primary Pod template so the metrics Service can target
// the attached sidecar.
func primarySidecarPodSelectorLabels(dbSync *yacdv1alpha1.CardanoDBSync) map[string]string {
	return map[string]string{
		labelDBSync: dbSyncWorkloadSelectorLabels(dbSync)[labelDBSync],
	}
}

// primarySidecarMetricsSelectorLabels returns the Service selector for a
// primary-sidecar metrics Service: the immutable primary Pod selector plus the
// CardanoDBSync discriminator label.
func primarySidecarMetricsSelectorLabels(dbSync *yacdv1alpha1.CardanoDBSync, network *yacdv1alpha1.CardanoNetwork) map[string]string {
	labels := primaryNetworkSelectorLabels(network)
	maps.Copy(labels, primarySidecarPodSelectorLabels(dbSync))

	return labels
}

// primarySidecarVolumes renders the volumes CardanoNetwork appends to the
// primary Pod when attaching db-sync.
func primarySidecarVolumes(resources PrimarySidecarAttachmentResources) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: networkArtifactsVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: resources.NetworkArtifactsConfigMapName},
				},
			},
		},
		{
			Name: dbSyncConfigMapVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: resources.ConfigMapName},
				},
			},
		},
		{
			Name: dbSyncStateVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: resources.StatePVCName,
				},
			},
		},
		{
			Name: dbSyncPGPassSecretVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  resources.PGPassSecretName,
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
	}
}

// primaryNetworkDeploymentName returns the referenced CardanoNetwork primary
// Deployment name using the shared primary-pod vocabulary.
func primaryNetworkDeploymentName(network *yacdv1alpha1.CardanoNetwork) string {
	return primarypod.WorkloadName(network)
}

// primaryNetworkSelectorLabels returns the referenced CardanoNetwork primary
// Pod selector labels using the shared primary-pod vocabulary.
func primaryNetworkSelectorLabels(network *yacdv1alpha1.CardanoNetwork) map[string]string {
	return primarypod.SelectorLabels(network)
}
