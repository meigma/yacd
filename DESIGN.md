# Yet Another Cardano Deployer Design

YACD is a Kubernetes-native development environment manager for Cardano. It is
aimed at people building on Cardano, not validators, stake pool operators, or
production network operators.

This document is intentionally high level. It captures the initial vision,
architecture direction, and early decisions that should guide the first
prototype. It does not define final CRD schemas, controller contracts, CLI
flags, service APIs, or long-term product behavior.

## Goals

YACD should make it easy to stand up useful Cardano development environments in
Kubernetes. The first target is a local Kind/Tilt workflow, but the same
operator should be usable in a hosted cluster shared by a team.

The environment should be more than a raw `cardano-node`. Developers should get
the core node plus practical services that make application prototyping
possible: query APIs, transaction submission paths, indexing services, funded
test wallets, topup flows, and eventually other Cardano developer tools.

The project should prefer Kubernetes-native reconciliation for long-lived
desired state while keeping imperative developer actions ergonomic through a
companion CLI.

## Non-Goals For The First Prototype

The first prototype is not a complete Cardano platform. It does not need to:

- support every Cardano network mode
- expose every useful Cardano service
- define a final CRD surface
- solve production validator or stake pool operation
- provide a general-purpose wallet platform
- replace existing Cardano developer tools
- support every possible socket-sharing topology

The goal is to prove a narrow, useful path and let the design mature from what
works.

## Product Shape

YACD should have two primary user-facing components.

The Kubernetes operator owns declarative cluster state: Cardano environment
workloads, generated or mounted configuration, genesis material, node services,
supporting services, status, readiness, and long-lived resources such as PVCs,
Secrets, ConfigMaps, Services, Deployments, and StatefulSets.

The companion CLI owns local developer workflow: compiling a single
developer-facing config into Kubernetes manifests, applying those manifests,
waiting for readiness, printing connection details, port-forwarding, topping up
wallets, inspecting known wallets, and other one-off actions that do not fit
comfortably as CRD state.

This split is important. Kubernetes is a good model for desired state such as
"this environment should exist with db-sync enabled." It is a poor model for
ad hoc commands such as "top this address up right now."

## Primary Environment CRD

YACD should have a primary CRD that represents a Cardano development
environment or network. The exact kind name is still open, but it should not be
named so narrowly that it only describes a single node.

The primary CRD should own the core Cardano substrate:

- network mode, such as fresh local genesis or joining an existing network
- genesis and node configuration material
- one initial primary node for the first prototype
- future multi-node topology
- bootstrap peer information
- status and connection details for dependent services
- default Ogmios sidecar exposure
- developer-oriented tuning for specific test scenarios

The first implementation can be much smaller: one primary Cardano node running
in a StatefulSet, with Ogmios as a default sidecar and a ClusterIP Service for
network access.

Ogmios should be part of the first core environment because it gives YACD and
developers a practical chain-access API without requiring every service to
share the node Unix socket directly.

## Supporting Service CRDs

Supporting services should be modeled as separate CRDs with separate
controllers. These CRDs reference the primary environment and derive chain
information from it.

This keeps the operator architecture easier to reason about. A db-sync
controller can focus on db-sync. A Yaci Store controller can focus on Yaci
Store. The primary environment controller can focus on the Cardano network
substrate.

The first supporting services to consider are:

- db-sync, as the highest priority because it matches the team's existing
  internal workflows
- Yaci Store, as an optional Blockfrost-like/indexer profile and useful
  influence from the Yaci ecosystem

Network-only supporting services can reconcile independent workloads. Services
that only need HTTP, WebSocket, or node-to-node TCP access do not need to be
part of the primary node Pod.

## Node Socket Access

Many Cardano services want direct access to a node Unix socket. That has
important Kubernetes implications.

A Unix socket is a local filesystem IPC object. It is not a cluster-wide
endpoint and cannot be exposed through a Kubernetes Service. The safest simple
pattern is to share it within one Pod using an ephemeral volume such as
`emptyDir`.

For the primary node, the node data directory should live on a PVC while the
socket directory should be ephemeral. A sidecar such as Ogmios can mount the
same socket directory and expose a network API for other services.

YACD should avoid making RWX PVCs or hostPath socket sharing part of the
default design. Those approaches are fragile, scheduler-sensitive, and poor
fits for hosted clusters.

## Heavy IPC Services And Follower Nodes

Heavy services that need raw node IPC should default to a dedicated follower
node colocated with the service, rather than mutating the primary node Pod.

For example, a db-sync CRD can own a StatefulSet containing:

- a follower `cardano-node`
- db-sync
- any required database sidecars or references
- local socket sharing inside that Pod

