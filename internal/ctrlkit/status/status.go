package status

import (
	"context"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConditionError represents a known controller condition that should be
// surfaced through status rather than returned as an unexpected reconcile error.
type ConditionError struct {
	// Reason is the stable condition reason published on the resource status.
	Reason string
	// Message is the user-facing description of the condition.
	Message string
}

func (e ConditionError) Error() string {
	return e.Message
}

// NewConditionError returns a ConditionError with a stable reason and
// formatted user-facing message.
func NewConditionError(reason string, format string, args ...any) ConditionError {
	return ConditionError{
		Reason:  reason,
		Message: fmt.Sprintf(format, args...),
	}
}

// Condition constructs a Kubernetes status condition with a transition time.
func Condition(conditionType string, status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
}

// SetObserved sets observedGeneration on each update before applying it to the
// condition slice by type.
func SetObserved(conditions *[]metav1.Condition, observedGeneration int64, updates ...metav1.Condition) {
	for _, condition := range updates {
		condition.ObservedGeneration = observedGeneration
		apimeta.SetStatusCondition(conditions, condition)
	}
}

// PatchIfChanged patches the status subresource when the reconciler changed the
// object after taking original as its pre-mutation copy.
func PatchIfChanged(ctx context.Context, writer client.StatusWriter, current client.Object, original client.Object) error {
	if equality.Semantic.DeepEqual(original, current) {
		return nil
	}

	return writer.Patch(ctx, current, client.MergeFrom(original))
}

// AggregateReady returns a Ready-style condition that is true only when every
// dependency is true. The first non-true dependency supplies the false reason
// and message so callers publish one clear blocker.
func AggregateReady(conditionType string, readyReason string, readyMessage string, dependencies ...metav1.Condition) metav1.Condition {
	for _, dependency := range dependencies {
		if dependency.Status != metav1.ConditionTrue {
			return Condition(conditionType, metav1.ConditionFalse, dependency.Reason, dependency.Message)
		}
	}

	return Condition(conditionType, metav1.ConditionTrue, readyReason, readyMessage)
}

// ProgressingForReady derives a Progressing-style condition from aggregate
// readiness and the set of false Ready reasons that still mean work is actively
// converging.
func ProgressingForReady(
	conditionType string,
	readyReason string,
	readyMessage string,
	ready metav1.Condition,
	progressingReasons ...string,
) metav1.Condition {
	if ready.Status == metav1.ConditionTrue {
		return Condition(conditionType, metav1.ConditionFalse, readyReason, readyMessage)
	}
	if slices.Contains(progressingReasons, ready.Reason) {
		return Condition(conditionType, metav1.ConditionTrue, ready.Reason, ready.Message)
	}

	return Condition(conditionType, metav1.ConditionFalse, ready.Reason, ready.Message)
}
