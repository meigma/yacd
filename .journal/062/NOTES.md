---
id: 062
title: External-access P2 (devnet plumbing) + P3 (CLI resolver)
started: 2026-06-05
---

## 2026-06-05 07:27 — Kickoff

Goal for the session: continue session 060's external-access work. P1 (the
API/operator half) shipped in PR #112 (`dfa20b8`). The remaining phases from
`.journal/060/EXTERNAL_ACCESS_DESIGN.md` are **P2 (devnet plumbing)** and **P3
(CLI resolver)**. The user asked me to review session 060, take note of the
remaining work, and pause before proceeding.

Current state of the world:
- **P1 is live on master** (PR #112, squash `dfa20b8`): `spec.chainAPI.{ogmios,
  kupo}` now carry `service.{type,nodePort}` (ClusterIP default | NodePort;
  nodePort 0=auto, CRD `Maximum=32767`, Go-enforced 30000 floor) and an
  optional `externalURL` string. The controller renders NodePort Services,
  mirrors `externalURL` additively into `status.endpoints.{ogmios,kupo}.
  externalURL`, validates Go→`UnsupportedSpec`/Degraded, and `MutateService`
  now preserves a k8s-assigned NodePort.
- **Faucet removal is fully done** (sessions 059+061, released as **v0.2.0**).
  The CLI funds via direct Ogmios/Kupo txns (`internal/cardano/tx`); there is
  no faucet HTTP service. So P3's resolver applies to the post-faucet funding
  path with no faucet-specific branch.
- master HEAD is `dfa20b8`. A concurrent docs branch (`docs/mkdocs-site`,
  PR #91) and the journal branch exist; nothing else is in flight that I know
  of, but re-fetch/rebase before any push (shared origin/master moves).

### Remaining work (from `.journal/060/SUMMARY.md` "Open Threads" + design §7)

**P2 — devnet plumbing (CLI, `yacd devnet`).** Make the localhost URL route.
devnet owns both the k3d cluster and the default network spec, so pin constants
on both sides:
- Create the k3d cluster with host port mappings —
  `--port "1337:30137@loadbalancer"` and `--port "1442:30442@loadbalancer"` —
  in `cli/internal/cluster/k3d/ensure.go`'s `create` (stable create-time
  `--port`; serverlb proxies host port → same node port, hence NodePort
  required). k3d pinned v5.9.0.
- Author the devnet default spec (`cli/internal/cli/devnet.yaml`, a byte copy
  of `examples/local/yacd.yaml`, drift-guarded by `TestDefaultDevnetEnvIsValid`)
  with `chainAPI.{ogmios,kupo}.service.type: NodePort` + pinned nodePorts
  (30137/30442) and `externalURL: ws://localhost:1337` /
  `http://localhost:1442`.
- **Needs a live k3d run to verify** host routing (envtest can't prove it).
- Constraints: nodePorts 30000–32767; pinned host ports (1337/1442) can clash
  with something already bound — surface a clear error (relates to local-
  lifecycle P7 hardening).

**P3 — CLI resolver (CLI).** Add a shared resolution chain in the forward path:
explicit flag (`--ogmios-url`/`--kupo-url`) → ambient `YACD_OGMIOS_URL`/
`YACD_KUPO_URL` env → `status.endpoints.*.externalURL` (probe with a short
connect timeout, cache the verdict) → ephemeral port-forward fallback (today's
behavior). Wire it into the funding/`run` paths. Current force-forward lives in
`cli/internal/cli/forward.go` (`forwardEndpoints`) and `envcontract.go:
loopbackURL`; `connect` stays forward-only (the remote-cluster tool).

Ordering: P2 before P3 (P3's resolver needs status to carry `externalURL`, and
a live P2 devnet is the smoke target for P3).

Plan: pause here per the user's instruction. Next, confirm scope/approach with
the user, then likely start with P2. Dev stack startup (`moon run root:dev-up`)
is the implementation-session prerequisite — but P2/P3 want a **k3d** devnet
(not the Kind/Tilt stack) for live verification; clarify which environment to
bring up when proceeding.

## 2026-06-05 13:04 — P2 implemented, reviewed, live-proven, PR #114 opened

**P2 is done and up as PR #114** (branch `feat/devnet-external-access-p2`, on
master `dfa20b8`). Approved plan:
`~/.claude/plans/please-propose-a-plan-delightful-graham.md`.

