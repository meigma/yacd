// Package cli builds the Cobra command tree for the yacd-cardano-tools
// binary, the single utility YACD uses for Cardano network artifact
// operations.
//
// NewRootCommand wires the command tree from an Options struct that supplies
// construction-time injection seams (stdio, build metadata, and the Viper
// instance used to resolve flags and YACD_* environment variables). The tree
// holds the generate, fetch, serve, and report verbs plus version. Every
// subcommand reads its configuration through the shared Viper instance bound
// in PersistentPreRunE, so flag, environment, and default precedence is
// uniform across verbs.
//
// The package exports Options and BuildInfo for construction; everything else
// is unexported.
package cli
