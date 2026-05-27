---
id: 024
title: Post-refactor manual functional test pass
date: 2026-05-26
status: complete
repos_touched: [yacd]
related_sessions: [015, 020, 021, 022, 023]
---

## Goal
After PRs #37 (ctrlkit), #38+#39 (cardanonetwork), #40 (cardanodbsync), and
#41 (cli) landed back-to-back, run a comprehensive manual functional test
pass against the local Kind/Tilt dev stack to build confidence the cumulative
refactor surface had not regressed. Stop and triage when bugs surface, get
user approval on fixes, then resume.

## Outcome
Goal met. All ten phases of the plan plus pre-flight passed. The pass
surfaced one real dev-loop bug (the published `cardano-testnet:11.0.1-yacd.4`
image lags PR #31's publisher enrichment, breaking CardanoDBSync on
`dev-up`); the fix was approved, implemented, validated end to end, and
landed as PR #42 (squash commit `f5bbfbb`). CI green, Kusari Inspector clean.
Master fast-forwarded; `test/post-refactor-validation` worktree and branch
removed; dev stack stopped.

## Key Decisions
- **Override + Tilt rebuild over a new release cut (option A1).** The user
  approved plumbing a `--default-cardano-testnet-image` flag through the
  manager and a `cardanoTestnet.image.*` chart value, with Tilt rebuilding
  the image into `:tilt` locally. Faster than cutting `cardano-testnet/v11.0.1-yacd.5`
  and durable (the seam future releases reuse whenever the published tag
  lags publisher code).
- **One PR with two commits, not two PRs.** The cardanonetwork and
  cardanodbsync controllers each have independent image-resolution paths
  but share the manager flag, chart value, and Tilt resource. Splitting
  would have shipped an intermediate state where only the primary node
  used the fresh image and CardanoDBSync follower-node still failed.
- **The kupo-off + faucet-on cascade is not a bug.** Mid-phase 3, I
  observed that disabling kupo while faucet is enabled produces
  `UnsupportedSpec` AND tears down the faucet Service + auth Secret +
  sidecar container. Reading `controller.go:93` + `delete.go:124-138`
  showed this is intentional `revokePrimaryFaucetExposure` security
  behavior. Surfaced to user, replanned Phase 3 to test the clean
  cascade path (single patch disabling both) which passed cleanly.
- **Matched Chainsaw's dbsync config exactly for verification.** First
  attempt at Phase 4 used `disable_all + inmemory`; second attempt with
  defaults hit a separate `setupURing: unsupported operation` crash in
  the Kind container runtime. Chainsaw's `phase6-managed` uses
  `inmemory + disable_all + parameters.maxParallelMaintenanceWorkers: 0
  + runtime.cache: false + runtime.epochTable: false` precisely because
  of these Kind constraints. Adopted the same shape and reached
  `Ready=True / Synced=True / lagBlocks=0 / dbBlockHeight=290`.
- **Manager restart vs. waiting 10 minutes for faucet auth Secret
  repair.** `faucetSecretRepairRequeueAfter = 10 * time.Minute` is the
  controller's only repair signal for the deleted Secret (no live Secret
  watch by design). Restarting the manager pod triggered immediate
  reconcile; documented the production behavior in the session report
  rather than waiting through the full 10-minute window.

## Changes
- `cmd/options.go`, `cmd/setup.go`, `cmd/options_test.go` — new
  `DefaultCardanoTestnetImage` field with `default:""`, plumbed into both
  Reconcilers, default-and-override parser test cases.
- `internal/controller/cardanonetwork/controller.go`,
  `internal/controller/cardanonetwork/builder.go`,
  `internal/controller/cardanonetwork/init_container.go` — added
  Reconciler + builder field, converted `cardanoTestnetImage` from a free
  function to a method on `primaryWorkloadBuilder` that honors the
  override; converted `faucetSourceAddressInitContainer` to a method for
  the same reason.
- `internal/controller/cardanonetwork/containers.go`,
  `internal/controller/cardanonetwork/resources.go` — call sites use
  `b.cardanoTestnetImage(...)` and `b.faucetSourceAddressInitContainer(...)`.
- `internal/controller/cardanonetwork/init_container_test.go` — new
  `TestCardanoTestnetImageHonorsInjectedOverride` covers all three
  containers (create-env init, faucet source-address init, default
  cardano-node).
- `internal/controller/cardanodbsync/controller.go`,
  `internal/controller/cardanodbsync/builder.go`,
  `internal/controller/cardanodbsync/settings.go` — same shape on the
  dbsync side; `followerNodeImage` consults the override.
- `internal/controller/cardanodbsync/builder_test.go` — new
  `TestFollowerNodeImageHonorsInjectedOverride`.
- `charts/yacd/values.yaml`, `charts/yacd/values.schema.json` — new
  `cardanoTestnet.image.{repository,tag,digest}` block with empty
  defaults (so released installs keep the built-in revision).
- `charts/yacd/templates/_helpers.tpl` — `yacd.cardanoTestnetImage`
  helper that returns the empty string when no repository is set so the
  deployment template can omit the flag.
