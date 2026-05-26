// Package cardanodbsync reconciles CardanoDBSync custom resources. The
// controller renders a two-container dbsync workload (follower cardano-node
// plus cardano-db-sync) backed by an external or controller-owned managed
// Postgres database, then probes Postgres and the referenced CardanoNetwork
// Ogmios endpoint to publish chain-sync progress through CardanoDBSync
// status.
//
// The package separates side-effect-free planning from side-effecting
// reconciliation:
//
//   - builder.go, settings.go, validate.go, containers.go, resources.go,
//     postgres_identity.go: pure builders. Given a CardanoDBSync spec and
//     resolved dependencies they produce desired Kubernetes objects and the
//     managed-Postgres identity fingerprint in memory; they never touch the
//     API server, time, or the file system. The managed-Postgres password
//     generator in database.go is the only crypto/rand caller.
//   - controller.go, apply.go, callbacks.go, status.go, readiness.go,
//     database.go, runtime_probe.go: side-effecting reconciler. Reads from
//     and writes to the cluster, runs Postgres and Ogmios probes, and
//     publishes status.
//
// Owned-child apply is routed through ctrlkit/apply.ApplyOwnedObject with
// per-resource Validate/Mutate callbacks (callbacks.go). The managed-Postgres
// auth Secret is the deliberate exception: it has create-once token
// semantics that do not fit ApplyOwnedObject's mutate-on-update model, and
// is reconciled inline in database.go.
//
// Status conditions follow the standard Progressing / Degraded / Ready
// shape with per-component {FollowerNode,DBSync,Postgres}Ready and a
// dedicated Synced condition for chain-sync progress. Condition
// type/reason/message strings are package-private typed constants in
// conditions.go.
//
// The dbsync workload is one Deployment with two long-running containers
// (the follower cardano-node and cardano-db-sync). The managed Postgres
// workload is a separate Deployment owned by the same CardanoDBSync; it
// exists only when spec.database.managed is set.
package cardanodbsync
