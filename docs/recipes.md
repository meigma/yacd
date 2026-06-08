# Recipes

Copy-paste manifests for the most common YACD setups. Each recipe is mirrored
verbatim from a file under `examples/`. Use them as a starting point and adjust
field values to suit your cluster.

!!! tip
    `yacd init` prints a commented version of the single-pool local devnet below,
    ready to redirect into a file (`yacd init > yacd.yaml`). It is the quickest
    way to start a custom network; see [Defining networks](developer/networks.md).

Developer environment files (`kind: Environment`) are applied with the CLI:

```sh
yacd up dev -f yacd.yaml
```

DB-Sync resources (`kind: CardanoDBSync`) are applied with `kubectl`:

```sh
kubectl apply -f cardanodbsync.yaml
```

For the full field semantics, defaults, and enums behind these manifests, see
the reference pages linked under each section. For every CLI flag and default,
see [CLI reference](reference/cli.md).

## Local networks

A local network runs a self-contained devnet with a fast block schedule. Every
local network is created with a genesis-funded `faucet` wallet that you fund
developer wallets from with [`yacd wallet`](developer/funding.md). See the
[Environment file reference](reference/environment.md) for every field.

### Single-pool local devnet

Use when you want an isolated, fast local Cardano network.

```yaml
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: local
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 2Gi
    # Ogmios and Kupo are enabled by default. A local network is automatically
    # given a genesis-funded `faucet` wallet — the network's funded wallet and
    # the default source for `yacd wallet topup`.
    local:
      networkMagic: 42
      era: conway
      timing:
        slotLength: 100ms
        epochLength: 500
      topology:
        pools:
          count: 1
```

## Public networks

A public network syncs a node against an upstream Cardano network. Select the
target with `spec.network.public.profile`. See the
[Environment file reference](reference/environment.md) for every field.

### Preview testnet

Use when you want to develop against the public Preview testnet.

```yaml
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: public
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 20Gi
    public:
      profile: preview
```

### Preprod testnet

Use when you want to develop against the public Preprod testnet.

```yaml
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: public
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 20Gi
    public:
      profile: preprod
```

### Mainnet

Use when you need to sync against Cardano mainnet.

```yaml
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: public
    node:
      version: "11.0.1"
      port: 3001
    public:
      profile: mainnet
      bootstrap:
        mithril: {}
```

!!! warning "Mainnet has hard requirements"
    - A [Mithril](https://mithril.network) bootstrap (`bootstrap.mithril`) is
      required to seed the chain database; syncing mainnet from genesis is not
      practical.
    - Mainnet needs **large persistent storage** well beyond the 20Gi used for
      testnets. Size `node.storage.size` for the full mainnet chain.
    - The CLI refuses to apply a mainnet network unless you pass
      `--allow-mainnet`. See [CLI reference](reference/cli.md).

## DB-Sync

[cardano-db-sync](https://github.com/IntersectMBO/cardano-db-sync) indexes a
network's chain into PostgreSQL. A `CardanoDBSync` references an existing
network through `spec.networkRef` and connects to either an operator-managed or
an external database. See the
[CardanoDBSync reference](reference/cardanodbsync.md) for every field.

### Managed PostgreSQL

Use when you want YACD to provision and own the PostgreSQL database for you.

```yaml
apiVersion: yacd.meigma.io/v1alpha1
kind: CardanoDBSync
metadata:
  name: dbsync
  namespace: yacd-smoke
spec:
  networkRef:
    name: devnet
  database:
    managed: {}
```

### External PostgreSQL

Use when you connect DB-Sync to a PostgreSQL instance you manage yourself,
supplying the password through a `Secret`.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: dbsync-postgres
  namespace: yacd-smoke
type: Opaque
stringData:
  password: change-me
---
apiVersion: yacd.meigma.io/v1alpha1
kind: CardanoDBSync
metadata:
  name: dbsync
  namespace: yacd-smoke
spec:
  networkRef:
    name: devnet
  database:
    external:
      host: postgres.yacd-smoke.svc.cluster.local
      passwordSecretRef:
        name: dbsync-postgres
```

### Preprod DB-Sync on a dedicated follower

Use when you index a public Preprod network with a dedicated follower node and
a minimal insert preset to reduce write volume.

```yaml
apiVersion: yacd.meigma.io/v1alpha1
kind: CardanoDBSync
metadata:
  name: preprod-dbsync
  namespace: yacd-smoke
spec:
  networkRef:
    name: preprod-smoke
  placement:
    mode: dedicatedFollower
  database:
    managed: {}
  config:
    insert:
      preset: disable_all
```

### Preview DB-Sync as a primary sidecar

Use when you co-locate DB-Sync with the primary node as a sidecar and tune its
runtime for a lightweight, low-overhead index.

```yaml
apiVersion: yacd.meigma.io/v1alpha1
kind: CardanoDBSync
metadata:
  name: preview-dbsync-sidecar
  namespace: yacd-smoke
spec:
  networkRef:
    name: preview-smoke
  placement:
    mode: primarySidecar
  database:
    managed:
      parameters:
        maxParallelMaintenanceWorkers: 0
  config:
    ledgerBackend: inmemory
    runtime:
      cache: false
      epochTable: false
      forceIndexes: false
      metricsPort: 8080
    insert:
      preset: disable_all
```
