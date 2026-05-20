# YACD Initial Prototype Plan

This is a rough component-focused path from the current design direction to the
first usable YACD prototype. It is not a complete backlog, implementation spec,
or final architecture. Keep it current enough to guide the next few slices, and
let prototype work refine the details.

## Prototype Target

The first meaningful prototype should let a developer create a local Cardano
development environment in Kubernetes, access it through Ogmios, perform a
basic topup flow, and optionally run db-sync against the environment.

The target shape is:

- one primary Cardano environment reconciled by the operator
- one primary Cardano node with Ogmios exposed as the default chain API
- a companion CLI that turns a small local config into Kubernetes resources
- a narrow faucet/topup path backed by Ogmios
- a db-sync supporting service using its own follower node

## 1. Operator Shell And Project Identity

Replace enough of the template operator surface to make the repo feel like
YACD rather than `template-k8s`.

Build:

- project/chart/API naming cleanup
- removal or replacement of the template `NginxDeployment` sample
- first YACD API group/version
- generated CRDs and chart wiring still flowing through Moon

Proof:

- the operator builds, tests, and deploys in Kind
- the chart installs cleanly with no template sample API left as the user-facing
  product

## 2. Primary Environment Component

Build the first primary environment CRD and controller. The CRD should be
environment/network-shaped even if the first runtime is only one local node.

Build:

- primary environment CRD
- single-node local Cardano runtime
- ConfigMaps/Secrets needed for node config and genesis material
- node StatefulSet, PVC, Service, and basic status conditions
- enough tuning surface to support the initial local test network

Proof:

- applying one environment custom resource creates a running local Cardano node
- status reports meaningful readiness and connection information
- the node can be queried inside the cluster

## 3. Ogmios Sidecar And Default Chain API

Add Ogmios as the default sidecar for the primary node.

Build:

- shared ephemeral IPC volume for the node socket
- Ogmios sidecar in the primary node Pod
- ClusterIP Service for Ogmios
- status fields or conditions that make the Ogmios endpoint discoverable

Proof:

- clients can reach Ogmios through the cluster Service
- the CLI or a smoke test can query chain state through Ogmios
- no supporting service needs direct access to the primary node Unix socket

## 4. Developer Config And CLI Foundation

Build the first companion CLI path around a small checked-in developer config.

Build:

- local config format for the first supported environment shape
- render/apply flow that produces the primary environment custom resource
- wait/status command for the environment
- connection-info output for Ogmios and future services

Proof:

- a developer can stand up the prototype without hand-authoring CRDs
- the CLI can wait until the environment is usable
- the generated Kubernetes resources are inspectable and not hidden behind the
  CLI

## 5. Faucet And Topup Component

Build the narrowest useful funding path.

Build:

- funding material generated or mounted for the local environment
- faucet/topup service or helper path backed by Ogmios
- CLI command for topping up an address or known wallet
- minimal wallet metadata support only where needed for bootstrap/topup

Proof:

- a user can top up an address on the local chain
- the topup path submits through Ogmios
- wallet handling does not grow into a general wallet platform

## 6. db-sync Supporting Service

Build db-sync as the first supporting service CRD.

Build:

- db-sync CRD referencing the primary environment
- db-sync controller
- dedicated follower node colocated with db-sync for local IPC access
- Postgres or other required database wiring
- db-sync configuration generated from environment/network status
- readiness and sync-progress status

Proof:

- applying a db-sync custom resource creates the follower node, database, and
  db-sync workload
- the follower connects to the primary node over node-to-node TCP
- db-sync indexes the local chain and exposes observable progress
- adding db-sync does not mutate or restart the primary node Pod

## 7. Prototype Hardening

Tighten only the pieces needed to make the prototype repeatable.

Build:

- focused envtest coverage for the primary environment and db-sync controllers
- one Kind/Chainsaw smoke for the installed operator path
- clear status conditions and Events for user-visible state
- README or quickstart notes for the prototype flow
- cleanup/reset behavior only where the prototype actually needs it

Proof:

- a fresh Kind cluster can run the end-to-end prototype
- repeated create/delete cycles do not leave confusing state behind
- failures surface through Kubernetes status, Events, logs, or CLI output

## Deferred Until After The Prototype

Do not block the first prototype on:

- final CRD names or exhaustive schemas
- generic node attachment frameworks
- broad multi-node topology controls
- Yaci Store support
- Blockfrost Platform or Dolos support
- preprod, preview, or mainnet joining
- a general wallet service
- production-grade faucet policy
- every possible Cardano service integration

Yaci Store is still the next likely optional service after db-sync, but it
should wait until the supporting-service model is proven.
