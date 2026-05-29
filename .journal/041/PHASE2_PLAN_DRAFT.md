# YACD Test Harness — Phase 2 Implementation Plan (DRAFT v1, for adversarial review)

Phase 2 = "Host access and the integration contract" from `TEST_HARNESS_PLAN.md`.
Local story complete: `run`/`connect`/`exec`, `topup --await`, and the `YACD_*`
env-var contract, all over one shared forwarding engine.

This is a DRAFT for adversarial refinement. It is grounded in the real code:
`cli/internal/{cli,kube,render,devconfig}`, `internal/cardano/primarypod`,
`api/v1alpha1` status types, `cli/cmd/yacd/main.go`.

## 0. Scope and exit criteria

Deliver (from the proposal §4/§6 and plan Phase 2):
1. The `YACD_*` env-var contract — defined, typed, documented, versioned.
2. A shared client-go port-forward engine (NOT shelled `kubectl`).
3. `yacd run NAME [-n ns] -- <cmd...>` — scoped forwards → inject `YACD_*` env →
   exec child on host → tear forwards down on exit. No `--cmd` ⇒ `$SHELL`.
4. `yacd connect NAME [-n ns]` — foreground supervised forwards, writes
   `.yacd/<network>/endpoints.json` (loopback URLs), runs until Ctrl-C.
5. `yacd exec NAME [-n ns] -- <cmd...>` — in-pod exec (kubectl-exec semantics)
   with in-pod env incl. `CARDANO_NODE_SOCKET_PATH` + `YACD_NETWORK_MAGIC`.
6. `topup --await` — poll Kupo for the destination UTxO before returning.
7. (Optional) `yacd env NAME` — print the `YACD_*` block for `eval`.

Exit criteria (plan): a developer can (a) run an arbitrary test command locally
that reaches the network purely through `YACD_*`, (b) fund a checked-in address
and see funds confirmed, (c) use socket-bound tooling via the in-pod path.

Standards to hold: hexagonal ports/adapters (Rule 7 — `NewClient` returns the
concrete adapter; command layer depends on interfaces; tests mock); readability
& maintainability (per-command files, `doc.go` package contracts, godoc on
exported symbols, typed vocabulary, restrained inline comments matching existing
density); Mockery v3 + Testify; Moon task front door.

## 1. Architecture and boundaries

### 1.1 New access ports on the kube adapter

The existing `kube.Client` port wraps controller-runtime `client.Client`
(Get/List/Apply/Delete/Secret/Namespace). Port-forward and exec need
lower-level access: a `*rest.Config` + SPDY round-tripper and the Pod
`portforward`/`exec` subresource. So the `Adapter` must also retain the
`*rest.Config` it builds in `NewClient` (today it discards it), and the package
gains new ports:

```go
// PortForwardSpec maps a remote container port to forward.
type PortForwardSpec struct {
    Remote int32  // container port on the primary Pod
    Name   string // logical name (ogmios|kupo|faucet) for diagnostics
}

// ForwardSession is a live set of port-forwards to one Pod.
type ForwardSession interface {
    LocalPort(remote int32) (int, bool) // assigned local (random) port
    Done() <-chan struct{}              // closed when forwarding stops
    Err() error                         // non-nil reason once Done fires
    Close() error                       // tear down forwards
}

// Forwarder establishes port-forwards to a named Pod.
type Forwarder interface {
    Forward(ctx context.Context, namespace, podName string, specs []PortForwardSpec) (ForwardSession, error)
}

// ExecRequest carries an in-pod command invocation.
type ExecRequest struct {
    Namespace, PodName, Container string
    Command []string
    Stdin io.Reader; Stdout, Stderr io.Writer
    TTY bool
}

// Executor runs a command inside a Pod container (kubectl-exec semantics).
// Returns the remote process exit code via a typed error.
type Executor interface {
    Exec(ctx context.Context, req ExecRequest) error
}

// PodFinder resolves the ready primary node Pod for a network.
type PodFinder interface {
    PrimaryPodName(ctx context.Context, namespace, networkName string) (string, error)
}
```

`Adapter` implements `Forwarder` (client-go `portforward.New` with `:remote`
specs for random local ports; read `ForwardedPorts()` for assigned ports),
`Executor` (`remotecommand.NewSPDYExecutor` + `StreamWithContext`), and
`PodFinder`. `NewClient` keeps returning `*Adapter` (Rule 7); the command layer
holds the narrow interfaces. Mockery generates mocks for the new ports.

### 1.2 The CLI ↔ controller-internal boundary (KEY DECISION — needs human sign-off)

`run`/`connect`/`exec` need three operator facts: (a) primary Pod **selector**,
(b) chain-API **port numbers** to forward, (c) for `exec`, the node **socket
path** + node **container name**. Today the CLI imports only `api/v1alpha1`.

