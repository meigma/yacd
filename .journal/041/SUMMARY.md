---
id: 041
title: Test harness Phase 2 — host access and the YACD_* contract
date: 2026-05-30
status: complete
repos_touched: [yacd]
related_sessions: [030, 036]
---

## Goal
Review `TEST_HARNESS_PLAN.md` Phase 2 (host access + the integration contract),
develop an implementation plan, refine it with adversarial agents against the
hexagonal/readability bar, present it for human review, then implement it
PR-by-PR — pausing before each merge for review.

## Outcome
Goal met. Designed the Phase 2 plan, hardened it with a 5-lens adversarial
workflow (42 agents: hexagonal/readability/feasibility/security/scope → per-
finding verification → synthesis; 33 findings survived, 3 refuted), presented
it, and implemented all of Phase 2 across **7 squash-merged PRs** (#59, #60,
#61, #62, #63, #66, #67), each given a focused adversarial review and — where a
runtime path existed — a live proof on Kind. Phase 2 is complete: the local
story works end to end (`up` → `run`/`connect`/`exec` reach the live network
through forwards → `topup --await` confirms funding on-chain → `down`), and the
`YACD_*` contract is documented.

## Key Decisions
- §1 boundary = **Option A + A'** -> the CLI resolves the primary Pod from the
  operator's published node-to-node Service selector (no `internal/...` import,
  staying `api/v1alpha1`-pure), and pins the node container name +
  `/ipc/node.socket` as documented CLI-local constants (the draft's "guard test"
  was impossible — the controller const is unexported). The cleaner
  `status.access` contract was fenced out of Phase 2 as operator-side work.
- Extend the existing `kube.Client` port with `PrimaryPodName`/`Forward`/`Exec`
  -> the only command-layer seam returns `kube.Client`, not `*Adapter`; widening
  the interface keeps the existing factory + single mock and forbids
  type-asserting to `*Adapter`.
- `exec` is **argv-only** (`env KEY=VAL … cmd`, never a shell) and **omits
  `YACD_FAUCET_TOKEN` in-pod** -> a Bearer token in `PodExecOptions.Command`
  would leak to apiserver audit logs and `/proc`; socket tooling does not need it.
- Host URLs **derive their scheme from the published status URL** (Ogmios stays
  `ws://`) rather than hard-coding -> a hard-coded scheme would silently break
  WebSocket tooling, the exact CI failure the harness exists to catch.
- `topup --await` requires Kupo via `--kupo-url`/`YACD_KUPO_URL` with **no
  self-forward** -> a Kupo-only self-forward is inconsistent (standalone topup
  also needs a reachable faucet); under `yacd run` both URLs are already set.
- `yacd env` **cut** from Phase 2 -> redundant with `run`'s `$SHELL` drop-in and
  `connect`'s file.
- One **focused adversarial reviewer per PR** (not the full multi-agent
  workflow) -> the user confirmed this cadence is right for self-contained CLI
  diffs; the plan got the heavy workflow.

## Changes
All under `cli/` unless noted; one PR each, squash-merged to `master`.
- #59 `kube/access.go` + `kube.Client` extension (`Forward`/`Exec`/
  `PrimaryPodName`, Adapter retains REST config/client) + `cli/exit.go`
  (`exitError`/`ResolveExit`) + `cmd/yacd/main.go` exit-code wiring.
- #60 `cli/envcontract.go` (the `YACD_*` builders) + `cli/forward.go`
  (`connectNetwork`, readiness gate, `requireFreshStatus` shared with topup).
- #61 `cli/run.go` — `yacd run` (scoped forwards + env + host exec + exit-code
  propagation + forward-drop handling).
- #62 `cli/exec.go` — `yacd exec` (in-pod, argv-only, socket env, TTY;
  `golang.org/x/term` promoted to a direct dep).
- #63 `cli/connect.go` — `yacd connect` (supervised forwards + token-free
  `.yacd/<network>/endpoints.json`); consolidated the chain-endpoint vocabulary
  (`hostBindings`); `.gitignore` += `.yacd/`.
- #66 `cli/topup_await.go` + `topup.go` — `topup --await` (poll Kupo via a new
  `UTxOConfirmer` port wrapping vendored `kugo`).
- #67 `docs/host-access.md` + README — the `YACD_*` contract reference and verb
  documentation.

## Open Threads
- **Phase 3** (first operator/chart release), **Phase 4** (the `yacd-env`
  GitHub Action), and **Phase 5** (`examples/e2e/` + a Diátaxis how-to) are not
  started. The full how-to/examples were deferred from #67 by design.
- **Flaky controller envtest** `TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync`
  (`internal/controller/cardanonetwork`, `Eventually`/"Condition never
  satisfied" under CI load) intermittently blocks merges — flaked on #61 (1×)
  and #67 (2× in a row, passed on the 3rd). It is unrelated to the CLI work; a
  de-flake (longer timeout / sturdier wait) is a worthwhile separate PR.
- `connect` detects a dropped forward **lazily** (on next use), documented in
  `runConnect`; fine for an idle session.
- Pre-existing: the Ogmios client pulls in the discontinued Gorilla WebSocket
  toolkit via `ogmigo` (no called vulns) — replacing/upstreaming is a durable
  follow-up.

## References
- PRs: #59, #60, #61, #62, #63, #66, #67 (all squash-merged to `master`,
  ending at `e45ad76`).
- Plan/design: `.journal/041/PHASE2_PLAN.md` (+ `PHASE2_PLAN_DRAFT.md`),
  `.journal/TEST_HARNESS_{PLAN,PROPOSAL,DESIGN}.md`.
- Doc shipped: `docs/host-access.md`.
- Prior: `.journal/030/SUMMARY.md` (harness design), `.journal/036/SUMMARY.md`
  (Phase 0 + Phase 1).

## Lessons
- **Live proofs caught what unit tests and static review did not:** the `exec`
  `--help` example was wrong (argv-only does not expand `$VAR`); the cardano-node
  container root FS is read-only (generate keys in writable `/ipc`); `kugo`'s
  default logger spams stderr (silence with `ogmigo.NopLogger`); and an idle
  port-forward only notices pod deletion on next use. Run the real thing per PR,
  not just the tests.
- A genuinely flaky test in a shared package becomes a recurring tax on every
  PR's merge, even docs-only ones. Worth quarantining/fixing at the source.
