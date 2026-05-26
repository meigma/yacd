package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const testRequestedStorageClassAnnotation = "testing.example/requested-storage-class"

func TestRequestedStorageClass(t *testing.T) {
	value, ok := RequestedStorageClass(map[string]string{
		testRequestedStorageClassAnnotation: "fast",
	}, testRequestedStorageClassAnnotation)

	assert.True(t, ok)
	assert.Equal(t, "fast", value)

	value, ok = RequestedStorageClass(nil, testRequestedStorageClassAnnotation)
	assert.False(t, ok)
	assert.Empty(t, value)
}

func TestRequestedStorageClassDriftFor(t *testing.T) {
	drift, changed := RequestedStorageClassDriftFor(
		map[string]string{testRequestedStorageClassAnnotation: "slow"},
		map[string]string{testRequestedStorageClassAnnotation: "fast"},
		testRequestedStorageClassAnnotation,
	)

	assert.True(t, changed)
	assert.Equal(t, "slow", drift.Current)
	assert.True(t, drift.CurrentSet)
	assert.Equal(t, "fast", drift.Desired)
	assert.True(t, drift.DesiredSet)
	assert.Equal(t, "slow", drift.CurrentDisplay())
	assert.Equal(t, "fast", drift.DesiredDisplay())

	drift, changed = RequestedStorageClassDriftFor(nil, map[string]string{testRequestedStorageClassAnnotation: "fast"}, testRequestedStorageClassAnnotation)
	assert.True(t, changed)
	assert.Equal(t, "<default>", drift.CurrentDisplay())
	assert.Equal(t, "fast", drift.DesiredDisplay())

	_, changed = RequestedStorageClassDriftFor(
		map[string]string{testRequestedStorageClassAnnotation: "fast"},
		map[string]string{testRequestedStorageClassAnnotation: "fast"},
		testRequestedStorageClassAnnotation,
	)
	assert.False(t, changed)
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