- (b) port numbers: already in `status.endpoints.{ogmios,kupo,faucet}.Port` —
  the CLI reads these from status. No coupling. ✅
- (a) pod selector + (c) socket path/container name: NOT in status.
  `internal/cardano/primarypod` already holds `SelectorLabels`,
  `CardanoNodeContainerName`, port names; the socket path `/ipc/node.socket`
  lives in controller-internal `containers.go`.

Three candidate resolutions (panel: pick/critique):

- **Option A — Discover from live cluster objects (recommended).** `PodFinder`
  resolves the pod by reading the published Service (`status.endpoints.*.ServiceName`),
  taking its `.spec.selector`, and listing matching Ready Pods. The CLI treats
  the operator's *published Services* as the contract, not its Go vocabulary —
  zero new internal import, robust to operator refactors. For `exec`, the node
  container name is discovered from the resolved Pod's containers; the socket
  path is the one value not discoverable → publish it in status
  (`status.access.nodeSocketPath` or reuse node endpoint) as a tiny operator
  addition, OR (A′) keep a CLI-local `const nodeSocketPath = "/ipc/node.socket"`
  pinned by a guard test.
- **Option B — Import `internal/cardano/primarypod`.** CLI imports the
  deliberately controller-free vocabulary package for `SelectorLabels`,
  `CardanoNodeContainerName`; promote the socket path into `primarypod` as
  `DefaultNodeSocketPath` (controller imports it too). DRY, no API change, but
  breaks the CLI's "only api/v1alpha1" purity.
- **Option C — Publish an access contract in status.** Operator publishes
  `status.access` { podSelector?, nodeContainer, nodeSocketPath, ports } as the
  explicit host-access contract; CLI consumes only status. Cleanest contract,
  largest operator-side scope (API + controller + envtest), arguably bleeds
  into Phase 1/operator work.

