# YACD Snapshot And Restore Design

Status: draft proposal for session 029.

This document captures the current design direction for snapshot and restore
support in YACD. It is intentionally a working proposal, not a final API
contract. The first goal is to prove a narrow restore path with existing
Cardano tooling, then refine the manifest, CLI, and CRD surface from what
works.

## 1. Introduction

YACD needs snapshot and restore support for two related but different use
cases:

- YACD-native checkpoints for local and team test environments.
- Public-network bootstrap from already-hosted artifacts such as Mithril
  Cardano DB snapshots and upstream cardano-db-sync state snapshots.

The common product model should be simple: a user points YACD at a trusted
snapshot source, selects which components to restore, and lets the operator
materialize those components before normal reconciliation starts.

The implementation model should not pretend all snapshots are structurally the
same. A node database snapshot, a db-sync snapshot, and a YACD environment
bundle have different existing tools and safety requirements. The common
contract should therefore be a small YACD snapshot manifest that describes
component artifacts, not one mandatory archive layout for every source.

Terminology:

- Node snapshot means a restorable cardano-node database, not only the
  `db/ledger` subdirectory.
- db-sync snapshot means the upstream cardano-db-sync state snapshot bundle:
  PostgreSQL dump plus db-sync ledger state, and LSM data when applicable.
- YACD-native snapshot means a snapshot produced by the `yacd` CLI from an
  existing YACD-managed environment.
- Public snapshot means an existing externally hosted artifact, such as a
  Mithril Cardano DB snapshot or an IOHK-hosted cardano-db-sync `.tgz`.

### Design Principles

- Keep the CRD restore surface small and stable.
- Use existing Cardano restore tools wherever possible.
- Avoid forcing users to repackage large public artifacts.
- Let YACD-created local snapshots be easy to create, upload, and restore.
- Treat snapshot metadata as accepted identity: changing source, checksum,
  network identity, schema, tool version, or component set after restore should
  be rejected unless an explicit reset path exists.
- Start with an imperative CLI workflow before adding broader declarative
  snapshot management.

## 2. Manifest Specification

The snapshot manifest is the common restore contract between the CLI, external
snapshot publishers, and the operator.

The CRD should reference the manifest and selected components, rather than
copying every artifact detail into Kubernetes spec fields:

```yaml
spec:
  restore:
    source:
      url: https://snapshots.example.com/yacd/mainnet-13461393.json
      sha256: sha256:...
    components:
      node: true
      dbsync: true
```

The manifest should be JSON to keep it easy to generate, validate, sign, and
consume from init containers.

Initial sketch:

```json
{
  "apiVersion": "yacd.meigma.io/snapshot/v1alpha1",
  "kind": "SnapshotManifest",
  "metadata": {
    "name": "mainnet-13461393",
    "createdAt": "2026-05-28T14:30:00Z",
    "createdBy": "yacd/0.0.0"
  },
  "network": {
    "mode": "public",
    "profile": "mainnet",
    "networkMagic": 764824073
  },
  "tip": {
    "block": 13461393,
    "slot": 157000000,
    "hash": "..."
  },
  "components": {
    "node": {
      "format": "mithril-cardano-db-v1",
      "tool": {
        "name": "mithril-client",
        "version": "main-2478748"
      },
      "source": {
        "mode": "mithril",
        "aggregator": "https://aggregator.release-mainnet.api.mithril.network/aggregator",
        "snapshot": "latest"
      },
      "artifact": {
        "url": "mithril://release-mainnet/latest",
        "digest": "...",
        "sizeBytes": 0
      }
    },
    "dbsync": {
      "format": "cardano-db-sync-state-v13",
      "tool": {
        "name": "postgresql-setup.sh",
        "version": "13.7.1.0"
      },
      "compatibility": {
        "dbSyncImage": "ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0",
        "schema": "13.7",
        "ledgerBackend": "lsm",
        "architecture": "x86_64"
      },
      "artifact": {
        "url": "https://update-cardano-mainnet.iohk.io/cardano-db-sync/13.7/db-sync-snapshot-schema-13.7-block-13461393-x86_64.tgz",
        "sha256": "sha256:...",
        "sizeBytes": 78806251520
      }
    }
  }
}
```

This sketch deliberately separates:

- The manifest identity: what YACD is being asked to restore.
- Component identity: which node/db-sync artifacts are part of the snapshot.
- Artifact location: where bytes come from.
- Restore tooling: which existing tool should unpack or restore the bytes.
- Compatibility metadata: what must match the target CR's immutable runtime
  state.

### Packaging Modes

The same manifest can support two packaging modes.

YACD bundle mode:

