---
id: 056
title: Manual devnet functional test → KUBECONFIG-handling bug fix
date: 2026-06-02
status: complete
repos_touched: [yacd]
related_sessions: [049, 053, 054, 055]
---

## Goal
Run a full manual functional test of the `yacd devnet` all-in-one local k3d
lifecycle (shipped P1–P6 across sessions 049/053/054/055, PR #90) — the feature
had only one gated happy-path live test — then address whatever the test found.

## Outcome
**Met.** Drove a comprehensive + adversarial manual pass (P0–P18) against real
k3d. Nearly every behavior matched spec (cold k3d fetch, bring-up to Ready, node
on the pinned `v1.32.5+k3s1`, operator SSA, **wallet funded 100k ADA on-chain**,
all host-access verbs + `YACD_*` contract, lock contention, Ctrl-C converge,
short-timeout rollback, orphan reconcile incl. the transient-error guard,
targeting precedence, capture/restore, `--bare`, one real-`~/.kube/config` run).
The pass uncovered and **live-confirmed a HIGH-severity bug chain**, which was
then planned, fixed, verified, and **merged as PR #92** (squash `79761f2`). All
CI green (`ci`, `e2e`, `cardano-tools-image`, Kusari). Machine restored to
baseline after both the test and the fix verification; no collateral.

## Key Decisions
- **Skipped the Tilt/KinD dev stack** — the devnet path is k3d + the *published*
  operator image; the operator dev stack is irrelevant to CLI-only work.
- **Fix scope = full hardening + read-path Status change** (user choice): root
  fix alone clears the chain in normal flow, but the defense-in-depth + read-path
  fixes make a silent destructive recreate impossible regardless of how the path
  went bad.
- **Root fix is one idea**: the cluster's health/identity must always be keyed to
  the *recorded* kubeconfig (where its context lives), never the ambient
  `KUBECONFIG`. A kubeconfig-load probe failure is an environment error, not
  cluster unhealth, so it must abort — not delete+recreate.
- **F4 fix = clear current-context on empty prior** rather than let k3d's
  `cluster delete` repoint it (observed leaving kubectl on a prod EKS context).

## The bug chain (F1→F2→F3, HIGH) + F4
Root: on the healthy no-op path `k3d.infoFor` returned `defaultKubeconfigPath()`
(ambient KUBECONFIG), not the recorded path. So switching KUBECONFIG between
`yacd devnet` runs (common): **F1** operator install built from the wrong path →
fails; **F2** the record was saved with that wrong path *before* the failing
operator step → persisted corruption; **F3** the next `devnet` health-probed the
corrupted path, `statusVia` collapsed the load error into `Healthy=false`, and
`EnsureCluster` **delete+recreated the healthy cluster, silently destroying it**
(chain data is ephemeral, but the destruction was silent on an expected no-op).
**F4**: from an unset current-context, up→down left kubectl on an arbitrary
(prod) context. Also minor: doubled `acquire cluster lock:` wrap; raw
`signal: killed` on create timeout.

## Changes
Fix landed in PR #92 (all under `cli/internal/`):
- `cluster/k3d/ensure.go` — `infoFor(name, kubeconfigPath)`; no-op reports the
  recorded path, create/heal reports the default; create timeout/cancel message.
- `cluster/k3d/k3d.go` + `list.go` — typed `probeConfigError`; `statusVia` aborts
  on a kubeconfig-load probe error instead of marking unhealthy.
- `cluster/cluster.go` (+ `lifecycle/manager.go` Status, `cli/orphan.go`, mock
  regen) — `Provisioner.Status(ctx, name, kubeconfigPath)` probes the recorded file.
- `lifecycle/manager.go` — `Down` clears current-context on empty prior.
- `clusterstate/file/lock.go` — drop the duplicate lock-error wrap.
- Tests added in `k3d`, `lifecycle`, new `kube/context_test.go`.

## Open Threads
- **Recon/plan corrections worth remembering**: Kupo port is **1442** (not 3001);
  the `yacd.meigma.io/install=operator` label *is* stamped on CRDs but CRDs are
  excluded from the prune GVK set, so the never-prune guarantee holds.
- **Environment (not a yacd bug)**: a cold devnet bring-up pulls the large
  `cardano-testnet`/`cardano-tools` images from ghcr.io; in-cluster DNS to ghcr.io
  was intermittently flaky (ImagePullBackOff) but recovered within the 12m default.
  A devnet image-preload option / preflight is **P7** material.
- **P7 (only remaining local-lifecycle phase)** still open: typed failure taxonomy,
  Docker/disk preflight, `devnet down --purge`, `--isolate-kubeconfig`, WSL2/ARM
  guards, first-run banner.
- Carried: operator GitHub *draft* releases v0.1.0/v0.1.1 await a human Publish;
  release-please root PR #7; deterministic primary-sidecar manager-envtest refactor;
  TEST_REPORT F2/F4; test-harness `yacd-env` Action + examples/how-to; docs session.

## References
- PR #92 (`79761f2`, squash-merged) — the fix. Reviewed the full manual pass in
  this session's `NOTES.md` (P0–P18 + the F1–F4 findings + live fix verification).
- Prior: `.journal/055/SUMMARY.md` (P6 devnet shipped), `.journal/049/
  LOCAL_LIFECYCLE_{PLAN,DESIGN}.md`.

## Lessons
- **A manual functional test earns its keep on the paths automation can't reach.**
  The gated `YACD_DEVNET_LIVE` test isolates `KUBECONFIG`+`XDG_STATE_HOME` to temp
  dirs and never drifts them — exactly the dimension where the HIGH-severity bug
  lived. The bug was found by *changing KUBECONFIG between runs*, which no test did.
- **"Healthy probe failed" must not be conflated with "delete the cluster."** A
  config/context load error and an unreachable API are different failures; only the
  latter justifies a destructive recreate. Conflating them turned a config typo into
  silent data loss.
- **Tie cluster identity to the runtime/record, not the ambient env.** Every place
  that judged or located the cluster via the current `KUBECONFIG` instead of the
  recorded kubeconfig was a latent footgun (probe, operator client, saved record,
  status, teardown restore). The fix was to thread the recorded path everywhere.
- **k3d `cluster delete` repoints kubectl's current-context** to an arbitrary
  remaining entry; on a machine with prod contexts that is dangerous. yacd must own
  restoring the user's pre-devnet state (including "no current-context").
