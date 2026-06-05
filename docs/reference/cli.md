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
| `install` | Install or upgrade the YACD operator on a cluster. |
| `init` | Print a commented `yacd.yaml` environment template to stdout. |
| `up NAME` | Create or update a YACD environment and wait for readiness. |
| `down NAME` | Delete a YACD environment and wait for clean removal. |
| `list` | List YACD environments across all namespaces (or one with `-n`). |
| `info NAME` | Print CardanoNetwork status and connection information. |
| `wallet <verb> NET` | Manage developer wallets: add, list, topup, export, remove. |
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

## install

```text
yacd install [flags]
```

Installs or upgrades the YACD operator on the targeted cluster, then waits for
the manager to become ready. The target is the explicit `--kubeconfig`/`--context`
(or the `YACD_KUBECONFIG`/`YACD_KUBE_CONTEXT` variables), otherwise the ambient
current-context; `install` never targets the managed devnet. The global
`-n`/`--namespace` flag selects the install namespace and defaults to
`yacd-system` (created if absent).

`install` reconciles the cluster to the operator version this CLI embeds: it
installs when absent, upgrades an older same-major install, re-applies an equal
version to heal drift, and refuses a newer or major-mismatched in-cluster version
with actionable guidance. The operator image is pinned to the chart's appVersion
(the version this CLI embeds); the supported way to change the operator version
is to upgrade the CLI.

