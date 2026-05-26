package storage

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
)

// RequestedStorageClassDrift reports a change to the controller-owned
// requested storage class annotation.
type RequestedStorageClassDrift struct {
	// Current is the requested storage class on the existing object.
	Current string
	// CurrentSet is true when the existing object carries the annotation.
	CurrentSet bool
	// Desired is the requested storage class on the desired object.
	Desired string
	// DesiredSet is true when the desired object carries the annotation.
	DesiredSet bool
}

// PersistentVolumeClaimDriftReason classifies immutable or unsupported PVC
// drift that a controller should map into its own status contract.
type PersistentVolumeClaimDriftReason string

const (
	// PersistentVolumeClaimDriftRequestedStorageClass reports drift in the
	// controller-owned annotation that records the originally requested storage
	// class.
	PersistentVolumeClaimDriftRequestedStorageClass PersistentVolumeClaimDriftReason = "RequestedStorageClass"
	// PersistentVolumeClaimDriftStorageClass reports an unsupported bound
	// storageClassName change.
	PersistentVolumeClaimDriftStorageClass PersistentVolumeClaimDriftReason = "StorageClass"
	// PersistentVolumeClaimDriftAccessModes reports an unsupported access mode
	// change.
	PersistentVolumeClaimDriftAccessModes PersistentVolumeClaimDriftReason = "AccessModes"
	// PersistentVolumeClaimDriftStorageDecrease reports an unsupported storage
	// shrink.
	PersistentVolumeClaimDriftStorageDecrease PersistentVolumeClaimDriftReason = "StorageDecrease"
)

// PersistentVolumeClaimDrift describes the first PVC drift detected by
// PersistentVolumeClaimDriftFor.
type PersistentVolumeClaimDrift struct {
	// Reason classifies the immutable or unsupported drift.
	Reason PersistentVolumeClaimDriftReason
	// Current is the existing object's value for the drifted field.
	Current string
	// Desired is the desired object's value for the drifted field.
	Desired string
}

// CurrentDisplay formats the current requested storage class for messages.
func (d RequestedStorageClassDrift) CurrentDisplay() string {
	return annotationValue(d.Current, d.CurrentSet)
}

// DesiredDisplay formats the desired requested storage class for messages.
func (d RequestedStorageClassDrift) DesiredDisplay() string {
	return annotationValue(d.Desired, d.DesiredSet)
}

// RequestedStorageClassDriftFor compares the controller-owned requested
// storage class annotation on current and desired object annotations.
func RequestedStorageClassDriftFor(current map[string]string, desired map[string]string, annotationKey string) (RequestedStorageClassDrift, bool) {
	currentStorageClass, currentSet := requestedStorageClass(current, annotationKey)
	desiredStorageClass, desiredSet := requestedStorageClass(desired, annotationKey)
	drift := RequestedStorageClassDrift{
		Current:    currentStorageClass,
		CurrentSet: currentSet,
		Desired:    desiredStorageClass,
		DesiredSet: desiredSet,
	}

	return drift, currentSet != desiredSet || currentStorageClass != desiredStorageClass
}

// PersistentVolumeClaimDriftFor compares the shared immutable PVC fields a
// controller preserves on owned children after creation. Storage expansion is
// allowed; shrinking is reported as drift.
func PersistentVolumeClaimDriftFor(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim, annotationKey string) (PersistentVolumeClaimDrift, bool) {
	if drift, changed := RequestedStorageClassDriftFor(current.Annotations, desired.Annotations, annotationKey); changed {
		return PersistentVolumeClaimDrift{
			Reason:  PersistentVolumeClaimDriftRequestedStorageClass,
			Current: drift.CurrentDisplay(),
			Desired: drift.DesiredDisplay(),
		}, true
	}
	if !storageClassCompatible(current.Spec.StorageClassName, desired.Spec.StorageClassName) {
		return PersistentVolumeClaimDrift{
			Reason:  PersistentVolumeClaimDriftStorageClass,
			Current: stringPtrValue(current.Spec.StorageClassName),
			Desired: stringPtrValue(desired.Spec.StorageClassName),
		}, true
	}
	if !reflect.DeepEqual(current.Spec.AccessModes, desired.Spec.AccessModes) {
		return PersistentVolumeClaimDrift{
			Reason: PersistentVolumeClaimDriftAccessModes,
		}, true
	}

	currentStorage := current.Spec.Resources.Requests[corev1.ResourceStorage]
	desiredStorage := desired.Spec.Resources.Requests[corev1.ResourceStorage]
	if currentStorage.Cmp(desiredStorage) > 0 {
		return PersistentVolumeClaimDrift{
			Reason:  PersistentVolumeClaimDriftStorageDecrease,
			Current: currentStorage.String(),
			Desired: desiredStorage.String(),
		}, true
	}

	return PersistentVolumeClaimDrift{}, false
}
