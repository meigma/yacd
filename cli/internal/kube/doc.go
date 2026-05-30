// Package kube is the Kubernetes adapter package for the YACD developer CLI.
//
// Client is the port consumed by the CLI command layer. Adapter is the
// controller-runtime-backed implementation; NewClient returns the concrete
// adapter so callers that own the lifecycle hold a typed value, while the
// command layer holds the port interface for testability.
//
// FreshCondition and the typed ConditionType vocabulary are pure helpers
// reused by WaitReady and the topup readiness gate; they do not depend on a
// live Kubernetes API. WaitReady and WaitGone poll the network through the
// Client port for readiness and teardown completion respectively. Config and
// the kubeconfig loader sit alongside the adapter because they are part of the
// same construction surface.
//
// The host-access seam (access.go) extends the same Client port with
// PrimaryPodName, Forward, and Exec for the run/connect/exec verbs. These reach
// Pod subresources that the high-level controller-runtime client does not
// expose, so the Adapter also retains the REST config and a core/v1 REST
// client. PrimaryPodName resolves the target Pod from the operator's published
// node-to-node Service rather than any controller-internal labels; Forward and
// Exec require a live cluster (a kubelet) and are therefore proven by manual
// and end-to-end runs rather than envtest.
package kube
