// Package cardanonetwork reconciles CardanoNetwork custom resources. The
// controller renders a primary cardano-node workload, the optional ogmios /
// kupo / faucet chain API sidecars, the selected CardanoDBSync primary-sidecar
// attachment, owned artifact-publisher RBAC, and a per-CR ConfigMap of network
// artifacts; it then publishes endpoints and readiness state through
// CardanoNetwork status.
//
// The package separates side-effect-free planning from side-effecting
// reconciliation:
//
//   - builder.go, settings.go, validate.go, containers.go, resources.go,
//     artifacts.go, init_container.go: pure builders. Given a CardanoNetwork
//     spec they produce desired Kubernetes objects in memory and never
//     touch the API server, time, randomness, or the file system.
//   - controller.go, apply.go, callbacks.go, delete.go, faucet_auth.go,
//     status.go, readiness.go: side-effecting reconciler. Reads from and
//     writes to the cluster, generates and hashes faucet auth tokens
//     (faucet_auth.go is the only crypto/rand caller), and publishes status.
//
// Owned-child apply is routed through ctrlkit/apply.ApplyOwnedObject with
// per-resource Validate/Mutate callbacks (callbacks.go). The network
// artifacts ConfigMap is the deliberate exception: it has delete-and-recover
// semantics that do not fit ApplyOwnedObject's mutate-in-place model and is
// reconciled inline in apply.go.
//
// Status conditions follow the standard Progressing / Degraded / Ready
// shape with per-component {Node,Ogmios,Kupo,Faucet,Artifacts}Ready
// conditions. Condition type/reason/message strings are package-private
// constants in conditions.go.
package cardanonetwork
