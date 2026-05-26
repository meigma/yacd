package metadata

import (
	"fmt"
	"maps"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// OwnerConflictError reports that an existing object cannot be treated as the
// desired controller-owned child.
type OwnerConflictError struct {
	Key     types.NamespacedName
	Message string
}

func (e *OwnerConflictError) Error() string {
	return e.Message
}

// ObjectKey returns a NamespacedName for Kubernetes objects and lightweight
// object-like values used in tests.
func ObjectKey(obj interface {
	GetName() string
	GetNamespace() string
}) types.NamespacedName {
	return types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

// OverlayStringMap overlays desired onto current and returns nil for an empty
// result, matching Kubernetes object metadata conventions.
func OverlayStringMap(current map[string]string, desired map[string]string) map[string]string {
	merged := map[string]string{}
	maps.Copy(merged, current)
	maps.Copy(merged, desired)
	if len(merged) == 0 {
		return nil
	}

	return merged
}

// MergeOwnedAnnotations preserves unrelated annotations while reconciling only
// the explicitly listed controller-owned annotation keys.
func MergeOwnedAnnotations(current map[string]string, desired map[string]string, keys ...string) map[string]string {
	merged := map[string]string{}
	maps.Copy(merged, current)
	for _, key := range keys {
		if value, ok := desired[key]; ok {
			merged[key] = value
			continue
		}
		delete(merged, key)
	}
	if len(merged) == 0 {
		return nil
	}

	return merged
}

// ControlledBy returns true when obj has a controller owner reference matching
// owner and the supplied owner API version/kind.
func ControlledBy(obj metav1.Object, owner metav1.Object, apiVersion string, kind string) bool {
	controller := metav1.GetControllerOf(obj)
	return controller != nil &&
		controller.APIVersion == apiVersion &&
		controller.Kind == kind &&
		controller.Name == owner.GetName() &&
		controller.UID == owner.GetUID()
}

// ValidateControllerOwner verifies that current is controlled by the same
// controller owner reference declared on desired.
func ValidateControllerOwner(current metav1.Object, desired metav1.Object) error {
	desiredController := metav1.GetControllerOf(desired)
	if desiredController == nil {
		return ownerConflict(
			desired,
			"resource %s has no desired controller owner",
			ObjectKey(desired),
		)
	}

	currentController := metav1.GetControllerOf(current)
	if currentController == nil {
		return ownerConflict(
			desired,
			"resource %s already exists without a controller owner",
			ObjectKey(desired),
		)
	}
	if currentController.APIVersion != desiredController.APIVersion ||
		currentController.Kind != desiredController.Kind ||
		currentController.Name != desiredController.Name ||
		currentController.UID != desiredController.UID {
		return ownerConflict(
			desired,
			"resource %s is already controlled by %s/%s",
			ObjectKey(desired),
			currentController.Kind,
			currentController.Name,
		)
	}

	return nil
}

func ownerConflict(obj metav1.Object, format string, args ...any) error {
	return &OwnerConflictError{
		Key:     ObjectKey(obj),
		Message: fmt.Sprintf(format, args...),
	}
}
