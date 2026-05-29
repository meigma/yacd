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
//     postgres_identity.go, placement.go, placement_claims.go,
//     primary_sidecar.go, and primary_sidecar_status.go: pure builders. Given
//     a CardanoDBSync spec and resolved dependencies they produce desired
//     Kubernetes objects, status contracts, attachment fragments,
//     primary-sidecar incumbent selection, and the managed-Postgres identity
//     fingerprint in memory; they never touch the API server, time, or the
//     file system. The managed-Postgres password generator in database.go is
//     the only crypto/rand caller.
//   - controller.go, apply.go, callbacks.go, status.go, readiness.go,
//     database.go, managed_postgres_auth.go, placement_handoff.go, and
//     runtime_probe.go:
//     side-effecting reconciler. Reads from and writes to the cluster, runs
//     Postgres and Ogmios probes, checks placement handoff state, and
//     publishes status.
//
// Owned-child apply is routed through ctrlkit/apply.ApplyOwnedObject with
// per-resource Validate/Mutate callbacks (callbacks.go). The managed-Postgres
// auth Secret is the deliberate exception: it has create-once token
// semantics and a narrow restore-and-adopt path that do not fit
// ApplyOwnedObject's mutate-on-update model, and is reconciled in
// managed_postgres_auth.go.
//
// Status conditions follow the standard Progressing / Degraded / Ready
// shape with per-component {FollowerNode,NodeSocket,DBSync,Postgres}Ready,
// SidecarMaterialReady, and a dedicated Synced condition for chain-sync
// progress. Condition type/reason/message strings are package-private typed
// constants in conditions.go. The accepted database identity is read from
// owned runtime material and mirrored into status; direct status edits are
// repaired, not trusted.
//
// The default dbsync workload is one Deployment with two long-running
// containers (the follower cardano-node and cardano-db-sync). In
// primarySidecar placement, CardanoDBSync owns only db-sync material while
// CardanoNetwork composes the db-sync sidecar into its primary Deployment.
// The managed Postgres workload is a separate Deployment owned by the same
// CardanoDBSync; it exists only when spec.database.managed is set.
package cardanodbsync
