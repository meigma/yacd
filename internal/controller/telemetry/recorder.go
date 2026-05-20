package telemetry

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const EventReasonChildResourcesApplied = "ChildResourcesApplied"

// EventSink is the subset of Kubernetes event recorder behavior this package
// needs. It mirrors the Eventf method on record.EventRecorder so production
// code can pass a real recorder while tests pass a fake.
type EventSink interface {
	// Eventf emits a Kubernetes Event of the given type and reason against
	// object using the format string and arguments.
	Eventf(object runtime.Object, eventtype string, reason string, messageFmt string, args ...any)
}

// Recorder records controller-specific metrics and Kubernetes Events.
// Implementations must be safe for concurrent use by the reconciler.
type Recorder interface {
	// RecordChildApplies records metrics and emits a single aggregated
	// Kubernetes Event for the child resource apply outcomes in applies.
	RecordChildApplies(object runtime.Object, applies []ChildApply)

	// RecordStatusTransition records a metric and emits a Kubernetes Event
	// for a persisted condition transition.
	RecordStatusTransition(object runtime.Object, condition metav1.Condition)
}

// recorder is the default Recorder implementation, combining optional metrics
// with an optional Kubernetes Event sink.
type recorder struct {
	// metrics is the Prometheus collector bundle; nil disables metric
	// emission.
	metrics *Metrics

	// events is the Kubernetes Event sink; nil disables event emission.
	events EventSink
}

// noopRecorder discards all observations. It is the fallback returned by
// NoopRecorder so reconcile code can rely on a non-nil Recorder.
type noopRecorder struct{}

// NewRecorder combines optional metrics and Kubernetes Events behind one controller dependency.
func NewRecorder(metrics *Metrics, events EventSink) Recorder {
	return recorder{
		metrics: metrics,
		events:  events,
	}
}

// NoopRecorder returns a recorder that intentionally drops all observations.
func NoopRecorder() Recorder {
	return noopRecorder{}
}

// RecordChildApplies records a metric for each child apply that represents an
// actual create or update and emits a single aggregated Kubernetes Event
// summarising the changes. When every apply was a no-op it emits nothing.
func (r recorder) RecordChildApplies(object runtime.Object, applies []ChildApply) {
	summary := newApplySummary()
	for _, apply := range applies {
		if r.metrics != nil {
			if !r.metrics.recordChildApply(apply) {
				continue
			}
		} else if _, ok := metricOperation(apply.Operation); !ok {
			continue
		}
		summary.add(apply)
	}

	if r.events == nil || len(summary.parts) == 0 {
		return
	}
	r.events.Eventf(
		object,
		corev1.EventTypeNormal,
		EventReasonChildResourcesApplied,
		"Applied child resources: %s",
		summary.String(),
	)
}

// RecordStatusTransition records the transition in metrics and emits a
// Kubernetes Event whose reason matches the condition reason so operators
// see why the state changed.
func (r recorder) RecordStatusTransition(object runtime.Object, condition metav1.Condition) {
	if r.metrics != nil {
		r.metrics.recordStatusTransition(condition)
	}
	if r.events == nil {
		return
	}
	r.events.Eventf(
		object,
		corev1.EventTypeNormal,
		condition.Reason,
		"%s condition is %s: %s",
		condition.Type,
		condition.Status,
		condition.Message,
	)
}

// RecordChildApplies discards the child-apply observations.
func (noopRecorder) RecordChildApplies(runtime.Object, []ChildApply) {}

// RecordStatusTransition discards the status transition observation.
func (noopRecorder) RecordStatusTransition(runtime.Object, metav1.Condition) {}

// applySummary accumulates per-child apply descriptions so they can be
// rendered as a single deterministic event message.
type applySummary struct {
	// parts is the human-readable child apply descriptions appended in
	// reconcile order; String sorts them before joining.
	parts []string
}

// newApplySummary returns an empty applySummary preallocated for the three
// owned children the controller currently manages.
func newApplySummary() applySummary {
	return applySummary{
		parts: make([]string, 0, 3),
	}
}

// add appends a "<Kind> <operation>" entry for the given apply when the
// operation maps to a metric-worthy create or update outcome.
func (s *applySummary) add(apply ChildApply) {
	operation, _ := metricOperation(apply.Operation)
	s.parts = append(s.parts, fmt.Sprintf("%s %s", childResourceEventName(apply.Resource), operation))
}

// String returns the parts joined as "ConfigMap created, Service updated".
// Parts are sorted so the rendered message is independent of reconcile order.
func (s applySummary) String() string {
	sort.Strings(s.parts)
	return strings.Join(s.parts, ", ")
}

// childResourceEventName returns the Kubernetes kind label used inside event
// messages for a given ChildResource. Unknown resources fall through as their
// raw label value so future additions still produce a readable message.
func childResourceEventName(resource ChildResource) string {
	switch resource {
	case ChildResourceConfigMap:
		return "ConfigMap"
	case ChildResourceDeployment:
		return "Deployment"
	case ChildResourceService:
		return "Service"
	default:
		return string(resource)
	}
}
