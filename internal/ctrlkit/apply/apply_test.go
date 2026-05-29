package apply

import (
	"context"
	"errors"
	"testing"

	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestApplyOwnedObjectCreatesMissingObject(t *testing.T) {
	ctx := context.Background()
	c := newApplyTestClient(t)
	desired := ownedConfigMap(t)

	result, current, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current: &corev1.ConfigMap{},
		Default: func(desired *corev1.ConfigMap) error {
			desired.Annotations = map[string]string{"defaulted": "true"}
			return nil
		},
		Validate: func(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
			t.Fatal("Validate must not run for newly-created objects")
			return nil
		},
		Mutate: func(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
			t.Fatal("Mutate must not run for newly-created objects")
			return nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, controllerutil.OperationResultCreated, result)
	assert.Equal(t, map[string]string{"key": "desired"}, current.Data)
	assert.Equal(t, map[string]string{"defaulted": "true"}, current.Annotations)

	stored := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "child", Namespace: "testing"}, stored))
	assert.Equal(t, map[string]string{"key": "desired"}, stored.Data)
	assert.Equal(t, map[string]string{"defaulted": "true"}, stored.Annotations)
}

func TestApplyOwnedObjectRejectsDesiredWithoutControllerOwner(t *testing.T) {
	ctx := context.Background()
	c := newApplyTestClient(t)
	desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "testing"}}

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current: &corev1.ConfigMap{},
	})

	assert.Equal(t, controllerutil.OperationResultNone, result)
	var ownerConflict *ctrlmetadata.OwnerConflictError
	require.ErrorAs(t, err, &ownerConflict)
	assert.Equal(t, "resource testing/child has no desired controller owner", err.Error())

	stored := &corev1.ConfigMap{}
	assert.Error(t, c.Get(ctx, client.ObjectKey{Name: "child", Namespace: "testing"}, stored))
}

func TestApplyOwnedObjectPatchesChangedObject(t *testing.T) {
	ctx := context.Background()
	current := ownedConfigMap(t)
	current.Data = map[string]string{"key": "old"}
	c := newApplyTestClient(t, current)
	desired := ownedConfigMap(t)

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current: &corev1.ConfigMap{},
		Mutate: func(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
			current.Labels = ctrlmetadata.OverlayStringMap(current.Labels, desired.Labels)
			current.Data = desired.Data
			return nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, controllerutil.OperationResultUpdated, result)

	stored := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "child", Namespace: "testing"}, stored))
	assert.Equal(t, map[string]string{"key": "desired"}, stored.Data)
}

func TestApplyOwnedObjectDefaultsToPatchUpdateMode(t *testing.T) {
	ctx := context.Background()
	current := ownedConfigMap(t)
	current.Data = map[string]string{"key": "old"}

	var patchCalls int
	var updateCalls int
	c := newApplyTestClientWithInterceptor(t, []client.Object{current}, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			patchCalls++
			return c.Patch(ctx, obj, patch, opts...)
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			updateCalls++
			return c.Update(ctx, obj, opts...)
		},
	})
	desired := ownedConfigMap(t)

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current: &corev1.ConfigMap{},
		Mutate: func(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
			current.Data = desired.Data
			return nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, controllerutil.OperationResultUpdated, result)
	assert.Equal(t, 1, patchCalls)
	assert.Zero(t, updateCalls)
}

func TestApplyOwnedObjectUsesUpdateModeUpdate(t *testing.T) {
	ctx := context.Background()
	current := ownedConfigMap(t)
	current.Data = map[string]string{"key": "old"}

	var patchCalls int
	var updateCalls int
	c := newApplyTestClientWithInterceptor(t, []client.Object{current}, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			patchCalls++
			return c.Patch(ctx, obj, patch, opts...)
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			updateCalls++
			return c.Update(ctx, obj, opts...)
		},
	})
	desired := ownedConfigMap(t)

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current:    &corev1.ConfigMap{},
		UpdateMode: UpdateModeUpdate,
		Mutate: func(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
			current.Data = desired.Data
			return nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, controllerutil.OperationResultUpdated, result)
	assert.Zero(t, patchCalls)
	assert.Equal(t, 1, updateCalls)
}

func TestApplyOwnedObjectMapsPatchError(t *testing.T) {
	ctx := context.Background()
	current := ownedConfigMap(t)
	current.Data = map[string]string{"key": "old"}
	sourceErr := errors.New("patch failed")
	c := newApplyTestClientWithInterceptor(t, []client.Object{current}, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			return sourceErr
		},
	})
	desired := ownedConfigMap(t)
	var updateErrCalls int

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current: &corev1.ConfigMap{},
		Mutate: func(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
			current.Data = desired.Data
			return nil
		},
		UpdateError: func(current *corev1.ConfigMap, desired *corev1.ConfigMap, err error) error {
			updateErrCalls++
			assert.Equal(t, map[string]string{"key": "old"}, current.Data)
			assert.Equal(t, map[string]string{"key": "desired"}, desired.Data)
			assert.ErrorIs(t, err, sourceErr)
			return mappedUpdateError{message: "mapped patch error"}
		},
	})

	assert.Equal(t, controllerutil.OperationResultNone, result)
	require.ErrorAs(t, err, &mappedUpdateError{})
	assert.Equal(t, "mapped patch error", err.Error())
	assert.Equal(t, 1, updateErrCalls)
}

