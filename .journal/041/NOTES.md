---
id: 041
title: TEST_REPORT follow-through
started: 2026-05-29
---

## 2026-05-29 15:56 — Kickoff
Goal for the session: Not yet stated. Session opened via `session-new`; awaiting
the user's actual request. Expected direction, based on recent sessions, is
continued work on the remaining `.journal/TEST_REPORT.md` findings, but this is
unconfirmed.
Current state of the world:
- Sessions 037 (D1), 038 (D2), and 039 (D6) closed out their TEST_REPORT
  findings; the journal notes list F0 and F2/F4 as the remaining concrete
  findings.
- Session 040 was started but did no work (kickoff only); the user chose to
  leave it in-progress and open a fresh session 041 rather than reuse it.
- `master` is at `c7825f8` (PR #58, CLI up/down/list lifecycle + CLI-driven
  identity). Working tree clean.
Plan: Wait for the user's direction, then inspect `.journal/TEST_REPORT.md` and
the relevant live controller code before proposing or implementing the next fix
and its manual validation path. Start the dev stack from the implementation
worktree once one is selected.

## 2026-05-29 16:10 — Goal set: Test Harness Phase 2 plan
The user's actual request (not TEST_REPORT): review `TEST_HARNESS_PLAN.md`
Phase 2 (host access + integration contract), develop an implementation plan,
run adversarial agents against it for refinement, ensure hexagonal +
readability/maintainability standards, and **present the plan for human review
before executing** (do NOT implement yet).

Phase 2 scope: `YACD_*` env-var contract, shared client-go port-forward engine,
`yacd run` (scoped forwards + env + host exec), `yacd connect` (foreground +
`.yacd/<network>/endpoints.json`), `yacd exec` (in-pod, node socket), and
`topup --await` (poll Kupo). Optional `yacd env`.

Grounding gathered (read in full): TEST_HARNESS_{PLAN,PROPOSAL,DESIGN}.md; all
of `cli/internal/{cli,kube,render,devconfig}`; `internal/cardano/primarypod`;
CardanoNetwork status endpoint types; `cli/cmd/yacd/main.go`. Key facts:
- CLI is part of the **root module** (`github.com/meigma/yacd`, no `cli/go.mod`),
  but currently imports only `api/v1alpha1` — never `internal/...`. Reusing
  `internal/cardano/primarypod` (selector labels, container name, node socket
  path) is *technically allowed* but crosses that established boundary →
  central architectural decision for the panel.
- `k8s.io/client-go v0.36.1` is a direct dep and ships `tools/portforward` +
  `tools/remotecommand`; port-forward/exec are otherwise greenfield in cli/.
- Node socket path `/ipc/node.socket` lives in a *controller-internal* const
  (containers.go), NOT in status and NOT in primarypod. `exec` needs its value.
- `main.go` maps every error → exit 1; `run`/`exec` need a typed exit-code
  error to propagate child status.
- Existing kube.Client port wraps controller-runtime client.Client; forwarding
  and exec need `*rest.Config`/SPDY → new ports on the kube adapter.
Next: draft plan → adversarial workflow (multi-lens review + verify) →
synthesize → present via ExitPlanMode.

## 2026-05-29 16:55 — Drafted, adversarially reviewed, finalized Phase 2 plan
- Wrote `PHASE2_PLAN_DRAFT.md` (detailed, file-by-file, grounded in real code).
- Ran workflow `wf_ba25fb6d-22f` (42 agents, ~2.05M tok): 5 adversarial lenses
  (hexagonal/readability/feasibility/security/scope) → per-finding skeptical
  verification → completeness/synthesis. 33 findings survived, 3 refuted.
