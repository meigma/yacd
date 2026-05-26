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

func TestValidateConfigMapData(t *testing.T) {
	contract := Contract{RequiredKeys: []string{"a"}, OptionalKeys: []string{"b"}}
	data := map[string]string{"a": "one", "b": "two"}

	assert.NoError(t, ValidateConfigMapData(&corev1.ConfigMap{Data: data}, contract, ComputeDataHash(data)))
	assert.EqualError(t,
		ValidateConfigMapData(&corev1.ConfigMap{Data: map[string]string{"a": "one", "z": "bad"}}, contract, ComputeDataHash(map[string]string{"a": "one", "z": "bad"})),
		"artifact ConfigMap contains unsupported key z",
	)
	assert.EqualError(t,
		ValidateConfigMapData(&corev1.ConfigMap{Data: map[string]string{"b": "two"}}, contract, ComputeDataHash(map[string]string{"b": "two"})),
		"artifact ConfigMap is missing a",
	)
	assert.EqualError(t,
		ValidateConfigMapData(&corev1.ConfigMap{BinaryData: map[string][]byte{"a": []byte("one")}}, contract, ComputeDataHash(data)),
		"artifact ConfigMap contains binary data",
	)
	assert.EqualError(t,
		ValidateConfigMapData(&corev1.ConfigMap{Data: data}, contract, ComputeDataHash(map[string]string{"a": "different", "b": "two"})),
		"artifact ConfigMap data hash does not match data",
	)
}

func TestHasPublishedData(t *testing.T) {
	const (
		schemaVersionAnnotation = "testing.example/artifact-schema-version"
		dataHashAnnotation      = "testing.example/artifact-data-hash"
	)

	assert.False(t, HasPublishedData(nil))
	assert.False(t, HasPublishedData(&corev1.ConfigMap{}))
	assert.True(t, HasPublishedData(&corev1.ConfigMap{Data: map[string]string{"a": "one"}}))
	assert.True(t, HasPublishedData(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{schemaVersionAnnotation: "v1"}},
	}, schemaVersionAnnotation, dataHashAnnotation))
	assert.True(t, HasPublishedData(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{dataHashAnnotation: "sha256:test"}},
	}, schemaVersionAnnotation, dataHashAnnotation))
	assert.False(t, HasPublishedData(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{schemaVersionAnnotation: "v1"}},
	}, dataHashAnnotation))
}

func TestValidDataHash(t *testing.T) {
	assert.True(t, ValidDataHash("sha256:7bae275b3e58d8fa6b370e69b0d3dddc090c7b8601a566ac7d0286773ff7969a"))
	assert.False(t, ValidDataHash(""))
	assert.False(t, ValidDataHash("sha256:test"))
	assert.False(t, ValidDataHash("sha256:7BAE275B3E58D8FA6B370E69B0D3DDDC090C7B8601A566AC7D0286773FF7969A"))
}
