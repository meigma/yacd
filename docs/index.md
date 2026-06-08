# yacd

yacd is a Kubernetes-native manager for Cardano development environments. It is
aimed at people building on [Cardano](https://developers.cardano.org), not
validators, stake pool operators, or production network operators.

yacd ships as two surfaces:

- A **Kubernetes operator** that owns declarative cluster state. The
  `CardanoNetwork` CRD reconciles a Cardano node with [Ogmios](https://ogmios.dev)
  as the default chain API, [Kupo](https://cardanosolutions.github.io/kupo/) as
  the default chain index. Local networks also get a genesis-funded `faucet`
  wallet for funding development.
- A companion **`yacd` CLI** that owns local developer workflow: standing up a
  devnet, applying a checked-in config, waiting for readiness, printing
  connection details, forwarding endpoints to your host, and funding wallets.

## Where to go next

=== "Developing locally"

    Build and test against a Cardano network on your own machine. Start with the
    [Getting started tutorial](developer/getting-started.md).

=== "Running on a cluster"

    Install the operator into an existing Kubernetes cluster — one `yacd install`,
    or Helm — so a team can share YACD-managed networks. See
    [Operator installation](operator/installation.md).

## Try it in one command

A single `yacd devnet` provisions a local cluster, installs the operator, and
applies a funded network. It needs [Docker](https://www.docker.com/) running and
fetches a pinned [k3d](https://k3d.io) binary on first use:

```sh
yacd devnet
```

Follow the [Getting started tutorial](developer/getting-started.md) for the full
path, including how to inspect the network and wire it into your tests. Every
flag and default is listed in the [CLI reference](reference/cli.md).

## External tools

yacd composes existing Cardano and Kubernetes tooling. These links go to the
upstream projects:

- [Cardano developer portal](https://developers.cardano.org)
- [Ogmios](https://ogmios.dev) — chain API
- [Kupo](https://cardanosolutions.github.io/kupo/) — chain index
- [cardano-db-sync](https://github.com/IntersectMBO/cardano-db-sync) — relational chain index
- [Mithril](https://mithril.network) — fast bootstrap for public networks
- [Helm](https://helm.sh) and [k3d](https://k3d.io) — packaging and the local cluster
- [Kubernetes](https://kubernetes.io)
