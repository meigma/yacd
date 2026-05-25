package conditions

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
