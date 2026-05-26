package metadata

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestObjectKey(t *testing.T) {
	obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "testing"}}

	assert.Equal(t, types.NamespacedName{Namespace: "testing", Name: "child"}, ObjectKey(obj))
}

func TestOverlayStringMap(t *testing.T) {
	current := map[string]string{"keep": "current", "replace": "old"}
	desired := map[string]string{"replace": "new", "add": "desired"}

	got := OverlayStringMap(current, desired)

	assert.Equal(t, map[string]string{"keep": "current", "replace": "new", "add": "desired"}, got)
	assert.Equal(t, "old", current["replace"])
}

func TestOverlayStringMapReturnsNilWhenEmpty(t *testing.T) {
	assert.Nil(t, OverlayStringMap(nil, nil))
}

func TestMergeOwnedAnnotations(t *testing.T) {
	current := map[string]string{
		"owned.keep":   "old",
		"owned.delete": "old",
		"external":     "value",
	}
	desired := map[string]string{
		"owned.keep": "new",
	}

	got := MergeOwnedAnnotations(current, desired, "owned.keep", "owned.delete")

	assert.Equal(t, map[string]string{"owned.keep": "new", "external": "value"}, got)
}

func TestMergeOwnedAnnotationsReturnsNilWhenEmpty(t *testing.T) {
	got := MergeOwnedAnnotations(map[string]string{"owned": "old"}, nil, "owned")

	assert.Nil(t, got)
}

func TestControlledBy(t *testing.T) {
	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "parent", UID: types.UID("parent-uid")}}
	child := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		OwnerReferences: []metav1.OwnerReference{
			ownerReference("v1", "ConfigMap", "parent", "parent-uid"),
		},
	}}

	assert.True(t, ControlledBy(child, owner, "v1", "ConfigMap"))
	assert.False(t, ControlledBy(child, owner, "v1", "Secret"))
}

func TestValidateDesiredControllerOwner(t *testing.T) {
	desired := desiredWithOwner("child", "testing", "v1", "ConfigMap", "parent", "uid-1")

	require.NoError(t, ValidateDesiredControllerOwner(desired))

	err := ValidateDesiredControllerOwner(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "testing"}})

	var conflict *OwnerConflictError
	require.True(t, errors.As(err, &conflict))
	assert.Equal(t, "resource testing/child has no desired controller owner", conflict.Error())
}

func TestValidateControllerOwner(t *testing.T) {
	current := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      "child",
		Namespace: "testing",
		OwnerReferences: []metav1.OwnerReference{
			ownerReference("testing.example/v1", "Parent", "parent", "uid-1"),
		},
	}}
	desired := current.DeepCopy()

	require.NoError(t, ValidateControllerOwner(current, desired))
}

func TestValidateControllerOwnerConflicts(t *testing.T) {
	tests := []struct {
		name        string
		current     *corev1.ConfigMap
		desired     *corev1.ConfigMap
		wantMessage string
	}{
		{
			name: "desired has no controller owner",
			current: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name:      "child",
				Namespace: "testing",
				OwnerReferences: []metav1.OwnerReference{
					ownerReference("v1", "ConfigMap", "parent", "uid-1"),
				},
			}},
			desired:     &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "testing"}},
			wantMessage: "resource testing/child has no desired controller owner",
		},
		{
			name:        "current has no controller owner",
			current:     &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "testing"}},
			desired:     desiredWithOwner("child", "testing", "v1", "ConfigMap", "parent", "uid-1"),
			wantMessage: "resource testing/child already exists without a controller owner",
		},
		{
			name: "current controller differs",
			current: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name:      "child",
				Namespace: "testing",
				OwnerReferences: []metav1.OwnerReference{
					ownerReference("v1", "ConfigMap", "other", "uid-2"),
				},
			}},
			desired:     desiredWithOwner("child", "testing", "v1", "ConfigMap", "parent", "uid-1"),
			wantMessage: "resource testing/child is already controlled by ConfigMap/other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateControllerOwner(tt.current, tt.desired)

			var conflict *OwnerConflictError
			require.True(t, errors.As(err, &conflict))
			assert.Equal(t, tt.wantMessage, conflict.Error())
		})
	}
}

func desiredWithOwner(name string, namespace string, apiVersion string, kind string, ownerName string, uid types.UID) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		OwnerReferences: []metav1.OwnerReference{
			ownerReference(apiVersion, kind, ownerName, uid),
		},
	}}
}

func ownerReference(apiVersion string, kind string, name string, uid types.UID) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		UID:        uid,
		Controller: &controller,
	}
}
