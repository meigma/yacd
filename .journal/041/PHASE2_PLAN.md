# YACD Test Harness — Phase 2 Implementation Plan (FINAL, for human review)

Phase 2 = "Host access and the integration contract" (`TEST_HARNESS_PLAN.md`).
This plan is the product of a draft (`PHASE2_PLAN_DRAFT.md`) hardened by a 5-lens
adversarial review (hexagonal / readability / feasibility / security / scope),
per-finding skeptical verification (33 findings survived, 3 refuted), and a
completeness/synthesis pass. Every load-bearing claim below is grounded in the
real code; file:line citations are from the live tree at `c7825f8`.

**Status: NOT STARTED. Awaiting human sign-off, especially the §1 boundary gate.**

---

## 0. Scope and exit criteria

Deliver (proposal §4/§6, plan Phase 2):
1. The `YACD_*` env-var contract — typed, documented, versioned.
2. A shared client-go port-forward engine (NOT shelled `kubectl`).
3. `yacd run NAME [-n ns] -- <cmd...>` — scoped forwards → inject `YACD_*` env →
   exec child on host → tear forwards down on exit. No `-- cmd` ⇒ `$SHELL`.
4. `yacd connect NAME [-n ns]` — foreground, supervised forwards; writes
   `.yacd/<network>/endpoints.json` (loopback URLs); runs until Ctrl-C.
5. `yacd exec NAME [-n ns] -- <cmd...>` — in-pod exec (kubectl-exec semantics)
   with in-pod env incl. `CARDANO_NODE_SOCKET_PATH` + `YACD_NETWORK_MAGIC`.
6. `topup --await` — poll Kupo for the destination UTxO before returning.

Exit criteria: a developer can (a) run an arbitrary test command locally that
reaches the network purely through `YACD_*`; (b) fund a checked-in address and
see funds confirmed; (c) use socket-bound tooling via the in-pod path.

**Cut from Phase 2** (review finding 31; smallest-viable-surface rule):
`yacd env` — redundant with `run`'s `$SHELL` drop-in and `connect`'s file. Defer.

Standards held throughout: hexagonal ports/adapters (Rule 7 — `NewClient`
returns concrete `*Adapter`; command layer depends on interfaces; Mockery v3 +
Testify); one file per command; `doc.go` package contracts; godoc on every
exported symbol; typed vocabulary; the sticky-error `infoWriter` and
stable-`json`-tag projection patterns; Moon as the only task front door.

---

## 1. DECISION GATE — CLI ↔ controller boundary (sign off before any code)

`run`/`connect`/`exec` need three operator facts. Where each comes from:

- **Port numbers (✅ solved by status today):** `PortForwardSpec.Remote` is read
  directly from `status.endpoints.{ogmios,kupo,faucet}.Port`. For these three,
  the Service port equals the container port by construction (`status.go:219/228/240`
  read `Service.Spec.Ports[0].Port`; `containers.go:192/248/313` set the matching
  `ContainerPort`), so no Pod-spec/containerPort discovery is needed.
  `nodeToNode` is **intentionally skipped** (published `tcp://`, a host test does
  not speak that protocol — proposal §6).
- **Primary Pod selector + node socket path / container name (the decision):**
  not in status.

**DECISION: Option A + A′** (recommended; confirmed by findings 4/16/17):
- Resolve the primary Pod by reading the published Service
  (`status.endpoints.*.ServiceName`) → its `.spec.selector` → the Ready Pods
  matching it. The CLI treats the operator's **published Services** as the
  contract, keeping it `api/v1alpha1`-pure with **zero new `internal/...`
  imports**.
- Pin the node socket path (`/ipc/node.socket`) **and** node container name
  (`cardano-node`) as **documented CLI-local constants**. The draft's "guard
  test against the controller constant" is **impossible and dropped**:
  `cardanoNodeSocketPath` (`containers.go:25`) is unexported. Document these as a
  deliberate, stable coupling to the fixed localnet container layout.
