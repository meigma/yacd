# yacd CLI — Local Cluster Lifecycle (Design)

One command takes a Docker-only machine to a running, usable local Cardano
devnet: provision a local Kubernetes cluster → install the yacd operator →
create a default `CardanoNetwork` → wait until Ready → tell the user how to use
it. Runtime is k3d. This document specifies the behavior and the code
architecture.

## 1. User story & persona

> *"As a yacd user, I want to spin up a local Cardano network as fast as
> possible without having to worry about setup."*

The persona is a Cardano app/dapp developer, not a Kubernetes operator. The only
assumed prerequisite is **Docker**. After bring-up they want to *use* the network
— query the tip, fund an address they own, run `cardano-cli`, point an app at
Ogmios/Kupo — and tear it down cleanly.

## 2. Goals & non-goals

**Goals.** Docker is the only prerequisite. A single command yields a usable
devnet. Every step is idempotent and re-runnable. Teardown is clean and complete.

**Out of scope (v1).** Multi-node local clusters; public mainnet quickstart; a
local image registry / operator-image inner loop (that remains on the separate
KinD + Tilt dev stack); Windows-native (WSL2 is the Windows path); chain-data
persistence across cluster deletion (`--persist` is a later addition).

## 3. The experience

```text
$ # Prereq the user already has: Docker running. Nothing else installed.
$ yacd devnet
==> Preparing local environment (first run pulls images, ~2–4 min)
    Docker .................................. ok (24.0 GB free)
    Fetching k3d v5.8.3 (sha256 verified) ... ok
    Creating local cluster "k3d-yacd" ....... ok (k3s v1.33.x)
    Installing yacd operator (v0.6.0) ....... ready
==> Starting Cardano devnet "devnet" (node + Ogmios + Kupo + faucet)
    pulling cardano-node ............ ok
    waiting for first block ......... ready (31s)
==> devnet is ready.   (kubectl context: k3d-yacd)
    Ogmios          ws://127.0.0.1:1337
    Kupo            http://127.0.0.1:1442
    Funded wallet   addr_test1qz…   (100,000 ADA)
    Try:  yacd exec devnet -- cardano-cli query tip --testnet-magic 42
          yacd topup devnet --address <addr> --lovelace 1000000000
          yacd run  devnet -- <your-app>      (env wired to the net)
          yacd devnet down                    (tear it all down)
```

Warm runs (cluster already up) skip to the network step (~30s).

## 4. Mental model

Two layers:

- **Cluster** — a single, shared, long-lived, yacd-managed k3d cluster (fixed
  context `k3d-yacd`). The "platform." Created once, reused.
- **Network** — a `CardanoNetwork` CR in its own namespace. One CR per network.
  **One cluster hosts many networks.**

`yacd devnet` brings up the cluster (if absent) plus one default network. The
existing primitives (`up`, `down`, `info`, …) manage *networks* within the
cluster, so a second/third network is `yacd up NAME -f FILE`.

## 5. Command surface

New:

| Command | Purpose |
|---|---|
| `yacd devnet [--bare]` | Zero-config all-in-one (takes no name). Ensure the singleton cluster → ensure/upgrade operator → (unless `--bare`) apply the embedded **default local devnet** network (named `devnet`) → wait Ready → print endpoints + funded wallet + next steps. `--bare` stops after the operator (cluster + operator only). The only verb that provisions a cluster. Always operates on the managed cluster. |
| `yacd devnet down` | Delete the k3d cluster (operator, all networks, managed context) in one shot. `--keep-data` preserves a bind-mounted chain-data dir; `--purge` also removes fetched binaries and managed state. Warns that ephemeral chain data is discarded. |
| `yacd devnet status` | One view: Docker reachable? k3d binary + pinned version? cluster up? operator version + Ready? networks + endpoints? |

Unchanged: `up NAME -f FILE`, `down NAME`, `list`, `info NAME`, `topup NAME`,
`run NAME`, `connect NAME`, `exec NAME`. These manage networks; none provision a
cluster.

Querying the chain uses `yacd exec NAME -- cardano-cli …`. `devnet`/`up`/`info`
print a copy-pasteable `exec` tip-query hint with the network magic interpolated
from the published status. `info` remains the structured-status inspector.

Exactly one managed cluster — `devnet` neither takes nor implies a cluster name
(the context is always `k3d-yacd`).

## 6. Cluster & kubeconfig model

**Provisioning.** `devnet` ensures the cluster by name; the underlying runtime
adapter shells out to a pinned, checksum-verified k3d binary. `EnsureCluster` is
idempotent: absent → create with a pinned k3s image; present-and-healthy → no-op;
present-but-unhealthy (API server unreachable) → delete and recreate. A create
that fails partway is rolled back (delete the partial cluster) before returning.
Readiness gates on the API server (`k3d --wait/--timeout`) and then on workload
readiness.

