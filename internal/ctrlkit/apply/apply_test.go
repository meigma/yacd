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

func TestUnsupported(t *testing.T) {
	err := Unsupported("ResourceConflict", "resource %s is unavailable", "testing/child")

	assert.Equal(t, "ResourceConflict", err.Reason)
	assert.Equal(t, "resource testing/child is unavailable", err.Message)
	assert.Equal(t, err.Message, err.Error())
}

func TestUnsupportedErrorSupportsErrorsAs(t *testing.T) {
	err := error(Unsupported("UnsupportedSpec", "bad spec"))

	var unsupported UnsupportedError
	require.True(t, errors.As(err, &unsupported))
	assert.Equal(t, "UnsupportedSpec", unsupported.Reason)
	assert.Equal(t, "bad spec", unsupported.Message)
}

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

func TestApplyOwnedObjectMapsOwnerConflict(t *testing.T) {
	ctx := context.Background()
	current := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: "testing"}}
	c := newApplyTestClient(t, current)
	desired := ownedConfigMap(t)

	result, _, err := ApplyOwnedObject(ctx, c, desired, OwnedObjectOptions[*corev1.ConfigMap]{
		Current: &corev1.ConfigMap{},
		OwnerConflict: func(err error) error {
			return Unsupported("ResourceConflict", "%s", err.Error())
		},
	})

	assert.Equal(t, controllerutil.OperationResultNone, result)
	var unsupported UnsupportedError
	require.True(t, errors.As(err, &unsupported))
	assert.Equal(t, "ResourceConflict", unsupported.Reason)
	assert.Equal(t, "resource testing/child already exists without a controller owner", unsupported.Message)
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