func TestApplyOwnedObjectMapsUpdateError(t *testing.T) {
	ctx := context.Background()
	current := ownedConfigMap(t)
	current.Data = map[string]string{"key": "old"}
	sourceErr := errors.New("update failed")
	c := newApplyTestClientWithInterceptor(t, []client.Object{current}, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			return sourceErr
		},
	})
	desired := ownedConfigMap(t)
	var updateErrCalls int

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current:    &corev1.ConfigMap{},
		UpdateMode: UpdateModeUpdate,
		Mutate: func(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
			current.Data = desired.Data
			return nil
		},
		UpdateError: func(current *corev1.ConfigMap, desired *corev1.ConfigMap, err error) error {
			updateErrCalls++
			assert.Equal(t, map[string]string{"key": "old"}, current.Data)
			assert.Equal(t, map[string]string{"key": "desired"}, desired.Data)
			assert.ErrorIs(t, err, sourceErr)
			return mappedUpdateError{message: "mapped update error"}
		},
	})

	assert.Equal(t, controllerutil.OperationResultNone, result)
	require.ErrorAs(t, err, &mappedUpdateError{})
	assert.Equal(t, "mapped update error", err.Error())
	assert.Equal(t, 1, updateErrCalls)
}

func TestApplyOwnedObjectReturnsNoneWhenUnchanged(t *testing.T) {
	ctx := context.Background()
	current := ownedConfigMap(t)
	c := newApplyTestClient(t, current)
	desired := ownedConfigMap(t)

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current: &corev1.ConfigMap{},
		Mutate: func(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
			current.Data = desired.Data
			return nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, controllerutil.OperationResultNone, result)
}

func TestApplyOwnedObjectDoesNotMapValidationError(t *testing.T) {
	ctx := context.Background()
	sourceErr := errors.New("validation failed")
	c := newApplyTestClient(t, ownedConfigMap(t))
	desired := ownedConfigMap(t)
	var updateErrCalls int

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current: &corev1.ConfigMap{},
		Validate: func(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
			return sourceErr
		},
		UpdateError: func(current *corev1.ConfigMap, desired *corev1.ConfigMap, err error) error {
			updateErrCalls++
			return err
		},
	})

	assert.Equal(t, controllerutil.OperationResultNone, result)
	assert.ErrorIs(t, err, sourceErr)
	assert.Zero(t, updateErrCalls)
}

func TestApplyOwnedObjectDoesNotMapUnchangedObject(t *testing.T) {
	ctx := context.Background()
	c := newApplyTestClient(t, ownedConfigMap(t))
	desired := ownedConfigMap(t)
	var updateErrCalls int

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current: &corev1.ConfigMap{},
		Mutate: func(current *corev1.ConfigMap, desired *corev1.ConfigMap) error {
			current.Data = desired.Data
			return nil
		},
		UpdateError: func(current *corev1.ConfigMap, desired *corev1.ConfigMap, err error) error {
			updateErrCalls++
			return err
		},
	})

	require.NoError(t, err)
	assert.Equal(t, controllerutil.OperationResultNone, result)
	assert.Zero(t, updateErrCalls)
}

func TestApplyOwnedObjectMapsOwnerConflict(t *testing.T) {
	ctx := context.Background()
	current := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "testing"}}
	c := newApplyTestClient(t, current)
	desired := ownedConfigMap(t)

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current: &corev1.ConfigMap{},
		OwnerConflict: func(err error) error {
			return mappedOwnerConflict{message: err.Error()}
		},
	})

	assert.Equal(t, controllerutil.OperationResultNone, result)
	var mapped mappedOwnerConflict
	require.ErrorAs(t, err, &mapped)
	assert.Equal(t, "resource testing/child already exists without a controller owner", err.Error())
}

type mappedUpdateError struct {
	message string
}

func (e mappedUpdateError) Error() string {
	return e.message
}

type mappedOwnerConflict struct {
	message string
}

func (e mappedOwnerConflict) Error() string {
	return e.message
}

func newApplyTestClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

func newApplyTestClientWithInterceptor(t *testing.T, objects []client.Object, funcs interceptor.Funcs) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithInterceptorFuncs(funcs).
		Build()
}

func ownedConfigMap(t *testing.T) *corev1.ConfigMap {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	parent := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "parent",
			Namespace: "testing",
			UID:       types.UID("parent-uid"),
		},
	}
	child := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "child",
			Namespace: "testing",
			Labels:    map[string]string{"app": "test"},
		},
		Data: map[string]string{"key": "desired"},
	}
	require.NoError(t, controllerutil.SetControllerReference(parent, child, scheme))

	return child
}
