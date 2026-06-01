// Package config builds a validated stage runtime configuration from a Viper
// instance already bound to the stage subcommand's flags (and therefore
// transparently reading the matching YACD_* environment variables).
//
// The stage verb has non-trivial configuration derivation — synthesizing the
// node-to-node URL from host and port and passing the plan manifest path
// through verbatim — so it gets a dedicated, tested loader here. The generate,
// fetch, serve, and sync verbs read their flags inline.
//
// The package exports StageConfig and LoadStage; everything else is unexported.
package config
