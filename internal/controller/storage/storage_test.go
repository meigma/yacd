package storage

import (
	"testing"

	ctrlstorage "github.com/meigma/yacd/internal/ctrlkit/storage"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
