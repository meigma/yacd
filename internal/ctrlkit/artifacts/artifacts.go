package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	SchemaVersionAnnotation     = "yacd.meigma.io/artifact-schema-version"
	DataHashAnnotation          = "yacd.meigma.io/artifact-data-hash"
	CardanoNetworkSchemaVersion = "yacd.meigma.io/cardano-network-artifacts/v1alpha1"

	ReasonReady                 = "Ready"
	ReasonConfigMapMissing      = "ConfigMapMissing"
	ReasonConfigMapDeleting     = "ConfigMapDeleting"
	ReasonSchemaVersionMismatch = "SchemaVersionMismatch"
	ReasonDataHashMissing       = "DataHashMissing"
	ReasonBinaryDataUnsupported = "BinaryDataUnsupported"
	ReasonUnsupportedKey        = "UnsupportedKey"
	ReasonMissingKey            = "MissingKey"
	ReasonDataHashMismatch      = "DataHashMismatch"
)

var cardanoNetworkRequiredKeys = []string{
	"configuration.yaml",
	"byron-genesis.json",
	"shelley-genesis.json",
	"alonzo-genesis.json",
	"conway-genesis.json",
	"primary-topology.json",
	"yacd-localnet-plan.json",
	"connection.json",
}

var cardanoNetworkOptionalKeys = []string{
	"dijkstra-genesis.json",
}

// Contract describes the schema and key allowlist for a ConfigMap artifact set.
type Contract struct {
	SchemaVersion string
	RequiredKeys  []string
	OptionalKeys  []string
}

// Result reports whether a ConfigMap satisfies an artifact contract.
type Result struct {
	Ready    bool
	DataHash string
	Reason   string
	Message  string
}

// CardanoNetworkContract returns the localnet artifact contract shared by the
// CardanoNetwork producer and dependent controllers.
func CardanoNetworkContract() Contract {
	return Contract{
		SchemaVersion: CardanoNetworkSchemaVersion,
		RequiredKeys:  slices.Clone(cardanoNetworkRequiredKeys),
		OptionalKeys:  slices.Clone(cardanoNetworkOptionalKeys),
	}
}

// ValidateConfigMap validates a non-secret ConfigMap artifact payload against a
// contract and expected data hash. If expectedHash is empty, the ConfigMap hash
// annotation is used as the expected value.
func ValidateConfigMap(configMap *corev1.ConfigMap, contract Contract, expectedHash string) Result {
	if configMap == nil {
		return result(false, "", ReasonConfigMapMissing, "artifact ConfigMap is missing")
	}
	if !configMap.DeletionTimestamp.IsZero() {
		return result(false, "", ReasonConfigMapDeleting, "artifact ConfigMap is deleting")
	}

	if contract.SchemaVersion != "" && configMap.Annotations[SchemaVersionAnnotation] != contract.SchemaVersion {
		return result(false, "", ReasonSchemaVersionMismatch, "artifact ConfigMap schema version does not match")
	}

	expectedHash = strings.TrimSpace(expectedHash)
	if expectedHash == "" {
		expectedHash = strings.TrimSpace(configMap.Annotations[DataHashAnnotation])
	}
	if !ValidDataHash(expectedHash) {
		return result(false, "", ReasonDataHashMissing, "artifact ConfigMap data hash is not published")
	}

	if err := ValidateConfigMapData(configMap, contract, expectedHash); err != nil {
		dataHash := ""
		if strings.Contains(err.Error(), "data hash does not match data") {
			dataHash = ComputeDataHash(configMap.Data)
		}
		return result(false, dataHash, reasonForError(err), err.Error())
	}

	return result(true, ComputeDataHash(configMap.Data), ReasonReady, "artifact ConfigMap is published and verified")
}

// ValidateConfigMapData validates a ConfigMap payload after callers have
// already checked producer-specific metadata such as schema annotations.
func ValidateConfigMapData(configMap *corev1.ConfigMap, contract Contract, expectedHash string) error {
	if len(configMap.BinaryData) > 0 {
		return fmt.Errorf("artifact ConfigMap contains binary data")
	}
	if key, ok := unsupportedDataKey(configMap.Data, contract); ok {
		return fmt.Errorf("artifact ConfigMap contains unsupported key %s", key)
	}

	for _, key := range contract.RequiredKeys {
		if _, ok := configMap.Data[key]; !ok {
			return fmt.Errorf("artifact ConfigMap is missing %s", key)
		}
	}

	actualHash := ComputeDataHash(configMap.Data)
	if strings.TrimSpace(expectedHash) != actualHash {
		return fmt.Errorf("artifact ConfigMap data hash does not match data")
	}

	return nil
}

// HasPublishedData returns true when a ConfigMap appears to contain a published
// artifact payload or publishing metadata.
func HasPublishedData(configMap *corev1.ConfigMap) bool {
	if configMap == nil {
		return false
	}
	if configMap.Annotations[SchemaVersionAnnotation] != "" ||
		configMap.Annotations[DataHashAnnotation] != "" {
		return true
	}

	return len(configMap.Data) > 0
}

// ComputeDataHash returns the stable sha256 digest for ConfigMap string data.
func ComputeDataHash(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	digest := sha256.New()
	for _, key := range keys {
		value := data[key]
		_, _ = fmt.Fprintf(digest, "%d:%s\n%d:", len(key), key, len(value))
		_, _ = io.WriteString(digest, value)
		_, _ = io.WriteString(digest, "\n")
	}

	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func unsupportedDataKey(data map[string]string, contract Contract) (string, bool) {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if !dataKeyAllowed(key, contract) {
			return key, true
		}
	}

	return "", false
}

func dataKeyAllowed(key string, contract Contract) bool {
	return slices.Contains(contract.RequiredKeys, key) ||
		slices.Contains(contract.OptionalKeys, key)
}

// ValidDataHash validates the canonical sha256 artifact data hash format.
func ValidDataHash(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return false
	}
	for _, char := range strings.TrimPrefix(value, "sha256:") {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}

	return true
}

func reasonForError(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "binary data"):
		return ReasonBinaryDataUnsupported
	case strings.Contains(message, "unsupported key"):
		return ReasonUnsupportedKey
	case strings.Contains(message, "is missing"):
		return ReasonMissingKey
	case strings.Contains(message, "data hash does not match"):
		return ReasonDataHashMismatch
	default:
		return ReasonDataHashMismatch
	}
}

func result(ready bool, dataHash string, reason string, message string) Result {
	return Result{
		Ready:    ready,
		DataHash: dataHash,
		Reason:   reason,
		Message:  message,
	}
}