- Independently re-verified the highest-risk claims against client-go v0.36.1
  and the repo: portforward `New`/`GetPorts` (draft's `ForwardedPorts` was
  wrong), `spdy.RoundTripperFor`+`NewDialer` (draft's `NewDialerForRestConfig`
  doesn't exist), remotecommand `StreamWithContext` (no env injection), `kugo
  v1.3.0` vendored (`Matches`/`Address`/`OnlyUnspent`/`TransactionID`),
  controller URL schemes `ws/http/http` (`defaults.go:50/77/103`), `.yacd/`
  absent from `.gitignore`.
- Wrote `PHASE2_PLAN.md` (FINAL) folding in all 8 top changes + 5 decisions + 6
  gaps. Headline resolutions:
  - Boundary = **Option A+A′** (discover pod via Service selector; pin socket
    path + container name as CLI-local consts; the draft's "guard test" is
    impossible — controller const is unexported; fence status-publish OUT of
    Phase 2). **Zero-code gate, sign off before PR1.**
  - Wiring = **extend `kube.Client`** with the 3 new methods (the only seam
    returns `kube.Client`, not `*Adapter`); forbid type-asserting to `*Adapter`.
  - `exec` = **argv-only** `wrapExecCommand` (ordered slice, no `sh -c`); **drop
    `YACD_FAUCET_TOKEN` in-pod** (audit-log/`/proc` leak).
  - `hostEnv` parses scheme from status URL (don't hard-code; ws vs http).
  - `topup --await` = reuse `kugo`, **no self-forward** (need `YACD_KUPO_URL`/
    `--kupo-url`).
  - exit codes via single `exitError`; `run` 128+signal (SIGINT→130); suppress
    main.go duplicate stderr.
  - `yacd env` **cut** from Phase 2.
- Presenting plan for human review; NOT implementing until approved.

## 2026-05-29 17:20 — Approved; PR-by-PR implementation with pre-merge pauses
User approved the plan. Directive: complete each PR as described, **pause before
each PR merge for human review**, then continue to the next.
- Created impl worktree `feat/cli-host-access-ports` at
  `.wt/feat-cli-host-access-ports` from `master` (c7825f8).
- `moon run root:dev-up` succeeded (operator ready ~60s; Tilt bg, Kind
  `kind-yacd-dev`, logs `.run/yacd-dev/tilt.log`). Stack stays warm for the
  PR3/PR4 live-path proofs.
- Verified client-go v0.36.1 call signatures I'll use directly: `spdy.NewDialer`,
  `remotecommand.NewSPDYExecutor`, `StreamOptions{Stdin,Stdout,Stderr,Tty}`,
  `util/exec.ExitError.ExitStatus()`, `scheme.ParameterCodec`.
- Starting **PR1** (WB1 + WB9): extend `kube.Client` with
  `PrimaryPodName`/`Forward`/`Exec` (+ access types), retain restConfig+REST
  client on Adapter, regen mocks, add `ResolveExit`/`exitError` + main.go wiring,
  envtest for PrimaryPodName. No live-path proof in PR1 (forward/exec adapters
  are manual/e2e-only, proven in PR3/PR4).

## 2026-05-29 17:50 — PR1 done & open: PR #59 (PAUSED for human review)
Branch `feat/cli-host-access-ports` (commit 7464c27). Files: `kube/access.go`
(+`access_test.go`, `access_envtest_test.go`), `kube/client.go` (Client iface +3
methods, Adapter retains restConfig+restClient, NewClient builds clientset),
`kube/doc.go`, `cli/exit.go` (+`exit_test.go`), `cmd/yacd/main.go` (ResolveExit),
regenerated `mocks/client.go`, `go.mod` (+moby/spdystream indirect via tidy).
- `go mod tidy` was required: `transport/spdy`/`portforward` pull in
  `moby/spdystream`, missing from go.sum. (Heads-up for future PRs touching new
  k8s.io subpackages.)
- Verified call signatures live (client-go v0.36.1): `portforward.New`/`GetPorts`
  (NOT ForwardedPorts), `spdy.RoundTripperFor`+`NewDialer`, `remotecommand`
  `StreamWithContext`/`StreamOptions{...,Tty}`, `util/exec.ExitError`.
- `moon run root:check` + `root:test` green; new tests verified executing:
  TestPrimaryPodName* (envtest), TestForwardSessionLifecycle, TestResolveExit,
  TestExitErrorMessage.
- Adversarial review of the diff (1 agent): verdict **ship**; addressed both
  non-blockers (added forwardSession lifecycle test locking Done/Err ordering;
  tightened Forward godoc re ctx cancel).
- PR #59 opened against master; CI (`ci`+`e2e`) pending. Per user directive:
  **paused before merge for human review.** Next on approval: PR2 = WB2 (env
  contract) + WB3 (forward orchestration) from updated master.
