package storage

import (
	"errors"
	"testing"

	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	ctrlstorage "github.com/meigma/yacd/internal/ctrlkit/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestUnsupportedPersistentVolumeClaimDrift(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "state", Namespace: "testing"},
	}

	tests := []struct {
		name  string
		drift ctrlstorage.PersistentVolumeClaimDrift
		want  string
	}{
		{
			name: "requested storage class",
			drift: ctrlstorage.PersistentVolumeClaimDrift{
				Reason:  ctrlstorage.PersistentVolumeClaimDriftRequestedStorageClass,
				Current: "<default>",
				Desired: "fast",
			},
			want: "PVC testing/state requested storageClassName cannot be changed from <default> to fast",
		},
		{
			name: "storage class",
			drift: ctrlstorage.PersistentVolumeClaimDrift{
				Reason:  ctrlstorage.PersistentVolumeClaimDriftStorageClass,
				Current: "slow",
				Desired: "fast",
			},
			want: "PVC testing/state storageClassName cannot be changed from slow to fast",
		},
		{
			name:  "access modes",
			drift: ctrlstorage.PersistentVolumeClaimDrift{Reason: ctrlstorage.PersistentVolumeClaimDriftAccessModes},
			want:  "PVC testing/state accessModes drifted from desired value",
		},
		{
			name: "storage decrease",
			drift: ctrlstorage.PersistentVolumeClaimDrift{
				Reason:  ctrlstorage.PersistentVolumeClaimDriftStorageDecrease,
				Current: "2Gi",
				Desired: "1Gi",
			},
			want: "PVC testing/state storage cannot be decreased from 2Gi to 1Gi",
		},
		{
			name:  "fallback",
			drift: ctrlstorage.PersistentVolumeClaimDrift{},
			want:  "PVC testing/state drifted from desired value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UnsupportedPersistentVolumeClaimDrift("UnsupportedStorageChange", pvc, tt.drift)

			assert.Equal(t, "UnsupportedStorageChange", err.Reason)
			assert.Equal(t, tt.want, err.Message)
		})
	}
}

func TestPersistentVolumeClaimUpdateErrorMapsExpansionRejection(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "forbidden",
			err: apierrors.NewForbidden(
				corev1.Resource("persistentvolumeclaims"),
				"state",
				errors.New("only dynamically provisioned pvc can be resized and the storageclass that provisions the pvc must support resize"),
			),
		},
		{
			name: "invalid",
			err: apierrors.NewInvalid(
				schema.GroupKind{Kind: "PersistentVolumeClaim"},
				"state",
				field.ErrorList{
					field.Invalid(field.NewPath("spec", "resources", "requests", "storage"), "5Gi", "storageclass does not support resize"),
				},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := PersistentVolumeClaimUpdateError(
				"StorageExpansionRejected",
				storagePVC("2Gi"),
				storagePVC("5Gi"),
				tt.err,
			)

			var conditionErr ctrlstatus.ConditionError
			require.ErrorAs(t, err, &conditionErr)
			assert.Equal(t, "StorageExpansionRejected", conditionErr.Reason)
			assert.Contains(t, conditionErr.Message, "PVC testing/state storage expansion from 2Gi to 5Gi was rejected by Kubernetes")
			assert.Contains(t, conditionErr.Message, tt.err.Error())
		})
	}
}

func TestPersistentVolumeClaimUpdateErrorPassesThroughOtherErrors(t *testing.T) {
	sourceErr := errors.New("apiserver temporarily unavailable")

	err := PersistentVolumeClaimUpdateError("StorageExpansionRejected", storagePVC("2Gi"), storagePVC("5Gi"), sourceErr)

	assert.ErrorIs(t, err, sourceErr)
}

func TestPersistentVolumeClaimUpdateErrorPassesThroughNonExpansionRejections(t *testing.T) {
	sourceErr := apierrors.NewForbidden(
		corev1.Resource("persistentvolumeclaims"),
		"state",
		errors.New("update rejected"),
	)

	err := PersistentVolumeClaimUpdateError("StorageExpansionRejected", storagePVC("5Gi"), storagePVC("5Gi"), sourceErr)

	assert.Same(t, sourceErr, err)
}

func storagePVC(size string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "state", Namespace: "testing"},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(size),
				},
			},
		},
	}
}
