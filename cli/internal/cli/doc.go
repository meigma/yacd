// Package cli builds the YACD developer CLI command tree.
//
// NewRootCommand wires the cobra command tree (up, down, list, info, topup)
// and the per-process dependencies into a commandContext that each subcommand
// reads at RunE time. Subcommands are side-effecting orchestrators: they load
// the developer environment through devconfig, synthesise manifests through
// render, and call into kube through the Client port. Environment identity
// (name and namespace) is a command-line concern, resolved from the NAME
// argument and the --namespace flag, not read from the spec file.
//
// The host-access verbs share two building blocks. The YACD_* environment
// contract (envcontract.go) is the stable integration surface tests consume;
// hostEnv builds loopback URLs over port-forwards while podEnv builds in-pod
// ClusterIP URLs, with identical variable names. connectNetwork (forward.go)
// gates on readiness, resolves the primary Pod, forwards the published
// chain-API endpoints, and returns a live session carrying that host
// environment.
//
// The package exports an Options struct for construction-time injection
// (test seams for the kube client and HTTP transport),
// a BuildInfo struct for the linker-injected version metadata, a
// RuntimeConfig struct for the persistent-flag payload, and the HTTPDoer
// interface so mockery can generate the faucet-transport mock. Everything
// else is unexported.
package cli
