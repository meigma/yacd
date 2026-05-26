package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	testRequestedStorageClassAnnotation = "testing.example/requested-storage-class"
	testFastStorageClass                = "fast"
	testSlowStorageClass                = "slow"
)

func TestRequestedStorageClass(t *testing.T) {
	value, ok := RequestedStorageClass(map[string]string{
		testRequestedStorageClassAnnotation: testFastStorageClass,
	}, testRequestedStorageClassAnnotation)

	assert.True(t, ok)
	assert.Equal(t, testFastStorageClass, value)

	value, ok = RequestedStorageClass(nil, testRequestedStorageClassAnnotation)
	assert.False(t, ok)
	assert.Empty(t, value)
}

func TestRequestedStorageClassDriftFor(t *testing.T) {
	drift, changed := RequestedStorageClassDriftFor(
		map[string]string{testRequestedStorageClassAnnotation: testSlowStorageClass},
		map[string]string{testRequestedStorageClassAnnotation: testFastStorageClass},
		testRequestedStorageClassAnnotation,
	)

	assert.True(t, changed)
	assert.Equal(t, testSlowStorageClass, drift.Current)
	assert.True(t, drift.CurrentSet)
	assert.Equal(t, testFastStorageClass, drift.Desired)
	assert.True(t, drift.DesiredSet)
	assert.Equal(t, testSlowStorageClass, drift.CurrentDisplay())
	assert.Equal(t, testFastStorageClass, drift.DesiredDisplay())

	drift, changed = RequestedStorageClassDriftFor(nil, map[string]string{testRequestedStorageClassAnnotation: testFastStorageClass}, testRequestedStorageClassAnnotation)
	assert.True(t, changed)
	assert.Equal(t, "<default>", drift.CurrentDisplay())
	assert.Equal(t, testFastStorageClass, drift.DesiredDisplay())

	_, changed = RequestedStorageClassDriftFor(
		map[string]string{testRequestedStorageClassAnnotation: testFastStorageClass},
		map[string]string{testRequestedStorageClassAnnotation: testFastStorageClass},
		testRequestedStorageClassAnnotation,
	)
	assert.False(t, changed)
}

func TestStorageClassCompatible(t *testing.T) {
	fast := testFastStorageClass
	slow := testSlowStorageClass

	assert.True(t, StorageClassCompatible(nil, nil))
	assert.True(t, StorageClassCompatible(&fast, nil))
	assert.True(t, StorageClassCompatible(&fast, &fast))
	assert.False(t, StorageClassCompatible(nil, &fast))
	assert.False(t, StorageClassCompatible(&fast, &slow))
}

func TestPersistentVolumeClaimDriftFor(t *testing.T) {
	fast := testFastStorageClass
	slow := testSlowStorageClass
	current := testPVC("2Gi", &fast)
	current.Annotations = map[string]string{testRequestedStorageClassAnnotation: testFastStorageClass}
	desired := testPVC("1Gi", &slow)
	desired.Annotations = map[string]string{testRequestedStorageClassAnnotation: testFastStorageClass}

	drift, changed := PersistentVolumeClaimDriftFor(current, desired, testRequestedStorageClassAnnotation)

	assert.True(t, changed)
	assert.Equal(t, PersistentVolumeClaimDriftStorageClass, drift.Reason)
	assert.Equal(t, testFastStorageClass, drift.Current)
	assert.Equal(t, testSlowStorageClass, drift.Desired)

	desired.Spec.StorageClassName = &fast
	drift, changed = PersistentVolumeClaimDriftFor(current, desired, testRequestedStorageClassAnnotation)
	assert.True(t, changed)
	assert.Equal(t, PersistentVolumeClaimDriftStorageDecrease, drift.Reason)
	assert.Equal(t, "2Gi", drift.Current)
	assert.Equal(t, "1Gi", drift.Desired)
}

func testPVC(storage string, storageClass *string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: storageClass,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(storage),
			}},
		},
	}
}

func TestAnnotationValue(t *testing.T) {
	assert.Equal(t, testFastStorageClass, AnnotationValue(testFastStorageClass, true))
	assert.Equal(t, "<default>", AnnotationValue("", false))
}

func TestStringPtrValue(t *testing.T) {
	value := testFastStorageClass

	assert.Equal(t, testFastStorageClass, StringPtrValue(&value))
	assert.Equal(t, "<default>", StringPtrValue(nil))
}
