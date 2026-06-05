---
id: 060
title: New session
started: 2026-06-03
---

## 2026-06-03 18:54 — Kickoff
Goal for the session: TBD — session started via `session-new`; awaiting the user's
actual request.

Current state of the world:
- Latest closed session is 058 (`yacd install` command shipped in PRs #94/#96).
  Session 059 is still `in-progress` (CLI-native wallets + faucet-removal design &
  plan paused for review at `.journal/059/WALLET_REARCH_PLAN.md`); 051 and 052 also
  remain `in-progress` in INDEX.
- `master` is at `5383f76` (`feat(cli): add yacd install command (#96)`); origin
  in sync.
- Active Worktrunk worktrees: `journal/jmgilman`, `feat/faucet-wallet-secret`
  (ahead 1: `f96f1b6` genesis-funded faucet wallet), `docs/mkdocs-site` (PR #91,
  diverged). No implementation worktree selected for this session yet.
- Carried open threads: PR3 for `yacd install` (uninstall + OCI version fetch);
  local-lifecycle P7 hardening; the session-059 faucet-removal plan; docs PR #91
  follow-ups; operator draft releases v0.1.0/v0.1.1 await a human Publish;
  release-please root PR #7; TEST_REPORT F2/F4; test-harness `yacd-env` Action.

Plan: wait for the user's request before selecting/creating an implementation
worktree, starting the dev stack, or doing substantive work.

## 2026-06-03 21:15 — External-access design converged; doc written
Goal landed: improve how the CLI connects to local clusters (the `yacd topup`
per-command port-forward is slow); make `yacd devnet` fast without a background
`connect`.

Design discussion (no code yet). Explored the relevant surfaces (see the doc's §2
for file:line grounding): status endpoints are single in-cluster URLs
(`status.go:setEndpointStatus`), Services are all ClusterIP, the CLI is hardwired
to forward (`forward.go` + `envcontract.go:loopbackURL`), devnet's k3d create has
no `--port` mappings, and the checked-in config is the Environment doc consumed
only by `up`/`devnet`.

Key turns in the discussion:
- Initially I objected to localhost-in-CRD on layering grounds and proposed a
  separate CLI config file. User pushed back (correctly): localhost is a *valid
  assertion* for a co-located devnet, and one CRD field unifies devnet + remote.
  Reversed — dropped the separate CLI config file entirely.
- Corrected my own error: the checked-in config IS the Environment doc
  (`devconfig`, consumed by `up -f`), not a (nonexistent) Viper config file.
- Verified k3d mechanics: create-time `--port` is STABLE (not experimental);
  only `k3d cluster edit --port-add` is experimental. serverlb proxies host port
  → same node port → needs NodePort (30000–32767) or Traefik. Pinning at create
  time avoids the experimental path.

Converged design (in `EXTERNAL_ACCESS_DESIGN.md`):
1. API/operator: `spec.chainAPI.{ogmios,kupo}.service.{type,nodePort}` +
   `externalURL`; mirror externalURL → `status.endpoints.*.externalURL` (additive).
2. devnet: pin host+node ports, k3d `--port "1337:30137@loadbalancer"` etc.,
   author default spec with NodePort + localhost externalURL.
3. CLI resolution (shared forward path): flag > YACD_* env > status.externalURL
   (probed) > port-forward fallback. Benefits `run` + topup `--await`.
Decided: spec→status mirror; keep the liveness probe. Faucet/topup-faucet leg
out of scope (token trust gate + session 059 faucet-removal plan).

Phased: P1 API+operator, P2 devnet plumbing, P3 CLI resolver (order matters).

Next: user reviews the design doc. No implementation worktree created yet; dev
stack not started.

## 2026-06-04 07:30 — P1 plan approved & banked (gated on 059)
Design LGTM'd. Effort set to ultracode. Drafted the P1 (API + operator) plan in
plan mode: explored via 3 Explore agents + 2 Plan agents (one adversarial),
verified the risky bits myself.

Plan approved and saved durably to `.journal/060/PHASE1_PLAN.md` (the
`~/.claude/plans/` copy is transient). P1 scope: add `service.{type,nodePort}` +
`externalURL` to `spec.chainAPI.{ogmios,kupo}`, render NodePort Services, mirror
externalURL → `status.endpoints.*.externalURL`.

Key findings/decisions baked into the plan:
- **Landmine found:** `ctrlkit/resources.MutateService` does
  `current.Spec.Ports = desired.Spec.Ports` wholesale → wipes a k8s-assigned
  NodePort every reconcile (thrash), despite its doc comment claiming it
  preserves NodePort. Only 2 callers (cardanonetwork + db-sync); db-sync is
  ClusterIP (resources.go:380,447) so a guarded fix is a no-op for it. Existing
  `...CorrectsPrimary/Ogmios/KupoService...` tests (controller_test.go:1023/1083/1143)
  tamper ClusterIP→NodePort and expect restoration — a `desired.Type==NodePort`
  guard keeps them green.
- User-confirmed (AskUserQuestion): externalURL scheme validation **lenient**
  (absolute+host, ws/wss/http/https); NodePort fix in **shared MutateService**
  (guarded); validation **Go→Degraded UnsupportedSpec** + simple CRD markers
  (Enum on type, Maximum on nodePort — NOT Minimum=30000, which would reject
  0=auto). externalURL is a **sibling** of `service`, mirrored as a peer of `url`.
- No network-identity fingerprint includes these fields (safe); values.schema.json
  unrelated; envtest can't reproduce the thrash (no NodePort allocator) so the
  ctrlkit unit test is the real guard.

**HOLDING implementation** per the user's "finish 059 first" directive: P1 is
logically independent of the faucet removal but edits the SAME files
(settings.go/resources.go/status.go/builder.go/cardanonetwork_types.go), so
building on pre-059 master would cause rebase conflicts. Awaiting user's call:
start now (eat rebase) vs. wait for 059. No worktree created; dev stack not
started.

## 2026-06-04 19:33 — P1 implemented + green; pre-existing master-red found
059 landed on master (faucet deleted: #108, wallet verbs #106, #107; release
0.2.0 #87). Implemented P1 in worktree `.wt/feat-chainapi-external-access` off
`origin/master`. Dev stack WAIVED for P1 (envtest-only; user-confirmed). Ultracode
turned off mid-session — implemented directly (no workflow).

Committed `9666ecd` on `feat/chainapi-external-access`:
- API: `ServiceExposureSpec{type,nodePort}` + `externalURL` on Ogmios/Kupo;
  `externalURL` on shared `ServiceEndpointStatus` (also surfaces on the dbsync
  CRD's metrics/postgres endpoints — additive, harmless).
- settings resolvers map serviceType/nodePort/externalURL (shared
  `resolveServiceExposure`); builders render Type + `pinnedNodePort`.
- validation (Go→UnsupportedSpec): `validateChainAPIServiceExposure` +
  `validateExternalURL` wired into `chainAPISettings`; markers = Enum on type,
  Maximum=32767 on nodePort (NOT Minimum=30000, to keep 0=auto legal).
- **MutateService fix** (the landmine): `mergeServicePorts` preserves a
  k8s-assigned NodePort by name when desired type=NodePort & desired==0; verbatim
  (clears tampered NodePort) for ClusterIP. Guarded so the 3 existing
  `...CorrectsService...` tests stay green; no-op for db-sync (ClusterIP).
- status: mirror trimmed spec externalURL into endpoints inside the
  service-present branch (nil-guarded helpers; minimal signature, no plumbing).
- tests: 3 ctrlkit unit tests (the thrash guard — envtest can't reproduce it),
  6 builder reject-table rows, 2 controller reconcile tests (NodePort render +
  auto-assign preservation), 1 envtest (externalURL mirror + pinned NodePort +
  CRD Maximum rejection).

Verification: `moon run root:generate` clean; `moon run root:check` PASS (lint,
drift guard, helm, chainsaw); `moon run root:test` — `cardanonetwork` +
`ctrlkit/resources` PASS with all new tests.

**Pre-existing master breakage found:** master CI is RED since the 0.2.0 release
(#87) — it bumped Chart.yaml appVersion to v0.2.0 but left 6 ssa version
"tripwire" tests asserting v0.1.1 (`cli/internal/operator/ssa`). Unrelated to P1
but blocks its CI. User chose a separate fix PR: opened **PR #111**
(`fix/operator-version-tripwire`, test-only, v0.1.1→v0.2.0, full `root:test`
green). Next: once #111 merges, rebase `feat/chainapi-external-access` on green
master and open the P1 PR. P1 not yet pushed.

## 2026-06-04 19:40 — #111 merged, P1 rebased + PR opened
Per user: merged **#111** (squash `b61089b`), removed the fix worktree, deleted
its remote branch. Rebased `feat/chainapi-external-access` onto origin/master
(`b61089b`) cleanly (no conflicts — #111 only touched ssa test files). Post-rebase
`moon run root:test` is FULLY green (ssa + cardanonetwork + ctrlkit/resources all
ok, no FAIL). Pushed and opened **P1 PR #112**
(`feat(cardanonetwork): add NodePort/externalURL service exposure`). Awaiting CI +
review/merge. Worktree `.wt/feat-chainapi-external-access` kept until #112 merges.

State: P1 = PR #112 open. Design + plan banked (`EXTERNAL_ACCESS_DESIGN.md`,
`PHASE1_PLAN.md`). Remaining phases: P2 (devnet k3d --port + pinned NodePort +
localhost externalURL) and P3 (CLI shared resolver: flag > YACD_* env > probed
status.externalURL > port-forward fallback).

## 2026-06-04 19:46 — P1 merged
#112 CI green (ci, e2e, cardano-tools-image, Kusari), squash-merged to master
(`dfa20b8`). Removed the feat worktree, deleted its remote branch. **P1 (API +
operator) is landed.** Dev stack was never started (waived for P1) — nothing to
tear down. Remaining: P2 (devnet) and P3 (CLI resolver); both will need the dev
stack / a k3d run for live verification.
