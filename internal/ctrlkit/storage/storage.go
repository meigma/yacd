package storage

import (
	"reflect"

	corev1 "k8s.io/api/core/v1"
)

// RequestedStorageClass returns the originally requested storage class encoded
// on a controller-owned PVC.
func RequestedStorageClass(annotations map[string]string, annotationKey string) (string, bool) {
	if annotations == nil {
		return "", false
	}

	value, ok := annotations[annotationKey]
	return value, ok
}

// RequestedStorageClassDrift reports a change to the controller-owned
// requested storage class annotation.
type RequestedStorageClassDrift struct {
	Current    string
	CurrentSet bool
	Desired    string
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
	Reason  PersistentVolumeClaimDriftReason
	Current string
	Desired string
}

// CurrentDisplay formats the current requested storage class for messages.
func (d RequestedStorageClassDrift) CurrentDisplay() string {
	return AnnotationValue(d.Current, d.CurrentSet)
}

// DesiredDisplay formats the desired requested storage class for messages.
func (d RequestedStorageClassDrift) DesiredDisplay() string {
	return AnnotationValue(d.Desired, d.DesiredSet)
}

// RequestedStorageClassDriftFor compares the controller-owned requested
// storage class annotation on current and desired object annotations.
func RequestedStorageClassDriftFor(current map[string]string, desired map[string]string, annotationKey string) (RequestedStorageClassDrift, bool) {
	currentStorageClass, currentSet := RequestedStorageClass(current, annotationKey)
	desiredStorageClass, desiredSet := RequestedStorageClass(desired, annotationKey)
	drift := RequestedStorageClassDrift{
		Current:    currentStorageClass,
		CurrentSet: currentSet,
		Desired:    desiredStorageClass,
		DesiredSet: desiredSet,
	}

	return drift, currentSet != desiredSet || currentStorageClass != desiredStorageClass
}

// PersistentVolumeClaimDriftFor compares the shared immutable PVC fields YACD
// controllers preserve after creation. Storage expansion is allowed; shrinking
// is reported as drift.
func PersistentVolumeClaimDriftFor(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim, annotationKey string) (PersistentVolumeClaimDrift, bool) {
	if drift, changed := RequestedStorageClassDriftFor(current.Annotations, desired.Annotations, annotationKey); changed {
		return PersistentVolumeClaimDrift{
			Reason:  PersistentVolumeClaimDriftRequestedStorageClass,
			Current: drift.CurrentDisplay(),
			Desired: drift.DesiredDisplay(),
		}, true
	}
	if !StorageClassCompatible(current.Spec.StorageClassName, desired.Spec.StorageClassName) {
		return PersistentVolumeClaimDrift{
			Reason:  PersistentVolumeClaimDriftStorageClass,
			Current: StringPtrValue(current.Spec.StorageClassName),
			Desired: StringPtrValue(desired.Spec.StorageClassName),
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

// StorageClassCompatible returns true when the desired storage class can be
// reconciled onto the current PVC without changing the bound class.
func StorageClassCompatible(current *string, desired *string) bool {
	if desired == nil {
		return true
	}
	if current == nil {
		return false
	}

	return *current == *desired
}

// AnnotationValue formats annotation presence for status and error messages.
func AnnotationValue(value string, ok bool) string {
	if !ok {
		return "<default>"
	}

	return value
}

// StringPtrValue formats optional string fields for status and error messages.
func StringPtrValue(value *string) string {
	if value == nil {
		return "<default>"
	}

	return *value
}
