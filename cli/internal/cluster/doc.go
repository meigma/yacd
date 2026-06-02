// Package cluster is the local Kubernetes cluster provisioning port for the
// YACD CLI.
//
// Provisioner idempotently creates, heals, deletes, and inspects the singleton
// managed cluster. The k3d subpackage is the implementation; it shells out to a
// pinned, checksum-verified k3d binary. The runtime is the source of truth for a
// cluster's existence and health (the cluster has a fixed name, so it is always
// discoverable directly); the clusterstate record is only supplementary
// bookkeeping.
//
// EnsureCluster is not internally serialized. Cluster-mutating operations must
// be run under the clusterstate lock, which the lifecycle orchestrator holds
// across cluster creation, operator install, and state writes; this port stays
// decoupled from clusterstate so each is mocked independently.
//
// ManagedName, ManagedContext, and K3sImage are the single source of truth for
// the managed cluster's identity and pinned runtime.
package cluster
