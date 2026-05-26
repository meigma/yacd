// Package cli builds the YACD developer CLI command tree.
//
// NewRootCommand wires the cobra command tree (deploy, info, topup) and the
// per-process dependencies into a commandContext that each subcommand reads
// at RunE time. Subcommands are side-effecting orchestrators: they load the
// developer environment through devconfig, synthesise manifests through
// render, and call into kube through the Client port.
//
// The package exports an Options struct for construction-time injection
// (test seams for the kube client, namespace resolver, and HTTP transport),
// a BuildInfo struct for the linker-injected version metadata, a
// RuntimeConfig struct for the persistent-flag payload, and the HTTPDoer
// interface so mockery can generate the faucet-transport mock. Everything
// else is unexported.
package cli
