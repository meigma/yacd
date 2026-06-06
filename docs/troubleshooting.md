# Troubleshooting

How to diagnose and fix the most common problems with a `CardanoNetwork` or
`CardanoDBSync`. Start by reading the status, then match your symptom to a
failure mode below.

## Start here: read the status

For a network, run [`yacd info`](reference/cli.md) with the network name. It
fetches the `CardanoNetwork` and prints its conditions, network identity, and
endpoints:

```sh
yacd info my-net
```

The output has a `Conditions:` section with one line per condition, formatted as
`Type: Status (Reason) - Message`:

```
Conditions:
  Ready: False (Progressing) - waiting for the primary node to synchronize
  NodeReady: True (NodeRunning) - primary node container is running
  NodeSynchronized: False (Syncing) - node is catching up to the inferred tip
```

Read it top-down:

- **`Ready`** is the rollup. `Ready: True` means the network is usable through
  its published endpoints. `Ready: False` means it is not, and the per-component
  conditions tell you which part is blocking.
- **`Degraded: True`** means the resource failed to reach or maintain its
  desired state. Read its `Message` first; it carries the specific error.
- **`Progressing`** means the resource is still being created or updated. A
  `False` rollup with `Progressing: True` is normal during initial bring-up.

The per-component conditions a `CardanoNetwork` publishes are `NodeReady`
(primary node container running), `NodeSynchronized` (caught up to the inferred
network tip), `NodeProgressing` (tip advancing or already synchronized),
`ArtifactsReady` (artifact bundle staged and served over HTTP), `OgmiosReady`,
`KupoReady`, and `DBSyncAttachmentReady` (a primary-sidecar db-sync attachment
is not blocking the primary Pod). See the
[CardanoNetwork reference](reference/cardanonetwork.md) for the full list.

For machine-readable status, including the `reason` and `message` of every
condition, add `--json`:

```sh
yacd info my-net --json
```

For a `CardanoDBSync`, `yacd info` does not apply. Read its conditions directly:

```sh
kubectl get cardanodbsync my-dbsync -o jsonpath='{.status.conditions}' | jq
```

Its component conditions are `Ready`, `FollowerNodeReady`, `NodeSocketReady`,
`SidecarMaterialReady`, `PostgresReady`, `DBSyncReady`, and `Synced`. See the
[CardanoDBSync reference](reference/cardanodbsync.md).

## Network never reaches Ready

**Symptom.** `yacd info` shows `Ready: False` and stays there.

**Cause.** Look at which child condition is `False`. The common cases:

