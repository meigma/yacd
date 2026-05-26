// Package kube is the Kubernetes adapter package for the YACD developer CLI.
//
// Client is the port consumed by the CLI command layer. Adapter is the
// controller-runtime-backed implementation; NewClient returns the concrete
// adapter so callers that own the lifecycle hold a typed value, while the
// command layer holds the port interface for testability.
//
// FreshCondition and the typed ConditionType vocabulary are pure helpers
// reused by WaitReady and the topup readiness gate; they do not depend on a
// live Kubernetes API. Config and the kubeconfig loader sit alongside the
// adapter because they are part of the same construction surface.
package kube
