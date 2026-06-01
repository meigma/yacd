// Package cardanonetwork reconciles CardanoNetwork custom resources. The
// controller renders a primary cardano-node workload, the optional ogmios /
// kupo / faucet chain API sidecars, an always-on cardano-tools serve sidecar
// (and its owned artifacts Service) that exposes the staged network artifacts
// over HTTP, and the selected CardanoDBSync primary-sidecar attachment; it then
// publishes endpoints and readiness state through CardanoNetwork status.
//
// Network artifacts live on the node-state PVC at /state/artifacts: a local
// network generates them with cardano-testnet create-env, a curated public
// profile fetches them with cardano-tools, and the serve sidecar exposes that
// flat directory so the node, ogmios, and out-of-cluster consumers (db-sync,
// CLI) read it without the controller copying genesis content into any
// Kubernetes object.
//
// The package separates side-effect-free planning from side-effecting
// reconciliation:
//
//   - builder.go, settings.go, validate.go, containers.go, resources.go,
//     init_container.go: pure builders. Given a CardanoNetwork
//     spec they produce desired Kubernetes objects in memory and never
//     touch the API server, time, randomness, or the file system.
//   - controller.go, apply.go, callbacks.go, delete.go, faucet_auth.go,
//     status.go, readiness.go: side-effecting reconciler. Reads from and
//     writes to the cluster, generates and hashes faucet auth tokens
//     (faucet_auth.go is the only crypto/rand caller), and publishes status.
//
// Owned-child apply is routed through ctrlkit/apply.ApplyOwnedObject with
// per-resource Validate/Mutate callbacks (callbacks.go).
//
// Status conditions follow the standard Progressing / Degraded / Ready
// shape with per-component {Node,Ogmios,Kupo,Faucet,Artifacts}Ready
// conditions. Condition type/reason/message strings are package-private
// constants in conditions.go.
package cardanonetwork