- **Fenced OUT of Phase 2:** Option C and the "publish socket path in
  `status.access`" half of Option A — both are operator-side API/controller/
  envtest work that would bleed Phase 2 into operator scope (finding 16).

§1 is a **zero-code design gate**: PodFinder's first line depends on this choice,
so it must be signed off **before PR1**.

---

## 2. Architecture

### 2.1 Access ports — extend the existing `kube.Client` seam (finding 1, major)

The **only** command-layer seam is `KubeClientFactory func(kube.Config)
(kube.Client, error)` (`options.go:56`); every command acquires a `kube.Client`
through it (`up.go:82`, `down.go:35`, …). `*Adapter` is never named in the
command package. Therefore the new capabilities must arrive **through that seam**.

**Extend the `kube.Client` interface** with the new methods (`*Adapter` already
will implement them; the single mockery `mocks.Client` regenerates to carry
them). **Forbid** type-asserting `kube.Client` to `*Adapter` — `mocks.Client` is
not an `*Adapter`, so an assertion would break every mock-based command test.

```go
// added to the kube.Client interface (client.go)
PrimaryPodName(ctx, namespace, networkName string) (string, error)
Forward(ctx, namespace, podName string, specs []PortForwardSpec) (ForwardSession, error)
Exec(ctx, req ExecRequest) error
```

Supporting types (in a new `kube/access.go`, with `doc.go` updated):

```go
type PortForwardSpec struct{ Remote int32; Name string }      // Name: ogmios|kupo|faucet
type ForwardSession interface {
    LocalPort(remote int32) (int, bool) // assigned random local port
    Done() <-chan struct{}              // closed when forwarding stops
    Err() error                         // reason once Done fires
    Close() error
}
type ExecRequest struct {
    Namespace, PodName, Container string
    Command []string                    // argv — NOT a shell line (see §4)
    Stdin io.Reader; Stdout, Stderr io.Writer; TTY bool
}
```

`NewClient` must **build and retain a CoreV1 REST client** (not merely the
`*rest.Config` the draft said) so the adapter can construct the
`pods/<name>/{portforward,exec}` subresource URLs. Today `restConfig` is built in
`config.go` and discarded after `client.New`; retain it on `Adapter`.

### 2.2 Forwarder/Executor adapter mechanics (findings 3/9/10/11 — corrected API)

The draft's API names were wrong. Correct client-go v0.36.1 construction:

- **Forward:** `transport, upgrader := spdy.RoundTripperFor(restConfig)` →
  build the portforward subresource URL via the CoreV1 REST client →
  `dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)`
  → `pf, _ := portforward.New(dialer, ports, stopCh, readyCh, out, errOut)` where
  each `ports` entry is `"0:<remote>"` (local 0 ⇒ random). Run `go pf.ForwardPorts()`,
  then **race `readyCh` against an early error** from the goroutine, then call
  `pf.GetPorts()` (NOT `ForwardedPorts()`) → `[]ForwardedPort{Local, Remote uint16}`;
  convert with `int(p.Local)`. `ForwardSession.Done()/Err()` wrap the goroutine's
  termination (incl. `portforward.ErrLostConnectionToPod`); `Close()` closes
  `stopCh`.
- **Exec:** build the exec subresource URL → `remotecommand.NewSPDYExecutor(restConfig, "POST", url)`
  → `exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin,Stdout,Stderr,Tty})`.
  Remote exit code arrives as `k8s.io/utils/exec.ExitError` (`.ExitStatus()`).

**Testability (honest):** `PrimaryPodName` is envtest-testable (real apiserver,
fake Pods + Service). `Forward`/`Exec` adapters need a live kubelet → **not**
envtest-able; they stay thin and are proven only by manual/e2e. All command-layer
logic is unit-tested against mocked ports.

### 2.3 Forward orchestration helper (`cli/internal/cli/forward.go`)

