package kube

import (
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConditionType is the typed vocabulary of CardanoNetwork status conditions
// the CLI reads.
type ConditionType string

const (
	// ConditionReady is the aggregate readiness condition published by the
	// CardanoNetwork controller.
	ConditionReady ConditionType = "Ready"

	// ConditionDegraded indicates the controller stopped reconciling and is
	// surfacing a terminal-for-now failure.
	ConditionDegraded ConditionType = "Degraded"

	// ConditionFaucetReady indicates the faucet sidecar is reachable and the
	// auth Secret is published.
	ConditionFaucetReady ConditionType = "FaucetReady"
)

// FreshCondition returns the named status condition only when it observes the
// current generation of network. A nil result means the condition is missing
// or the controller has not yet reconciled the latest spec, so callers must
// treat the status as untrustworthy rather than as a definitive negative.
func FreshCondition(network *yacdv1alpha1.CardanoNetwork, conditionType ConditionType) *metav1.Condition {
	condition := apimeta.FindStatusCondition(network.Status.Conditions, string(conditionType))
	if condition == nil {
		return nil
	}
	if condition.ObservedGeneration < network.Generation {
		return nil
	}

	return condition
}
