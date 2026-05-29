---
id: 036
title: Test harness Phase 0 + e2e build fix + Phase 1 CLI lifecycle
date: 2026-05-29
status: complete
repos_touched: [yacd]
related_sessions: [029, 030, 035]
---

## Goal
Session was primed for TEST_REPORT follow-through, but the user redirected it to
the test-harness plan (`.journal/TEST_HARNESS_PLAN.md`): complete **Phase 0**
(validate feasibility), then **Phase 1** (CLI identity + lifecycle verbs). A
build defect surfaced during Phase 0 became a third, prerequisite piece of work.

## Outcome
All three met.
- **Phase 0 — GO.** A throwaway hosted-runner spike proved KinD + operator +
  a representative local `CardanoNetwork` cold-starts to `Ready` in ~27s (full
  pipeline ~112s) vs the 10–12m budget; `delete cardanonetwork` GC's all 11
  owned children in ~3s; the `run` (port-forward) and `exec` (in-pod socket)
  host-access paths both work and agree on the chain tip. Evidence + go/no-go in
  `.journal/TEST_HARNESS_PHASE0_RESULTS.md`. Spike was evidence-only (no PR;
  branch discarded).
- **e2e build fix — merged (PR #55, `0bb852d`).** Phase 0's first CI run failed
  on the manager image build: the root `.dockerignore` stripped the `go:embed`
  publicnet profiles. The same bug broke the production `release.yml` manager
  build, so the `release 1.0.0` dry-run was red. Fixed at `.dockerignore`
  (repairs e2e + release together), wired the Chainsaw e2e into CI as a new `e2e`
  job, and confirmed the 1.0.0 dry-run went green after merge.
- **Phase 1 — merged (PR #58, `c7825f8`).** CLI-driven identity + `up`/`down`/
  `list` verbs + the breaking `metadata`-drop from the devconfig `Environment`.
  Proven end-to-end on the live dev cluster and gated by the CI e2e (now drives
  `yacd up`).

## Key Decisions
- Phase 0 measured **CardanoNetwork-only**, evidence-only on a hosted runner ->
  the harness `up` target; CI is the only faithful place to measure cold-start.
- Fix the e2e at **`.dockerignore`, not by switching test-e2e.sh to ko** ->
  `docker build` is the *production* manager build path (release.yml), so ko
  would have fixed the test but left releases broken.
- Phase 1: **rename `deploy` -> `up`** (not keep both) and apply the
  NAME-default-namespace model to `info`/`topup` too -> a coherent CLI; `up foo`
  then `info foo` must target the same namespace.
- **`EnsureNamespace` via server-side apply** with ownership labels -> idempotent
  create-or-stamp that doesn't clobber labels owned by other field managers
  (e.g. the PSS label the Chainsaw test sets).
- **`WaitGone` polls through the `Client` port** (like `WaitReady`) using a new
  `ErrNotFound` sentinel + `IsNotFound` -> mockable, and the single source of
  not-found semantics.
- Did **not** add a `BREAKING CHANGE:` footer for the metadata drop -> safe
  pre-1.0; `feat(cli):` minor bump. Flagged for the user at merge.

## Changes
- `.dockerignore` - re-include `internal/cardano/publicnet/profiles/**` (PR #55).
- `.github/workflows/ci.yml`, `moon.yml`, `test/chainsaw/manager-smoke/` - new
  CI `e2e` job; `test-e2e`/`deploy`/`undeploy` flipped to `runInCI: true`;
  chainsaw scripts made dash-portable (`set -eu`); `deploy` -> `up` (PR #55, #58).
- `cli/internal/devconfig/config.go` - drop the `Metadata` struct/field.
- `cli/internal/render/render.go` - `CardanoNetwork(env, name, namespace)`;
  remove dead `Namespace` helper.
- `cli/internal/kube/{client,wait,doc}.go` - `Delete`/`List`/`EnsureNamespace`,
  `ErrNotFound`/`IsNotFound`, `WaitGone`; mocks regenerated.
- `cli/internal/cli/` - new `identity.go`, `up.go` (was `deploy.go`), `down.go`,
  `list.go`; `info`/`topup` adopt `resolveIdentity`; removed dead
  `KubeNamespaceResolver`.
- `examples/*/yacd.yaml` (5 Environments), `README.md`, `cli/.../doc.go` - drop
  metadata / refresh CLI docs.
- Journal: `TEST_HARNESS_PHASE0_RESULTS.md` added; PLAN/PROPOSAL/DESIGN
  force-added + co-located at `.journal/` root.

## Open Threads
- Test-harness **Phase 2** (host access: `run`/`exec`/`connect`, `topup --await`,
  the `YACD_*` env contract) is next per the plan. Phase 0 confirmed the
  host-access assumptions it depends on.
- Phase 4 (CI action) should preload Ogmios/Kupo (Docker Hub rate-limit jitter)
  and validate on a 2-core private-tier runner (Phase 0 ran on 4 vCPU/16 GB).
- Remaining `.journal/TEST_REPORT.md` findings (D1/D2/D6/F0/F2/F4) — the
  original 036 intent — were NOT worked here; concurrent sessions 037–040 have
  been taking those.
- Two low-priority `topup` flag-polish review items were deferred as out of
  Phase 1 scope.

## References
- PR #55: https://github.com/meigma/yacd/pull/55 (`0bb852d`) — e2e build fix.
- PR #58: https://github.com/meigma/yacd/pull/58 (`c7825f8`) — Phase 1 CLI.
- `.journal/TEST_HARNESS_PHASE0_RESULTS.md`, `TEST_HARNESS_PLAN.md`,
  `TEST_HARNESS_PROPOSAL.md`.
- Prior: `.journal/030/SUMMARY.md` (harness design), `.journal/029/SUMMARY.md`
  (adversarial pass).

## Lessons
- The e2e had **never run on Linux/CI**, only macOS locally, which hid three
  stacked CI-only failures (dockerignore embed, Moon `runInCI:false` filtering of
  nested `moon run` calls, dash vs bash `set -o pipefail`). "Passes locally" is
  not "passes in CI" when the local shell is bash and the runner's is dash.
- A test-only build path that diverges from the production build path (here:
  `docker build` for release vs ko for the dev stack) lets a release-breaking
  bug hide behind a `runInCI:false` task.
