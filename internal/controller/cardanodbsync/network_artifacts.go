package cardanodbsync

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

var requiredNetworkArtifactKeys = []string{
	"configuration.yaml",
	"byron-genesis.json",
	"shelley-genesis.json",
	"alonzo-genesis.json",
	"conway-genesis.json",
	"primary-topology.json",
	"yacd-localnet-plan.json",
	"connection.json",
}

var optionalNetworkArtifactKeys = []string{
	"dijkstra-genesis.json",
}

func validateNetworkArtifactsConfigMapData(configMap *corev1.ConfigMap, expectedDataHash string) error {
	if len(configMap.BinaryData) > 0 {
		return fmt.Errorf("artifact ConfigMap contains binary data")
	}
	if key, ok := unsupportedNetworkArtifactDataKey(configMap.Data); ok {
		return fmt.Errorf("artifact ConfigMap contains unsupported key %s", key)
	}

	for _, key := range requiredNetworkArtifactKeys {
		if _, ok := configMap.Data[key]; !ok {
			return fmt.Errorf("artifact ConfigMap is missing %s", key)
		}
	}

	actualDataHash := computeNetworkArtifactDataHash(configMap.Data)
	if strings.TrimSpace(expectedDataHash) != actualDataHash {
		return fmt.Errorf("artifact ConfigMap data hash does not match data")
	}

	return nil
}

func unsupportedNetworkArtifactDataKey(data map[string]string) (string, bool) {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if !networkArtifactDataKeyAllowed(key) {
			return key, true
		}
	}

	return "", false
}

func networkArtifactDataKeyAllowed(key string) bool {
	return slices.Contains(requiredNetworkArtifactKeys, key) ||
		slices.Contains(optionalNetworkArtifactKeys, key)
}

func computeNetworkArtifactDataHash(data map[string]string) string {
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
