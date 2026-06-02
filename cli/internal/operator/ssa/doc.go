// Package ssa installs the YACD operator into a cluster by server-side apply of
// a build-time-rendered copy of the Helm chart. It is the adapter behind the
// operator.Installer port.
//
// The chart is pre-rendered by .dev/scripts/render-operator-chart.sh into
// manifests/operator.yaml and embedded (embed.go), so install needs neither the
// Helm SDK nor a network chart pull at runtime. New constructs a
// controller-runtime client against the target cluster; EnsureOperator parses
// the embedded manifests into unstructured objects, applies the
// CustomResourceDefinitions first and waits until they are Established, then
// applies the workload (ServiceAccount, RBAC, Service, Deployment) in a stable
// order, defaulting the install namespace onto namespaced objects the chart
// leaves unqualified. Every apply uses the CLI field owner with force ownership,
// and a label-based prune removes managed objects no longer in the manifest set
// (CustomResourceDefinitions are never pruned, since deleting one cascades to
// user resources).
//
// This phase pins the install namespace to the chart's render namespace
// (yacd-system); EnsureOperator rejects any other namespace because the chart's
// RBAC subjects are rendered against it. Configurable-namespace support is a
// later phase.
package ssa
