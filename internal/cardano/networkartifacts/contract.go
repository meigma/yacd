package networkartifacts

import (
	"slices"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlartifacts "github.com/meigma/yacd/internal/ctrlkit/artifacts"
	corev1 "k8s.io/api/core/v1"
)

// SchemaVersion identifies the CardanoNetwork artifact payload schema.
const SchemaVersion = "yacd.meigma.io/cardano-network-artifacts/v1alpha1"

// LocalnetFingerprintAnnotation holds the accepted localnet fingerprint.
const LocalnetFingerprintAnnotation = "yacd.meigma.io/localnet-fingerprint"

// ConfigMap data keys published for a CardanoNetwork localnet.
const (
	ConfigurationKey   = "configuration.yaml"
	ByronGenesisKey    = "byron-genesis.json"
	ShelleyGenesisKey  = "shelley-genesis.json"
	AlonzoGenesisKey   = "alonzo-genesis.json"
	ConwayGenesisKey   = "conway-genesis.json"
	DijkstraGenesisKey = "dijkstra-genesis.json"
	PrimaryTopologyKey = "primary-topology.json"
	PlanManifestKey    = "yacd-localnet-plan.json"
	ConnectionKey      = "connection.json"
)

var requiredKeys = []string{
	ConfigurationKey,
	ByronGenesisKey,
	ShelleyGenesisKey,
	AlonzoGenesisKey,
	ConwayGenesisKey,
	PrimaryTopologyKey,
	PlanManifestKey,
	ConnectionKey,
}

var optionalKeys = []string{
	DijkstraGenesisKey,
}

// RequiredKeys returns the required ConfigMap data keys.
func RequiredKeys() []string {
	return slices.Clone(requiredKeys)
}

// OptionalKeys returns the optional ConfigMap data keys.
func OptionalKeys() []string {
	return slices.Clone(optionalKeys)
}

// Contract returns the generic ConfigMap artifact contract for CardanoNetwork
// artifacts.
func Contract() ctrlartifacts.Contract {
	return ctrlartifacts.Contract{
		SchemaVersion: SchemaVersion,
		RequiredKeys:  RequiredKeys(),
		OptionalKeys:  OptionalKeys(),
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

	if configMap.Annotations[ctrlartifacts.SchemaVersionAnnotation] != SchemaVersion {
		return ProducerResult{
			Status:  status,
			Message: "artifact ConfigMap schema version is not published",
		}
	}
	status.SchemaVersion = SchemaVersion

	if configMap.Annotations[LocalnetFingerprintAnnotation] != expectedFingerprint {
		return ProducerResult{
			Status:  status,
			Message: "artifact ConfigMap localnet fingerprint does not match the accepted localnet",
		}
	}

	dataHash := strings.TrimSpace(configMap.Annotations[ctrlartifacts.DataHashAnnotation])
	if !ctrlartifacts.ValidDataHash(dataHash) {
		return ProducerResult{
			Status:  status,
			Message: "artifact ConfigMap data hash is not published",
		}
	}

	if err := ctrlartifacts.ValidateConfigMapData(configMap, Contract(), dataHash); err != nil {
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
	if configMap == nil || !ctrlartifacts.HasPublishedData(configMap) {
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
	if configMap.Annotations[ctrlartifacts.SchemaVersionAnnotation] != status.SchemaVersion ||
		configMap.Annotations[ctrlartifacts.DataHashAnnotation] != status.DataHash {
		return ConsumerConfigMapResult{
			Message: "Referenced CardanoNetwork artifact ConfigMap metadata does not match status",
		}
	}
	if err := ctrlartifacts.ValidateConfigMapData(configMap, Contract(), status.DataHash); err != nil {
		return ConsumerConfigMapResult{
			Message: "Referenced CardanoNetwork artifact ConfigMap is invalid: " + err.Error(),
		}
	}

	return ConsumerConfigMapResult{Ready: true}
}