Pure-ish orchestration over the ports + a network's status, reused by `run`,
`connect`, and (only when it already has a Kupo URL) not by `topup`:
1. **Readiness gate first** (new gap 3): reuse `requireFaucetReady`-style checks
   (`observedGeneration == generation`, not `Degraded`, fresh `Ready`/relevant
   sidecar condition via `kube.FreshCondition`) — mirroring `topup.go:61` and
   `up.go:105`, so `run`/`connect` fail with a clear "not ready" message instead
   of opaque per-connection forward errors.
2. Resolve Pod via `PrimaryPodName`.
3. Compute `[]PortForwardSpec` from **enabled + published** chain-API endpoints
   only (skip `nodeToNode`).
4. `Forward`, then build the host `YACD_*` env from assigned local ports.

---

## 3. The `YACD_*` env-var contract (`cli/internal/cli/envcontract.go`)

Defined once as exported, typed constants + two pure builders, with a package-doc
version note (findings 6/7 — the review accepted a focused file in package `cli`
over a new package, since values are produced by command orchestration and read
only here; but it must use the stable-`json`-tag/named-struct discipline of
`info.go`/`list.go`).

| Variable | host (`run`/`connect`) | in-pod (`exec`) | Condition |
|---|---|---|---|
| `YACD_NETWORK` | name | name | always |
| `YACD_NAMESPACE` | ns | ns | always |
| `YACD_NETWORK_MAGIC` | magic | magic | local mode (from `status.network.networkMagic`) |
| `YACD_OGMIOS_URL` | `ws://127.0.0.1:P` | `ws://<svc>…:port` | Ogmios published |
| `YACD_KUPO_URL` | `http://127.0.0.1:P` | ClusterIP URL | Kupo published |
| `YACD_FAUCET_URL` | `http://127.0.0.1:P` | ClusterIP URL | faucet published |
| `YACD_FAUCET_TOKEN` | token | **— (omitted)** | faucet published (host only) |
| `CARDANO_NODE_SOCKET_PATH` | — | `/ipc/node.socket` | exec only |

**Host URL derivation (new gap 1 — load-bearing):** `hostEnv` must
**parse the scheme from the published `status.endpoints.*.URL`**
(`url.Parse(...).Scheme`) and rebuild `<scheme>://127.0.0.1:<assigned local
port>` — NOT hard-code a scheme. Controller publishes `ws` for Ogmios, `http`
for Kupo/faucet (`defaults.go:50/77/103`); hard-coding `http` would hand a
WebSocket endpoint an `http://` URL and silently break Ogmios tooling — exactly
the CI failure the harness exists to catch. Unit test: `ws://` status URL ⇒
`ws://127.0.0.1:P`; `http://` ⇒ `http://127.0.0.1:P`.

**In-pod env (`exec`):** URLs reuse the ClusterIP forms already in
`status.endpoints.*.URL`; `YACD_NETWORK_MAGIC` from `status.network.networkMagic`;
`CARDANO_NODE_SOCKET_PATH` from the pinned constant. **`YACD_FAUCET_TOKEN` is
omitted in-pod** (see §4 — it would leak into the exec argv / audit logs and
socket tooling does not need it).

---

## 4. Security decisions (commit now — findings 2/8/13/30, gap)

- **`exec` is argv-only.** `PodExecOptions.Command` is documented "argv array.
  Not executed within a shell" and has no `Env` field, so env is injected by
  prepending the `env` binary: a `wrapExecCommand(env []string, argv []string)
  []string` helper builds
  `["env","CARDANO_NODE_SOCKET_PATH=/ipc/node.socket","YACD_NETWORK_MAGIC=<m>",…,userArgv…]`.
  Use an **ordered slice, never a map** (deterministic, test-stable). **Forbid
  `sh -c` and string concatenation** (quoting/injection hazard). Note: `env` must
  be on `PATH` in the cardano-node image (verify during manual proof).
