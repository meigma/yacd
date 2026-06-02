// Package clusterstate is the managed-cluster bookkeeping port for the YACD CLI.
//
// Store persists a small Record about the managed cluster (its owned context,
// the prior current-context to restore on teardown, and the pinned k3d version)
// and provides a process-scoped file lock that serializes cluster-mutating
// operations across parallel invocations or worktrees. The record is
// supplementary: the cluster runtime is authoritative for existence and health,
// so a lost record is re-derived and a stale one corrected. The file subpackage
// is the implementation, storing JSON under an XDG state directory.
package clusterstate
