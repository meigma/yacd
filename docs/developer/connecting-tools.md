# Connecting tools & tests

The operator publishes a network's chain APIs as **cluster-internal**
`*.svc.cluster.local` Service URLs that a laptop or CI runner cannot reach
directly. Use `yacd run`, `yacd exec`, and `yacd connect` to bridge that gap.
Each verb sets the same `YACD_*` environment variables, so your tools read
ordinary env vars and stay YACD-agnostic across local and CI runs.

This page covers choosing and using the verbs. For the full variable list and
defaults, see the [CLI reference](../reference/cli.md). All examples assume the
network is already `up` and `Ready`.

## Choose a verb

Pick the verb by how your tool reaches the node and how long you need access:

| You want to… | Use | Why |
|---|---|---|
| Run a test suite or one-off command against Ogmios or Kupo | [`yacd run`](#run-a-test-runner) | Resolves a host-usable URL per endpoint (a reachable `externalURL` or a loopback port-forward), injects `YACD_*`, runs your command, then tears any forwards down. |
| Run `cardano-cli` or any tool that needs the node's Unix socket | [`yacd exec`](#run-a-socket-bound-tool) | Runs the command inside the node Pod, where the socket is local. A port-forward cannot expose a Unix socket. |
| Keep endpoints open for a dev server, REPL, or repeated IDE test runs | [`yacd connect`](#hold-forwards-open) | Holds supervised forwards open in one terminal and writes the loopback URLs to `endpoints.json` for other processes to read. |

The deciding factor between `run` and `exec` is the transport: **`run` reaches
the TCP chain APIs** (Ogmios and Kupo) from the host; **`exec` runs in the Pod**
for anything that speaks to the node over its local Unix socket.

## Run a test runner

Use `yacd run NAME -- <cmd>` for the primary test/CI path. It resolves a
host-usable URL for Ogmios and Kupo — preferring a reachable `externalURL` (for
example `yacd devnet`'s `localhost` ports) and falling back to a scoped
port-forward — injects the `YACD_*` environment, runs `<cmd>` on the host, and
tears any forwards down when it exits. The command's exit code is propagated, so
a test failure survives the wrapper. The full resolution order is in the
[CLI reference](../reference/cli.md#chain-access-resolution).

Put `--` before any command that takes its own flags so they pass through to the
command instead of being parsed by `yacd`:

```sh
yacd run my-net -- go test ./e2e/...
```

Your test code reads the loopback URLs from the environment:

```go
ogmios := os.Getenv("YACD_OGMIOS_URL") // reachable ws:// URL (externalURL or loopback forward)
kupo := os.Getenv("YACD_KUPO_URL")     // reachable http:// URL
```

With no command, `run` drops into your `$SHELL` (falling back to `/bin/sh`) with
the same environment set, which is handy for poking at the network by hand:

```sh
yacd run my-net
```

If a forward drops while the command is running, `run` cancels the command and
reports the lost connection instead of a bare exit code.

See the [`YACD_*` variable table](../reference/cli.md) for every variable and
when it is present.

## Run a socket-bound tool

Use `yacd exec NAME -- <cmd>` for `cardano-cli` and anything that reaches the
node over its local Unix socket. `cardano-cli` talks to the node over a
`--socket-path`, not over TCP, so a port-forward cannot expose it. `exec` runs
the command **inside** the primary node Pod with `CARDANO_NODE_SOCKET_PATH` and
the `YACD_*` variables set in the pod environment, so `cardano-cli` finds the
socket automatically:

```sh
yacd exec my-net -- cardano-cli query tip --testnet-magic 42
```

`exec` runs the command directly, **not** through a shell, so `$VAR` references
in arguments are not expanded. To interpolate `YACD_*` variables into arguments,
run a shell explicitly:

```sh
yacd exec my-net -- sh -c 'cardano-cli query tip --testnet-magic "$YACD_NETWORK_MAGIC"'
```

When both stdin and stdout are a terminal, `exec` attaches an interactive TTY
(raw mode, with window resizes forwarded), so this opens an interactive shell
inside the node Pod. Piped or non-terminal invocations (for example in CI)
stream without a TTY:

```sh
yacd exec my-net -- sh
```

## Hold forwards open

Use `yacd connect NAME` when several host processes need the endpoints at once,
or across repeated runs: a dApp dev server, a REPL, or an IDE that re-runs tests.
It holds supervised forwards open in the foreground (one terminal) and writes the
loopback URLs to a gitignored endpoint state file for other processes to read.
Run it in one terminal and your tools in another:

```sh
yacd connect my-net
```

`connect` runs until you press Ctrl-C. If a forward drops (pod restart, idle
timeout) it re-resolves the primary Pod, reassigns local ports, and writes a
fresh endpoints file. A clean disconnect removes the file.

The endpoint state file lives under `.yacd/`. When the namespace defaults to the
network name, the path is:

```text
.yacd/<network>/endpoints.json
```

When `--namespace` is set, the path includes both identity parts so networks
with the same name in different namespaces do not collide:

```text
.yacd/<namespace>/<network>/endpoints.json
```

Read the loopback URLs from that file in any host process. The file is created
`0600` under `0700` directories, holds no secrets, and its ports are only live
while `connect` is running. For the document's field names and shape, see the
[endpoints.json schema](../reference/cli.md).

## Fund a wallet from a test

`yacd wallet topup NET WALLET LOVELACE --await` funds a target and polls Kupo
until the funding transaction's output appears, so a test never races chain
inclusion. The `WALLET` argument is a managed wallet name or a bech32
`addr_test...` address, and `topup` reaches Ogmios and Kupo on its own (a
reachable `externalURL` or a port-forward), so no `yacd run` wrapper is needed.
Pass `--ogmios-url` / `--kupo-url` to point it at specific endpoints:

```sh
export ADDR=addr_test1... # the address your test funds
yacd wallet topup my-net "$ADDR" 1000000 --await
```

Funds come from the network's genesis `faucet` wallet by default; `--from`
selects another managed wallet. See the
[CLI reference](../reference/cli.md#wallet) for all `wallet` flags and the
[funding guide](funding.md) for the full wallet workflow.

## See also

- [CLI reference](../reference/cli.md) — every flag, the `YACD_*` variable
  table, and the `endpoints.json` schema.
- [Architecture](../concepts/architecture.md) — why the contract is shaped this
  way.
