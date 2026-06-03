# Getting started

This tutorial takes you from nothing to a running local Cardano devnet and a
funded address in a handful of commands. You will install the `yacd` CLI, bring
up a devnet, inspect it, query the chain tip, fund an address, and tear it all
down again.

By the end you will have run a complete local development loop. For *why* the
pieces fit together this way, see
[Architecture](../concepts/architecture.md).

!!! note "What you need"
    A working [Docker](https://www.docker.com/) (or compatible container
    runtime) so the CLI can create a local [k3d](https://k3d.io) cluster.
    Everything else, including k3d itself, is provisioned for you.

## 1. Install the CLI

Install the `yacd` binary using one of the options below.

=== "Download a release"

    Download the binary for your platform from the project's GitHub Releases
    page, make it executable, and move it onto your `PATH`:

    ```sh
    chmod +x yacd
    sudo mv yacd /usr/local/bin/yacd
    ```

=== "Build from source"

    From a clone of the repository, build the CLI with the Go toolchain:

    ```sh
    go build -o yacd ./cli/cmd/yacd
    sudo mv yacd /usr/local/bin/yacd
    ```

Verify the install:

```sh
yacd --version
```

You should see a version line printed to your terminal. If the command is not
found, confirm the directory you moved `yacd` into is on your `PATH`.

## 2. Bring up the devnet

Create the cluster, install the operator, and apply a default funded network
with a single command:

```sh
yacd devnet
```

This provisions a managed k3d cluster, installs the YACD operator into it, and
applies one network named `devnet` in the `devnet` namespace. Progress streams
to your terminal while it works, and on success it prints a summary like this:

```text
devnet is ready.
  Cluster:  <cluster> (context <context>)
  Operator: <version>
  Ogmios:   <url>
  Kupo:     <url>
  Wallet:   <funded address>

Try:
  yacd exec devnet -- cardano-cli query tip --testnet-magic 42
  yacd devnet down
```

The `Wallet` line is a pre-funded developer address, and the network uses
network magic `42`. Note both: you will use them in the steps below. The two
commands under `Try:` are exactly the next two things you will run.

For what the cluster, operator, and network are and how they relate, see
[Architecture](../concepts/architecture.md).

## 3. List and inspect the network

List the networks in the cluster. Because `devnet` runs in its own `devnet`
namespace, list across all namespaces with `-A`:

```sh
yacd list -A
```

The `devnet` network you just created appears in the output. Inspect it in
detail, including its status and connection information:

```sh
yacd info devnet
```

`yacd info` prints the network's readiness and the endpoint URLs your tools
connect to. Keep this command handy while you work; it is the quickest way to
check what a network is exposing.

## 4. Query the chain tip

Run `cardano-cli` *inside* the node Pod to query the current chain tip. This is
the first command from the `Try:` hint:

```sh
yacd exec devnet -- cardano-cli query tip --testnet-magic 42
```

`yacd exec` runs the command directly inside the primary node Pod, where the
node socket and the `YACD_*` environment are already set, so `cardano-cli` finds
the socket automatically. The `--testnet-magic 42` value is the devnet's network
magic from step 2.

The output is a JSON object describing the tip (slot, block, epoch, and sync
percentage). Run it again after a few seconds and the slot advances, confirming
the chain is producing blocks.

## 5. Fund an address

The faucet runs inside the cluster, so reach it through `yacd run`, which
forwards the faucet to a local port and exposes it as `$YACD_FAUCET_URL`.
Replace `<address>` with a Cardano testnet address you control (the `Wallet`
address from step 2 works), and `<lovelace>` with the amount to send
(1 ADA = 1,000,000 lovelace):

```sh
yacd run devnet -- sh -c \
  'yacd topup devnet --address <address> --lovelace <lovelace> --faucet-url "$YACD_FAUCET_URL"'
```

Both `--address` and `--lovelace` are required, and `--lovelace` must be greater
than zero. On success the faucet submits a funding transaction and `topup`
prints the transaction ID. The loopback faucet URL that `run` exposes is exempt
from the [trust gate](../concepts/security.md), so no extra flags are needed.

!!! warning "The faucet is local-only"
    The faucet and its auth token are part of your local devnet only. Never
    point `yacd topup` at a shared or public network, and never send the faucet
    token off your machine.

For the full funding workflow, including waiting for on-chain confirmation, see
the [funding guide](funding.md).

## 6. Tear it down

When you are finished, delete the managed cluster:

```sh
yacd devnet down
```

This removes the k3d cluster and everything in it. Your next `yacd devnet` will
start fresh.

## Where to go next

- [Working with networks](networks.md) — apply your own network definitions.
- [Connecting tools](connecting-tools.md) — wire Ogmios, Kupo, and other tools
  to a network from your host.
- [Funding](funding.md) — the full faucet and top-up workflow.
- [CLI reference](../reference/cli.md) — every command, flag, and default.
- [Architecture](../concepts/architecture.md) — why YACD is built this way.
