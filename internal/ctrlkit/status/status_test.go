package status

import (
	"context"
	"errors"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewConditionError(t *testing.T) {
	err := NewConditionError("ResourceConflict", "resource %s is unavailable", "testing/child")

	assert.Equal(t, "ResourceConflict", err.Reason)
	assert.Equal(t, "resource testing/child is unavailable", err.Message)
	assert.Equal(t, err.Message, err.Error())
}

func TestConditionErrorSupportsErrorsAs(t *testing.T) {
	err := error(NewConditionError("UnsupportedSpec", "bad spec"))

	var conditionErr ConditionError
	require.True(t, errors.As(err, &conditionErr))
	assert.Equal(t, "UnsupportedSpec", conditionErr.Reason)
	assert.Equal(t, "bad spec", conditionErr.Message)
}

func TestSetObserved(t *testing.T) {
	conditions := []metav1.Condition{}

	SetObserved(&conditions, 7, Condition("Ready", metav1.ConditionTrue, "Ready", "done"))

	require.Len(t, conditions, 1)
	assert.Equal(t, int64(7), conditions[0].ObservedGeneration)
}

func TestPatchIfChanged(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	require.NoError(t, yacdv1alpha1.AddToScheme(scheme))
	network := &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(network).
		WithStatusSubresource(&yacdv1alpha1.CardanoNetwork{}).
		Build()
	current := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "test", Namespace: "default"}, current))
	original := current.DeepCopy()
	current.Status.ObservedGeneration = 7

	require.NoError(t, PatchIfChanged(ctx, c.Status(), current, original))

	stored := &yacdv1alpha1.CardanoNetwork{}
	require.NoError(t, c.Get(ctx, client.ObjectKey{Name: "test", Namespace: "default"}, stored))
	assert.Equal(t, int64(7), stored.Status.ObservedGeneration)
}

func TestAggregateReady(t *testing.T) {
	ready := Condition("NodeReady", metav1.ConditionTrue, "NodeReady", "node ready")
	blocked := Condition("OgmiosReady", metav1.ConditionFalse, "DeploymentProgressing", "ogmios not ready")

	got := AggregateReady("Ready", "Ready", "all ready", ready, blocked)

	assert.Equal(t, metav1.ConditionFalse, got.Status)
	assert.Equal(t, "DeploymentProgressing", got.Reason)
	assert.Equal(t, "ogmios not ready", got.Message)

	got = AggregateReady("Ready", "Ready", "all ready", ready)
	assert.Equal(t, metav1.ConditionTrue, got.Status)
	assert.Equal(t, "Ready", got.Reason)
	assert.Equal(t, "all ready", got.Message)
}

func TestProgressingForReady(t *testing.T) {
	ready := Condition("Ready", metav1.ConditionFalse, "DeploymentProgressing", "still rolling out")

	got := ProgressingForReady("Progressing", "Ready", "ready", ready, "DeploymentProgressing")

	assert.Equal(t, metav1.ConditionTrue, got.Status)
	assert.Equal(t, "DeploymentProgressing", got.Reason)

	ready = Condition("Ready", metav1.ConditionTrue, "Ready", "ready")
	got = ProgressingForReady("Progressing", "Ready", "ready", ready, "DeploymentProgressing")
	assert.Equal(t, metav1.ConditionFalse, got.Status)
	assert.Equal(t, "Ready", got.Reason)
}
