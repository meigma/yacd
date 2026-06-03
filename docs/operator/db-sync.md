# DB-Sync indexing

Index an existing `CardanoNetwork` with [cardano-db-sync](https://github.com/IntersectMBO/cardano-db-sync)
by applying a `CardanoDBSync` in the **same namespace**. db-sync follows the
chain through a node socket and writes a queryable Postgres schema.

This guide covers the two decisions you make before applying: where the Postgres
database comes from, and where db-sync runs relative to the network. For the full
spec and every `insert` option, see the
[CardanoDBSync reference](../reference/cardanodbsync.md). For the rationale behind
the placement and insert trade-offs, see
[Architecture](../concepts/architecture.md).

## Before you start

- A `CardanoNetwork` already exists and is `Ready` in the namespace you will use.
  See [Networks](../developer/networks.md) and the
  [yacd CLI reference](../reference/cli.md).
- You can apply manifests to that namespace with `kubectl`.

`CardanoDBSync` requires only `networkRef.name` and a `database` mode; every other
field has a default. The examples below use the `devnet` network in the
`yacd-smoke` namespace.

## Decision 1: choose the Postgres source

`spec.database` requires **exactly one** of `managed` or `external`.

### Managed Postgres (recommended for local development)

Set `database.managed` and the controller provisions Postgres for you. When you do
not supply credentials, it creates an owned Secret holding the generated password
and reports the name in `status.database.authSecretName`.

```yaml
spec:
  networkRef:
    name: devnet
  database:
    managed: {}
```

`managed: {}` accepts all defaults (image `postgres:17.2-alpine`, database
`cexplorer`, user `postgres`). To reuse a password you already hold, point
`managed.authSecretRef.name` at a same-namespace Secret with the password under
the key `password`. See the
[CardanoDBSync reference](../reference/cardanodbsync.md) for the full `managed`
surface.

### External Postgres

Set `database.external` to point db-sync at a Postgres instance you operate. You
must supply the password through `passwordSecretRef`, which references a
same-namespace Secret.

Create the Secret first:

```sh
kubectl -n yacd-smoke create secret generic dbsync-postgres \
  --from-literal=password=change-me
```

Then reference it from the `CardanoDBSync`:

```yaml
spec:
  networkRef:
    name: devnet
  database:
    external:
      host: postgres.yacd-smoke.svc.cluster.local
      passwordSecretRef:
        name: dbsync-postgres
```

`passwordSecretRef.key` defaults to `password`, so a Secret with that key needs no
`key` field. `port` (`5432`), `database` (`cexplorer`), `user` (`postgres`), and
`sslMode` (`disable`) all default; override them in `external` when your server
differs. See the
[CardanoDBSync reference](../reference/cardanodbsync.md) for the full `external`
surface, including `sslMode` values.

## Decision 2: choose where db-sync runs

`spec.placement.mode` selects how db-sync reaches the node socket.

- `dedicatedFollower` (default) runs a separate follower node colocated with
  db-sync, owned by the `CardanoDBSync` controller. Omitting `placement` keeps this
  behavior.
- `primarySidecar` injects db-sync into the referenced network's primary node Pod
  instead of running a separate follower.

```yaml
spec:
  placement:
    mode: primarySidecar
```

`followerNode` cannot be set when `placement.mode` is `primarySidecar`; the spec
rejects that combination. For when to prefer each mode, see
[Architecture](../concepts/architecture.md).

!!! warning "Placement is fixed for a database"
    The placement mode is recorded against the db-sync state in
    `status.database.acceptedPlacementMode`. Changing it later requires deleting
    and recreating the `CardanoDBSync` with a fresh or compatible database.

## Apply the CardanoDBSync

Use one of the manifests from [Recipes](../recipes.md) (mirrored from `examples/`),
or write your own from the decisions above. Apply it into the network's namespace:

```sh
kubectl apply -f cardanodbsync.yaml
```

## Check Ready and Synced

`CardanoDBSync` has no dedicated `yacd` subcommand; check it with `kubectl`. The
printed columns surface progress at a glance:

```sh
kubectl -n yacd-smoke get cardanodbsync
```

```text
NAME     NETWORK   READY   SYNCED   AGE
dbsync   devnet    True    True     5m
```

- `READY` (`Ready` condition) reports that db-sync is usable through its published
  database endpoint.
- `SYNCED` (`Synced` condition) reports that db-sync has caught up to the node tip.

Wait for both conditions to read `True`:

```sh
kubectl -n yacd-smoke wait --for=condition=Ready cardanodbsync/dbsync --timeout=15m
kubectl -n yacd-smoke wait --for=condition=Synced cardanodbsync/dbsync --timeout=60m
```

For more detail, inspect the resource status. It reports the Postgres endpoint,
indexing progress (block heights and lag), and, for managed Postgres without your
own Secret, the generated credentials Secret name:

```sh
kubectl -n yacd-smoke get cardanodbsync/dbsync -o yaml
```

The Postgres endpoint and metrics endpoint appear under `status.endpoints`. For
the complete status surface, including every condition type, see the
[CardanoDBSync reference](../reference/cardanodbsync.md).

## Next steps

- Tune what db-sync writes with `spec.config.insert`; see the
  [CardanoDBSync reference](../reference/cardanodbsync.md).
- Understand the placement and insert trade-offs in
  [Architecture](../concepts/architecture.md).
- Read the upstream
  [cardano-db-sync documentation](https://github.com/IntersectMBO/cardano-db-sync)
  for schema details and query guidance.
