// Package lifecycle orchestrates the yacd-managed local devnet: it composes the
// cluster provisioner, the cluster-state store, the operator installer, and the
// network kube client into the end-to-end up/down/status flows behind the
// `yacd devnet` command.
//
// The package depends only on the light port packages (cluster, clusterstate,
// operator, kube, devconfig, render); the concrete adapters are wired in the
// command composition root. This keeps the Manager unit-testable against the
// generated port mocks without Docker or a live cluster.
//
// The Manager holds the cluster lock across the whole mutating sequence of Up
// and Down, treats the cluster runtime as authoritative, and reconciles the
// supplementary state record (including the prior kubectl context to restore on
// teardown) on every run.
package lifecycle