The follower node connects to the primary environment's bootstrap node over
normal node-to-node TCP. db-sync consumes the follower's local Unix socket.

This costs more CPU, memory, storage, and startup time, but it preserves clean
controller ownership and remains the default db-sync placement:

- adding db-sync does not restart the primary node
- the db-sync controller owns its workload, PVCs, database wiring, config, and
  status
- the primary environment only needs to publish enough information for
  dependent controllers to configure follower nodes

`primarySidecar` is an explicit exception for cases where duplicate node cost
is worse than sharing the primary socket. In that mode, CardanoDBSync still
owns its database, config, state, metrics, and status, but CardanoNetwork
composes the db-sync sidecar into the primary Deployment. Enabling or changing
that attachment rolls the primary Deployment, trading workload isolation for a
single node copy and direct socket access. The sidecar path is supported for
local networks and non-mainnet public profiles; public mainnet db-sync remains
blocked until a bootstrap and sizing path is proven. Once db-sync state has
accepted a placement mode, changing between `primarySidecar` and
`dedicatedFollower` requires recreating the CardanoDBSync with fresh or
intentionally compatible database state.

This pattern should be opt-in for heavyweight services, not automatic for every
helper.

## Faucet And Wallet Operations

YACD should include a narrow bespoke faucet/topup service if existing services
do not fit the local-dev workflow well enough.

The faucet should not become a general wallet platform. Its job is to support
developer operations such as funding known wallets or topping up a supplied
address. It can hold YACD-managed funding material, build and sign transactions,
and submit them through Ogmios.

Wallet generation can be declarative when it is part of environment bootstrap.
For example, the developer-facing config can request named wallets or generated
wallet sets. The operator can create the corresponding Kubernetes Secrets and
baseline funding state.

Ad hoc topups should remain imperative. The CLI can call the faucet service
instead of asking users to express one-off funding requests as CRD mutations.

## Developer-Facing Config

The cluster API should remain decomposed, but local setup should not force
developers to hand-author many CRDs.

YACD should provide a single developer-facing config file that can be checked
into a repository. The CLI compiles that config into the Kubernetes resources
needed by the operator.

This gives developers a single pane of glass while preserving Kubernetes
resource boundaries in the cluster.

The developer config can eventually cover:

- environment name and namespace
- local genesis or existing network selection
- node tuning
- enabled supporting services
- db-sync profile and resource sizing
- generated wallet sets
- initial wallet balances
- optional service exposure preferences

The first version should stay small and should only cover what the prototype
actually supports.

## Research Takeaways

Yaci DevKit is the closest existing product analogue. It provides a strong
developer experience: fast devnet startup, funded wallets, faucet/topup
operations, optional Ogmios/Kupo/Yaci Store integrations, local inspection
commands, reset behavior, and SDK-friendly endpoints.

YACD should learn from that experience, but not reuse Yaci DevKit as the
Kubernetes control plane. DevKit is process, home-directory, Docker, and NPM
oriented. YACD should be Kubernetes-native.

Yaci core is a real Java Cardano mini-protocol library, not merely a wrapper
around existing tools. It may be useful indirectly through services such as
Yaci Store, but it should not be embedded in the Go operator.

Yaci Store is a plausible optional indexer/API component. It is operationally
heavier than a small service because it is a Spring Boot app plus database and
indexing lifecycle, but it is comparatively self-contained.

Blockfrost-compatible APIs are useful for application developers, but they are
not the minimum chain-access layer for YACD-owned services. Ogmios is the more
direct first dependency for submit/evaluate flows and custom services such as a
faucet.

## First Prototype Direction

The first prototype should prove the smallest useful Kubernetes-native path:

1. A primary environment CRD reconciles one Cardano node StatefulSet.
2. Ogmios runs as a sidecar and exposes a ClusterIP endpoint.
3. The primary CRD publishes useful status and connection details.
4. A companion CLI can render/apply a small developer config.
5. A narrow faucet/topup path uses Ogmios.
6. db-sync is the first supporting service target, preferably using a dedicated
   follower-node pattern.

Yaci Store can follow as the next optional service once the db-sync path has
made the supporting-service model concrete.

## Open Questions

The following questions should remain open until prototype work provides better
evidence:

- What should the primary CRD be named?
- How much of the local genesis workflow should the operator own directly?
- Which node settings are important enough to expose early?
- What is the smallest useful db-sync configuration surface?
- Should the first faucet run in-cluster, in the CLI, or both?
- How should generated wallet Secrets be shaped?
- How should the CLI handle rendering, diffing, applying, and waiting?
- Which service readiness signals should become status conditions?
- How much multi-node topology should be modeled before the first prototype?

The design should evolve from these prototypes. The first working slice matters
more than predicting every final API shape now.
