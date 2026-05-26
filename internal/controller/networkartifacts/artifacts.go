package networkartifacts

import (
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	cardanonetworkartifacts "github.com/meigma/yacd/internal/cardano/networkartifacts"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	ctrlartifacts "github.com/meigma/yacd/internal/ctrlkit/artifacts"
	corev1 "k8s.io/api/core/v1"
)

func dataContract() ctrlartifacts.Contract {
	return ctrlartifacts.Contract{
		RequiredKeys: cardanonetworkartifacts.RequiredKeys(),
		OptionalKeys: cardanonetworkartifacts.OptionalKeys(),
	}
}

// ProducerResult reports whether a CardanoNetwork artifact ConfigMap is ready
// to publish in CardanoNetwork status.
type ProducerResult struct {
	Status  yacdv1alpha1.CardanoNetworkArtifactsStatus
	Ready   bool
	Message string
}

// ProducerConfigMap validates the producer-side artifact ConfigMap. The
// validation order matches CardanoNetwork status publication semantics.
func ProducerConfigMap(configMap *corev1.ConfigMap, expectedFingerprint string) ProducerResult {
	if configMap == nil {
		return ProducerResult{
			Message: "artifact ConfigMap is missing",
		}
	}

	status := yacdv1alpha1.CardanoNetworkArtifactsStatus{
		NetworkConfigMapName: configMap.Name,
	}

	if !configMap.DeletionTimestamp.IsZero() {
		return ProducerResult{
			Status:  status,
			Message: "artifact ConfigMap is deleting",
		}
	}

	if configMap.Annotations[ctrlannotations.ArtifactSchemaVersion] != cardanonetworkartifacts.SchemaVersion {
		return ProducerResult{
			Status:  status,
			Message: "artifact ConfigMap schema version is not published",
		}
	}
	status.SchemaVersion = cardanonetworkartifacts.SchemaVersion

	if configMap.Annotations[ctrlannotations.LocalnetFingerprint] != expectedFingerprint {
		return ProducerResult{
			Status:  status,
			Message: "artifact ConfigMap localnet fingerprint does not match the accepted localnet",
		}
	}

	dataHash := strings.TrimSpace(configMap.Annotations[ctrlannotations.ArtifactDataHash])
	if !ctrlartifacts.ValidDataHash(dataHash) {
		return ProducerResult{
			Status:  status,
			Message: "artifact ConfigMap data hash is not published",
		}
	}

	if err := ctrlartifacts.ValidateConfigMapData(configMap, dataContract(), dataHash); err != nil {
		return ProducerResult{
			Status:  status,
			Message: err.Error(),
		}
	}
	status.DataHash = dataHash

	return ProducerResult{
		Status: status,
		Ready:  true,
	}
}

// ProducerConfigMapNeedsRecovery returns true when a producer-owned ConfigMap
// appears published but fails producer validation.
func ProducerConfigMapNeedsRecovery(configMap *corev1.ConfigMap, expectedFingerprint string) bool {
	if configMap == nil || !ctrlartifacts.HasPublishedData(configMap,
		ctrlannotations.ArtifactSchemaVersion,
		ctrlannotations.ArtifactDataHash,
	) {
		return false
	}

	return !ProducerConfigMap(configMap, expectedFingerprint).Ready
}

// ConsumerStatusResult reports whether referenced CardanoNetwork artifact
// status is complete enough for a consumer to read the ConfigMap.
type ConsumerStatusResult struct {
	Ready         bool
	ConfigMapName string
	Message       string
}

// ConsumerStatus validates the status pointer a dependent controller consumes.
func ConsumerStatus(status *yacdv1alpha1.CardanoNetworkArtifactsStatus) ConsumerStatusResult {
	if status == nil ||
		status.NetworkConfigMapName == "" ||
		status.SchemaVersion == "" ||
		status.DataHash == "" {
		return ConsumerStatusResult{
			Message: "Referenced CardanoNetwork artifact status is incomplete",
		}
	}

	return ConsumerStatusResult{
		Ready:         true,
		ConfigMapName: status.NetworkConfigMapName,
	}
}

// ConsumerConfigMapResult reports whether a referenced CardanoNetwork artifact
// ConfigMap is valid for a consumer. Pending results should keep the consumer
// waiting; non-pending failures mean the referenced ConfigMap disagrees with
// published status or contract data.
type ConsumerConfigMapResult struct {
	Ready   bool
	Pending bool
	Message string
}

// ConsumerConfigMap validates a referenced CardanoNetwork artifact ConfigMap
// against the published status values.
func ConsumerConfigMap(
	configMap *corev1.ConfigMap,
	status yacdv1alpha1.CardanoNetworkArtifactsStatus,
) ConsumerConfigMapResult {
	if configMap == nil {
		return ConsumerConfigMapResult{
			Pending: true,
			Message: "Referenced CardanoNetwork artifact ConfigMap does not exist",
		}
	}
	if !configMap.DeletionTimestamp.IsZero() {
		return ConsumerConfigMapResult{
			Pending: true,
			Message: "Referenced CardanoNetwork artifact ConfigMap is deleting",
		}
	}
	if configMap.Annotations[ctrlannotations.ArtifactSchemaVersion] != status.SchemaVersion ||
		configMap.Annotations[ctrlannotations.ArtifactDataHash] != status.DataHash {
		return ConsumerConfigMapResult{
			Message: "Referenced CardanoNetwork artifact ConfigMap metadata does not match status",
		}
	}
	if err := ctrlartifacts.ValidateConfigMapData(configMap, dataContract(), status.DataHash); err != nil {
		return ConsumerConfigMapResult{
			Message: "Referenced CardanoNetwork artifact ConfigMap is invalid: " + err.Error(),
		}
	}

	return ConsumerConfigMapResult{Ready: true}
}
