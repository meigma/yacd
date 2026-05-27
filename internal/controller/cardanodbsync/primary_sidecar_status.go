package cardanodbsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
)

// primarySidecarMaterialRevisionInput is the stable JSON payload hashed into
// the opaque primary-sidecar rollout revision published in status.
type primarySidecarMaterialRevisionInput struct {
	ConfigMapName               string `json:"configMapName"`
	PGPassSecretName            string `json:"pgpassSecretName"`
	StatePVCName                string `json:"statePVCName"`
	MetricsServiceName          string `json:"metricsServiceName"`
	PlanFingerprint             string `json:"planFingerprint"`
	DatabaseIdentityFingerprint string `json:"databaseIdentityFingerprint"`
	CredentialFingerprint       string `json:"credentialFingerprint"`
	ArtifactDataHash            string `json:"artifactDataHash"`
}

// placementStatus returns the effective placement status shell for the
// supplied CardanoDBSync.
func placementStatus(dbSync *yacdv1alpha1.CardanoDBSync) *yacdv1alpha1.CardanoDBSyncPlacementStatus {
	return &yacdv1alpha1.CardanoDBSyncPlacementStatus{
		Mode: effectivePlacementMode(dbSync),
	}
}

// primarySidecarPlacementStatus renders the attachable primarySidecar status
// contract from the applied DB Sync-owned material.
func primarySidecarPlacementStatus(
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
	resources *primarySidecarDBSyncResources,
) (*yacdv1alpha1.CardanoDBSyncPlacementStatus, error) {
	revision, err := primarySidecarMaterialRevision(resources)
	if err != nil {
		return nil, err
	}

	status := placementStatus(dbSync)
	status.PrimarySidecar = &yacdv1alpha1.CardanoDBSyncPrimarySidecarStatus{
		NetworkName: network.Name,
		Revision:    revision,
		Resources: yacdv1alpha1.CardanoDBSyncPrimarySidecarResourcesStatus{
			ConfigMapName:      resources.ConfigMap.Name,
			PGPassSecretName:   resources.PGPassSecret.Name,
			StatePVCName:       resources.PersistentVolumeClaim.Name,
			MetricsServiceName: resources.MetricsService.Name,
		},
	}

	return status, nil
}

// primarySidecarMaterialRevision hashes all mounted material identities that
// should roll the primary Pod when they change.
func primarySidecarMaterialRevision(resources *primarySidecarDBSyncResources) (string, error) {
	if resources == nil {
		return "", fmt.Errorf("primary-sidecar resources are required")
	}
	if resources.ConfigMap == nil {
		return "", fmt.Errorf("primary-sidecar ConfigMap is required")
	}
	if resources.PGPassSecret == nil {
		return "", fmt.Errorf("primary-sidecar pgpass Secret is required")
	}
	if resources.PersistentVolumeClaim == nil {
		return "", fmt.Errorf("primary-sidecar state PVC is required")
	}
	if resources.MetricsService == nil {
		return "", fmt.Errorf("primary-sidecar metrics Service is required")
	}

	input := primarySidecarMaterialRevisionInput{
		ConfigMapName:               resources.ConfigMap.Name,
		PGPassSecretName:            resources.PGPassSecret.Name,
		StatePVCName:                resources.PersistentVolumeClaim.Name,
		MetricsServiceName:          resources.MetricsService.Name,
		PlanFingerprint:             resources.ConfigMap.Annotations[dbSyncPlanFingerprintAnno],
		DatabaseIdentityFingerprint: resources.ConfigMap.Annotations[dbSyncDatabaseIdentityAnno],
		CredentialFingerprint:       resources.PGPassSecret.Annotations[dbSyncSecretVersionAnno],
		ArtifactDataHash:            resources.ConfigMap.Annotations[dbSyncArtifactDataHashAnno],
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal primary-sidecar material revision input: %w", err)
	}
	sum := sha256.Sum256(payload)

	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
