// Package operator is the operator-install port for the YACD developer CLI.
//
// Installer is the port consumed by the lifecycle orchestrator: it installs or
// upgrades the YACD operator into a managed cluster idempotently and reports the
// installed state. The ssa subpackage is the implementation; it renders an
// embedded copy of the Helm chart in-memory at install time through the lean
// Helm render subset and applies the result over the cluster's API by
// server-side apply, so the CLI needs no network chart pull at runtime.
//
// This package stays dependency-light: it holds only the interface, the request
// and result types, and the pure version-reconcile decision (Decide). The heavy
// machinery — the embedded chart filesystem, the in-memory renderer, and the
// server-side-apply client — lives entirely in the ssa adapter so consumers
// depend only on the port.
//
// Decide is the version-reconciliation policy expressed as a pure function so it
// is unit-testable without a cluster: absent installs, an equal version
// re-applies to heal drift, an older same-major version upgrades, and a newer or
// major-mismatched in-cluster version is refused with an actionable error.
package operator
