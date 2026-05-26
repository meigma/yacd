// Package dbsync builds deterministic cardano-db-sync runtime plans from a
// validated Spec. It is a pure-domain planner with no side effects: callers
// pass a Spec, BuildPlan returns a normalized Plan containing the rendered
// db-sync configuration, follower topology, CLI invocation, libpq environment,
// and fingerprints used by reconcilers to detect change.
package dbsync
