---
id: 036
title: TEST_REPORT follow-through
started: 2026-05-29
---

## 2026-05-29 12:32 — Kickoff
Goal for the session: not yet stated by the user. `/session-new` was invoked to
prime the session; the specific request is pending. The standing campaign across
sessions 031–035 has been fixing `.journal/TEST_REPORT.md` findings one slice
per session, so the most likely next request is another TEST_REPORT
follow-through, but await the user's actual pick before starting implementation.

Current state of the world:
- `master` is at `dea708e` (`fix(controller): surface rejected PVC expansion in
  status`, PR #53). Local checkout clean.
- TEST_REPORT findings fixed so far: A3 (PR #49), A4 (PR #50), B1 (PR #51),
  B2 (PR #52), B6 (PR #53). Remaining open findings: D1, D2, D6, F0, F2/F4.
  Consult `.journal/TEST_REPORT.md` for concrete reproductions and suggested
  fixes before touching the relevant code paths.
- Session-startup note: the journal worktree carried an uncommitted session-035
  closeout (NOTES/INDEX/TECH_NOTES dirty, SUMMARY.md written on disk but never
  `git add -f`'d, so it was ignored and would have been lost on a clean
  checkout). Committed it as `122af54` (`docs(journal): close session 035`) and
  pushed before priming 036. A prior `wip: cleanup` (`9118e41`, already pushed)
  deleted some old 030 planning docs and the SNAPSHOT_* design files.
- Implementation worktree not yet created; `moon run root:dev-up` not yet run.
  Per `.session.md`, start the dev stack once after the implementation worktree
  is selected/created.

Plan: wait for the user's request. When it lands, create the implementation
worktree from fetched `master`, run `moon run root:dev-up`, load
`k8s-operator` and any other task-relevant skills, then implement.

## 2026-05-29 13:05 — Phase 0 (test harness) kicked off
Request: review and complete Phase 0 of `.journal/TEST_HARNESS_PLAN.md` (background
in `.journal/TEST_HARNESS_PROPOSAL.md`). Effort set to ultracode.

Journal-hygiene note: PLAN/PROPOSAL were deleted from `.journal/030/` by the
`wip: cleanup` commit and now exist UNTRACKED at `.journal/` root
(`git check-ignore` confirms `.gitignore:5:.journal/`); only
`.journal/030/TEST_HARNESS_DESIGN.md` remains tracked. Needs a decision at
close: force-add the root copies or restore the tracked 030 copies. Flagged to
user, not yet resolved.

Phase 0 is a de-risking/EVIDENCE phase, not feature code. Three deliverables:
(1) cold-start time-to-Ready for KinD+operator+representative localnet on a
standard hosted runner vs ~10–12m budget; (2) teardown completeness (delete
CardanoNetwork → all owned children GC'd); (3) host-access (run/exec/connect)
assumptions hold.

Recon workflow (6 agents) findings:
- ① UNKNOWN. e2e is `runInCI: false`; the Chainsaw smoke runs nowhere in CI, so
  zero historical cold-start evidence. Must measure fresh on `ubuntu-latest`.
  `.dev/scripts/test-e2e.sh` already encodes the kind+build+load+chainsaw path.
- ② Code-clean but unproven. All 11 child types carry controller ownerRefs
  (Deployment, PVC, 4 Services, faucet Secret, network-artifacts ConfigMap,
  artifact-publisher SA/Role/RoleBinding); no finalizers; no per-network
  cluster-scoped RBAC. Chainsaw only deletes the namespace, never asserts
  child GC after `delete cardanonetwork`.
- ③ Confirmed from code: all 4 chain-API containers (cardano-node, ogmios,
  kupo, faucet) co-locate in ONE primary Pod; all 4 Services select it on
  container-named ports (single port-forward serves run/connect); node socket
  `/ipc/node.socket` on shared EmptyDir mounted in cardano-node+ogmios
  (in-pod exec viable). All containers `PullIfNotPresent`.
- Budget flag: localnet sets NO cpu/mem requests (only mainnet does); with
  `Recreate` strategy an OOM evict wouldn't self-heal. Cold-start doubles as a
  schedulability test on 2 vCPU/7–8 GB.

Decisions (user): EVIDENCE-ONLY spike (throwaway branch, no permanent CI);
measure CardanoNetwork localnet ONLY (Ogmios+Kupo+faucet), no db-sync.
Deferred local `moon run root:dev-up` — evidence comes from the hosted runner,
not local iteration.

Did: created throwaway worktree `spike/phase0-ci-feasibility` (`.wt/...`) off
master; added `.github/workflows/phase0-feasibility.yml` (push-triggered,
ubuntu-latest, 35m cap) + `.dev/scripts/phase0-measure.sh` (times each
cold-start stage; verifies host-access via port-forward + in-pod
`cardano-cli query tip`; verifies teardown by deleting the CR and polling for
ownerRef'd children to hit 0). Committed `c7d1a7d`, pushed; CI run
`26659474962` in progress. Awaiting numbers, then write the go/no-go.

## 2026-05-29 13:28 — Phase 0 complete (GO)
First run (`26659474962`) FAILED at image_build: root `.dockerignore` ignores
all then re-includes only `**/*.go`+`go.{mod,sum}`, so `docker build .` strips
the embedded `internal/cardano/publicnet/profiles/*/*` assets and
`//go:embed profiles/.../*` errors `no matching files found`. Same breakage
hits `.dev/scripts/test-e2e.sh` (the documented `moon run root:test-e2e`), which
is `runInCI:false` so it went unnoticed since 2026-05-27. Cancelled it.
Fix: build with **ko** (`.dev/ko-build.sh` / `.dev/ko-build-faucet.sh`), the
operator's real build path (builds from the Go module tree → embeds resolve).
Committed `cd2c58e`, re-ran.

Second run (`26660099746`) SUCCEEDED — all three Phase 0 deliverables GREEN:
- ① cold-start to `Ready` = 27s; full pipeline (kind+preload+install+up) = 112s
  vs 10–12m budget. ~6× margin.
- ② teardown: `delete cardanonetwork` rc=0; 11 owner-ref'd children → 0 in 3s,
  no finalizer stall (closes proposal §10 unverified risk).
- ③ host-access: Ogmios/Kupo/faucet 200 via host port-forward; in-pod
  `cardano-cli query tip` OK — port-forward and exec agree on slot 130 / same
  hash. Budget: 0 OOM, 0 evictions.
CAVEAT: runner was 4 vCPU/16 GB (public-repo `ubuntu-latest` was upgraded from
2 vCPU/7 GB) — the 2-core private tier is untested but the margin makes it very
likely fine. Single sample; Ogmios/Kupo Docker Hub pulls didn't rate-limit this
run (preload them in Phase 4 to be safe).

Wrote go/no-go evidence to `.journal/TEST_HARNESS_PHASE0_RESULTS.md`; marked
Phase 0 DONE in `TEST_HARNESS_PLAN.md`; recorded the Phase 0 result + the
`test-e2e.sh` defect in `TECH_NOTES.md`. Journal hygiene: force-added the
untracked root `TEST_HARNESS_PLAN.md`/`PROPOSAL.md` and `git mv`'d
`TEST_HARNESS_DESIGN.md` from `030/` to root so the doc set is co-located and
the relative `./TEST_HARNESS_DESIGN.md` links resolve. Spike branch/worktree to
be discarded (evidence-only).

## 2026-05-29 13:58 — Fix the e2e build defect (PR #55)
User: fix the e2e defect, make it run in CI, open a PR, verify green, then pause.

Bigger than expected: the manager `docker build .` is the PRODUCTION build path
too — `release.yml` builds it via `docker/build-push-action` (`context: .`,
root Dockerfile). So the same `.dockerignore`/embed bug fails the release image
build on both arches, and the `release 1.0.0` release-please dry-run is RED
(confirmed runs 26660606449, 26657609056, 26652588873 — same
`pattern profiles/mainnet/*: no matching files found`). So the correct fix is
NOT "switch test-e2e.sh to ko" (that would leave release broken) — it's fixing
`.dockerignore` at the root, which repairs e2e + release + dry-run together.
Only one `//go:embed` in the manager tree: publicnet profiles.

Did (branch `fix/manager-build-embed` off master, worktree `.wt/...`):
- `.dockerignore`: add `!internal/cardano/publicnet/profiles/**`. Verified
  locally — `docker build .` now compiles the manager (previously-failing
  `go build -a -o manager ./cmd` step passes, image written).
- `.github/workflows/ci.yml`: add a dedicated `e2e` job (ubuntu-latest, 45m cap)
  running `moon run root:test-e2e`. Tools are proto-pinned (`.prototools`:
  kind/ko/chainsaw/kubectl/helm) so `moonrepo/setup-toolchain` auto-installs
  them; Docker is preinstalled. No manual tool installs (unlike the spike).
- `moon.yml`: `test-e2e` `runInCI: false → true`, added `.dockerignore` input.
Committed `ecdb942`, pushed, opened PR #55. CI run `26661842560` in progress
(`ci` job + the new `e2e` job). e2e runs the FULL chainsaw smoke incl. db-sync
(~25m unknown on hosted runner). Awaiting green; pause for user review after.
NOTE: concurrent session 037 owns `feat/d1-faucet-auth-recovery` (independent).

## 2026-05-29 14:26 — PR #55 GREEN (paused for review)
The e2e had never run on Linux/CI before (only macOS locally), so it surfaced
THREE stacked CI-only failures, each masking the next:
1. `.dockerignore` strips `go:embed` publicnet profiles → manager `docker build`
   fails. Fixed by re-including `internal/cardano/publicnet/profiles/**`
   (`ecdb942`). This ALSO unblocks release: `release.yml` builds the manager via
   `docker/build-push-action` (root Dockerfile), so the `release 1.0.0`
   release-please dry-run was red on both arches with the same embed error.
2. Chainsaw shells out to `moon run root:deploy`/`undeploy`, both
   `runInCI:false` → Moon filters them under CI=true ("No tasks found"). Flipped
   both to `runInCI:true` (`33ad2bf`); dev-up/dev-down stay false.
3. Chainsaw runs script `content` via `/usr/bin/sh` = dash on Linux (bash on
   macOS), so `set -euo pipefail` → "Illegal option". Audited all inline scripts
   (only bashism was pipefail) and made them `set -eu` POSIX-portable
   (`c2c6d4d`).
Result (CI run 26662661765): ci SUCCESS (1m), e2e SUCCESS (8m). The FULL smoke —
CardanoNetwork→Ready AND CardanoDBSync managed-Postgres — passes in 8m on a
hosted runner, so db-sync is NOT too heavy for CI. PR #55 mergeStateStatus=CLEAN.
Run 2's ci failure was a PRE-EXISTING flaky envtest
(`TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync`, "Condition
never satisfied") — passed on identical code in runs 1 and 3; flag in review, not
a regression. PAUSED for user review; NOT merged. TODO on merge: update
TECH_NOTES (supersede the earlier "use ko" note — the real fix is .dockerignore;
ko would have left release broken) and confirm the 1.0.0 dry-run goes green.

## 2026-05-29 14:40 — PR #55 merged
User approved. Squash-merged PR #55 as `0bb852d` on master
(`fix(build): re-include embedded publicnet profiles in docker context (#55)`).
Deleted the remote `fix/manager-build-embed` branch, removed the worktree +
merged local branch via `wt remove`, fast-forwarded local master to `0bb852d`.
Updated TECH_NOTES: the e2e now runs in CI (~8m, full smoke incl. db-sync);
documented the three CI-only root causes; corrected the build-path note (manager
PRODUCTION build is `docker build .` via release.yml's build-push-action, NOT ko
— ko is dev-only). Next: confirm the `release 1.0.0` release-please dry-run goes
green now that master carries the .dockerignore fix (release-please rebases its
branch on push to master).

## 2026-05-29 14:48 — Release 1.0.0 dry-run confirmed green
Release-please rebased the `release 1.0.0` PR (#7) onto master (head `44dcde2`,
includes `0bb852d`). Release Dry Run `26663558229` SUCCEEDED — prior runs
(`ddc0ad1`, `6d2481a`, pre-fix) failed on the manager embed build. Release path
unblocked; the e2e build defect work (PR #55) is fully done and verified end to
end (e2e in CI + release dry-run both green). Task complete.

## 2026-05-29 15:31 — Phase 1 (CLI lifecycle) implemented, PR #58 open
Request: review & complete Phase 1 of the test-harness plan; pause before merge.
Scope: CLI-driven identity + up/down/list verbs + drop the devconfig metadata
block (the one intentional breaking change).

Approach: recon workflow (6 agents) mapped the CLI (kube port, commands,
devconfig, render, mocks, CardanoNetwork API). Implemented inline:
- devconfig: removed the `Metadata` struct/field; Validate drops the
  metadata.name check (validateExplicitFields keeps its env param — it reads
  Spec.Network, not metadata).
- render: `CardanoNetwork(env, name, namespace)`; removed the dead
  `render.Namespace` helper.
- kube: `ErrNotFound` sentinel + `IsNotFound`; `DeleteCardanoNetwork`
  (idempotent on NotFound), `ListCardanoNetworks` (ns="" = all), `EnsureNamespace`
  (SSA namespace + ownership labels managed-by=yacd/created-by=yacd-cli);
  `WaitGone` poller. `GetCardanoNetwork` now wraps `ErrNotFound`.
- cli: `resolveIdentity` (NAME positional + ns default-to-NAME + DNS-1123);
  `deploy`→`up` (up.go; --wait default true, --timeout 12m, EnsureNamespace
  before Apply); new `down` (delete+WaitGone, --timeout 5m) and `list`
  (-A/-n/--json, table+json projection). info/topup adopted the same identity
  model. Removed the now-dead `KubeNamespaceResolver` type/field.
- examples: dropped metadata from all 5 Environment yacd.yaml files (local +
  4 public; the cardanodbsync-*.yaml examples are raw CRs, left alone).
- chainsaw e2e: `deploy -f ... --namespace yacd-smoke` → `up phase4-smoke -n
  yacd-smoke -f ...` (so the CI e2e now exercises `up`).
Mocks regenerated via `moon run root:generate`.

Tests: delegated the suite to a focused agent (test-only edits, report-don't-fix
production bugs); it correctly flagged my commandContext gofmt misalignment +
the missing nolint for client.Apply in EnsureNamespace (both fixed by me).

Verification: `moon run root:check` + `root:test` + `git diff --check` all green.
Proved exit criteria END-TO-END on the live dev cluster: up phase1 → Ready;
list (table+json w/ endpoints); ns ownership stamp present; info; down phase1 →
CR + owned children GC'd, list empty; idempotent re-down. Cleaned up test ns.

Review: adversarial workflow (5 dims → per-finding verify); 10/23 confirmed.
Fixed: stale `deploy` refs in README + cli/kube doc.go; a MISSED metadata block
in `examples/public-custom/yacd.yaml` (my earlier grep was `head`-truncated —
the review caught it); contextual `list` empty message; extra WaitGone
already-gone test. Deferred 2 low topup flag-polish items (out of Phase 1 scope).

Committed `a0290cb` (impl) + `b5e58a2` (review fixes); pushed; opened PR #58.
CI run `26665502208` in progress (ci + e2e, e2e now drives `yacd up`).
Breaking-change versioning: PR titled `feat(cli):` (minor); did NOT add a
BREAKING CHANGE footer (safe pre-1.0) — flagged for user's call at merge.
PAUSED before merge per request. Local dev stack still UP (kind-yacd-dev).
