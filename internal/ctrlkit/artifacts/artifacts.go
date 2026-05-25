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
	SchemaVersionAnnotation = "yacd.meigma.io/artifact-schema-version"
	DataHashAnnotation      = "yacd.meigma.io/artifact-data-hash"
)

// Contract describes the schema and key allowlist for a ConfigMap artifact set.
type Contract struct {
	SchemaVersion string
	RequiredKeys  []string
	OptionalKeys  []string
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
