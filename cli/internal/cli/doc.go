// Package cli builds the YACD developer CLI command tree.
//
// NewRootCommand wires the cobra command tree (up, down, list, info, wallet,
// run, exec, connect) and the per-process dependencies into a commandContext
// that each subcommand reads at RunE time. Subcommands are side-effecting orchestrators:
// they load the developer environment through devconfig, synthesise manifests
// through render, and call into kube through the Client port. Environment
// identity (name and namespace) is a command-line concern, resolved from the
// NAME argument and the --namespace flag, not read from the spec file.
//
// The host-access verbs share two building blocks. The YACD_* environment
// contract (envcontract.go) is the stable integration surface tests consume;
// hostEnvFromURLs builds the env from resolved URLs while podEnv builds in-pod
// ClusterIP URLs, with identical variable names. resolveChainAccess
// (forward_resolve.go) is the shared endpoint resolver: per endpoint it prefers
// an explicit override, then an ambient YACD_* value, then the operator-asserted
// status.externalURL when a probe finds it reachable, and only otherwise opens an
// ephemeral port-forward — so on a co-located devnet no forward is established.
// run (run.go) resolves chain access and execs a host command or shell with that
// environment, propagating the command's exit code. The funding path
// (wallet_fund.go) resolves the same way and submits a locally-signed
// transaction. exec (exec.go) runs a command inside the primary node Pod instead,
// for socket-bound tools that cannot use a forwarded TCP port. connect
// (connect.go) is deliberately forward-only — the remote-cluster tool — using
// connectNetwork to hold the forwards open in the foreground, writing the
// token-free loopback URLs to .yacd/<network>/endpoints.json and re-establishing
// them if they drop.
//
// The package exports an Options struct for construction-time injection
// (test seams for the kube client, the chain-index confirmer, and the
// funding-transaction submitter), a BuildInfo struct for the linker-injected
// version metadata, a RuntimeConfig struct for the persistent-flag payload, and
// the UTxOConfirmer and tx.Submitter ports so mockery can generate their mocks.
// Everything else is unexported.
package cli