### What shipped (10 files, +264/−13, all CLI)
- `cluster.go`: `PortMapping{HostPort,NodePort}`, exported consts
  Ogmios 1337/30137 + Kupo 1442/30442, `DefaultPortMappings`, `Spec.PortMappings`
  populated by `DefaultSpec()`.
- `k3d/ensure.go` `create()`: renders `--port "H:N@loadbalancer"` per mapping
  (preallocated slice — lint) + `isHostPortConflict`/`hostPortList` friendly
  collision error (the bare `"bind:"` marker was dropped in review).
- `cli/devnet.yaml`: fully-spelled `chainAPI` block (ogmios/kupo
  enabled+image+port + `service{NodePort,nodePort}` + localhost `externalURL`) —
  the full spelling is FORCED by `devconfig.validateExplicitFields` (chainAPI
  present ⇒ enabled/image/port required). `embed.go` comment updated: devnet.yaml
  now intentionally diverges from `examples/local` (which stays ClusterIP).
- `cli/devnet.go`: NEW — banner advertises `endpointAddress()` = `externalURL`
  when set else in-cluster `URL` (this was the review blocker; see below).
- Tests: `cluster_test.go` (DefaultSpec mappings + nodePort range), `k3d_test.go`
  (exact `--port` set via `portFlagValues` + collision case), `devnet_test.go`
  (yaml↔cluster-constant cross-check + banner shows localhost), `testhelpers_test.go`
  (stub now carries externalURL), `k3d_live_test.go` (gated: real `--port` + dial).

### Adversarial review (workflow `wf_d4ae6b22-ea6`): 7 confirmed findings
- **BLOCKER (fixed):** `printDevnetUp` printed `endpoint.URL` (in-cluster DNS),
  not `externalURL` — the whole payoff was invisible. Now uses `endpointAddress`.
- Fixed: over-broad `"bind:"` collision marker; flimsy `indexOf-1` test →
  exact-set `portFlagValues`; no `--port` count assertion → exact-set covers it;
  gated live test now exercises mappings + dials the host ports.
- Documented (not fixed, approved scope): pre-P2/pre-P1 cluster has NodePort
  Services but no host mapping → localhost dormant until recreate (P3 probe
  degrades gracefully). Minor scheme/host literal duplication left as-is (port —
  the routing-critical number — IS cross-checked to the constant).

### Live k3d proof (real cluster, then torn down)
- `yacd devnet` came up: cluster + operator + network Ready. Banner showed the
  **in-cluster** URL, NOT localhost — see the finding below.
- **Routing PROVEN:** created a standalone NodePort Service on 30137/30442 backed
  by the real Ogmios/Kupo pods (byte-equivalent to what a P1 operator renders);
  `curl http://localhost:1337/health` → **HTTP 200 + live chain data**
  (slot 1508, networkSynchronization 1.0, conway); `localhost:1442/health` →
  **HTTP 200** (Kupo). The k3d `--port` mapping routes host→NodePort end-to-end.
- Gated live test (`YACD_CLUSTER_LIVE=1 TestEnsureClusterLive`) **PASS** (35.9s):
  real k3d v5.9.0 accepts `--port "H:N@loadbalancer"` and the serverlb publishes
  the host ports (dial succeeds). Devnet torn down, ports 1337/1442 freed.

### ★ KEY FINDING — release ordering (P1 must be RELEASED for the payoff)
`yacd devnet` installs the operator **by appVersion tag = v0.2.0**, but P1
(#112) merged AFTER the v0.2.0 release. Live-confirmed: the installed **CRD** (from
the master-based embedded chart) accepts + stores the NodePort/externalURL spec,
but the **v0.2.0 operator binary** ignores it → renders ClusterIP, no externalURL
in status. So P2's devnet payoff is dormant until an operator release containing
P1 ships (then the CLI installs it and host routing lights up). P2 is
forward-compatible + harmless to merge meanwhile (banner stays honest via the
URL fallback; nothing reads the host ports until P3). **Decision owed from user:**
cut a P1 operator release now, or merge P2 and let the next release activate it.
TECH_NOTES has no bullet for this yet — add one at close.

### Remaining this session
- P3 (CLI resolver) not started — depends on status carrying externalURL, which
  needs a P1 operator at runtime (ties into the release-ordering decision).
- Await user decision on release ordering; then either P3 or release work.

## 2026-06-05 15:55 — Cutting v0.2.1 (P1+P2 operator release)

