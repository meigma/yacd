# Getting started

This tutorial takes you from nothing to a running local Cardano devnet and a
funded address in a handful of commands. You will install the `yacd` CLI, bring
up a devnet, inspect it, query the chain tip, fund an address, and tear it all
down again.

By the end you will have run a complete local development loop. For *why* the
pieces fit together this way, see
[Architecture](../concepts/architecture.md).

!!! note "Prerequisites"
    `yacd devnet` needs a running [Docker](https://www.docker.com/) (or a
    compatible container runtime), because [k3d](https://k3d.io) runs the local
    cluster as containers. You do not install k3d yourself: on first run the CLI
    downloads a pinned, checksum-verified k3d binary and caches it under
    `~/.local/share/yacd/bin`, so the first `yacd devnet` is slower than later
    ones.

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

!!! note "Keep these handy"
    The network uses network magic `42` (you will use it when you query the
    chain), and the `Wallet` line shows the network's genesis-funded `faucet`
    wallet, which funds the wallets you create. The two commands under `Try:` are
    the next two things you will run.

For what the cluster, operator, and network are and how they relate, see
[Architecture](../concepts/architecture.md).

## 3. List and inspect the network

List the networks in the cluster (`yacd list` shows every namespace by default):

```sh
yacd list
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

## 5. Fund a wallet

Every local network is created with a genesis-funded `faucet` wallet. Create a
new managed wallet and fund it from the faucet in one command, waiting for
on-chain confirmation:

```sh
yacd wallet add devnet --topup 5000000 --await
```

This generates a wallet, sends it 5,000,000 lovelace (5 ADA) from the `faucet`
wallet, and prints the new wallet's address and the funding transaction id.
`yacd wallet` builds and submits the transaction directly over Ogmios and Kupo;
there is no separate faucet service.

!!! note "Wallets fund local development"
    The `faucet` wallet and the wallets you create exist to fund development on
    local networks. Public networks have no faucet wallet; fund those from your
    own keys.

For the full wallet workflow — funding an existing address, listing, exporting,
and removing wallets — see the [funding guide](funding.md).

## 6. Tear it down

When you are finished, delete the managed cluster:

```sh
yacd devnet down
```

This removes the k3d cluster and everything in it. Your next `yacd devnet` will
start fresh.

## Where to go next

- [Working with networks](networks.md) — scaffold your own config with `yacd
  init` and apply it.
- [Connecting tools](connecting-tools.md) — wire Ogmios, Kupo, and other tools
  to a network from your host.
- [Funding](funding.md) — the full faucet and top-up workflow.
- [CLI reference](../reference/cli.md) — every command, flag, and default.
- [Architecture](../concepts/architecture.md) — why YACD is built this way.
