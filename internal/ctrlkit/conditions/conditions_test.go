package conditions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCondition(t *testing.T) {
	got := Condition("Ready", metav1.ConditionTrue, "Ready", "resource is ready")

	assert.Equal(t, "Ready", got.Type)
	assert.Equal(t, metav1.ConditionTrue, got.Status)
	assert.Equal(t, "Ready", got.Reason)
	assert.Equal(t, "resource is ready", got.Message)
	assert.False(t, got.LastTransitionTime.IsZero())
}

func TestSetObservedAddsAndReplacesConditions(t *testing.T) {
	conditions := []metav1.Condition{
		Condition("Ready", metav1.ConditionFalse, "Progressing", "old"),
	}

	SetObserved(&conditions, 7,
		Condition("Ready", metav1.ConditionTrue, "Ready", "new"),
		Condition("Degraded", metav1.ConditionFalse, "ReconcileSucceeded", "ok"),
	)

	require.Len(t, conditions, 2)
	ready := apimeta.FindStatusCondition(conditions, "Ready")
	require.NotNil(t, ready)
	assert.Equal(t, metav1.ConditionTrue, ready.Status)
	assert.Equal(t, int64(7), ready.ObservedGeneration)
	assert.Equal(t, "new", ready.Message)

	degraded := apimeta.FindStatusCondition(conditions, "Degraded")
	require.NotNil(t, degraded)
	assert.Equal(t, int64(7), degraded.ObservedGeneration)
}
