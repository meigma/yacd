package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequestedStorageClass(t *testing.T) {
	value, ok := RequestedStorageClass(map[string]string{
		RequestedStorageClassAnnotation: "fast",
	})

	assert.True(t, ok)
	assert.Equal(t, "fast", value)

	value, ok = RequestedStorageClass(nil)
	assert.False(t, ok)
	assert.Empty(t, value)
}

func TestStorageClassCompatible(t *testing.T) {
	fast := "fast"
	slow := "slow"

	assert.True(t, StorageClassCompatible(nil, nil))
	assert.True(t, StorageClassCompatible(&fast, nil))
	assert.True(t, StorageClassCompatible(&fast, &fast))
	assert.False(t, StorageClassCompatible(nil, &fast))
	assert.False(t, StorageClassCompatible(&fast, &slow))
}

func TestAnnotationValue(t *testing.T) {
	assert.Equal(t, "fast", AnnotationValue("fast", true))
	assert.Equal(t, "<default>", AnnotationValue("", false))
}

func TestStringPtrValue(t *testing.T) {
	value := "fast"

	assert.Equal(t, "fast", StringPtrValue(&value))
	assert.Equal(t, "<default>", StringPtrValue(nil))
}