```text
yacd-snapshot.tar.zst
├── manifest.json
├── node/
│   └── node-db.tar.zst
├── network/
│   ├── config.json
│   ├── topology.json
│   └── genesis files
└── dbsync/
    └── db-sync-snapshot.tgz
```

External-artifacts mode:

```text
manifest.json
  -> node source uses Mithril or another existing public source
  -> dbsync source points at upstream cardano-db-sync .tgz + sha256sum
```

The operator should consume either shape through the same manifest parser. It
should not require public artifacts to be repackaged into a YACD bundle.

### Manifest Validation

Minimum operator validation:

- Manifest URL checksum matches the CRD source checksum.
- `apiVersion` and `kind` are recognized.
- Requested components exist in the manifest.
- Network identity matches the target `CardanoNetwork`.
- Component artifact checksums are present for ordinary URLs.
- Component formats are recognized.
- Compatibility metadata matches the accepted runtime identity for the target
  resource.
- Restore is only attempted onto fresh or explicitly reset persistent state.

The first version can be strict. Unknown component formats, unknown component
names, missing checksums, and mismatched network identity should fail closed.

## 3. YACD-Native Snapshotting

YACD-native snapshotting should be CLI-first. The operator owns long-lived
state, but creating a checkpoint is an imperative workflow: quiesce, inspect,
export, package, upload, then resume.

### CLI Responsibilities

Initial CLI commands could be shaped around two workflows:

```sh
yacd snapshot create NETWORK --include node --include dbsync --output ./snapshot.tar.zst
yacd snapshot inspect ./snapshot.tar.zst
yacd snapshot manifest ./snapshot.tar.zst --output manifest.json
```

The first implementation does not need to be general-purpose backup software.
It should target YACD-owned local/dev environments and prove the restore path.

For node snapshots, the CLI should capture the full cardano-node database PVC
content plus the generated network material needed to run it correctly. It
should not snapshot only the `db/ledger` directory.

For db-sync snapshots, the CLI should call the upstream db-sync tooling:

- stop or suspend db-sync
- run `cardano-db-tool prepare-snapshot --state-dir <state-dir>`
- run the generated `postgresql-setup.sh --create-snapshot ...`
- record the generated `.tgz` and `.sha256sum` in the manifest

For local YACD environments, the CLI may create a self-contained bundle by
default because the artifacts are naturally produced together and are small
enough to move as one unit in most test workflows.

### Operator Responsibilities

The operator should restore YACD-native snapshots through init containers or
job-like bootstrap steps before the normal workloads start.

For `CardanoNetwork`:

- ensure target node PVC is fresh or explicitly reset
- download and verify the manifest
- download and verify the node artifact
- unpack the node database into the expected state directory
- restore or regenerate network artifact ConfigMaps and required metadata
- persist accepted restore identity on the PVC/status

For `CardanoDBSync`:

- support restore first for managed Postgres only
- ensure Postgres and db-sync state PVCs are fresh or explicitly reset
- download and verify the db-sync artifact
- run upstream `postgresql-setup.sh --restore-snapshot <snapshot.tgz> <state-dir>`
- persist accepted restore identity on the managed database PVC/status
- start db-sync normally after restore

The first implementation should reject restore into external Postgres. YACD
does not own database emptiness, destructive restore safety, permissions, or
rollback for an external database.

### Consuming YACD-Native Snapshots

The developer-facing config and CRD should eventually let a user restore a
YACD-native bundle without manually extracting it:

```yaml
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
metadata:
  name: demo
spec:
  restore:
    source:
      url: https://snapshots.example.com/demo.tar.zst
      sha256: sha256:...
    components:
      node: true
      dbsync: true
```

The CLI can compile this into the relevant `CardanoNetwork` and
`CardanoDBSync` restore specs. The exact CRD split remains open: restore could
live on each component CR, or the CLI could project one developer-facing
restore stanza into component-specific restore fields.

## 4. Public Snapshot Consumption

Public snapshot consumption should preserve existing public artifact flows
instead of forcing repackaging.

### Node Public Snapshots

For public mainnet node bootstrap, Mithril is the preferred first path because
it already provides discovery, verification, and download tooling for Cardano
DB snapshots.

The manifest can represent a Mithril source without embedding every resolved
download URL:

```json
{
  "format": "mithril-cardano-db-v1",
  "source": {
    "mode": "mithril",
    "aggregator": "https://aggregator.release-mainnet.api.mithril.network/aggregator",
    "snapshot": "latest"
  }
}
```

The operator can continue using `mithril-client cardano-db download
--include-ancillary` in the bootstrap init container. If `latest` is allowed,
the restore identity must record the resolved digest or immutable snapshot ID
after download. Otherwise a reconciler could restore different content for the
same CR spec over time.