**Concurrency.** All cluster-mutating operations (create/delete/upgrade, operator
install) hold a file lock scoped to the managed cluster, so parallel invocations
or worktrees cannot race k3d's non-idempotent create. Read-only verbs take no
lock.

**Kubeconfig & context.** Bring-up switches the kubectl context to `k3d-yacd`
(the runtime's default merge-and-switch), so `kubectl`/`k9s`/tutorial commands
work immediately. Independently, yacd records that it owns context `k3d-yacd` and
defaults all its own verbs to it — so yacd stays correct even if the user later
changes their current-context, and never targets a foreign cluster. Teardown
clears the record and restores the prior current-context.

**State & source of truth.** The managed cluster has a fixed name (`k3d-yacd`),
so its existence and health are always discoverable directly from the runtime
(`cluster.Status` → `k3d cluster list` + an API probe); yacd never depends on
local state to *find* its cluster. The `clusterstate` record under
`$XDG_STATE_HOME/yacd/` (plus a `cluster.lock`) is supplementary bookkeeping the
runtime can't hold — the owned context, the prior current-context to restore on
teardown, and the pinned k3d version. The runtime is authoritative: `devnet` /
`status` reconcile against it and re-derive the record, so a deleted record (with
the cluster still up) is rebuilt, and a stale record (with the cluster gone) is
corrected on the next `devnet`. Read verbs use the record's context cheaply for
targeting; if the cluster is actually gone, the connection fails with a "run
`yacd devnet`" hint.

**Targeting precedence** (one resolver, used by every verb):

```
explicit --kubeconfig / --context (or YACD_KUBECONFIG / YACD_KUBE_CONTEXT)
  >  yacd's tracked managed context (k3d-yacd), when the managed cluster exists
  >  ambient KUBECONFIG env / ~/.kube/config current-context
```

The tracked-context tier exists only after a user runs `devnet`, so automation
that sets an explicit target is unaffected. Every mutating verb prints the
resolved target (context + server). `--isolate-kubeconfig` opts into a dedicated
kubeconfig file that never touches `~/.kube/config`.

## 7. Operator install model

The operator chart is **pre-rendered at build time** (a generate step runs
`helm template` over `charts/yacd` at the CLI's release version) and **embedded**
in the CLI binary. Install applies the embedded manifests by **server-side apply**
through the cluster's API: CRDs first (wait until Established), then RBAC,
ServiceAccount, and the manager Deployment, all under a yacd field-owner with a
label-based prune set so removed objects are cleaned up. Default image references
resolve to the published `ghcr.io/meigma/yacd` images at the matching version, so
no registry is required.

**Version reconciliation.** The installed operator version is recorded in-cluster.
On each `devnet`/`status`: absent → install; older, same major → upgrade the
operator (apply CRDs + Deployment); newer than the CLI, or a major mismatch →
refuse with an actionable message. CRD upgrades happen only on this path and are
reported.

## 8. Default network & funded wallet

`yacd devnet` (without `--bare`) applies an **embedded, fully-specified default
local Environment** (Conway, single pool, Ogmios + Kupo + faucet), so no input
file is needed. Custom or additional networks use `yacd up NAME -f FILE`.

**Dependency.** The user story's "fund an address" requires the developer to own
a funded address on day zero. The default devnet must therefore ship at least one
**pre-generated, pre-funded wallet** (operator bootstraps keys into a Secret), and
the CLI surfaces its address (and a key-export path) in `devnet`/`info` output.
This is operator/CRD work and is a prerequisite for the story to be fully met;
the CLI lifecycle can ship ahead of it but is incomplete without it.

## 9. Behavior details

- **Preflight.** Probe the Docker daemon and available VM disk before any work;
  fail fast with actionable messages.
- **Progress.** Stream stepwise status to stderr, including image-pull and
  pod-readiness sub-progress (watched via the API and the operator's published
  conditions), so a slow first run never reads as a hang. The first run is
  explicitly framed as image-pull-bound; warm runs are fast.
- **Failure mapping.** Adapters return typed errors (Docker unavailable, host
  port conflict, disk pressure / pod eviction, checksum mismatch); the command
  layer renders each as a human, actionable message rather than raw output.
- **Binary management.** Fetch `k3d-<os>-<arch>` from the pinned release tag and
  verify it against a SHA256 embedded in the CLI at build time (not a
  runtime-fetched checksum file); store under an XDG path; GC superseded binaries
  on fetch. A pre-staged binary (present, or `YACD_K3D_PATH`) skips the fetch.
- **Uninstall.** `devnet down --purge` removes the cluster, the managed state,
  and fetched binaries — the full on-disk footprint.

## 10. Code architecture (hexagonal)

The CLI already follows ports-and-adapters: `kube.Client` is a port,
`kube.Adapter` its implementation, constructed by a factory on `Options` and
mocked in tests. New port/adapter pairs follow the repo convention of a **port
package** (interface + domain types + `doc.go`) with each **adapter as a
subpackage beneath it** — so the port stays dependency-light and only the
composition root imports the heavy adapters. The lifecycle adds three such ports
plus a state port and a use-case orchestrator, all under `cli/internal`, all
mockable.

### 10.1 Package layout

```
cli/internal/
  kube/              # UNCHANGED port (+ adapter), reused for network apply + host access
  devconfig/         # UNCHANGED, reused (embedded default Environment)
  render/            # UNCHANGED, reused (Environment -> CardanoNetwork)
  cluster/           # NEW port: Provisioner + types  (cluster.go, doc.go)
    k3d/             #   adapter: k3d shell-out
  toolbin/           # NEW port: Resolver + types
    ghrelease/       #   adapter: GitHub-release fetch + embedded-checksum verify
  operator/          # NEW port: Installer + types
    ssa/             #   adapter: server-side apply over embedded rendered manifests
  clusterstate/      # NEW port: Store (record + lock) + types
    file/            #   adapter: filesystem-backed store + flock
  lifecycle/         # NEW use-case: Manager composing the ports
  cli/
    devnet.go        # NEW command subtree (devnet / down / status); thin
    target.go        # NEW shared targeting resolver (precedence in §6)
    options.go       # extend: factories for the new ports
  mocks/             # generated mocks for the new port interfaces
```

Each port package holds only its interface + domain types (+ `doc.go`); the
adapter subpackage imports the port and implements it. So only `cluster/k3d`
pulls the shell-out/exec machinery, only `operator/ssa` pulls the SSA client +
embedded `fs.FS`, and consumers (commands, `lifecycle`) depend on the light port
packages.

### 10.2 Ports

```go
// cluster — the local Kubernetes cluster runtime.
package cluster

type Spec struct {
    Name     string        // managed cluster name -> context "k3d-<Name>"
    K3sImage string        // pinned, e.g. rancher/k3s:v1.33.x-k3s1
    DataDir  string        // host bind-mount for chain data ("" = ephemeral)
    Timeout  time.Duration
}

type Info struct {
    Name, Context, KubeconfigPath string
    Running                       bool
}

type Status struct {
    Exists, Running, Healthy bool
    K3sImage, Context        string
}

// Provisioner is idempotent: create / heal / no-op as needed.
type Provisioner interface {
    EnsureCluster(ctx context.Context, spec Spec) (Info, error)
    DeleteCluster(ctx context.Context, name string) error
    Status(ctx context.Context, name string) (Status, error)
}
```

```go
// toolbin — locate a verified, pinned tool binary, fetching on demand.
package toolbin

type Pin struct {
    Version   string
    AssetURL  string            // template by os/arch
    SHA256    map[string]string // os/arch -> expected digest (embedded at build)
}

type Resolver interface {
    Resolve(ctx context.Context) (path string, err error)
}
```

```go
// operator — idempotent operator install/upgrade into a cluster.
package operator

type InstallSpec struct {
    Namespace string
    Version   string // embedded chart/app version
    Values    Values // image refs etc.; defaults to published ghcr.io images
}

type State struct {
    Installed, Ready bool
    Version          string
}

type Installer interface {
    EnsureOperator(ctx context.Context, spec InstallSpec) (State, error) // CRDs-first SSA + prune + version reconcile
    OperatorState(ctx context.Context) (State, error)
}
```

```go
// clusterstate — the managed-cluster record and a process lock.
package clusterstate

type Record struct {
    ClusterName, Context, PriorContext string
    K3dVersion, KubeconfigPath         string
}

type Store interface {
    Load() (Record, bool, error)
    Save(Record) error
    Clear() error
    Lock(ctx context.Context) (release func() error, err error) // file lock on the managed cluster
}
```

### 10.3 Adapters (subpackages)

- `cluster/k3d` — implements `cluster.Provisioner` by shelling out to a resolved
  k3d binary through an injected command runner (testable without Docker). Owns
  the `EnsureCluster` state machine and partial-create rollback.
  `k3d.New(resolver toolbin.Resolver, runner exec.Runner) *k3d.Provisioner`.
- `toolbin/ghrelease` — implements `toolbin.Resolver`: downloads the pinned
  `k3d-<os>-<arch>` asset, verifies against the embedded digest, installs under
  XDG, GCs superseded versions; injectable HTTP client.
  `ghrelease.New(pin toolbin.Pin, dir string, http HTTPDoer) *ghrelease.Resolver`.
- `operator/ssa` — implements `operator.Installer` over an embedded manifest
  `fs.FS`, constructed against the managed cluster, using a generic
  server-side-apply client internally (CRDs first, then RBAC/SA/Deployment,
  label-based prune).
  `ssa.New(kubeconfig, context string, manifests fs.FS) (operator.Installer, error)`.
- `clusterstate/file` — implements `clusterstate.Store` under
  `$XDG_STATE_HOME/yacd` (JSON record + `flock`). `file.New(dir string) *file.Store`.

### 10.4 Use-case orchestrator

```go
// lifecycle — composes the ports; the command layer stays thin.
package lifecycle

type Manager struct {
    Provisioner  cluster.Provisioner
    State        clusterstate.Store
    NewInstaller func(kubeconfig, context string) (operator.Installer, error)
    NewNetworks  func(kubeconfig, context string) (kube.Client, error) // == existing KubeClientFactory
    Report       Reporter // stepwise progress
}

type UpOptions struct {
    Bare        bool
    Env         devconfig.Environment // embedded default unless overridden
    NetworkName string                // "devnet"
    Timeout     time.Duration
}

func (m *Manager) Up(ctx context.Context, o UpOptions) (Result, error)
func (m *Manager) Down(ctx context.Context, o DownOptions) error
func (m *Manager) Status(ctx context.Context) (Report, error)
```

`Manager.Up` flow: acquire `State.Lock` → preflight → `Provisioner.EnsureCluster`
→ `State.Save` (record context + prior context) → build the installer against the
returned kubeconfig/context and `EnsureOperator` → unless `Bare`, build the kube
client, render the default Environment, `EnsureNamespace` + `ApplyCardanoNetwork`
+ wait Ready → return endpoints/wallet for the command layer to print. Each port
is mocked independently, so the whole flow is unit-testable without Docker or a
cluster.

### 10.5 Wiring & shared targeting

`Options` gains factory fields next to `KubeClientFactory` (defaulting to the
subpackage adapters — `k3d.New`, `ghrelease.New`, `ssa.New`, `file.New` —
overridable by tests):

```go
ClusterProvisionerFactory func(cluster.Spec) (cluster.Provisioner, error)
OperatorInstallerFactory  func(kubeconfig, context string) (operator.Installer, error)
ClusterStateFactory       func() (clusterstate.Store, error)
```

`devnet.go` builds a `lifecycle.Manager` from these and runs `Up`/`Down`/`Status`.

All verbs resolve their cluster target through one function so the precedence in
§6 lives in a single place:

```go
// target.go
func ResolveTarget(cfg RuntimeConfig, st clusterstate.Store) (kubeconfig, context string)
```

`up`, `info`, `run`, `exec`, `topup`, `connect`, `list`, `down` call
`ResolveTarget` and feed the result into the existing `kube.NewClient(kubeconfig,
context)` — no change to the `kube` port itself.

## 11. Phased plan

- **Slice 0 — provisioning core.** `toolbin` (fetch + embedded-checksum verify),
  `cluster` port + k3d adapter + `EnsureCluster` state machine, `clusterstate`
  (record + lock), context switch + tracking, Docker/disk preflight. Verbs:
  `devnet status`, `devnet down`. Proves cluster up/down and deterministic
  targeting.
- **Slice 1 — operator install.** Build-time chart render → embed; `operator`
  port + SSA adapter (CRDs-first, prune); version stamp + reconcile.
- **Slice 2 — the all-in-one.** `lifecycle.Manager` + `devnet` (+ `--bare`):
  cluster → operator → embedded default network → wait → result output (incl. the
  magic-interpolated `exec` tip hint) + progress streaming.
- **Slice 3 — hardening.** Failure mapping, `--purge` uninstall + binary GC,
  first-run banner, WSL2 validation, ARM multi-arch CI guard. (User documentation
  is handled separately.)
- **Dependency — funded-wallet bootstrap** (§8): operator/CRD work; sequence
  relative to Slice 2 per the open question below.

## 12. Open questions

1. **Verb name** — `devnet` vs `dev` / `quickstart` / `start`.
2. **Funded-wallet sequencing** — build the operator-side funded wallet before the
   all-in-one (story fully met on day one) or ship the lifecycle first?
3. **Chain-data default** — ephemeral for v1 with `--persist` later, or persist by
   default given the "build against it" use case?
