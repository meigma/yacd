# Host access and the `YACD_*` contract

The operator publishes a network's chain APIs as **cluster-internal**
`*.svc.cluster.local` Service URLs, which a laptop or CI runner cannot reach.
The `run`, `connect`, and `exec` verbs bridge that gap, and the `YACD_*`
environment variables are the **integration surface**: your tests read ordinary
env vars and never parse a YACD file, so the test runner stays YACD-agnostic and
the same test works locally and in CI.

This page is a reference for that contract. It assumes a network is already
`up` and `Ready`.

## The verbs

| Verb | What it does |
|---|---|
| `yacd run NAME -- <cmd>` | Establish scoped port-forwards to the chain APIs, inject the `YACD_*` environment, run `<cmd>` on the host, and tear the forwards down when it exits. No command drops into `$SHELL`. The command's exit code is propagated. This is the primary test/CI path. |
| `yacd connect NAME` | Hold the forwards open in the foreground (one terminal) while you work in another, writing the loopback URLs to `.yacd/<network>/endpoints.json`. Re-establishes dropped forwards; runs until Ctrl-C. |
| `yacd exec NAME -- <cmd>` | Run `<cmd>` **inside** the primary node Pod, for tools that reach the node over its local Unix socket. |
| `yacd topup NAME --await …` | Fund an address through the faucet and wait for the funding transaction to be confirmed on-chain. |

## `run` vs `exec`: which one?

- Use **`run`** for tooling that speaks to **Ogmios, Kupo, or the faucet over
  TCP** — it forwards those Services to loopback ports and points `YACD_*` at
  them.
- Use **`exec`** for **`cardano-cli` and anything that needs the node's Unix
  domain socket**. `cardano-cli` talks to the node over a local socket
  (`--socket-path`), not TCP, so a port-forward cannot expose it; `exec` runs
  the command in the Pod where the socket is local and sets
  `CARDANO_NODE_SOCKET_PATH`.

`exec` runs the command directly, **not** through a shell, so `$VAR` references
in arguments are not expanded. `cardano-cli` reads `CARDANO_NODE_SOCKET_PATH`
from the environment automatically:

```sh
yacd exec my-net -- cardano-cli query tip --testnet-magic 42
```

To interpolate `YACD_*` variables into arguments, run a shell explicitly:

```sh
yacd exec my-net -- sh -c 'cardano-cli query tip --testnet-magic "$YACD_NETWORK_MAGIC"'
```

When stdin and stdout are a terminal, `exec` attaches an interactive TTY (raw
mode, with window resizes forwarded), so this opens an interactive shell inside
the node Pod; piped or non-terminal invocations (for example in CI) stream
without a TTY:

```sh
yacd exec my-net -- sh
```

## The `YACD_*` environment variables

The variable **names are identical** whether a command runs on the host
(`run`/`connect`) or inside the Pod (`exec`); only the values adapt, which is
what makes a test transparent to where it runs. This is contract **version 1**:
adding a variable is backward compatible; renaming or removing one is a breaking
change.

| Variable | Host (`run`/`connect`) | In-pod (`exec`) | Present when |
|---|---|---|---|
| `YACD_NETWORK` | network name | same | always |
| `YACD_NAMESPACE` | namespace | same | always |
| `YACD_NETWORK_MAGIC` | e.g. `42` | same | the controller publishes a network magic |
| `YACD_OGMIOS_URL` | `ws://127.0.0.1:<port>` | `ws://<svc>.<ns>.svc.cluster.local:<port>` | Ogmios published |
| `YACD_KUPO_URL` | `http://127.0.0.1:<port>` | `http://<svc>.<ns>.svc.cluster.local:<port>` | Kupo published |
| `YACD_FAUCET_URL` | `http://127.0.0.1:<port>` | `http://<svc>.<ns>.svc.cluster.local:<port>` | faucet published |
| `YACD_FAUCET_TOKEN` | faucet auth token | *(omitted)* | faucet ready, **host only** |
| `CARDANO_NODE_SOCKET_PATH` | — | `/ipc/node.socket` | `exec` only |

The host URL schemes are taken from what the operator published (Ogmios stays
`ws://`), with the host rewritten to a random loopback port. `YACD_FAUCET_TOKEN`
is deliberately **never** set in the in-pod (`exec`) environment: a Bearer token
in the exec command line would leak into apiserver audit logs, and in-pod
tooling does not need it.

## `connect` and endpoint state files

`connect` writes the loopback URLs to a gitignored endpoint state file for
other host processes (a dApp dev server, a REPL, repeated IDE test runs) to
read. When `--namespace` is not set and the namespace defaults to the network
name, the path stays:

```text
.yacd/<network>/endpoints.json
```

When `--namespace` is set, the path includes both identity parts so networks
with the same name in different namespaces do not collide:

```text
.yacd/<namespace>/<network>/endpoints.json
```

The document shape is the same in either location:

```json
{
  "network": "my-net",
  "namespace": "my-net",
  "networkMagic": 42,
  "ogmiosUrl": "ws://127.0.0.1:34521",
  "kupoUrl": "http://127.0.0.1:34522",
  "faucetUrl": "http://127.0.0.1:34523"
}
```

The file is created `0600` under `0700` state directories and **never contains
the faucet token**. Its ports are only live while `connect` is running. A clean
disconnect removes the file, and a dropped forward removes the stale file before
re-establishing, reassigning local ports, and writing a fresh document.

## Funding with `topup --await`

`yacd topup NAME --address ADDR --lovelace N --await` funds `ADDR` through the
faucet and then polls Kupo until the funding transaction's output appears, so a
test never races chain inclusion. `--await` requires a Kupo URL: pass
`--kupo-url`, or run under `yacd run` (which sets `YACD_KUPO_URL`):

```sh
yacd run my-net -- sh -c \
  'yacd topup my-net --address "$ADDR" --lovelace 1000000 --faucet-url "$YACD_FAUCET_URL" --await'
```

The loopback faucet URL is exempt from the `topup` trust gate, so no
`--trust-faucet-url` flag is needed against a `run`/`connect` forward.

## See also

- The full operator and CLI surface in the [README](../README.md).
- The architecture direction in [DESIGN.md](../DESIGN.md).
