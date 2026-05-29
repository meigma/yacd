package storage

import (
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	ctrlstorage "github.com/meigma/yacd/internal/ctrlkit/storage"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// UnsupportedPersistentVolumeClaimDrift maps shared PVC drift detection into a
// status-facing controller condition error.
func UnsupportedPersistentVolumeClaimDrift(
	reason string,
	desired *corev1.PersistentVolumeClaim,
	drift ctrlstorage.PersistentVolumeClaimDrift,
) ctrlstatus.ConditionError {
	switch drift.Reason {
	case ctrlstorage.PersistentVolumeClaimDriftRequestedStorageClass:
		return ctrlstatus.NewConditionError(
			reason,
			"PVC %s requested storageClassName cannot be changed from %s to %s",
			ctrlmetadata.ObjectKey(desired),
			drift.Current,
			drift.Desired,
		)
	case ctrlstorage.PersistentVolumeClaimDriftStorageClass:
		return ctrlstatus.NewConditionError(
			reason,
			"PVC %s storageClassName cannot be changed from %s to %s",
			ctrlmetadata.ObjectKey(desired),
			drift.Current,
			drift.Desired,
		)
	case ctrlstorage.PersistentVolumeClaimDriftAccessModes:
		return ctrlstatus.NewConditionError(
			reason,
			"PVC %s accessModes drifted from desired value",
			ctrlmetadata.ObjectKey(desired),
		)
	case ctrlstorage.PersistentVolumeClaimDriftStorageDecrease:
		return ctrlstatus.NewConditionError(
			reason,
			"PVC %s storage cannot be decreased from %s to %s",
			ctrlmetadata.ObjectKey(desired),
			drift.Current,
			drift.Desired,
		)
	default:
		return ctrlstatus.NewConditionError(
			reason,
			"PVC %s drifted from desired value",
			ctrlmetadata.ObjectKey(desired),
		)
	}
}

// PersistentVolumeClaimUpdateError maps Kubernetes PVC update rejections that
// are meaningful to users into a status-facing controller condition error.
// Other errors are returned unchanged so controller-runtime treats them as
// unexpected reconcile errors.
func PersistentVolumeClaimUpdateError(
	reason string,
	current *corev1.PersistentVolumeClaim,
	desired *corev1.PersistentVolumeClaim,
	err error,
) error {
	if !apierrors.IsForbidden(err) && !apierrors.IsInvalid(err) {
		return err
	}

	currentStorage := current.Spec.Resources.Requests[corev1.ResourceStorage]
	desiredStorage := desired.Spec.Resources.Requests[corev1.ResourceStorage]
	if currentStorage.Cmp(desiredStorage) >= 0 {
		return err
	}

	return ctrlstatus.NewConditionError(
		reason,
		"PVC %s storage expansion from %s to %s was rejected by Kubernetes: %s",
		ctrlmetadata.ObjectKey(desired),
		currentStorage.String(),
		desiredStorage.String(),
		err.Error(),
	)
}