Draft recommendation: **Option A with A′** (discover pod from Service selector;
CLI-local socket-path constant guarded by a cross-package test that fails if the
controller's constant drifts). Rationale: smallest blast radius, keeps the CLI
decoupled, and the only hard-coded value is pinned by a test. Panel to confirm
or override.

### 1.3 Shared forward orchestration

A `cli`-package helper (not a command) wraps the `Forwarder` + `PodFinder`
ports and a network's status into a "connected session": resolve pod → compute
`[]PortForwardSpec` from enabled+published endpoints → `Forward` → build the
`YACD_*` env from assigned local ports + status. Used by `run`, `connect`, and
`topup --await`. File: `cli/internal/cli/forward.go`.

## 2. The `YACD_*` env-var contract

Defined once as exported typed constants + builders. Proposed home:
`cli/internal/cli/envcontract.go` (exported `const` names so tests/docs
reference them; builders `hostEnv(...)` and `podEnv(...)`).

| Variable | host (`run`/`connect`) | in-pod (`exec`) | Condition |
|---|---|---|---|
| `YACD_NETWORK` | name | name | always |
| `YACD_NAMESPACE` | ns | ns | always |
| `YACD_NETWORK_MAGIC` | magic | magic | local mode (from status.network.networkMagic) |
| `YACD_OGMIOS_URL` | `ws://127.0.0.1:P` | `ws://<svc>.<ns>.svc.cluster.local:port` | Ogmios published |
| `YACD_KUPO_URL` | `http://127.0.0.1:P` | ClusterIP URL | Kupo published |
| `YACD_FAUCET_URL` | `http://127.0.0.1:P` | ClusterIP URL | faucet published |
| `YACD_FAUCET_TOKEN` | token | token | faucet published (read from auth Secret) |
| `CARDANO_NODE_SOCKET_PATH` | — | `/ipc/node.socket` | exec only |

Names are stable across host/in-pod; only values adapt. Version note in the
package doc. The in-pod URLs reuse the ClusterIP forms already in
`status.endpoints.*.URL`.

## 3. Work breakdown (ordered; each is a reviewable unit)

**WB1 — kube access ports + adapter (foundation).**
- `kube/client.go` (or new `kube/access.go`): add `Forwarder`, `Executor`,
  `PodFinder`, `ForwardSession`, spec/request types. `Adapter` retains
  `restConfig` from `NewClient`; implement the three ports.
- `kube/doc.go`: extend the contract description.
- Regenerate mocks (`.mockery.yml` add the new interfaces; `moon run root:generate`).
- Tests: `PodFinder` is envtest-testable (real apiserver, fake Pods). `Forwarder`
  and `Executor` adapters need a live kubelet → NOT envtest-able; covered by
  manual/e2e and kept thin. Document this honestly.

**WB2 — env contract.** `cli/internal/cli/envcontract.go` + test. Pure
functions over (network status, resolved local ports) → `[]string`. Fully unit
testable.

**WB3 — forward orchestration.** `cli/internal/cli/forward.go` + test. Uses
`PodFinder` + `Forwarder` ports (mocked in tests). Computes specs from enabled
endpoints; returns a session + the host `YACD_*` env.

**WB4 — `yacd run`.** `cli/internal/cli/run.go` + test. `cobra` `ArgsLenAtDash`
to split NAME from `-- cmd`. No cmd ⇒ `$SHELL`. `os/exec` child with env =
`os.Environ()` + `YACD_*`; stdio inherited; context-cancel on Ctrl-C; propagate
child exit code via typed `exitError`. Tear down forwards on exit (defer).

**WB5 — `yacd connect`.** `cli/internal/cli/connect.go` + test. Foreground;
write `.yacd/<network>/endpoints.json` (gitignored — extend `.gitignore` with
`.yacd/`); print env block; block on `ctx.Done()` or session drop. Supervision:
v1 = on drop, attempt re-establish with capped backoff; if it can't recover,
exit non-zero. (Panel: right-size supervision vs defer.)

**WB6 — `yacd exec`.** `cli/internal/cli/exec.go` + test. Resolve pod + node
container; wrap command to set in-pod env (`env K=V ... cmd` or via the
`Executor` request); TTY detection (`golang.org/x/term`); propagate remote exit
code. Requires the node socket path (see §1.2 decision).

**WB7 — `topup --await`.** `cli/internal/cli/topup_await.go` + test, plus a
`--await` flag and `--await-timeout` on `topup`. After a successful POST, poll
Kupo `GET /matches/<address>` for the result `txId`'s output. Kupo is ClusterIP
→ establish a scoped Kupo forward (reuse WB3) unless `YACD_KUPO_URL` is already
set (running under `yacd run`). If Kupo disabled ⇒ clear error
("`--await` requires Kupo"). Preserve all `topup_trust.go` invariants.

**WB8 — `yacd env` (optional).** `cli/internal/cli/env.go`. Print the `YACD_*`
block from a live forward (or from status as ClusterIP). Lowest priority; cut if
time-boxed.

**WB9 — exit-code propagation.** `cli/internal/cli/exit.go`: typed
`exitError{code int}` implementing `error`. `cli/cmd/yacd/main.go`: `errors.As`
to return the child/remote exit code instead of always 1.

**WB10 — root wiring.** `root.go`: register `run`/`connect`/`exec`/`env`;
`doc.go` updated; `root_test.go` unaffected (version test only) but add
registration coverage if the panel wants it.

**WB11 — minimal docs.** Reference doc for the `YACD_*` contract + `run`/`exec`
distinction. Full how-to/examples are Phase 5; keep this to a contract reference
so the surface is documented when it lands.

## 4. Testing strategy

- Unit (mocked ports): env contract, forward orchestration spec computation,
  `run` env assembly + exit-code mapping, `exec` command/env wrapping, `topup
  --await` poll loop + Kupo-disabled error, trust-gate invariants
  (`AssertNotCalled` GetSecretValue preserved).
- envtest (`kube/client_envtest_test.go`): `PodFinder` against a real apiserver.
- Manual/e2e (dev stack): the actual forward/exec/await against Kind — the only
  place port-forward and in-pod exec truly run. Add a focused manual validation
  script in NOTES; consider one Chainsaw assertion only if it needs the packaged
  operator (per CLAUDE.md, don't duplicate envtest in Chainsaw).

## 5. Risks / open decisions for the human

1. **§1.2 boundary decision (A/B/C).** Architectural; needs sign-off.
2. **`connect` supervision depth.** Auto-reconnect (more code, manual-only test)
   vs foreground-report-and-exit (simpler). 
3. **`topup --await` self-forward.** Should standalone `topup --await` open its
   own Kupo forward, or only work under `yacd run`/with `YACD_KUPO_URL`?
4. **`exec` env injection mechanism.** `env K=V cmd` wrapper vs `sh -c` vs
   relying on container env — affects quoting/robustness.
5. **PR breakdown.** One PR per WB cluster, or a few larger PRs?

## 6. Proposed sequencing / PRs

- PR 1: WB1 (+WB9 exit type) — ports + adapter + mocks + envtest PodFinder.
- PR 2: WB2 + WB3 — env contract + forward orchestration (pure, mock-tested).
- PR 3: WB4 (`run`) — the primary path.
- PR 4: WB6 (`exec`) — socket-bound path (carries the §1.2 socket decision).
- PR 5: WB5 (`connect`) — foreground + `.yacd/`.
- PR 6: WB7 (`topup --await`).
- PR 7: WB8 (`env`, optional) + WB11 (contract doc).

Each PR: `moon run root:generate` (if markers/mocks), `moon run root:check`,
`moon run root:test`, manual dev-stack proof where the path only runs live.
