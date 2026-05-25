package artifacts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestComputeDataHash(t *testing.T) {
	dataA := map[string]string{"b": "two", "a": "one"}
	dataB := map[string]string{"a": "one", "b": "two"}

	assert.Equal(t, ComputeDataHash(dataA), ComputeDataHash(dataB))
	assert.Equal(t, "sha256:7bae275b3e58d8fa6b370e69b0d3dddc090c7b8601a566ac7d0286773ff7969a", ComputeDataHash(dataA))
}

func TestValidateConfigMap(t *testing.T) {
	contract := Contract{
		SchemaVersion: "yacd.meigma.io/example/v1alpha1",
		RequiredKeys:  []string{"configuration.yaml", "genesis.json"},
		OptionalKeys:  []string{"optional.json"},
	}
	data := map[string]string{
		"configuration.yaml": "config",
		"genesis.json":       "genesis",
		"optional.json":      "optional",
	}
	hash := ComputeDataHash(data)

	got := ValidateConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				SchemaVersionAnnotation: contract.SchemaVersion,
				DataHashAnnotation:      hash,
			},
		},
		Data: data,
	}, contract, "")

	assert.Equal(t, Result{
		Ready:    true,
		DataHash: hash,
		Reason:   ReasonReady,
		Message:  "artifact ConfigMap is published and verified",
	}, got)
}

func TestValidateConfigMapUsesExpectedHash(t *testing.T) {
	contract := Contract{SchemaVersion: "v1", RequiredKeys: []string{"a"}}
	data := map[string]string{"a": "one"}
	hash := ComputeDataHash(data)

	got := ValidateConfigMap(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{SchemaVersionAnnotation: "v1"}},
		Data:       data,
	}, contract, hash)

	assert.True(t, got.Ready)
	assert.Equal(t, hash, got.DataHash)
}

func TestValidateConfigMapRejectsInvalidInputs(t *testing.T) {
	contract := Contract{
		SchemaVersion: "v1",
		RequiredKeys:  []string{"a", "b"},
		OptionalKeys:  []string{"c"},
	}
	validData := map[string]string{"a": "one", "b": "two"}
	validHash := ComputeDataHash(validData)
	validConfigMap := func() *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					SchemaVersionAnnotation: "v1",
					DataHashAnnotation:      validHash,
				},
			},
			Data: validData,
		}
	}
	deleting := validConfigMap()
	now := metav1.Now()
	deleting.DeletionTimestamp = &now

	tests := []struct {
		name   string
		cm     *corev1.ConfigMap
		hash   string
		reason string
	}{
		{
			name:   "missing ConfigMap",
			cm:     nil,
			reason: ReasonConfigMapMissing,
		},
		{
			name:   "deleting ConfigMap",
			cm:     deleting,
			reason: ReasonConfigMapDeleting,
		},
		{
			name: "schema mismatch",
			cm: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{SchemaVersionAnnotation: "v2"}},
				Data:       validData,
			},
			hash:   validHash,
			reason: ReasonSchemaVersionMismatch,
		},
		{
			name:   "missing hash",
			cm:     &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{SchemaVersionAnnotation: "v1"}}, Data: validData},
			reason: ReasonDataHashMissing,
		},
		{
			name: "binary data",
			cm: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{SchemaVersionAnnotation: "v1"}},
				Data:       validData,
				BinaryData: map[string][]byte{"payload": []byte("bytes")},
			},
			hash:   validHash,
			reason: ReasonBinaryDataUnsupported,
		},
		{
			name: "unsupported key",
			cm: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{SchemaVersionAnnotation: "v1"}},
				Data:       map[string]string{"a": "one", "b": "two", "z": "unsupported"},
			},
			hash:   ComputeDataHash(map[string]string{"a": "one", "b": "two", "z": "unsupported"}),
			reason: ReasonUnsupportedKey,
		},
		{
			name: "missing required key",
			cm: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{SchemaVersionAnnotation: "v1"}},
				Data:       map[string]string{"a": "one"},
			},
			hash:   ComputeDataHash(map[string]string{"a": "one"}),
			reason: ReasonMissingKey,
		},
		{
			name:   "hash mismatch",
			cm:     validConfigMap(),
			hash:   ComputeDataHash(map[string]string{"a": "changed", "b": "two"}),
			reason: ReasonDataHashMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateConfigMap(tt.cm, contract, tt.hash)

			assert.False(t, got.Ready)
			assert.Equal(t, tt.reason, got.Reason)
		})
	}
}
