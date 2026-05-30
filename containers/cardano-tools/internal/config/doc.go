// Package config builds a validated report runtime configuration from a Viper
// instance already bound to the report subcommand's flags (and therefore
// transparently reading the matching YACD_* environment variables).
//
// The report verb is the only one with non-trivial configuration derivation —
// resolving the ConfigMap namespace from the projected ServiceAccount file, the
// API URL from pod-injected service env, and synthesizing the node-to-node URL
// — so it gets a dedicated, tested loader here. The generate, fetch, and serve
// verbs read their flags inline.
//
// The package exports the projected-ServiceAccount default paths, ReportConfig,
// and LoadReport; everything else is unexported.
package config