User chose **"cut a P1+P2 operator release"** (so `yacd devnet` installs a
P1-containing operator) and the **root-cause** tripwire fix.

- **PR #114 (P2)** squash-merged to master (`ca0a049`); all CI green.
- **release-please PR #113** = `chore(master): release 0.2.1` (root component):
  bumps Chart.yaml `version`/`appVersion` → v0.2.1, CHANGELOG lists #112 (P1) +
  #114 (P2). Merging it tags v0.2.1 → release.yml publishes operator image +
  chart + CLI binaries; the embedded chart's appVersion becomes v0.2.1 so the
  CLI installs `ghcr.io/meigma/yacd:v0.2.1`.
- **Tripwire papercut (session 060 recurrence):** 6 ssa version assertions
  hardcoded `v0.2.0` and would red master at the appVersion bump. Fixed at the
  ROOT in **PR #115** (`test(operator): track chart appVersion in operator-install
  version tripwires`, merged `13122b6`): the 6 assertions now read
  `embeddedChartAppVersion(t)` (helper already in render_test.go), so releases
  never re-break them. Scenario literals (v0.9.9 refuse case) untouched.
- **Stale-base gotcha:** release-please did NOT rebase #113 onto #115 (test:
  commits don't trigger regeneration), so #113's `ci` was failing (v0.2.1 chart +
  old v0.2.0 tripwire). Fixed by rebasing #113's branch onto master (now incl.
  #115) and force-pushing (`e0cbed5`) — clean rebase, release commit replays on
  top. Auto-merge (squash) armed on #113; watching CI → merge → release.yml.
- **TECH_NOTES to add at close:** (1) the release-ordering rule — `yacd devnet`
  installs the operator by appVersion tag, so a feature touching BOTH the operator
  and the devnet path only works end-to-end once a release carrying the operator
  half ships; (2) prefer the dynamic appVersion tripwire (PR #115) over hardcoded
  re-syncs; (3) when a `test:`/non-releasable commit lands after a release PR is
  open, rebase the release-please branch onto master before merge or its CI is
  stale-red.

## 2026-06-05 16:10 — v0.2.1 PUBLISHED + P2 proven end-to-end (loop closed)

Release pipeline all green (no red master this time — the dynamic tripwire fix
worked):
- **Release workflow: success** — `ghcr.io/meigma/yacd:v0.2.1` + chart + CLI
  binaries published to GHCR; v0.2.1 GitHub release left as a **draft** for a
  human to Publish (GHCR artifacts are already live).
- **master CI: success** (commit `d5c0b92`). Tripwire papercut avoided.

**End-to-end P2 proof against the RELEASED v0.2.1 operator** (rebuilt CLI from
master, embedded chart appVersion now v0.2.1 → installs `:v0.2.1`):
- Banner shows **`Ogmios: ws://localhost:1337` / `Kupo: http://localhost:1442`**
  with `Operator: v0.2.1` (vs. the in-cluster URL the v0.2.0 operator produced).
- Operator-rendered services are **NodePort 30137/30442** (P1 renders them).
- `status.endpoints.{ogmios,kupo}.externalURL` **mirrored** to the localhost URLs
  (additive to the in-cluster `url`).
- `curl http://localhost:1337/health` → **HTTP 200 + live chain data**;
  `localhost:1442/health` → **HTTP 200** — routed through the operator's own
  NodePort services (no hand-made svc). Devnet torn down; ports freed.