- **No faucet token in-pod.** Remove `YACD_FAUCET_TOKEN` from the exec env (a
  Bearer token in `PodExecOptions.Command` lands in apiserver audit logs and
  `/proc/<pid>/cmdline`).
- **`run` injects `YACD_FAUCET_TOKEN` into the host child env** (incl. transitive
  deps). Accepted: it is an ephemeral localnet token. **Document** the behavior;
  do **not** add an opt-out (finding 30) — it would complicate the primary path
  for marginal benefit on a low-value credential.
- **`connect`'s `endpoints.json` is loopback-only and token-free.** Define it
  from a named, `json`-tagged "stable field names" struct (like `listItem`);
  keys = per-service loopback `*_URL` + `network`/`namespace`/`networkMagic`;
  **never `YACD_FAUCET_TOKEN`**. Create `.yacd/` `0700` and the file `0600`.
  Add `.yacd/` to the repo-root `.gitignore` (**currently absent — confirmed**)
  as an explicit checklist item.

---

## 5. Work breakdown

- **WB1 — kube access ports + adapter + exit type.** `kube/access.go` (types +
  three interfaces), extend `kube.Client`, retain REST client on `Adapter`,
  implement `PrimaryPodName`/`Forward`/`Exec` per §2.2; `kube/doc.go`; add the
  three interfaces to `.mockery.yml`; `moon run root:generate`. **+WB9 exit
  type.** Tests: envtest `PrimaryPodName`; forwarder/executor adapters thin
  (manual/e2e).
- **WB2 — env contract.** `cli/internal/cli/envcontract.go` + test (scheme
  parsing, conditional vars, host vs in-pod). Pure, fully unit-tested.
- **WB3 — forward orchestration.** `cli/internal/cli/forward.go` + test
  (readiness gate, spec computation skipping `nodeToNode`, host env assembly).
  Mocked ports.
- **WB4 — `yacd run`.** `cli/internal/cli/run.go` + test. `cobra` `ArgsLenAtDash`
  splits NAME from `-- cmd`; no cmd ⇒ `$SHELL`. `os/exec` child, env =
  `os.Environ()` + `YACD_*`, inherited stdio, `signal.NotifyContext` cancel.
  Forwards torn down on exit (defer). **Forward-drop mid-command (gap 6):** if
  `ForwardSession.Done()` fires before the child exits, cancel the child, surface
  `session.Err()` (e.g. `ErrLostConnectionToPod`) as a distinct diagnostic, exit
  non-zero.
- **WB5 — `yacd connect`.** `cli/internal/cli/connect.go` + test. Foreground;
  write `endpoints.json` (§4); print env block via the **sticky-error writer**
  pattern (finding 23). Supervision **kept as the proposal's v1 core** (finding
  18): on drop, re-establish with capped backoff and **re-resolve the primary Pod**
  (name changes across restarts); exit non-zero if unrecoverable. Block on
  `ctx.Done()` / unrecoverable drop.
- **WB6 — `yacd exec`.** `cli/internal/cli/exec.go` + test. Resolve Pod + pinned
  `cardano-node` container; build argv via `wrapExecCommand` (§4); TTY via
  `golang.org/x/term` (promote from indirect → direct, no new download); propagate
  remote exit code.
- **WB7 — `topup --await`.** `cli/internal/cli/topup_await.go` + `--await` /
  `--await-timeout` flags. After a successful POST, poll Kupo via the **vendored
  `kugo`** (`kugo.New(WithEndpoint(kupoURL)).Matches(ctx, kugo.Address(addr),
  kugo.OnlyUnspent())`), succeed when any `Match.TransactionID == result.TxID`,
  behind a **narrow CLI port** for unit-testability (do not hand-roll the decode).
  **No self-forward (decision):** consume `YACD_KUPO_URL` (set under `yacd run`)
  or an explicit `--kupo-url`; clear error if neither is set or Kupo is disabled.
  Preserve every `topup_trust.go` invariant (tests keep `AssertNotCalled`).
