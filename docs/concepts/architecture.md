# Architecture

YACD is a Kubernetes-native development environment manager for Cardano. It is
built for people developing applications on Cardano, not for validators, stake
pool operators, or production network operators. This page explains how the
system is put together and why it is shaped the way it is. For step-by-step
instructions see the [developer](../developer/getting-started.md) and
[operator](../operator/installation.md) guides; for exhaustive field and flag
facts see the [reference](../reference/cardanonetwork.md) section.

## Two artifacts: operator and CLI

YACD ships as two cooperating pieces: a Kubernetes operator and a companion
`yacd` CLI. The split is deliberate, and it follows the grain of Kubernetes
itself.

The operator owns **declarative cluster state**. It reconciles long-lived
desired state: node workloads, generated or fetched configuration, genesis
material, supporting services, persistent volumes, Secrets, ConfigMaps,
Services, and the status that reports whether all of that is healthy. If you can
sensibly express it as "this environment should exist, with these services
enabled," it belongs to the operator. The operator watches its custom resources
across namespaces and continuously drives the cluster toward the spec.

The CLI owns **imperative developer workflow**. It compiles a single
developer-facing config file into the Kubernetes resources the operator
consumes, applies them, waits for readiness, prints connection details,
forwards endpoints to your host, and runs one-off actions like topping up a
wallet. These are ad hoc commands, not desired state.

The reason for the boundary is that Kubernetes is an excellent model for desired
state and a poor model for one-off actions. "This network should run with
db-sync enabled" is a natural fit for a controller that reconciles toward it
forever. "Top this address up right now" is not: encoding a single funding
request as a resource mutation would be awkward and would leave stale objects
behind. So durable state lives in CRDs and the operator, and transient actions
live in the CLI. The CLI is a Kubernetes client; it holds no privileged state of
its own and talks to the same API server any other client would.

## CardanoNetwork: the primary resource

The primary custom resource is `CardanoNetwork` (API group `yacd.meigma.io`,
version `v1alpha1`). It is intentionally environment-shaped rather than
node-shaped: it describes a Cardano *network* and the chain-access services
exposed beside it, even though the first runtime reconciles a single primary
node.

A `CardanoNetwork` owns the core Cardano substrate. Its spec carries the network
`mode` (`local` or `public`), the primary `node` workload (shared by both
modes), the mode-specific `local` or `public` block, and a `chainAPI` block for
the services exposed next to the node. The controller reconciles the node into a
workload backed by a PVC for the node database, publishes resolved network
identity and cluster-local Service endpoints into status, and tracks health
through a set of `metav1.Condition` entries (`Ready`, `NodeReady`,
`NodeSynchronized`, `OgmiosReady`, `KupoReady`, `FaucetReady`, `WalletReady`,
`ArtifactsReady`, and others). Status reports only what the controller can
observe in-cluster, so consumers do not trust stale or hand-edited values.

