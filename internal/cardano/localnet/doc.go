// Package localnet is the pure-domain planner for cardano-testnet create-env
// inputs used by YACD local Cardano development networks.
//
// Callers pass a Spec; BuildPlan returns a normalized Plan that carries the
// cardano-testnet command invocation, the stable layout of generated paths, a
// fingerprint of the create-env inputs, and a JSON-serializable manifest that
// init-container code writes next to the generated environment for idempotency
// checks. The package has no side effects: no I/O, no clocks, no exec, and no
// Kubernetes clients.
package localnet
