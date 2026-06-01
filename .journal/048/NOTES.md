---
id: 048
title: F0 redesign — PR-B1 (remove network-artifacts ConfigMap + custom-public)
started: 2026-06-01
---

## 2026-06-01 07:11 — Kickoff

Goal for the session: pick up from session 047 and execute **PR-B1** of the F0
redesign — remove the `<net>-network-artifacts` ConfigMap concept and
custom-public entirely, repoint curated-public node + ogmios to read
`/state/artifacts` from the PVC, re-source the three ConfigMap-coupled signals
(`ArtifactsReady`, sync-timing, db-sync identity) onto single-path/served data,
and make db-sync single-path serve. This is the F0 mainnet unblock (mainnet
renders with zero ConfigMap). API-breaking but pre-1.0 and intended.

Current state of the world:
- master is at `231ccde` (PR #77, PR-C merged). F0 PR-A (#75) + PR-C (#77) are
  done+merged. PR-B1 is fully planned and APPROVED; the durable plan lives at
  `.journal/047/PR-B1-PLAN.md`.
- The prior PR-B WIP branch (`feat/f0-delete-network-configmap`) was discarded
  clean (no commits). PR-B1 should branch fresh off master.
- PR-B1 is a large (~23-file), compile-coupled change with no intermediate
  checkpoint per the plan: do the whole spine, then compile.
- Remaining after PR-B1: PR-B2 (delete publisher binary/module + Dockerfile
  stage + new cardano-testnet image) and PR-D (remove `report` verb, pin manager
  cardano-tools image to a published digest, DESIGN.md, drop e2e build+load
  hack). Carried: TEST_REPORT F2/F4; test-harness Phases 3-5.
- Dev stack: per session 046 notes the stack may have been left UP/orphaned on
  `kind-yacd-dev`; verify/clean before `dev-up` for this session's
  implementation worktree.

Plan: per `.journal/047/PR-B1-PLAN.md`. Awaiting user go-ahead before starting
implementation (user asked to review the plan and confirm readiness first).

## 2026-06-01 09:06 — PR-B1 implementation progress (ultracode)

User set effort=ultracode and approved a refined plan (saved to
`~/.claude/plans/ok-can-you-propose-noble-codd.md`). Ran a 6-agent verification
workflow first; it confirmed the approved plan and surfaced 4 refinements (folded
in): (1) sync-timing must be **serve-fetch** not a Status.Network field —
local systemStart is only known in-pod, so the plan-file's §2/§5 were
unimplementable; (2) sequence db-sync single-path before networkartifacts
Consumer*/Producer* deletion; (3) strip dangling create-env publisher env/volume;
(4) gate ArtifactsReady on the spec-based discriminator + serve-container
readiness, not status.endpoints.

Worktree: `.wt/feat-f0-delete-network-configmap` (off master `231ccde`). Dev
stack UP on `kind-yacd-dev`. Baseline `root:test` was green.

Done (source compiles clean, `go build ./...` exit 0):
- API removals (custom profile + NetworkConfigSource + ConfigSource CEL +
  Status.Artifacts); CRD+deepcopy regenerated.
- publicnet custom removal; custom-public controller machinery + `public_profile_source.go`
  deleted; CLI/db-sync custom validators removed.
- db-sync single-path serve (deleted ConfigMap fallback + networkConnection +
  the `networkArtifacts *ConfigMap` builder params + the cross-controller
  `NetworkArtifactsConfigMapName` contract); identity re-sourced from
  `Status.Network` fingerprint via new `networkIdentityFingerprint` helper.
- networkartifacts controller pkg Producer*/Consumer*/Connection deleted
  (artifacts.go + connection.go removed; doc.go rewritten).
- cardanonetwork ConfigMap+publisher machinery deleted (artifacts.go file gone;
  setDeploymentFaucetAuthTokenHash moved to faucet_auth.go); create-env init kept
  minus publisher env/volume; dead ArtifactData/publicConnectionJSON cleaned from plan.go.
- curated-public node+ogmios repointed to `/state/artifacts` (drop `/profile`
  ConfigMap mount; ogmios mounts node-state PVC RO for public too).
- ArtifactsReady from new `primaryArtifactsReadyCondition` (serve-container
  readiness, mirrors OgmiosReady); sync-timing rewritten to HTTP-GET
  shelley-genesis.json from `status.endpoints.artifacts.url` (new
  `cardanoNetworkTimingProber` seam mirroring the Ogmios prober; graceful empty-URL).
- RBAC: dropped serviceaccounts/roles/rolebindings markers + configmaps marker
  (cardanonetwork) + the 4 dead `.Owns` watches; chart `rbac-manager.yaml`
  mirrored to controller-gen output (serviceaccounts/roles/rolebindings gone;
  configmaps lost `delete`, now grouped with pvc). Verified against a direct
  controller-gen run.
- chainsaw `manager-smoke` rewritten: assert NO network-artifacts ConfigMap +
  artifacts Service + serve sidecar + `status.endpoints.artifacts.url`; kept the
  PR-C db-sync HTTP assertion; deleted publisher/Status.Artifacts assertions.
- examples/public-custom deleted; other examples + docs clean.

Bulk of steps 6-9 done via a delegated impl agent (reviewed diff + serve-fetch +
ArtifactsReady myself). Unit-test cleanup delegated to a second agent: cardanodbsync
+ cli/devconfig + publicnet test pkgs green; cardanonetwork test pkg in progress
(agent resumed after a transient API overload).

Remaining: finish cardanonetwork unit tests → add targeted new coverage
(public preprod+mainnet render w/ no ConfigMap; profile:custom CRD rejection) →
`root:check`/`root:test`/`root:test-e2e` → adversarial review → PR
`feat(cardanonetwork)!: ...` documenting API break + one-time db-sync identity churn.
Frozen wire test must stay green (NetworkArtifactHash wire shape unchanged).

## 2026-06-01 ~10:15 — PR-B1 DONE → PR #78 open, CI running

PR-B1 implemented, verified, and pushed. **PR #78**
(`https://github.com/meigma/yacd/pull/78`, base master), 2 commits on
`feat/f0-delete-network-configmap`:
- `6754f06` feat(cardanonetwork)!: remove network-artifacts ConfigMap + custom-public; read node config from the PVC (F0 PR-B1)
- `4d4f73e` docs(cardanonetwork): refresh package doc (adversarial-review residue)

Verification (all green):
- `moon run root:check` — gofmt, lint, generated drift, **RBAC byte-equivalence
  vs controller-gen** (`test/chart`), helm lint, chainsaw manifest validity.
- `moon run root:test` — all pkgs incl. manager envtests; re-ran fresh/uncached
  on both controllers (49s + 68s). Frozen wire test green.
- `moon run root:test-e2e` — Kind chainsaw `manager-smoke` **PASS (156.86s)**:
  local network creates with NO ConfigMap (serve sidecar + artifacts Service +
  ArtifactsReady=True + published endpoint), managed-Postgres db-sync fetches
  over HTTP → Synced, cleanup verified.
- grep-to-zero across repo for all removed symbols — clean.
- Adversarial multi-agent review (6 dims → verify, 10 agents): **no correctness
  defects**; 3 confirmed findings all LOW/doc-only (stale doc.go + dead godoc
  link) → fixed in `4d4f73e`. 1 dismissed (empty networkartifacts ctrl pkg —
  intentional).

CI on #78 running (ci/e2e/cardano-tools-image/Kusari pending) — NOT merged
(user's call after CI). Dev stack left UP on `kind-yacd-dev` (session not closed).

### CI flake chase + MERGE (later 2026-06-01)
PR #78 needed 4 extra commits to get CI green — all TEST-ONLY (product code
untouched, e2e passed throughout). The flaky test was the known load-sensitive
manager envtest `TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync`
(de-flaked once in 047), aggravated because ArtifactsReady now derives from serve
sidecar readiness. Chain of fixes:
1. `d1b04df` retry-on-conflict for the manual db-sync status bump (fixed the
   "object has been modified" conflict).
2. `660bed4` background availability ticker — WRONG/harmful (200ms Deployment
   STATUS updates conflicted with the controller's full-object Deployment apply);
   reverted in the next commit.
3. `e86ae40` re-publish the bumped revision every poll (robust re-enqueue) +
   reverted the ticker + 2m timeouts.
4. **`48a705e` THE REAL FIX**: the manager envtests set `syncProberOverride` but
   not the new `timingProberOverride`, so the controller ran the DEFAULT timing
   prober — a real HTTP GET of shelley-genesis.json from the (non-existent in
   envtest) serve endpoint on EVERY applied-status reconcile, blocking to the
   probe deadline. With one reconcile worker that serializes/starves reconciles
   under CI load → revision-handoff timeout. Added `syncedNodeTimingProber()` to
   all three manager envtests. Verified 3× under GOMAXPROCS=2 (also faster).
Lesson for future serve-fetch/probe work: any new reconcile-time HTTP probe needs
a test override wired into EVERY manager-backed envtest, or it does real
(failing, blocking) network calls that serialize reconciles and cause load flakes.

**MERGED**: PR #78 squash-merged to master as `606d800`; remote branch deleted.
Final CI all green (ci 3m52s, e2e 8m59s, cardano-tools-image, Kusari). The one
round-4 e2e failure (~20min stall) was Docker Hub/infra jitter, not the change.
Dev stack still UP. F0 PR-B1 COMPLETE.

## 2026-06-01 (later) — PR-B2: delete the dead cardano-testnet publisher

User: merge PR-B1 (done), then plan + implement PR-B2 as the LAST slice this
session (PR-D explicitly excluded). Switched to plan mode, explored, wrote the
approved plan (`~/.claude/plans/ok-can-you-propose-noble-codd.md`).

Key scoping insight: the explore agents read the STALE primary checkout (231ccde,
pre-PR-B1), so their operator-side findings (artifact-publisher RBAC, ConfigMap,
YACD_ARTIFACT_* env, ProducerConfigMap) were already-done-in-PR-B1 noise. Verified
merged master's operator side is clean. So PR-B2 = image-side ONLY + a revision bump.

Worktree `.wt/feat-f0-remove-publisher` off the merged master (606d800).
Commit `6118d3a` (`build(cardano-testnet)!: remove the dead artifact publisher
from the tools image` — `!` guarantees release-please cuts the prerelease bump):
- Deleted the `publisher` nested module + the `yacd-cardano-testnet-publisher`
  wrapper cmd + `internal/artifactpublisher` (23 files). Outer
  `containers/cardano-testnet` keeps a vestigial empty go.mod (avoids retyping the
  release-please `go` component).
- Dockerfile: dropped the `publisher` build stage + binary COPY + YACD_PUBLISHER_*
  ARGs; bumped the version ARG default to yacd.5. Init wrapper: removed
  `publish_artifacts()` + its 2 call sites (create-env pass-through kept).
- Pruned publisher refs from moon.yml (goSources + test steps), .dev/scripts/check.sh
  (lint+test), both release workflows (build-args + publisher-binary smoke),
  .dev/build-cardano-testnet.sh (comment).
- Bumped manager default cardano-testnet revision yacd.4→yacd.5 in
  cardanonetwork/init_container.go + cardanodbsync/defaults.go; updated the 5 test
  assertions; and **test-e2e.sh's hardcoded cardano-testnet tag yacd.4→yacd.5**
  (the e2e relies on the manager's computed default matching the kind-loaded tag —
  would have failed CI's e2e otherwise). Did NOT edit the release-please manifest
  (release-please owns it). README: dropped the stale publisher-SA sentence.

Verified: grep-to-zero (publisher symbols) CLEAN; `root:check` + `root:test` green;
**docker-built the slimmer image** — publisher binary ABSENT, cardano-node/cli/testnet
+ init wrapper PRESENT, entrypoint works. Local `root:test-e2e` running (self-contained
yacd-test-e2e cluster, builds yacd.5 image from source). Dev stack `kind-yacd-dev`
still up (from the PR-B1 session; stale source — using the self-contained e2e for
proof, not the dev stack).

Sequencing (accepted, cardano-tools precedent): manager points at yacd.5 before it's
published; dev/e2e source-build + inject; production gets yacd.5 once the release-please
release PR (proposed from this merge) is merged. PR-D (report verb, digest pin, DESIGN.md)
remains excluded.

### PR-B2 MERGED (#79, `22a5e8f`) + CI flake + release-versioning follow-up

**PR-B2 verified + merged.** Local: grep-to-zero clean, root:check+root:test green,
slimmer image docker-built (publisher binary absent, kept binaries + init wrapper
present), `root:test-e2e` PASS (create-env→serve→Synced on the yacd.5 image). Pushed
PR #79.

**CI flake (again): the same load-sensitive manager envtest**
`TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync` failed on PR-B2's CI
twice (PR-B2 does NOT modify it — inherited from PR-B1; e2e failure was a one-off
`ctr images import` infra error that passed on re-run). Per USER decision ("remove it
for now; refactor all tests in a future session"), removed the test + its 3 orphaned
helpers (primarySidecarExternalSecret, requireDeploymentContainerEventually,
requireDeploymentDBSyncSidecarRevisionEventually) + the unused ctrldbsync import
(commit on the PR). lint `unused` clean; cardanonetwork tests down to ~23s. **TODO
(future session): refactor the primary-sidecar manager envtest to be deterministic
(its revision-bump/incumbent-handoff choreography races two live controllers under
load); the behavior is covered by unit tests (SelectPrimarySidecarClaim, dbsync_sidecar,
builder).** CI then green (ci 3m7s, e2e, cardano-tools-image, Kusari) → squash-merged
#79 → master `22a5e8f`.

**Release-versioning bug found + fixed (#80, `7c13d14`, USER-approved follow-up):**
release-please derives the cardano-testnet version from Conventional-Commit semver, so
PR-A's `feat` drifted the base 11.0.1→11.1.0 and PR-B2's breaking `!`→12.0.0 (open
release PR #34 proposed `12.0.0-yacd.4`). WRONG: the base must equal the packaged
cardano-node version (the release workflow downloads cardano-node at the base; the
operator computes the image ref as `<node version>-yacd.N`). `12.0.0` would fail the
release build (no upstream cardano-node 12.0.0) and never match the operator's
`11.0.1-yacd.5`. Fix (PR #80): added `containers/cardano-testnet/README.md` documenting
the contract (base=node version, -yacd.N=packaging revision, set each release via a
`Release-As:` footer) + pinned this release via `Release-As: 11.0.1-yacd.5` in the
squash commit. **Verified:** release-please updated PR #34 → `cardano-testnet
11.0.1-yacd.5` and left root #7 / cardano-tools #76 untouched (footer scoped to the
component by changed path).

**SESSION SCOPE COMPLETE.** F0 PR-B1 (#78) + PR-B2 (#79) + versioning follow-up (#80)
all merged to master.

**RELEASE CUT (user asked me to merge + verify).** Squash-merged release-please PR #34
(`ca24030`) → release-please (GitHub App token) created tag `cardano-testnet/v11.0.1-yacd.5`
+ a draft prerelease (the repo's norm — yacd.2/3/4 are all draft prereleases too; the
release workflow ships the IMAGE, not the GitHub release) and relabeled #34
`autorelease: tagged`. The tag (App token, so it triggers downstream) fired
`release-cardano-testnet.yml` (run 26781069769) → SUCCESS: both arches built, image
released, inspection passed. Verified: `ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.5`
is a public multi-arch OCI index (linux/amd64+arm64 + SBOM/provenance); **pulled it and
confirmed the publisher binary is ABSENT** and cardano-node/cli/testnet + the
yacd-cardano-testnet-init wrapper are present. This is exactly the tag the manager default
(yacd.5) targets, so production now resolves the slimmer publisher-free image. F0 PR-B2 fully realized.

Dev stack `kind-yacd-dev` still UP (awaiting session close → dev-down).

REMAINING F0 (future): **PR-D** (remove cardano-tools `report` verb; pin manager
cardano-tools image to a published digest; drop e2e build+load hack; rewrite DESIGN.md
ConfigMap prose). Also: the planned **test refactor** (deterministic primary-sidecar
manager envtest) and the broader release-please base-drift concern (cardano-testnet
now relies on per-release `Release-As`; other components may want similar treatment).
Carried: TEST_REPORT F2/F4; test-harness Phases 3-5.

Decisions worth carrying: sync-timing is serve-fetch (NOT a Status.Network
field — local systemStart only known in-pod); ArtifactsReady from serve-container
readiness; db-sync identity from Status.Network fingerprint (Mode-based).

REMAINING F0 (unchanged): **PR-B2** (delete publisher binary/nested module +
Dockerfile stage + new cardano-testnet image) and **PR-D** (remove cardano-tools
`report` verb; pin manager cardano-tools image to published digest — release-please
PR #76 is open; drop e2e build+load hack; rewrite DESIGN.md ConfigMap prose).
Carried: TEST_REPORT F2/F4; test-harness Phases 3-5.
