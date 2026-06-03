// Package ssa installs the YACD operator into a cluster by server-side apply of
// the operator Helm chart, rendered in-memory at install time. It is the adapter
// behind the operator.Installer port.
//
// The chart is embedded as an in-package copy of charts/yacd (embed.go), kept in
// sync by .dev/scripts/sync-operator-chart.sh and drift-guarded by root:check, so
// install needs no network chart pull. render.go renders the chart through the
// lean Helm subset (loader, chartutil, engine) — clientless, since the chart uses
// no lookup — and adds the chart's CRDObjects, reproducing helm template
// --include-crds --no-hooks. New constructs a controller-runtime client against
// the target cluster; EnsureOperator resolves the install namespace, renders the
// chart against it and the spec's typed values, applies the
// CustomResourceDefinitions first and waits until they are Established, then
// applies the workload (ServiceAccount, RBAC, Service, Deployment) in a stable
// order, defaulting the resolved install namespace onto namespaced objects the
// chart leaves unqualified. Every apply uses the CLI field owner with force
// ownership, and a label-based prune removes managed objects no longer in the
// rendered set (CustomResourceDefinitions are never pruned, since deleting one
// cascades to user resources).
//
// The install namespace is a real render input: it drives the Helm release
// namespace, so the rendered RBAC subjects and the applied namespaced objects
// agree for any namespace. EnsureOperator defaults an empty namespace to
// yacd-system and validates any other value as a DNS-1123 label.
package ssa
