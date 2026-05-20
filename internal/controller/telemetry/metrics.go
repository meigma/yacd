package telemetry

import (
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	MetricChildApplyTotal       = "template_k8s_nginxdeployment_child_apply_total"
	MetricStatusTransitionTotal = "template_k8s_nginxdeployment_status_transition_total"

	childResourceConfigMap  = "configmap"
	childResourceDeployment = "deployment"
	childResourceService    = "service"

	statusReasonDeploymentReady       = "DeploymentReady"
	statusReasonDeploymentProgressing = "DeploymentProgressing"
	statusReasonDeploymentStale       = "DeploymentStatusStale"
)

// ChildResource identifies an owned resource with a bounded metric label
// value. Restricting child resource identifiers to a closed set of constants
// keeps the metric cardinality finite even as the controller grows.
type ChildResource string

const (
	ChildResourceConfigMap  ChildResource = childResourceConfigMap
	ChildResourceDeployment ChildResource = childResourceDeployment
	ChildResourceService    ChildResource = childResourceService
)

// ChildApply is the result of applying one owned child resource.
type ChildApply struct {
	// Resource identifies which owned child was applied.
	Resource ChildResource

	// Operation is the controllerutil result reported by CreateOrPatch for
	// this child resource (Created, Updated, Unchanged, ...).
	Operation controllerutil.OperationResult
}

// Metrics owns the controller-specific Prometheus collectors.
type Metrics struct {
	// childApplyTotal counts child-resource create or update operations,
	// labelled by resource kind and operation outcome.
	childApplyTotal *prometheus.CounterVec

	// statusTransitionTotal counts persisted NginxDeployment status condition
	// transitions, labelled by condition type, status, and reason.
	statusTransitionTotal *prometheus.CounterVec
}

// NewMetrics registers controller-specific metrics with the provided registerer.
func NewMetrics(registerer prometheus.Registerer) (*Metrics, error) {
	if registerer == nil {
		return nil, fmt.Errorf("metrics registerer is required")
	}

	metrics := &Metrics{
		childApplyTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "template_k8s",
				Subsystem: "nginxdeployment",
				Name:      "child_apply_total",
				Help:      "Total number of NginxDeployment child resources created or updated by the controller.",
			},
			[]string{"resource", "operation"},
		),
		statusTransitionTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "template_k8s",
				Subsystem: "nginxdeployment",
				Name:      "status_transition_total",
				Help:      "Total number of persisted NginxDeployment status condition transitions.",
			},
			[]string{"condition", "status", "reason"},
		),
	}
	metrics.initializeKnownSeries()

	if err := registerer.Register(metrics.childApplyTotal); err != nil {
		return nil, err
	}
	if err := registerer.Register(metrics.statusTransitionTotal); err != nil {
		return nil, err
	}

	return metrics, nil
}

// recordChildApply increments the childApplyTotal counter for the given
// apply, returning false when the operation did not represent an actual
// create or update so the caller can avoid emitting a redundant event.
func (m *Metrics) recordChildApply(apply ChildApply) bool {
	operation, ok := metricOperation(apply.Operation)
	if !ok {
		return false
	}
	m.childApplyTotal.WithLabelValues(string(apply.Resource), operation).Inc()
	return true
}

// recordStatusTransition increments the statusTransitionTotal counter for the
// supplied condition. The condition status is lowercased to keep label values
// consistent with the Prometheus convention used for boolean states.
func (m *Metrics) recordStatusTransition(condition metav1.Condition) {
	m.statusTransitionTotal.WithLabelValues(
		condition.Type,
		strings.ToLower(string(condition.Status)),
		condition.Reason,
	).Inc()
}

// initializeKnownSeries pre-creates the label combinations the controller can
// legitimately emit. Pre-creating these series makes Prometheus expose them
// at zero before the first event, which gives scrapers a stable schema and
// simpler rate(...) queries.
func (m *Metrics) initializeKnownSeries() {
	for _, resource := range []ChildResource{
		ChildResourceConfigMap,
		ChildResourceDeployment,
		ChildResourceService,
	} {
		for _, operation := range []string{
			string(controllerutil.OperationResultCreated),
			string(controllerutil.OperationResultUpdated),
		} {
			m.childApplyTotal.WithLabelValues(string(resource), operation)
		}
	}

	for _, transition := range []struct {
		condition string
		status    string
		reason    string
	}{
		{condition: "Available", status: "true", reason: statusReasonDeploymentReady},
		{condition: "Available", status: "false", reason: statusReasonDeploymentProgressing},
		{condition: "Available", status: "false", reason: statusReasonDeploymentStale},
	} {
		m.statusTransitionTotal.WithLabelValues(transition.condition, transition.status, transition.reason)
	}
}

// metricOperation maps a controllerutil OperationResult to the bounded label
// value used in metrics. It collapses the three update flavours into a single
// "updated" bucket and reports false for outcomes that should not be counted.
func metricOperation(operation controllerutil.OperationResult) (string, bool) {
	switch operation {
	case controllerutil.OperationResultCreated:
		return string(controllerutil.OperationResultCreated), true
	case controllerutil.OperationResultUpdated,
		controllerutil.OperationResultUpdatedStatus,
		controllerutil.OperationResultUpdatedStatusOnly:
		return string(controllerutil.OperationResultUpdated), true
	default:
		return "", false
	}
}