For non-mainnet public profiles, a node snapshot source is optional. Preview
and preprod can still sync from genesis unless a snapshot source is supplied and
validated.

### db-sync Public Snapshots

Public db-sync snapshots should use the upstream cardano-db-sync snapshot
format directly. Current official mainnet snapshots are hosted as `.tgz` files
with `.sha256sum` sidecars under the public `cardano-db-sync/` bucket prefix.

The manifest should record:

- artifact URL and sha256
- db-sync schema version
- db-sync image/version
- ledger backend
- architecture
- snapshot block/slot/hash when known
- expected restore command/tool

The operator should restore these only for managed Postgres in the first
implementation.

### CLI Responsibilities

The CLI should make public consumption ergonomic without hiding the trust
boundary.

Useful commands:

```sh
yacd snapshot discover dbsync --network mainnet --schema 13.7
yacd snapshot discover mithril --network mainnet
yacd snapshot manifest public \
  --node-mithril latest \
  --dbsync-url https://update-cardano-mainnet.iohk.io/cardano-db-sync/13.7/...tgz \
  --dbsync-sha256 sha256:... \
  --output mainnet-restore.json
```

Discovery should produce candidate manifests or manifest fragments, not apply
anything by itself. Applying restore should remain explicit.

### Operator Responsibilities

The operator should not scrape public indexes during normal reconciliation
unless the CRD explicitly asks for a discoverable source such as Mithril
`latest`. Prefer immutable URLs and checksums for ordinary HTTP artifacts.

When the operator does resolve a moving alias such as `latest`, it should write
the resolved immutable identity into status or accepted-state annotations and
refuse silent drift.

## 5. Open Design Questions

These are the main questions surfaced by this draft.

### Answered

1. Restore is modeled on each component CR as the authoritative operator
   contract. `CardanoNetwork.spec.restore` owns primary node restore behavior,
   while `CardanoDBSync.spec.restore` owns db-sync and managed Postgres restore
   behavior. The CLI and developer config may expose a single ergonomic restore
   stanza and compile it into component-specific restore specs.
2. Moving aliases such as `latest` are accepted only at the CLI and
   developer-config layer. Before apply, the CLI resolves them to immutable
   snapshot IDs, artifact URLs, and checksums. Component CR restore specs must
   be immutable and reproducible; the operator should reject moving aliases in
   `spec.restore`.
3. YACD-native node snapshots use a logical archive: the full cardano-node
   database directory plus generated network artifacts and YACD metadata. The
   format must preserve exact node DB contents, but it should not default to a
   full PVC-level archive that captures incidental filesystem or storage-driver
   details. Full PVC archive can remain an implementation detail or later
   backup mode.
4. YACD-native snapshots include only secrets required for chain/database
   continuity by default, such as localnet block-production material and
   managed Postgres credentials tied to restored state. Operational tokens and
   service access secrets are regenerated unless an explicit full-clone option
   is added later. Snapshot encryption is out of scope for the first pass and
   should be treated as a future hardening layer.
5. Restored localnets preserve pool, KES, VRF, opcert, and other
   block-production material exactly. First-pass restore is checkpoint
   restoration, not key rotation or chain reconstitution. Controlled key
   regeneration is deferred to a later, explicit migration feature.

### Pending

6. What accepted-state keys should be stamped for node restore and db-sync
   restore?
7. Should restore require fresh PVCs only, or should there be an explicit
   destructive reset flag/path?
8. Should YACD support external Postgres restore at all, or keep restore
   permanently managed-Postgres-only?
9. How should the manifest itself be authenticated beyond checksums: detached
   signatures, GitHub attestations, or a later provenance layer?
10. Should YACD-native bundles be tar.zst by default, or should the archive
    format be pluggable from the beginning?
11. How much public snapshot discovery belongs in the CLI versus documentation?
12. Should snapshot creation quiesce workloads by patching CR specs, scaling
    owned Deployments, or using a dedicated short-lived snapshot Job?
13. What is the first manual proof target: local node-only restore,
    local node-plus-db-sync restore, or public mainnet node-plus-db-sync
    restore?

## References

- Mithril Cardano DB bootstrap:
  https://mithril.network/doc/manual/getting-started/bootstrap-cardano-node/
- Mithril client:
  https://mithril.network/doc/manual/develop/nodes/mithril-client/
- cardano-db-sync state snapshots:
  https://github.com/IntersectMBO/cardano-db-sync/blob/13.7.1.0/doc/state-snapshot.md
- cardano-db-sync snapshot restore script:
  https://github.com/IntersectMBO/cardano-db-sync/blob/13.7.1.0/scripts/postgresql-setup.sh
- Official mainnet db-sync snapshots:
  https://update-cardano-mainnet.iohk.io/cardano-db-sync/index.html
