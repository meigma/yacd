# CLI reference

The `yacd` developer CLI manages YACD environments in a Kubernetes cluster and
wires local tools and tests to a running network. This page is the single source
of truth for every command, flag, default, and the `YACD_*` environment
contract. Other pages link here rather than restating flags.

## Synopsis

```text
yacd [command] [flags]
```

Each command that targets a network takes a positional `NAME`. `NAME` becomes
the CardanoNetwork name, and the namespace defaults to `NAME` unless you pass
`--namespace`. Both `NAME` and the resolved namespace must be valid DNS-1123
labels (lowercase alphanumeric and `-`); invalid input is rejected.

Commands:

| Command | Summary |
| --- | --- |
| `devnet` | Bring up a local Cardano devnet (cluster, operator, and a funded network). |
| `devnet down` | Delete the managed devnet cluster. |
| `devnet status` | Show the managed devnet cluster, operator, and network status. |
| `init` | Print a commented `yacd.yaml` environment template to stdout. |
| `up NAME` | Create or update a YACD environment and wait for readiness. |
| `down NAME` | Delete a YACD environment and wait for clean removal. |
| `list` | List YACD environments across all namespaces (or one with `-n`). |
| `info NAME` | Print CardanoNetwork status and connection information. |
| `topup NAME LOVELACE` | Submit a faucet top-up (self-forwards the faucet). |
| `run NAME [-- command ...]` | Run a command (or a shell) on the host with the `YACD_*` environment wired to forwarded endpoints. |
| `connect NAME` | Forward a network's endpoints and hold them open until interrupted. |
| `exec NAME -- command ...` | Run a command inside the primary node Pod (for socket-bound tools). |
| `completion` | Generate a shell autocompletion script. |

## Global flags

These persistent flags apply to every command. Each binds to a `YACD_*`
environment variable through the `YACD` env prefix; precedence is **flag > env >
default**.

| Flag | Short | Type | Default | Env override | Meaning |
| --- | --- | --- | --- | --- | --- |
| `--kubeconfig` | | string | `""` | `YACD_KUBECONFIG` | Path to the kubeconfig file. Empty defers to standard loading rules. |
| `--context` | | string | `""` | `YACD_CONTEXT` | Kubeconfig context to use. Empty defers to the current context. |
| `--namespace` | `-n` | string | `""` | `YACD_NAMESPACE` | Kubernetes namespace. Empty defers to `NAME` (per-environment namespace) or kubeconfig defaults. |
| `--log-level` | | string | `info` | `YACD_LOG_LEVEL` | Log level: `debug`, `info`, `warn`, `error`. |
| `--log-format` | | string | `text` | `YACD_LOG_FORMAT` | Log format: `text`, `json`. |
| `--help` | `-h` | bool | `false` | | Help for the command. |
| `--version` | `-v` | bool | `false` | | Print the version. Root command only. |

