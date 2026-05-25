package storage

const RequestedStorageClassAnnotation = "yacd.meigma.io/requested-storage-class"

// RequestedStorageClass returns the originally requested storage class encoded
// on a controller-owned PVC.
func RequestedStorageClass(annotations map[string]string) (string, bool) {
	if annotations == nil {
		return "", false
	}

	value, ok := annotations[RequestedStorageClassAnnotation]
	return value, ok
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
