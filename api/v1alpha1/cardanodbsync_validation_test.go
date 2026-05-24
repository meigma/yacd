package v1alpha1_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
)

func TestCardanoDBSyncDatabaseValidation(t *testing.T) {
	ctx := context.Background()
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "charts", "yacd", "crds")},
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.Eventually(t, func() bool {
			return testEnv.Stop() == nil
		}, time.Minute, time.Second)
	})

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))

	apiClient, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err)

	namespace := &corev1.Namespace{}
	namespace.Name = "cardanodbsync-validation"
	require.NoError(t, apiClient.Create(ctx, namespace))

	t.Run("accepts external database and defaults fields", func(t *testing.T) {
		object := validCardanoDBSyncValidationObject(namespace.Name, "external")
		require.NoError(t, apiClient.Create(ctx, object))

		current := cardanoDBSyncValidationObject()
		require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(object), current))

		port, found, err := unstructured.NestedInt64(current.Object, "spec", "database", "external", "port")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, int64(5432), port)

		database, found, err := unstructured.NestedString(current.Object, "spec", "database", "external", "database")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "cexplorer", database)

		user, found, err := unstructured.NestedString(current.Object, "spec", "database", "external", "user")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "postgres", user)

		passwordKey, found, err := unstructured.NestedString(current.Object, "spec", "database", "external", "passwordSecretRef", "key")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "password", passwordKey)

		sslMode, found, err := unstructured.NestedString(current.Object, "spec", "database", "external", "sslMode")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncPostgresSSLModeDisable), sslMode)

		_, found, err = unstructured.NestedFieldNoCopy(current.Object, "spec", "config")
		require.NoError(t, err)
		assert.False(t, found, "config should be optional for the default path")
	})

	t.Run("accepts storage class overrides without storage size", func(t *testing.T) {
		object := validCardanoDBSyncValidationObject(namespace.Name, "storage-class-only")
		require.NoError(t, unstructured.SetNestedField(object.Object, "fast-state", "spec", "stateStorage", "storageClassName"))
		require.NoError(t, unstructured.SetNestedField(object.Object, "fast-follower", "spec", "followerNode", "storage", "storageClassName"))
		require.NoError(t, apiClient.Create(ctx, object))

		current := cardanoDBSyncValidationObject()
		require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(object), current))

		stateStorageClass, found, err := unstructured.NestedString(current.Object, "spec", "stateStorage", "storageClassName")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "fast-state", stateStorageClass)

		_, found, err = unstructured.NestedFieldNoCopy(current.Object, "spec", "stateStorage", "size")
		require.NoError(t, err)
		assert.False(t, found)

		followerStorageClass, found, err := unstructured.NestedString(current.Object, "spec", "followerNode", "storage", "storageClassName")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "fast-follower", followerStorageClass)

		_, found, err = unstructured.NestedFieldNoCopy(current.Object, "spec", "followerNode", "storage", "size")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("does not default insert override fields", func(t *testing.T) {
		object := validCardanoDBSyncValidationObject(namespace.Name, "insert-preset")
		require.NoError(t, unstructured.SetNestedField(object.Object, map[string]any{
			"preset": string(yacdv1alpha1.CardanoDBSyncInsertPresetDisableAll),
			"txOut": map[string]any{
				"forceTxIn": true,
			},
			"metadata": map[string]any{
				"keys": []any{int64(42)},
			},
		}, "spec", "config", "insert"))
		require.NoError(t, apiClient.Create(ctx, object))

		current := cardanoDBSyncValidationObject()
		require.NoError(t, apiClient.Get(ctx, client.ObjectKeyFromObject(object), current))

		preset, found, err := unstructured.NestedString(current.Object, "spec", "config", "insert", "preset")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, string(yacdv1alpha1.CardanoDBSyncInsertPresetDisableAll), preset)

		for _, field := range []string{
			"txCbor",
			"ledger",
			"governance",
			"offchainPoolData",
			"offchainVoteData",
			"poolStats",
			"jsonType",
			"removeJsonbFromSchema",
		} {
			_, found, err := unstructured.NestedFieldNoCopy(current.Object, "spec", "config", "insert", field)
			require.NoError(t, err)
			assert.False(t, found, "expected insert.%s to remain unset", field)
		}

		for _, path := range [][]string{
			{"txOut", "mode"},
			{"txOut", "useAddressTable"},
			{"metadata", "enabled"},
		} {
			_, found, err := unstructured.NestedFieldNoCopy(current.Object, "spec", "config", "insert", path[0], path[1])
			require.NoError(t, err)
			assert.False(t, found, "expected insert.%s.%s to remain unset", path[0], path[1])
		}
	})

	testCases := []struct {
		name   string
		mutate func(*testing.T, *unstructured.Unstructured)
	}{
		{
			name: "rejects both database modes",
			mutate: func(t *testing.T, object *unstructured.Unstructured) {
				require.NoError(t, unstructured.SetNestedField(object.Object, map[string]any{}, "spec", "database", "managed"))
			},
		},
		{
			name: "rejects neither database mode",
			mutate: func(t *testing.T, object *unstructured.Unstructured) {
				require.NoError(t, unstructured.SetNestedField(object.Object, map[string]any{}, "spec", "database"))
			},
		},
		{
			name: "rejects invalid external database port",
			mutate: func(t *testing.T, object *unstructured.Unstructured) {
				require.NoError(t, unstructured.SetNestedField(object.Object, int64(0), "spec", "database", "external", "port"))
			},
		},
		{
			name: "rejects invalid external database ssl mode",
			mutate: func(t *testing.T, object *unstructured.Unstructured) {
				require.NoError(t, unstructured.SetNestedField(object.Object, "prefer", "spec", "database", "external", "sslMode"))
			},
		},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			object := validCardanoDBSyncValidationObject(namespace.Name, "invalid-"+string(rune('a'+index)))
			testCase.mutate(t, object)

			err := apiClient.Create(ctx, object)

			require.Error(t, err)
			assert.True(t, apierrors.IsInvalid(err), "expected invalid error, got %T: %v", err, err)
		})
	}
}

func validCardanoDBSyncValidationObject(namespace string, name string) *unstructured.Unstructured {
	object := cardanoDBSyncValidationObject()
	object.SetNamespace(namespace)
	object.SetName(name)
	object.Object["spec"] = map[string]any{
		"networkRef": map[string]any{
			"name": "devnet",
		},
		"database": map[string]any{
			"external": map[string]any{
				"host": "postgres.default.svc.cluster.local",
				"passwordSecretRef": map[string]any{
					"name": "dbsync-postgres",
				},
			},
		},
	}
	return object
}

func cardanoDBSyncValidationObject() *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "yacd.meigma.io",
		Version: "v1alpha1",
		Kind:    "CardanoDBSync",
	})
	return object
}