Two chain-access services are enabled by default, because a raw `cardano-node`
is not enough to build against. [Ogmios](https://ogmios.dev) is the default
chain-access API: it gives YACD and developers a JSON/RPC interface for query,
submit, and evaluate without every client having to share the node's Unix
socket. [Kupo](https://cardanosolutions.github.io/kupo/) is the default chain
index. A local-only faucet is also available but is **opt-in**, because it
exposes a spending endpoint; it must be explicitly enabled. For the exact
defaults, ports, and images, see the
[CardanoNetwork reference](../reference/cardanonetwork.md).

## Supporting-service CRDs: CardanoDBSync

Heavier services are modeled as **separate CRDs with separate controllers**,
rather than as ever-growing fields on `CardanoNetwork`. The first such service
is `CardanoDBSync`, which runs
[cardano-db-sync](https://github.com/IntersectMBO/cardano-db-sync) and its
Postgres database.

A supporting CRD references a same-namespace `CardanoNetwork` through its
`networkRef` and derives chain information from that network rather than
re-declaring it. This keeps each controller focused: the db-sync controller
reasons about db-sync, Postgres, and ledger state, while the network controller
reasons about the Cardano substrate. It also keeps ownership clean. Enabling
db-sync does not have to disturb the primary node, and the db-sync controller
owns its own workload, storage, database wiring, config, and status.

The default placement is a **dedicated follower node**. Rather than forcing the
primary node to share its Unix socket, the db-sync controller runs its own
follower `cardano-node` colocated with db-sync in one workload. The follower
joins the primary network over ordinary node-to-node TCP and exposes a local
socket that db-sync consumes inside the same Pod. This costs extra CPU, memory,
storage, and startup time, but it preserves controller isolation: the primary
node never restarts because you added an indexer.

A `primarySidecar` placement mode is the explicit exception. When the cost of a
duplicate node outweighs the benefit of isolation, db-sync can be composed
directly into the primary node Pod and consume the primary socket. That trades
isolation for a single node copy and rolls the primary workload when the
attachment changes; it is supported for local and non-mainnet public profiles,
but not for public mainnet. For the field-level details of both modes, see the
[CardanoDBSync reference](../reference/cardanodbsync.md) and the
[db-sync operator guide](../operator/db-sync.md).

## Why a Unix socket forces this shape

Many Cardano tools want direct access to a node's Unix socket, and that single
fact drives much of the architecture. A Unix socket is a local-filesystem IPC
object. It is not a cluster-wide endpoint and cannot be exposed through a
Kubernetes Service. The robust pattern is to share it *within one Pod* using an
ephemeral volume: the node data directory lives on a PVC, while the socket
directory is ephemeral and mounted by sidecars in the same Pod.

This is why Ogmios runs as a sidecar that mounts the node socket and re-exposes
chain access as a network API, and why socket-hungry services like db-sync
default to a colocated follower node instead of reaching across Pods. YACD
deliberately avoids RWX PVCs and hostPath socket sharing, which are fragile,
scheduler-sensitive, and a poor fit for shared clusters. The same constraint
shows up at the CLI boundary: tools that speak a network protocol can be
port-forwarded to your host, but a tool that needs the node socket directly
(notably `cardano-cli`) must run *inside* the node Pod.

## Local vs public networks

The two network modes differ mostly in where chain material comes from.

A **local** network is generated and owned by YACD. The controller produces
fresh genesis and node configuration in-cluster from the spec: network magic,
ledger era, slot and epoch timing, generated stake-pool topology, and a curated
genesis profile (for example a zero-fee preset for fast tests). The generated
artifacts are staged and served over HTTP from the node's PVC by a small
cardano-tools serve endpoint, alongside a `manifest.json`, so the node and any
follower nodes consume the same configuration from one source of truth. The
`ArtifactsReady` condition reports when that bundle is staged and being served.

A **public** network joins a known profile: `preprod`, `preview`, or `mainnet`.
Instead of generating genesis, the controller fetches the published
configuration for the profile. For most profiles the node can sync from genesis,
but mainnet is far too large to sync that way in a development setting. For
mainnet, the spec requires a [Mithril](https://mithril.network) bootstrap: an
init container uses a Mithril client to download and verify a Cardano database
snapshot, seeding the node's PVC so the node starts from a recent point instead
of from genesis. The same HTTP artifact-serving pattern applies, so resolved
configuration is reachable in-cluster.

!!! warning "Mainnet is gated"
    A public mainnet `CardanoNetwork` requires a Mithril bootstrap, large
    persistent storage, and an explicit opt-in (`--allow-mainnet` on the CLI).
    It is not the default development path. See the
    [CardanoNetwork reference](../reference/cardanonetwork.md) for the
    bootstrap and storage fields.

## Host access: bridging the cluster to your host

The services a `CardanoNetwork` exposes are **cluster-internal** Service URLs.
That is correct for in-cluster consumers and for supporting controllers, but a
developer's tests and tools usually run on the host. The CLI's host-access verbs
bridge that gap.

`run`, `connect`, and `exec` are the three ways across the boundary. `run` and
`connect` both establish supervised port-forwards from the cluster's chain-API
Services to loopback on your host, but they surface those loopback URLs
differently. `run` wraps a single command (the primary CI path), exposing the
loopback URLs through a small `YACD_*` environment contract so tests read
ordinary environment variables instead of parsing a YACD file. `connect` holds
the forwards open in one terminal while you work in another, writing the
loopback URLs to an `endpoints.json` file instead of wiring an environment.

`exec` is different, and it exists because of the socket constraint above. Tools
that speak a network protocol can be forwarded, but `cardano-cli` reaches the
node over its local Unix socket, which cannot be forwarded as a TCP port. So
`exec` runs the command *inside* the primary node Pod, where
`CARDANO_NODE_SOCKET_PATH` and the `YACD_*` variables are already set. The rule
is simple: forward network APIs to the host with `run`/`connect`; run
socket-bound tools in-pod with `exec`. For the verb behaviors, the `YACD_*`
table, and the `endpoints.json` schema, see the
[CLI reference](../reference/cli.md) and the
[connecting tools guide](../developer/connecting-tools.md).

!!! warning "The faucet is local-only and host-gated"
    The faucet funds addresses on local development networks only. The CLI
    forwards it to loopback and exempts the loopback URL from its trust gate;
    targeting a custom non-loopback faucet URL from the host requires explicit
    trust flags so the bearer token is never sent to an unexpected host. See the
    [funding guide](../developer/funding.md) and the
    [security model](security.md).

## Developer and operator workflows

The same system serves two overlapping audiences.

A **developer** typically authors one developer-facing config file, checks it
into a repository, and drives it with the CLI: render and apply the network,
wait for readiness, fund a wallet, and point tests at the forwarded endpoints.
They rarely hand-author CRDs; the CLI compiles their single config into the
decomposed Kubernetes resources the operator expects, giving them one pane of
glass while preserving clean resource boundaries in the cluster.

An **operator** installs and runs the YACD operator in a cluster (locally on
[k3d](https://k3d.io) or in a shared cluster), manages the manager Deployment
and its RBAC through the Helm chart, and reasons directly about CRDs, status
conditions, placement modes, and capacity for heavier services like db-sync.

The two workflows overlap because they act on the same resources through the
same API server. A developer's `yacd up` produces the very `CardanoNetwork` an
operator can inspect with `kubectl`; an operator's installed controller is what
makes a developer's config become a running network. The CLI is a convenience
layer over the cluster API, not a separate control plane. For the operator's
trust boundaries and the host-access trust gates, see the
[security model](security.md).
