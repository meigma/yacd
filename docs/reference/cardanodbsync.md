# CardanoDBSync

`CardanoDBSync` indexes a [`CardanoNetwork`](cardanonetwork.md) into a Postgres
database using [cardano-db-sync](https://github.com/IntersectMBO/cardano-db-sync).
This page is the dry, complete field reference for the custom resource.

| Property | Value |
| --- | --- |
| API group / version | `yacd.meigma.io/v1alpha1` |
| Kind | `CardanoDBSync` |
| List kind | `CardanoDBSyncList` |
| Plural | `cardanodbsyncs` |
| Singular | `cardanodbsync` |
| Scope | Namespaced |
| Status subresource | enabled |

For copy-paste manifests, see [Recipes](../recipes.md). The objects that
`CardanoDBSync` references (Secrets, the `CardanoNetwork`) must live in the same
namespace.

## Print columns

`kubectl get cardanodbsyncs` shows:

| Column | Source |
| --- | --- |
| `Network` | `.spec.networkRef.name` |
| `Ready` | `.status.conditions[?(@.type=="Ready")].status` |
| `Synced` | `.status.conditions[?(@.type=="Synced")].status` |
| `Age` | `.metadata.creationTimestamp` |

## Spec

`spec` is required.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `networkRef` | object | yes | — | Same-namespace `CardanoNetwork` that db-sync indexes. See [networkRef](#networkref). |
| `image` | string | yes | `ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0` | cardano-db-sync image reference. |
| `resources` | object | no | — | db-sync container resources (core `ResourceRequirements`). |
| `placement` | object | no | — | Where db-sync runs relative to the network. When omitted, the controller preserves the dedicated follower workload. See [placement](#placement). |
| `followerNode` | object | no | — | Dedicated follower node colocated with db-sync for local node socket access. See [followerNode](#followernode). |
| `database` | object | yes | — | Postgres database used by db-sync. Exactly one mode must be set. See [database](#database). |
| `stateStorage` | object | no | — | Persistent storage for db-sync ledger state. See [storage](#storage-spec). |
| `config` | object | no | — | Upstream db-sync behavior in Kubernetes-style field names. See [config](#config). |

### Spec-level validation

| Rule | Message |
| --- | --- |
| `!has(self.placement) \|\| self.placement.mode != 'primarySidecar' \|\| !has(self.followerNode)` | `followerNode cannot be set when placement.mode is primarySidecar` |

### networkRef

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Name of the referenced `CardanoNetwork`. Minimum length 1. |

### placement

| Field | Type | Required | Default | Enum | Description |
| --- | --- | --- | --- | --- | --- |
| `mode` | string | yes | `dedicatedFollower` | `dedicatedFollower`, `primarySidecar` | Whether db-sync uses a dedicated follower node or asks the referenced network's primary Pod to host it as a sidecar. |

`dedicatedFollower` keeps the two-container workload with a colocated follower
node owned by the `CardanoDBSync` controller. `primarySidecar` requests db-sync
placement inside the referenced `CardanoNetwork` primary node Pod; `followerNode`
must not be set in this mode.

### followerNode

Configures the follower node owned by db-sync. Only meaningful in
`dedicatedFollower` placement.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `image` | string | no | derived from the referenced `CardanoNetwork` | Overrides the follower cardano-node image. |
| `storage` | object | no | — | Persistent follower node database storage. See [storage](#storage-spec). |
| `resources` | object | no | — | Follower node container resources (core `ResourceRequirements`). |

### database

Exactly one of `external` or `managed` must be set.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `external` | object | no | — | References a Postgres instance managed outside this resource. See [database.external](#databaseexternal). |
| `managed` | object | no | — | YACD-managed Postgres for local development. See [database.managed](#databasemanaged). |

| Rule | Message |
| --- | --- |
| `has(self.external) != has(self.managed)` | `exactly one of database.external or database.managed must be set` |

#### database.external

| Field | Type | Required | Default | Enum | Description |
| --- | --- | --- | --- | --- | --- |
| `host` | string | yes | — | — | DNS name or IP address of the Postgres server. Minimum length 1. |
| `port` | int32 | yes | `5432` | — | Postgres server port. Range 1–65535. |
| `database` | string | yes | `cexplorer` | — | Postgres database name. |
| `user` | string | yes | `postgres` | — | Postgres user name. |
| `passwordSecretRef` | object | yes | — | — | Same-namespace Secret holding the Postgres password. See [secret key reference](#secret-key-reference). |
| `sslMode` | string | yes | `disable` | `disable`, `require`, `verify-ca`, `verify-full` | libpq `sslmode` used for Postgres connections. |

#### database.managed

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `image` | string | yes | `postgres:17.2-alpine` | Postgres image reference. |
| `database` | string | yes | `cexplorer` | Postgres database name. |
| `user` | string | yes | `postgres` | Postgres user name. |
| `authSecretRef` | object | no | controller creates an owned Secret | Same-namespace Secret with the Postgres password in key `password`. When omitted, the controller creates a Secret and reports its name in `status.database.authSecretName`. See [secret reference](#secret-reference). |
| `storage` | object | no | — | Persistent Postgres data storage. See [storage](#storage-spec). |
| `parameters` | object | no | — | Basic Postgres startup parameters. See [database.managed.parameters](#databasemanagedparameters). |
| `resources` | object | no | — | Postgres container resources (core `ResourceRequirements`). |

##### database.managed.parameters

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `maintenanceWorkMem` | quantity | no | — | Sets Postgres `maintenance_work_mem`. |
| `maxParallelMaintenanceWorkers` | int32 | no | — | Sets Postgres `max_parallel_maintenance_workers`. Minimum 0. |

### storage (spec)

The same `storage` shape is used by `spec.stateStorage`,
`spec.followerNode.storage`, and `spec.database.managed.storage`.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `size` | quantity | no | — | Requested persistent volume size. |
| `storageClassName` | string | no | — | Kubernetes StorageClass used for the PVC. |

### Secret references

#### secret reference

Used by `database.managed.authSecretRef`.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Name of the referenced Secret. Minimum length 1. |

#### secret key reference

Used by `database.external.passwordSecretRef`.

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Name of the referenced Secret. Minimum length 1. |
| `key` | string | yes | `password` | Secret data key containing the value. Minimum length 1. |

## config

Configures upstream db-sync behavior in Kubernetes-style field names. The
controller translates this object into the upstream db-sync configuration file.

| Field | Type | Required | Default | Enum | Description |
| --- | --- | --- | --- | --- | --- |
| `runtime` | object | no | — | — | db-sync runtime flags outside `insert_options`. See [config.runtime](#configruntime). |
| `ledgerBackend` | string | yes | `lsm` | `inmemory`, `lsm` | How db-sync stores its ledger UTxO set. `inmemory` keeps it in memory; `lsm` stores it on disk using LSM trees. |
| `snapshot` | object | no | — | — | Ledger state snapshot behavior. See [config.snapshot](#configsnapshot). |
| `insert` | object | no | — | — | Upstream `insert_options`. See [config.insert](#configinsert). |
| `ipfsGateways` | array of string | no | — | — | Gateways used for offchain metadata fetching. |

When `config` is provided, `ledgerBackend` is required within it.

### config.runtime

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `cache` | bool | yes | `true` | Whether db-sync caches are enabled. |
| `epochTable` | bool | yes | `true` | Whether db-sync populates the epoch table. |
| `forceIndexes` | bool | yes | `false` | Whether db-sync creates indexes at startup rather than later in the sync lifecycle. |
| `metricsPort` | int32 | yes | `8080` | db-sync Prometheus metrics port. Range 1–65535. |

### config.snapshot

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `nearTipEpoch` | int64 | yes | `580` | Epoch threshold where db-sync considers itself near tip for snapshot frequency. Minimum 0. |

### config.insert

Maps to upstream db-sync `insert_options`. `preset` selects a profile; the
explicit fields below are interpreted by the controller as overrides on top of
the preset.

| Field | Type | Required | Default | Enum | Description |
| --- | --- | --- | --- | --- | --- |
| `preset` | string | yes | `full` | `full`, `only_utxo`, `only_governance`, `disable_all` | Upstream insert profile. `full` enables the normal full schema surface; `only_utxo` keeps only UTxO-oriented data; `only_governance` keeps governance-oriented data; `disable_all` keeps only the minimum block and tx data. |
| `txCbor` | bool | no | — | — | Transaction CBOR collection. |
| `txOut` | object | no | — | — | Transaction output storage. See [config.insert.txOut](#configinserttxout). |
| `ledger` | string | no | — | `enable`, `disable`, `ignore` | Ledger state maintenance and use. `enable` maintains and uses ledger-derived data; `disable` avoids maintaining ledger state; `ignore` maintains state but does not use its data. |
| `shelley` | object | no | — | — | Shelley-era table inserts. See [config.insert.shelley](#configinsertshelley). |
| `multiAsset` | object | no | — | — | Multi-asset table inserts. See [config.insert.multiAsset](#configinsertmultiasset). |
| `metadata` | object | no | — | — | Transaction metadata inserts. See [config.insert.metadata](#configinsertmetadata). |
| `plutus` | object | no | — | — | Plutus and script table inserts. See [config.insert.plutus](#configinsertplutus). |
| `governance` | bool | no | — | — | Governance-related data inserts. |
| `offchainPoolData` | bool | no | — | — | Stake pool offchain metadata fetching. |
| `offchainVoteData` | bool | no | — | — | Governance offchain metadata fetching. |
| `poolStats` | bool | no | — | — | Pool stats inserts. |
| `jsonType` | string | no | — | `text`, `jsonb`, `disable` | Upstream `json_type` option. `text` stores JSON as text; `jsonb` stores it as jsonb; `disable` disables JSON storage where supported. |
| `removeJsonbFromSchema` | bool | no | — | — | Whether db-sync removes jsonb data types from affected schema columns. |

#### config.insert.txOut

| Field | Type | Required | Default | Enum | Description |
| --- | --- | --- | --- | --- | --- |
| `mode` | string | no | — | `enable`, `disable`, `consumed`, `prune`, `bootstrap` | Upstream `tx_out` value. `enable` stores all inputs and outputs; `disable` disables the tx input/output tables; `consumed` stores `consumed_by_tx_id` for direct UTxO queries; `prune` periodically prunes consumed outputs; `bootstrap` delays UTxO insertion until db-sync reaches the tip. |
| `forceTxIn` | bool | no | — | — | Keeps `tx_in` populated for `consumed`, `prune`, or `bootstrap` modes. |
| `useAddressTable` | bool | no | — | — | Enables the normalized address table schema variant. |

#### config.insert.shelley

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | no | — | Shelley-era data inserts. |
| `stakeAddresses` | array of string | no | — | Limits Shelley data to specific stake addresses. |

#### config.insert.multiAsset

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | no | — | Multi-asset data inserts. |
| `policies` | array of string | no | — | Limits multi-asset data to specific policy hashes. |

#### config.insert.metadata

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | no | — | Transaction metadata inserts. |
| `keys` | array of int64 | no | — | Limits metadata inserts to specific numeric labels. |

#### config.insert.plutus

| Field | Type | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `enabled` | bool | no | — | Plutus and script data inserts. |
| `scriptHashes` | array of string | no | — | Limits Plutus data to specific script hashes. |

## Status

All status fields are optional and populated by the controller.

| Field | Type | Description |
| --- | --- | --- |
| `observedGeneration` | int64 | Most recent generation observed by the controller. |
| `endpoints` | object | Cluster-local connection details. See [status.endpoints](#statusendpoints). |
| `database` | object | Database-specific runtime details. See [status.database](#statusdatabase). |
| `sync` | object | db-sync indexing progress. See [status.sync](#statussync). |
| `placement` | object | Effective placement mode and primary-sidecar contract. See [status.placement](#statusplacement). |
| `conditions` | array | Resource conditions. See [Conditions](#conditions). |

### status.endpoints

| Field | Type | Description |
| --- | --- | --- |
| `postgres` | object | Postgres endpoint used by db-sync and clients. |
| `metrics` | object | db-sync Prometheus metrics endpoint. |

Each endpoint object (`ServiceEndpointStatus`) contains:

| Field | Type | Description |
| --- | --- | --- |
| `serviceName` | string | Kubernetes Service name. |
| `port` | int32 | Service port. |
| `url` | string | Convenience URL for protocols with a stable URL shape. |

### status.database

| Field | Type | Enum | Description |
| --- | --- | --- | --- |
| `acceptedIdentityFingerprint` | string | — | Database-affecting plan identity the controller accepted on owned runtime material. Mirrors the value from the db-sync state PVC annotation; not the authority for identity validation. |
| `acceptedPlacementMode` | string | `dedicatedFollower`, `primarySidecar` | Placement mode accepted for the current db-sync state. Changing it requires deleting and recreating the resource with a fresh or compatible database. |
| `authSecretName` | string | — | Same-namespace Secret with generated database credentials when the user did not provide `authSecretRef`. |

### status.sync

| Field | Type | Description |
| --- | --- | --- |
| `nodeBlockHeight` | int64 | Latest block height reported by the follower node. |
| `dbBlockHeight` | int64 | Latest block height inserted into Postgres. |
| `dbSlotHeight` | int64 | Latest slot inserted into Postgres. |
| `dbQueueLength` | int64 | Current db-sync database event queue length. |
| `lagBlocks` | int64 | Difference between `nodeBlockHeight` and `dbBlockHeight`. |
| `epoch` | int64 | Latest epoch observed by db-sync. |

### status.placement

| Field | Type | Enum | Description |
| --- | --- | --- | --- |
| `mode` | string | `dedicatedFollower`, `primarySidecar` | Effective placement mode for this reconcile. |
| `primarySidecar` | object | — | Attachable material contract published when `SidecarMaterialReady=True`. |

`primarySidecar` contains:

| Field | Type | Description |
| --- | --- | --- |
| `networkName` | string | Referenced `CardanoNetwork` name this sidecar material is valid for. |
| `revision` | string | Opaque sha256 rollout revision over all sidecar-mounted material. |
| `resources` | object | `CardanoDBSync`-owned resources mounted by the primary Pod sidecar. |

`primarySidecar.resources` contains:

| Field | Type | Description |
| --- | --- | --- |
| `configMapName` | string | db-sync configuration ConfigMap name. |
| `pgpassSecretName` | string | db-sync pgpass Secret name. |
| `statePVCName` | string | db-sync state PVC name. |
| `metricsServiceName` | string | db-sync metrics Service name. |

### Conditions

`status.conditions` is a list of standard `metav1.Condition` objects keyed by
`type`. Each entry has `type`, `status` (`True`, `False`, or `Unknown`),
`reason`, `message`, `lastTransitionTime`, and `observedGeneration`.

| Type | Meaning |
| --- | --- |
| `Ready` | db-sync is usable through its published database endpoint. |
| `FollowerNodeReady` | The colocated follower node is running. |
| `NodeSocketReady` | The node socket used by db-sync is reachable. |
| `SidecarMaterialReady` | Primary-sidecar mounted material is attachable. |
| `PostgresReady` | Postgres is running and accepting local connections. |
| `DBSyncReady` | The db-sync process is running. |
| `Synced` | db-sync has caught up to the node tip. |
| `Progressing` | The resource is being created or updated. |
| `Degraded` | The resource failed to reach or maintain desired state. |