- **WB9 — exit-code propagation.** `cli/internal/cli/exit.go`: a single
  `exitError{code int}`. `main.go`: `errors.As` to return the carried code AND
  **suppress the duplicate stderr print** for that case (`main.go:48` currently
  always prints + returns 1). Mapping: `exec` → `utilexec.ExitError.ExitStatus()`;
  `run` → `*exec.ExitError` via `ProcessState` (`ExitCode()` when `Exited()`, else
  `128 + signal` so SIGINT ⇒ 130, since `ExitCode()` returns -1 for signal-killed
  children).
- **WB10 — root wiring.** `root.go` registers `run`/`connect`/`exec`; update
  `doc.go` (cli + kube contracts, findings 21/22); `root_test.go` version test
  unaffected.
- **WB11 — contract reference doc only.** A reference for the `YACD_*` contract +
  the `run`-vs-`exec` rule. **Bounded to a contract reference** — full how-to and
  `examples/e2e/` are Phase 5 (finding 33; avoid Phase-5 creep).

---

## 6. Testing strategy

- **Unit (mocked ports):** env contract incl. scheme parsing; forward spec
  computation (skips `nodeToNode`, only enabled+published); readiness gate;
  `run` env assembly + exit-code mapping + forward-drop behavior; `exec`
  `wrapExecCommand` argv ordering; `topup --await` poll loop + Kupo-disabled
  error + trust-gate `AssertNotCalled`.
- **envtest (`kube/client_envtest_test.go`):** `PrimaryPodName` (Service selector
  → Ready Pod) against a real apiserver.
- **Manual / e2e (dev stack on Kind):** the only place port-forward + in-pod exec
  truly run — `run go test`, `connect` + curl, `exec cardano-cli query tip`,
  `topup --await` confirms a UTxO, and the `env`-on-PATH check. Record a manual
  validation script in NOTES. No new Chainsaw case unless it genuinely needs the
  packaged operator (CLAUDE.md: don't duplicate envtest in Chainsaw).

---

## 7. Sequencing (7 PRs; §1 gate before PR1)

| PR | Contents | Notes |
|---|---|---|
| 1 | WB1 + WB9 | **blocks on §1 gate** — PodFinder body *is* the boundary decision |
| 2 | WB2 + WB3 | env contract + forward orchestration (pure, mock-tested) |
| 3 | WB4 (`run`) | primary path |
| 4 | WB6 (`exec`) | carries the §1 socket/container constants + argv-only injection |
| 5 | WB5 (`connect`) | foreground + supervised + `.yacd/` |
| 6 | WB7 (`topup --await`) | independent of WB1/WB3 (no self-forward) |
| 7 | WB10 wiring + WB11 doc | |

Per PR: `moon run root:generate` (when mocks/markers change), `moon run
root:check`, `moon run root:test`, `git diff --check`, and a dev-stack manual
proof for any path that only runs live.

---

## 8. Open items explicitly deferred / out of scope

- `yacd env` (cut), `connect --detach`/background daemon, namespace auto-delete,
  in-cluster-Job CI mode, k3d action support, snapshot/restore — all post-Phase-2
  (proposal §4/§9, plan backlog).
- Publishing host-access facts (`status.access`) in the CRD — a cleaner long-term
  contract than the pinned CLI constants, but deferred to keep Phase 2 CLI-only.

## 9. Residual risks for the human to weigh

1. **§1 boundary** — the one decision that needs explicit sign-off.
2. **`connect` supervision** is the only path with non-trivial logic that is
   *manual/e2e-only* testable (reconnect/backoff/pod re-resolve).
3. **`run` injects a faucet token into arbitrary child processes** — accepted as
   low-risk (ephemeral localnet token) and documented, not gated.
4. **Faucet `txId` ↔ kugo `TransactionID` format** assumed to be the same bare
   hex; confirm during WB7 manual proof.