- `NodeReady: False` — the primary node container is not running yet. Check the
  node Pod with `kubectl describe pod` for image pull or scheduling errors (see
  [image pull issues](#image-pull-issues) and [storage rejected](#pvc-too-small-or-storage-rejected)).
- `NodeSynchronized: False` with `NodeProgressing: True` — the node is running
  but still catching up to the tip. This is expected on public profiles and
  resolves on its own once the node syncs. The `sync` status sub-resource
  reports `lagSlots` and `lagSeconds` so you can watch progress.
- `OgmiosReady: False` or `KupoReady: False` — a chain API sidecar is enabled
  but not yet connected or synchronized. Ogmios and Kupo are enabled by default.

**Fix.** Wait out a genuine sync, or fix the blocking child condition using the
matching section below. To watch live:

```sh
kubectl get cardanonetwork my-net -w
```

## Mainnet stuck without a Mithril bootstrap

!!! warning "Mainnet requires a Mithril bootstrap"
    Mainnet is too large to sync from genesis in a development context. The
    `CardanoNetwork` CRD validation rejects a mainnet network that has no
    Mithril bootstrap, so the API server refuses the resource before it ever
    reaches the controller.

**Symptom.** Applying a `public` network with `spec.public.profile: mainnet`
fails immediately with:

```
bootstrap.mithril is required only when public.profile is mainnet
```

**Cause.** The CRD enforces that `spec.public.bootstrap.mithril` is present
exactly when (and only when) the profile is mainnet. A mainnet network with no
bootstrap, or a preprod/preview network that sets a bootstrap, is invalid.

**Fix.** Add a Mithril bootstrap block. The image and snapshot have defaults, so
an empty block is enough to satisfy the rule:

```yaml
spec:
  mode: public
  public:
    profile: mainnet
    bootstrap:
      mithril: {}
```

`mithril.snapshot` defaults to `latest` and `mithril.image` defaults to the
pinned `mithril-client` image. See the
[CardanoNetwork reference](reference/cardanonetwork.md) for the bootstrap
fields. Mainnet also needs large persistent storage and the host-only opt-in to
create it; see the [recipes](recipes.md) for a complete mainnet manifest.

## PVC too small or storage rejected

**Symptom.** `NodeReady: False`, and the node Pod is stuck `Pending`. A
`kubectl describe pod` shows the PVC is not bound, or events report a
provisioning failure.

**Cause.** The node database PVC requested by `spec.node.storage.size` could not
be provisioned. Typical reasons: the requested size exceeds what the cluster can
provision, the named `spec.node.storage.storageClassName` does not exist, or the
default StorageClass cannot bind the claim. The node storage size defaults to
`10Gi`, which is fine for local devnets but far too small for a public profile.

**Fix.** Inspect the claim and its events:

```sh
kubectl get pvc -l app.kubernetes.io/instance=my-net
kubectl describe pvc <claim-name>
```

Then set a size and StorageClass the cluster can satisfy:

```yaml
spec:
  node:
    storage:
      size: 200Gi
      storageClassName: standard
```

A PVC's requested size is immutable after creation. If you need to grow it,
either use a StorageClass with `allowVolumeExpansion: true` or recreate the
network. See [`spec.node.storage`](reference/cardanonetwork.md) for the fields.

## External Postgres unreachable (db-sync)

**Symptom.** `CardanoDBSync` shows `PostgresReady: False` or `DBSyncReady:
False`, and `Ready` never becomes `True`. The db-sync container logs report
connection refused, authentication failure, or TLS errors against the Postgres
host.

**Cause.** With an external database, db-sync connects to the host you supplied
in `spec.database.external`. The connection fails when the host or port is
wrong, the password Secret is missing or holds the wrong key, the database or
user does not exist, or `sslMode` does not match the server's TLS posture.

**Fix.** Verify each field against the running Postgres:

- `spec.database.external.host` and `port` (default `5432`) resolve from inside
  the cluster.
- `spec.database.external.database` (default `cexplorer`) and `user` (default
  `postgres`) exist on the server.
- `spec.database.external.passwordSecretRef` names a same-namespace Secret, and
  the referenced `key` (default `password`) holds the password.
- `spec.database.external.sslMode` (default `disable`) matches the server. Use
  `require`, `verify-ca`, or `verify-full` only if the server presents TLS.

Confirm the Secret exists and has the expected key:

```sh
kubectl get secret <password-secret-name> -o jsonpath='{.data}' | jq 'keys'
```

Note that `spec.database` must set exactly one of `external` or `managed`;
setting both, or neither, is rejected at admission with
`exactly one of database.external or database.managed must be set`. See the
[CardanoDBSync reference](reference/cardanodbsync.md) for the database fields.

## Wallet funding fails

**Symptom.** `yacd wallet add --topup` or `yacd wallet topup` fails to build,
submit, or confirm a funding transaction.

**Cause and fix.**

- **No `faucet` wallet to fund from.** Only `local` networks get a
  genesis-funded `faucet` wallet. On a public network there is nothing to spend
  from by default — fund the target with `--from` a wallet you have funded
  yourself.
- **The source wallet has insufficient funds.** Funding spends real UTxOs. Check
  the source with `yacd wallet list <network>`, then top it up (or use `--from` a
  funded wallet) before funding others.
- **`--await` times out.** Confirmation polls Kupo. If the network is still
  syncing or `KupoReady` is `False`, the output may not appear in time; raise
  `--await-timeout` or retry once the network is Ready.

See the [funding guide](developer/funding.md) and the
[`yacd wallet` reference](reference/cli.md#wallet).

## devnet host port already in use

**Symptom.** `yacd devnet` fails to create its cluster with a host-port collision
error naming port `1337` or `1442`.

**Cause.** `yacd devnet` maps Ogmios to host port `1337` and Kupo to `1442` so
they are reachable on `localhost` without a port-forward. If another process
(another devnet, a local Ogmios/Kupo, or anything else) already holds one of
those ports, k3d cannot bind it.

**Fix.** Free the port — stop the other process, or `yacd devnet down` a previous
devnet — then re-run `yacd devnet`.

!!! note "An advertised `externalURL` that is not reachable"
    `run` and the wallet commands probe a network's `externalURL` before using
    it and fall back to a port-forward when the probe fails, so an unreachable
    `externalURL` only adds a brief delay, never an error. To force a specific
    endpoint, pass `--ogmios-url` / `--kupo-url` (funding) or set
    `YACD_OGMIOS_URL` / `YACD_KUPO_URL`.

## Image pull issues

**Symptom.** A Pod is stuck `ImagePullBackOff` or `ErrImagePull`, and the owning
condition (`NodeReady`, `OgmiosReady`, `KupoReady`, or
`PostgresReady`/`DBSyncReady`) stays `False`.

**Cause.** Kubernetes could not pull a container image. The reference is wrong,
the tag or digest does not exist, or the registry needs credentials the cluster
does not have.

**Fix.** Identify the failing image from the Pod events:

```sh
kubectl describe pod <pod-name>
```

Then check the override on the spec. Each component has a pinned default image,
so a pull failure usually means a custom override is wrong:

- `spec.node.image` / `spec.node.version` (the controller derives the node image
  from `version`, default `11.0.1`, when `image` is unset).
- `spec.chainAPI.ogmios.image` and `spec.chainAPI.kupo.image` (pinned defaults).
- `spec.public.bootstrap.mithril.image` for the Mithril client.
- For db-sync: `spec.image` and `spec.database.managed.image` (default
  `postgres:17.2-alpine`).

If the image is private, add an `imagePullSecret` to the namespace. See the
[CardanoNetwork reference](reference/cardanonetwork.md) and
[CardanoDBSync reference](reference/cardanodbsync.md) for the exact field paths
and pinned default tags.