| Flag | Short | Type | Default | Meaning |
| --- | --- | --- | --- | --- |
| `--wait` | | bool | `true` | Wait for the manager Deployment to become Available. |
| `--timeout` | | duration | `5m0s` | Maximum time to wait for readiness (also bounds a `--dry-run` plan's reads). Must be greater than 0 when `--wait` is set. |
| `--dry-run` | | bool | `false` | Report the planned action without changing the cluster. |
| `--values` | `-f` | stringArray | `[]` | Path to a YAML file of operational chart value overrides (repeatable; later files win). |
| `--set` | | stringArray | `[]` | Set an operational chart value (Helm `--set` syntax, repeatable). |
| `--set-string` | | stringArray | `[]` | Set an operational chart value forced to a string (repeatable). |

The override flags customize **operational** chart values (replicas, resources,
scheduling, logging, metrics, and so on), validated against the chart's schema so
a bad value fails fast (under `--dry-run` too). Precedence, later wins: `-f` files
(in order) < `--set` < `--set-string`. The operator `image.*` values are not part
of the supported surface: a `--set image.tag`, `image.repository`, or
`image.digest` will repoint the operator image, but the supported way to change
the operator version is to upgrade the CLI.

`--dry-run` prints the action the next install would take and changes nothing:

```text
Plan: install operator (installed none -> v0.2.0) in namespace yacd-system
```

See [Installation](../operator/installation.md) for the full operator-install
workflow (including the Helm alternative), and the
[configuration reference](configuration.md) for every value the override flags
accept.

## init

```text
yacd init
```

Prints a fully-commented developer environment template to stdout and takes no
arguments or flags beyond the [global flags](#global-flags). The active
configuration is a ready-to-run local devnet; local networks automatically get a
genesis-funded `faucet` wallet. Commented blocks document the rest of the API,
including chain-API overrides and a public/mainnet alternative. Redirect it to a
file and apply it:

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
endpoint names (`node-to-node`, `ogmios`, `kupo`) or `-` when none are published
yet.

The `--json` output is an array of objects with fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `name` | string | CardanoNetwork name. |
| `namespace` | string | CardanoNetwork namespace. |
| `mode` | string | Requested network mode (`local` or `public`). |
| `ready` | bool | Fresh `Ready` condition observed as `True`. |
| `endpoints` | object | `nodeToNode`, `ogmios`, `kupo` URLs; empty when unpublished. |

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
| `endpoints` | object | `nodeToNode`, `ogmios`, `kupo`, each `{serviceName, port, url}` or absent. |
| `wallet` | object | `{address, keySecretName}` of the genesis-funded `faucet` wallet. Omitted when the network has none (non-local networks). |
| `conditions` | array | Each `{type, status, reason, message, observedGeneration, lastTransitionTime}` (RFC3339 timestamp). |

## wallet

```text
yacd wallet <command> NET [args] [flags]
```

Manages developer wallets for a network and funds them by building, signing, and
submitting transactions directly over the network's Ogmios and Kupo endpoints;
there is no in-cluster faucet service. Keys are stored as labeled Kubernetes
Secrets (`<network>-wallet-<name>`) in the network's namespace, and the CLI reads
them to sign locally.

Every local network has a reserved, operator-owned genesis-funded `faucet` wallet
that funding spends from by default. The `WALLET` argument of `topup`, `remove`,
and `export` accepts a managed wallet name, a public key (hex), or a bech32
`addr_test...` address.

### wallet list

```text
yacd wallet list NET [flags]
```

Lists the CLI-managed wallets for a network (name, address, and the
`managed-by-cli` source); the operator-owned `faucet` wallet is not listed.
`--json` prints a machine-readable array.

### wallet add

```text
yacd wallet add NET [flags]
```

Generates a new managed wallet and, with `--topup`, funds it from the `faucet`
wallet.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--name` | string | generated | Wallet name (default: a generated adjective-noun name). |
| `--topup` | string | `""` | Fund the new wallet with this many lovelace from the faucet. |
| `--await` | bool | `false` | Wait for the funding transaction to confirm on-chain (requires `--topup`). |
| `--await-timeout` | duration | `2m0s` | Maximum time to wait for `--await` confirmation. |
| `--json` | bool | `false` | Print machine-readable JSON. |

### wallet topup

```text
yacd wallet topup NET WALLET LOVELACE [flags]
```

Funds `WALLET` with `LOVELACE` (positional, must be greater than 0) from the
`faucet` wallet, or from another managed wallet with `--from`. It forwards Ogmios
and Kupo itself, so no `yacd run` wrapper or URL flags are needed.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--from` | string | `faucet` | Source wallet name to fund from (default: the faucet wallet). |
| `--await` | bool | `false` | Wait for the funding transaction to confirm on-chain. |
| `--await-timeout` | duration | `2m0s` | Maximum time to wait for `--await` confirmation. |
| `--json` | bool | `false` | Print machine-readable JSON. |

### wallet export

```text
yacd wallet export NET WALLET [flags]
```

Writes the wallet's `<name>.skey`, `<name>.vkey`, and `<name>.addr` files
(mode `0600`) to `.yacd/<namespace>/<network>/wallets/<name>/`.

| Flag | Type | Default | Meaning |
| --- | --- | --- | --- |
| `--out` | string | `.yacd/<ns>/<net>/wallets/<name>` | Directory to write the wallet files into. |
| `--force` | bool | `false` | Overwrite existing wallet files. |

### wallet remove

```text
yacd wallet remove NET WALLET
```

Deletes a managed wallet. The reserved `faucet` wallet cannot be removed.

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

!!! note "Loopback ports are ephemeral"
    The endpoints file holds no secrets, and its ports are only live while
    `connect` is running. See the [endpoints.json schema](#the-endpointsjson-schema).

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

`yacd wallet topup --await` forwards Ogmios and Kupo itself, so it confirms
on-chain without needing these variables; inside `yacd run` it reuses the
forwards already established.

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
  "kupoUrl": "http://127.0.0.1:51821"
}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `network` | string | CardanoNetwork name. Always present. |
| `namespace` | string | CardanoNetwork namespace. Always present. |
| `networkMagic` | int | Network magic. Omitted until the controller publishes it. |
| `ogmiosUrl` | string | Loopback Ogmios URL. Omitted when not forwarded. |
| `kupoUrl` | string | Loopback Kupo URL. Omitted when not forwarded. |

The ports it lists are only live while `connect` is running, and the file is
removed on disconnect.

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