!!! note "Env override naming"
    The env prefix is `YACD` and flag names are upper-cased with `-` replaced by
    `_`. `YACD_NAMESPACE` is both the override for `--namespace` and a value the
    CLI publishes into the `YACD_*` contract; see [the contract table](#the-yacd-environment-contract).

`yacd --version` prints `yacd <version> (<commit>) built <date>`.

## devnet

```text
yacd devnet [flags]
```

Brings a managed [k3d](https://k3d.io) cluster, the operator, and a default
funded local network to a ready state in one command. Takes no `NAME` and no
`--namespace` (it manages a fixed `devnet` network in a `devnet` namespace).

`devnet` requires a running [Docker](https://www.docker.com/) (or compatible
container runtime). It does not require a preinstalled k3d: on first use the CLI
downloads a version-pinned, SHA256-verified k3d binary and caches it under
`$XDG_DATA_HOME/yacd/bin` (default `~/.local/share/yacd/bin`).

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--bare` | bool | `false` | Stop after installing the operator; apply no network. |
| `--timeout` | duration | `12m0s` | Maximum time to wait for the cluster, operator, and network. Must be greater than 0. |

On success it prints the cluster context, the operator version, and (unless
`--bare`) the Ogmios and Kupo endpoints, the funded wallet address, and a
copy-pasteable `yacd exec` tip-query hint.

### devnet down

```text
yacd devnet down [flags]
```

Deletes the managed devnet cluster and restores the prior kubectl context.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--timeout` | duration | `5m0s` | Maximum time to wait for the cluster to be deleted. Must be greater than 0. |

### devnet status

```text
yacd devnet status
```

Read-only unified view of the managed cluster, operator, and networks. Takes no
flags beyond the [global flags](#global-flags). Prints a one-line hint when no
managed cluster exists.

## init

```text
yacd init
```

Prints a fully-commented developer environment template to stdout and takes no
arguments or flags beyond the [global flags](#global-flags). The active
configuration is a ready-to-run local devnet (faucet plus a pre-funded wallet);
commented blocks document the rest of the API, including chain-API overrides and
a public/mainnet alternative. Redirect it to a file and apply it:

```sh
yacd init > yacd.yaml
yacd up dev -f yacd.yaml
```

See [Defining networks](../developer/networks.md) for the scaffold-and-edit
workflow and the [Environment file reference](environment.md) for field details.

## up

```text
yacd up NAME [flags]
```

Loads a developer environment file, renders it into a CardanoNetwork under the
resolved identity, creates the namespace if needed, and server-side-applies the
network. Unless `--wait=false`, it then polls until the network is Ready or the
timeout elapses.

| Flag | Short | Type | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--file` | `-f` | string | `""` | Developer environment file. Required. |
| `--dry-run` | | bool | `false` | Render the manifest to stdout without applying it. |
| `--allow-mainnet` | | bool | `false` | Allow applying a mainnet CardanoNetwork. |
| `--wait` | | bool | `true` | Wait for the CardanoNetwork to become ready. |
| `--timeout` | | duration | `12m0s` | Maximum time to wait for readiness. Must be greater than 0 when `--wait` is set. |

!!! warning "Mainnet requires `--allow-mainnet`"
    Applying a mainnet network without `--allow-mainnet` is rejected because
    mainnet deployments create large persistent volumes and bootstrap from
    [Mithril](https://mithril.network). `--dry-run` renders a mainnet manifest
    without the flag but applies nothing.

## down

```text
yacd down NAME [flags]
```

Deletes the named CardanoNetwork and, unless `--wait=false`, blocks until the
object and its garbage-collected children are gone. Deletion is idempotent: a
network that is already absent is reported as success.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--wait` | bool | `true` | Wait for the CardanoNetwork and its resources to be removed. |
| `--timeout` | duration | `5m0s` | Maximum time to wait for removal. Must be greater than 0 when `--wait` is set. |

## list

```text
yacd list [flags]
```

Lists CardanoNetworks across all namespaces by default, projecting each into
`name`, `namespace`, `mode`, `ready`, and published `endpoints`. Scope to a
single namespace with the global `-n`/`--namespace` flag. Renders an aligned
table by default or JSON with `--json`.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--json` | bool | `false` | Print machine-readable JSON. |

The table columns are `NAME`, `NAMESPACE`, `MODE`, `READY`, `ENDPOINTS`.
`READY` reflects a fresh `Ready` condition observed as `True` (a stale status is
reported as not ready). `ENDPOINTS` is a comma-separated list of published
endpoint names (`node-to-node`, `ogmios`, `kupo`, `faucet`) or `-` when none are
published yet.

The `--json` output is an array of objects with fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | CardanoNetwork name. |
| `namespace` | string | CardanoNetwork namespace. |
| `mode` | string | Requested network mode (`local` or `public`). |
| `ready` | bool | Fresh `Ready` condition observed as `True`. |
| `endpoints` | object | `nodeToNode`, `ogmios`, `kupo`, `faucet` URLs; empty when unpublished. |

## info

```text
yacd info NAME [flags]
```

Fetches the named CardanoNetwork and prints its status and connection
information, as human-readable text by default or JSON with `--json`.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--json` | bool | `false` | Print machine-readable JSON. |

The `--json` object has stable field names. The `conditions` array is always
present (possibly empty); nil sub-statuses are omitted rather than emitted as
empty objects.

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | CardanoNetwork name. |
| `namespace` | string | CardanoNetwork namespace. |
| `observedGeneration` | int | Last generation the controller observed. Omitted when 0. |
| `network` | object | `mode`, `localnetFingerprint`, `networkMagic`, `profile`, `era`. |
| `endpoints` | object | `nodeToNode`, `ogmios`, `kupo`, `faucet`, each `{serviceName, port, url}` or absent. |
| `faucet` | object | `{authSecretName}`. Omitted when no faucet is published. |
| `wallet` | object | `{address, keySecretName, funded}`. Omitted when no developer wallet exists. |
| `conditions` | array | Each `{type, status, reason, message, observedGeneration, lastTransitionTime}` (RFC3339 timestamp). |

## topup

```text
yacd topup NAME LOVELACE [flags]
```

Submits a faucet top-up. `LOVELACE` is a positional argument: the exact amount to
send, which must be greater than 0. By default `topup` **self-forwards** — it
opens a short-lived port-forward to the cluster faucet (and to Kupo when
`--await` is set), so it works directly from your host with no `yacd run`
wrapper. It gates token transmission through the trust checks below, fetches the
auth token from the published Secret, then `POST`s to the faucet's `/v1/topups`
endpoint.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--address` | string | `""` | Destination Cardano testnet address. Required. |
| `--source` | string | `""` | Faucet source name, for example `utxo1`. Empty lets the faucet pick a default. |
| `--faucet-url` | string | `""` | Override the faucet URL from CardanoNetwork status. |
| `--trust-faucet-url` | bool | `false` | Allow sending the faucet auth token to a custom non-loopback URL. |
| `--allow-insecure-faucet-url` | bool | `false` | Allow trusted custom non-loopback HTTP faucet URLs. |
| `--json` | bool | `false` | Print machine-readable JSON. |
| `--await` | bool | `false` | Wait for the funding transaction to be confirmed on-chain (requires Kupo). |
| `--await-timeout` | duration | `2m0s` | Maximum time to wait for `--await` confirmation. Must be greater than 0. |
| `--kupo-url` | string | `""` | Kupo URL for `--await`. Falls back to `YACD_KUPO_URL`. |

The default target requires the CardanoNetwork to be faucet-ready: a fresh
status with `Ready` and `FaucetReady` conditions `True`, a published faucet
endpoint, and a published faucet auth Secret.

Faucet transport: with no override, `topup` self-forwards the cluster-internal
faucet Service to a loopback port for the duration of the request. Inside
[`yacd run`](#run) it instead reuses the ambient `YACD_FAUCET_URL` (and
`YACD_KUPO_URL` for `--await`) rather than opening a second forward. An explicit
`--faucet-url` suppresses self-forwarding and targets that URL directly; with an
override, `--await` needs an explicit `--kupo-url` (or `YACD_KUPO_URL`).

!!! warning "The faucet token leaves the cluster only with explicit acks"
    By default the token is sent only to a loopback target (the self-forwarded or
    `run`-inherited faucet URL). An explicit `--faucet-url` at a non-loopback
    host requires `--trust-faucet-url`; a trusted `http://` (plaintext) host
    additionally requires `--allow-insecure-faucet-url`. These gates prevent
    token exfiltration and plaintext eavesdropping.

The `--json` output mirrors the faucet's success envelope:

| Field | Type | Meaning |
| --- | --- | --- |
| `txId` | string | Funding transaction id. |
| `source` | string | Faucet source the funds came from. |
| `sourceAddress` | string | Source address. |
| `destinationAddress` | string | Address that was funded. |
| `lovelace` | int | Lovelace sent. |

## run

```text
yacd run NAME [-- command [args...]] [flags]
```

Establishes scoped port-forwards to the network's chain-API endpoints, injects
the [`YACD_*` environment](#the-yacd-environment-contract), and execs the command
(or your `$SHELL`, falling back to `/bin/sh`, when none is given) on the host
with that environment. The forwards are torn down when the command exits.

Put `--` before any command that takes its own flags so they are passed through
to the command instead of being parsed by `yacd`.

```sh
# Run a test suite against the network (note the -- before the command)
yacd run my-net -- go test ./e2e/...

# Open a shell with the YACD_* environment set
yacd run my-net
```

`run` has no command-specific flags beyond the [global flags](#global-flags).
The child inherits the CLI's stdio and process group, so an interactive Ctrl-C
reaches it. The child's exit status is propagated to your shell; a process
killed by a signal reports `128+signal` (for example `130` for SIGINT). If the
forwards drop while the command runs, `run` exits non-zero and reports the lost
connection.

## connect

```text
yacd connect NAME [flags]
```

Establishes supervised port-forwards to a network's chain-API endpoints, writes
the loopback URLs to `.yacd/<network>/endpoints.json` (or
`.yacd/<namespace>/<network>/endpoints.json` when `--namespace` is set), prints
them, and holds them open until interrupted (Ctrl-C). Run it in one terminal and
your tools in another. Dropped forwards are re-established automatically (with a
freshly resolved Pod and new local ports). On exit (or before each
re-establish), the endpoints file is removed.

`connect` has no command-specific flags beyond the [global flags](#global-flags).

!!! note "Loopback ports are ephemeral and token-free"
    The endpoints file never contains the faucet token, and its ports are only
    live while `connect` is running. See the [endpoints.json schema](#the-endpointsjson-schema).

## exec

```text
yacd exec NAME -- command [args...] [flags]
```

Runs a command inside the primary `cardano-node` Pod with kubectl-exec
semantics, for tools that reach the node over its local Unix socket (notably
`cardano-cli`) rather than over a forwarded TCP port. `CARDANO_NODE_SOCKET_PATH`
and the [`YACD_*` variables](#the-yacd-environment-contract) are set in the pod
environment, so `cardano-cli` finds the socket automatically. A command is
required.

```sh
# cardano-cli reads CARDANO_NODE_SOCKET_PATH from the pod environment:
yacd exec my-net -- cardano-cli query tip --testnet-magic 42

# To interpolate YACD_* variables into arguments, run a shell explicitly:
yacd exec my-net -- sh -c 'cardano-cli query tip --testnet-magic "$YACD_NETWORK_MAGIC"'

# From a terminal, open an interactive shell in the node Pod:
yacd exec my-net -- sh
```

`exec` has no command-specific flags beyond the [global flags](#global-flags).
The command is run directly, not through a shell, so `$VAR` references in
arguments are **not** expanded; wrap the command in `sh -c '...'` to interpolate
`YACD_*` variables. When both stdin and stdout are terminals, `exec` attaches an
interactive TTY; piped or non-terminal (CI) invocations stream without one. The
command's exit code is propagated to the caller. `exec` requires the
CardanoNetwork to be Ready.

## The YACD environment contract

`run`, `connect`, and `exec` publish a stable, versioned set of `YACD_*`
variables (contract version 1). Tests and tooling read these instead of parsing
any YACD file. The variable names are identical whether a command runs on the
host (`run`, over port-forwards) or inside the primary Pod (`exec`, over cluster
DNS); only the values adapt. Adding a variable is backward compatible; renaming
or removing one is a breaking change to the contract.

| Variable | Host (`run`) value | In-pod (`exec`) value | Present when |
| --- | --- | --- | --- |
| `YACD_NETWORK` | network name | network name | always |
| `YACD_NAMESPACE` | network namespace | network namespace | always |
| `YACD_NETWORK_MAGIC` | network magic (integer) | network magic (integer) | when the controller has published the network magic |
| `YACD_OGMIOS_URL` | loopback URL (scheme preserved, e.g. `ws://`) | published ClusterIP URL | when Ogmios is published and forwarded |
| `YACD_KUPO_URL` | loopback URL | published ClusterIP URL | when Kupo is published and forwarded |
| `YACD_FAUCET_URL` | loopback URL | published ClusterIP URL | when the faucet is published and forwarded |
| `YACD_FAUCET_TOKEN` | faucet auth Bearer token | *not set* | host `run` only, when the token is non-empty |
| `CARDANO_NODE_SOCKET_PATH` | *not set* | `/ipc/node.socket` | in-pod `exec` only |

Notes:

- On the host, each forwarded chain endpoint URL is rewritten onto
  `127.0.0.1:<local-port>`. Only host and port change; the scheme, path, query,
  and fragment carry through unchanged (a `ws://` Ogmios endpoint stays `ws://`).
- In the Pod, the published ClusterIP URLs are passed through verbatim.
- The node-to-node endpoint is intentionally excluded: it is a TCP peer
  protocol, not something host or in-pod test tooling speaks.
- `CARDANO_NODE_SOCKET_PATH` is unprefixed because that is the name
  `cardano-cli` already expects.

!!! warning "`YACD_FAUCET_TOKEN` is host-only"
    `exec` deliberately omits `YACD_FAUCET_TOKEN`: a Bearer token in the exec
    argv would land in apiserver audit logs and `/proc`. In-pod tooling does not
    need it. `yacd topup` reads the token from the cluster directly.

`yacd topup --await` reads `--kupo-url` from `YACD_KUPO_URL` through this
contract, so it works unchanged when run under `yacd run`.

## The endpoints.json schema

`yacd connect` writes a token-free connection document to
`.yacd/<network>/endpoints.json` (mode `0600`, in a `0700` directory), or
`.yacd/<namespace>/<network>/endpoints.json` when `--namespace` differs from
`NAME`. The field names are stable across releases.

```json
{
  "network": "my-net",
  "namespace": "my-net",
  "networkMagic": 42,
  "ogmiosUrl": "ws://127.0.0.1:51820",
  "kupoUrl": "http://127.0.0.1:51821",
  "faucetUrl": "http://127.0.0.1:51822"
}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `network` | string | CardanoNetwork name. Always present. |
| `namespace` | string | CardanoNetwork namespace. Always present. |
| `networkMagic` | int | Network magic. Omitted until the controller publishes it. |
| `ogmiosUrl` | string | Loopback Ogmios URL. Omitted when not forwarded. |
| `kupoUrl` | string | Loopback Kupo URL. Omitted when not forwarded. |
| `faucetUrl` | string | Loopback faucet URL. Omitted when not forwarded. |

The document deliberately never carries the faucet token, and the ports it lists
are only live while `connect` is running. The file is removed on disconnect.

## Install

Download the `yacd` binary for your platform from the
[releases page](https://github.com/meigma/yacd/releases) and place it on your
`PATH`, then verify:

```sh
yacd --version
```

## Shell completion

`yacd completion <shell>` generates an autocompletion script. Supported shells
are `bash`, `zsh`, `fish`, and `powershell`. Each script accepts
`--no-descriptions` to disable completion descriptions.

=== "macOS"

    ```sh
    # zsh
    yacd completion zsh > $(brew --prefix)/share/zsh/site-functions/_yacd

    # bash (requires the bash-completion package)
    yacd completion bash > $(brew --prefix)/etc/bash_completion.d/yacd
    ```

=== "Linux"

    ```sh
    # zsh
    yacd completion zsh > "${fpath[1]}/_yacd"

    # bash (requires the bash-completion package)
    yacd completion bash > /etc/bash_completion.d/yacd
    ```

Start a new shell for the setup to take effect. To load completions for the
current session only:

```sh
source <(yacd completion zsh)   # or bash, fish
```

For `zsh`, if completion is not yet enabled, run once:

```sh
echo "autoload -U compinit; compinit" >> ~/.zshrc
```