- `charts/yacd/templates/controller-deployment.yaml` — conditionally
  adds `--default-cardano-testnet-image=...` arg when the helper renders
  non-empty.
- `.dev/build-cardano-testnet.sh` — NEW. `docker build`s the tools
  image from `containers/cardano-testnet/` using the absolute repo path
  (`cd $(dirname $0)/..`) so Tilt's local_resource cwd doesn't matter.
- `Tiltfile` — adds `CARDANO_TESTNET_IMAGE` constant, the
  `cardano-testnet-image` `local_resource` that runs the build script
  then `kind load`s the result under `:tilt`, the
  `cardanoTestnet.image.repository=...` + `cardanoTestnet.image.tag=tilt`
  helm overrides, and `cardano-testnet-image` added to the controller's
  `resource_deps`.

## Open Threads
- **Cut `cardano-testnet/v11.0.1-yacd.5`** so the published tag catches
  up to PR #31's publisher enrichment. The override unblocks the dev
  loop, but `init_container.go:cardanoTestnetImageRevision = "yacd.4"`
  is still the operator's built-in default for released installs.
  Documented in the PR body as the durable seam future releases reuse.
- **Faucet auth Secret repair latency.** `faucetSecretRepairRequeueAfter
  = 10 * time.Minute` is the only repair signal when the Secret is
  externally deleted (no live Secret watch). Operationally acceptable
  but a sharp edge; a future hardening pass could either watch the
  Secret directly (cost: list RBAC) or shorten the requeue (cost:
  reconcile pressure). The trade-off is documented in TECH_NOTES.md.
- **CardanoDBSync pre-creates managed Postgres auth Secret before
  validating networkRef.** `dbs-reject-missing` (Phase 5 test 4) showed
  the controller creates `<dbsync>-postgres-auth` before the network
  check fails. Owner-referenced, garbage-collected on CR delete —
  harmless but slightly aggressive ordering. Worth flagging in case a
  future controller-refactor cycle revisits the resolve-database vs.
  resolve-network ordering.
- **The kupo-off + faucet-on user experience is sharp.** Disabling kupo
  on a Ready CardanoNetwork with faucet enabled triggers
  `UnsupportedSpec` rejection AND tears down the faucet Service + auth
  Secret + sidecar container in the live Deployment. The token is
  regenerated on revert, breaking any cached topup clients. This is the
  documented security model; a future UX pass could surface the
  consequence in the rejection message ("faucet exposure will be
  revoked"), but the behavior itself is intentional.
- **INDEX.md is still missing a row for session 016** (carried over
  from prior sessions; not introduced or affected by this session).

## References
- PR #42: https://github.com/meigma/yacd/pull/42 (squash commit `f5bbfbb`)
- Plan file: `/Users/josh/.claude/plans/we-ve-recently-gone-through-tidy-iverson.md`
- Reference precursors: `.journal/015/SUMMARY.md` (introduced
  `EnrichGenesisHashes` in PR #31), `.journal/020/SUMMARY.md` (ctrlkit),
  `.journal/021/SUMMARY.md` (cardanonetwork refactor),
  `.journal/022/SUMMARY.md` (cardanodbsync refactor),
  `.journal/023/SUMMARY.md` (cli refactor).
- Session notes: `.journal/024/NOTES.md`

## Lessons
- **The dev stack's image-deps surface is broader than I assumed.**
  Tilt only rebuilt the operator and faucet images. The
  cardano-testnet tools image was pulled from `ghcr.io/` despite being
  the primary container's image too. PR #31 introduced a publisher
  change that downstream controllers (db-sync) depend on, but the
  published tag stayed at `yacd.4`. No one noticed because Chainsaw
  rebuilds the image locally (`.dev/scripts/test-e2e.sh:37`) and the
  Tilt path was apparently untested for CardanoDBSync between PR #31
  (2026-05-25) and this session. Lesson: when a controller's behavior
  depends on a container image's contents (not just its presence),
  Tilt must rebuild that image locally. The `--default-*-image` +
  chart value + `local_resource` pattern is now the durable seam.
- **Test plans get scope-cut by reality.** My Phase 4 plan envisioned
  a quick happy-path verification; in practice it consumed the entire
  fix cycle. Phase 7's "delete faucet auth Secret" expected near-instant
  repair; the controller's 10-minute requeue interval forced me to
  trigger a manager restart instead. Phase 3's expected silent kupo
  cascade was wrong about the contract entirely. Lesson: when running
  a manual test, the failure-handling loop is the load-bearing part of
  the plan, not the assertions.
- **Diagnosis order matters when a bug masquerades as a flaky test.**
  When db-sync first crashed with `NodeConfigParseError`, my first
  instinct was "the dbsync controller is broken." Following the
  `ByronGenesisHash not found` error backward through the configuration
  layers (db-sync arg → mounted ConfigMap → publisher → published tag
  → release commit) took ~7 sequential hops but the root cause was
  unmistakable by the end. Less rigorous diagnosis would have wasted
  effort patching downstream symptoms.