**Session deliverables:** P2 (#114), tripwire root-cause (#115), release v0.2.1
(#113) — all merged. Merged worktrees cleaned up. No Kind/Tilt dev stack was ever
started (P2 is k3d/CLI work), so nothing to dev-down.

**Still open / next:** P3 (CLI resolver) not started — now unblocked (a P1+P2
operator is released, so status carries externalURL at runtime). At session-close,
add TECH_NOTES bullets: (1) external-access P2 shipped in v0.2.1 (devnet host
mappings + NodePort devnet spec + externalURL banner); (2) the operator-by-
appVersion release-ordering rule; (3) dynamic appVersion tripwires (#115) + the
rebase-the-release-PR-on-late-test-commits gotcha.

## 2026-06-05 17:53 — P3 (CLI resolver) implemented, reviewed, live-proven, PR #116

External-access **P3 is done and up as PR #116** (branch
`feat/external-access-p3-resolver`, base master `d5c0b92` = v0.2.1). Approved
plan covers the design. This COMPLETES the external-access 3-phase design.

### What shipped (15 files, +835/−151, all CLI)
- **`forward_resolve.go` (new):** `resolveChainAccess` — per-endpoint precedence
  (explicit flag > ambient `YACD_*_URL` env > probed `status.externalURL` >
  port-forward), forwarding ONLY the fall-through subset (one `Forward` call).
  Injected `endpointProber` (default: 2s scheme-agnostic TCP dial honoring ctx);
  `chainOverrides`; `connectChain` (run's fetch+gate+resolve entry).
- **`forward.go`:** `chainAccess` replaces `connectedSession` — NILLABLE session,
  nil-safe `Close/Done/Err` (`Done()`→nil channel so run reports no false "lost
  connection"). `forwardAll` is connect's forward-all path.
- **`envcontract.go`:** URL-based `hostEnvFromURLs`/`documentFromURLs` +
  `loopbackURLs` (forward adapter); dropped the localPort-based
  hostEnv/hostBindings/newEndpointsDocument. `chainEndpoints` now always returns
  both entries (endpoint nil when unpublished) so rungs 1–2 are independent of
  status.
- **run.go / wallet_fund.go / wallet.go:** run + funding use the resolver;
  `--ogmios-url`/`--kupo-url` flags on `wallet add`/`topup`. **No trust gate** —
  funding submits a locally-signed tx (key never leaves CLI), Kupo read-only.
- **connect stays forward-only** (connectNetwork→forwardAll); exec/podEnv
  untouched.
- Test determinism: removed externalURL from the shared `readyNetwork` stub (so
  forward-path tests don't flake on the real prober); set it only in
  `fundedNetwork` (banner) + `networkWithExternalURLs` (resolver tests).

### Adversarial review (`wf_6c4ec7de-af2`): 3 confirmed MINOR findings, all fixed
Root cause: `chainEndpoints` returned nil when `Status.Endpoints==nil`, so the
resolver loop skipped rungs 1–3 in that edge (operator never produces it). Fixed:
chainEndpoints always returns both entries; `resolveChainAccess` fails fast
("no chain-API endpoints to reach") when nothing resolves (restores run's
guard); pinned `YACD_KUPO_URL` in the ambient-env test. No blockers. 2nd commit
`1769949`.

### Live proof (k3d, v0.2.1 operator, torn down)
- `yacd run devnet` → `YACD_OGMIOS_URL=ws://localhost:1337` / `…:1442`, **NO
  forward**; both route HTTP 200 from inside run.
- `wallet add --topup --await` (externalURL) + `wallet topup --ogmios-url
  --kupo-url` (explicit override) → both **confirmed on-chain**.
- 2nd ClusterIP network (`examples/local`, no externalURL) → `run` falls back to
  port-forward (`ws://127.0.0.1:50407`). Full precedence matrix proven.
- CI on #116 pending at journal time.

### External-access design COMPLETE
P1 (v0.2.1), P2 (v0.2.1), P3 (#116). At session-close add TECH_NOTES: P3 resolver
+ precedence + the no-trust-gate rationale + chainAccess nillable-session
contract. Optional follow-up: the ogmigo ws-1006 genesis-config warning (issue
#110) still prints during funding (non-fatal).

## 2026-06-05 18:25 — Close

Session 062 closed. The external-access 3-phase design is COMPLETE.

**Merged (all squash-merged to master):**
- #114 — P2 (devnet host-port mappings + NodePort devnet spec + externalURL banner)
- #115 — operator-install version tripwires track chart appVersion (papercut fix)
- #113 — release **v0.2.1** (P1 + P2; `ghcr.io/meigma/yacd:v0.2.1` + chart + CLI)
- #116 — P3 (CLI resolver: flag > YACD_* env > probed externalURL > port-forward),
  squash `f2501b7`

**Handoff state:** master at `f2501b7`; local master fast-forwarded; all session
worktrees removed (P2/tripwire/release/P3). No Kind/Tilt dev stack was started
(used k3d devnet, torn down). SUMMARY.md + INDEX (062→complete) + TECH_NOTES
(external-access P2/P3 + release-ordering rule + dynamic tripwires) written.

**Open for a future session:** publish the v0.2.1 GitHub draft release if desired;
ship a CLI release for the P3 resolver (optional, separate decision); ogmigo
ws-1006 funding warning (issue #110); docs still describe the pre-faucet model
(README/DESIGN + MkDocs #91). Stale in-progress INDEX rows 051/052 predate this
session (052 = docs #91) — left untouched.
