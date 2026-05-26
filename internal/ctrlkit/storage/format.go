package storage

// requestedStorageClass returns the originally requested storage class encoded
// on a controller-owned PVC.
func requestedStorageClass(annotations map[string]string, annotationKey string) (string, bool) {
	if annotations == nil {
		return "", false
	}

	value, ok := annotations[annotationKey]
	return value, ok
}

// storageClassCompatible returns true when the desired storage class can be
// reconciled onto the current PVC without changing the bound class.
func storageClassCompatible(current *string, desired *string) bool {
	if desired == nil {
		return true
	}
	if current == nil {
		return false
	}

	return *current == *desired
}

// annotationValue formats annotation presence for status and error messages.
func annotationValue(value string, ok bool) string {
	if !ok {
		return "<default>"
	}

	return value
}

// stringPtrValue formats optional string fields for status and error messages.
func stringPtrValue(value *string) string {
	if value == nil {
		return "<default>"
	}

	return *value
}
