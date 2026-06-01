---
id: 047
title: F0 redesign PR-C — db-sync consumes configs over HTTP
started: 2026-05-31
---

## 2026-05-31 18:21 — Kickoff
Goal for the session: continue the F0 redesign series. PR-A is merged
(session 046, PR #75, `c61e0a6`). The next slice per the agreed order
(A → C → B → D) is **PR-C: CardanoDBSync consumes network configs over HTTP**.
Replace the CardanoDBSync ConfigMap mount with a cardano-tools `fetch` init
→ emptyDir + manifest verify, pointed at the primary network's serve endpoint
(`status.endpoints.artifacts.url`). PR-C MUST land before PR-B, which deletes
the `<net>-network-artifacts` ConfigMap that db-sync currently GETs by name.

Current state of the world:
- `master` at `c61e0a6` (PR-A merged, additive: serve sidecar + producer +
  `<net>-artifacts` Service + `status.endpoints.artifacts` alongside the
  existing ConfigMap path; node/ogmios/faucet containers unchanged).
- PR-C reworks ~6 `internal/controller/cardanodbsync` files plus a
  cross-controller edit to
  `internal/controller/cardanonetwork/dbsync_sidecar.go` (~lines 103, 113).
- Each F0 PR must keep the chainsaw e2e green (runs on every PR). Branch fresh
  off master.
- Carried non-F0 threads: TEST_REPORT F2/F4 open; test-harness Phases 3–5
  remain; KNOWN FLAKE
  `TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync`
  (load-sensitive envtest); e2e Docker Hub 429 jitter on ogmios/kupo.
- Dev stack was left UP but ORPHANED on `kind-yacd-dev` at the end of session
  046 (the F0 worktree that owned it was removed at merge; `.run/yacd-dev`
  ownership dangles). Tear down with `moon run root:dev-down` from the main
  checkout, or repair before starting implementation.

Plan (rough):
1. Wait for the user's actual request / confirm PR-C is the target.
2. Create a fresh implementation worktree off master via `wt`.
3. Start the dev stack (`moon run root:dev-up`) once after selecting the
   worktree — note the orphaned-stack cleanup may be needed first.
4. Implement PR-C, keep `root:check`/`root:test`/chainsaw green, PR + squash.

## 2026-05-31 19:57 — PR-C implemented; e2e in flight
Plan approved (cardano-tools command named `sync`; one bundled PR-C). Work done
on branch `feat/f0-dbsync-http` (off master `c61e0a6`), worktree
`.wt/feat-f0-dbsync-http`. Commits so far:
- `6052f4b` feat(cardano-tools): add `sync` command (package `artifactsync` to
  avoid shadowing stdlib `sync`; CLI verb stays `sync`). GET manifest.json →
  fetch+verify each listed file → write manifest verbatim. httptest tests.
- `dbdcf3b` feat(cardanodbsync): consume artifacts over HTTP on the serve path.
  Transport discriminator in `controller.go` (`servedArtifactsURL`); serve path
  skips ConfigMap GET + ConsumerConnection, keeps `Status.Artifacts.DataHash`
  for the identity fingerprint (zero churn), derives identity from status
  (networkConnection nil). `public_network.go` reads mode/profile from spec.
  dedicatedFollower: emptyDir + `network-artifacts-sync` init container
  (`syncInitContainer`) before pgpass init. primarySidecar: `artifactsMount()`
  override mounts the primary state PVC subPath `artifacts`. New builder fields:
  defaultCardanoToolsImage, servedArtifactsURL, artifactsVolumeOverride,
  artifactsSubPath. `artifactSource{serveURL, configMap}` threaded through
  reconcileReadyDBSync/reconcileWorkloads/reconcilePrimarySidecarWorkloads.
- `98d6534` fix(cardanonetwork): KEY DISCOVERY — the primary-sidecar attachment
  is built in the SAME reconcile that later publishes
  `status.endpoints.artifacts`, so that status field is nil at attachment-build
  time. Discriminate on the SPEC (`stagesServedArtifacts` = isLocal ||
  curated-public), not status, in `dbsync_sidecar.go`. (The db-sync controller
  is fine reading the already-reconciled referenced network's status + needs the
  URL anyway.) Caught by an attachment test I added.
- `c284ce5` lint; `b34dea4` chainsaw assert (sync init present, no ConfigMap
  volume on phase6-managed-dbsync).

Tests added (all green): cardanodbsync `artifacts_transport_test.go` (4 builder
cases: dedicatedFollower serve emptyDir+sync-init, dedicatedFollower configmap,
primarySidecar serve PVC-subPath, primarySidecar configmap) + controller
decoupling test (Artifacts endpoint set, ConfigMap absent → still reconciles);
cardanonetwork attachment test now asserts the staged-PVC subPath mount, and the
former "skip when ConfigMap missing" local-network test is flipped to assert
serve-path attaches WITHOUT a ConfigMap (the PR-B enablement).

Gates green: `root:generate` (idempotent, no API change), `root:check`,
`root:test` (full suite, envtest incl. frozen fingerprint untouched).
Custom-public KEEPS the ConfigMap path (no regression). NOW: `root:test-e2e`
running in background (chainsaw on real Kind, builds cardano-tools from source
so it carries `sync`). After green: PR, then journal close.

## 2026-05-31 20:30 — e2e exposed a genesis-hash gap; fixed; recovered a cwd slip
First e2e FAILED (real bug, not flake): the `network-artifacts-sync` init synced
9 artifacts fine and the follower-node started, but `cardano-db-sync` died with
`NodeConfigParseError "key ByronGenesisHash not found"`. Root cause: the
cardano-testnet create-env `configuration.yaml` on disk omits genesis hashes
(cardano-node computes them; db-sync REQUIRES them), and the legacy ConfigMap
publisher enriched only in-memory. PR-A's `stage` flattened the RAW on-disk
config → served config lacked hashes. Invisible until PR-C made db-sync consume
the served config. FIX (`b2dc24e`): `cardano-tools stage` now enriches
configuration.yaml via the shared `generate.EnrichGenesisHashes` (cardano-cli;
image ships it at /opt/cardano/bin + CARDANO_CLI). Injectable `Hasher` so unit
tests fake it. Public `fetch` configs already ship hashes — untouched. stage
unit test now references all four genesis files + asserts the hashes appear.

ENVIRONMENTAL (not a PR-C problem): `test/chart` `TestManagerRBACMatchesControllerGen`
fails LOCALLY with phantom `example.meigma.io/nginxdeployments` + `events`
markers that exist NOWHERE in source (chart is clean, `go list ./...` shows no
such pkg, grep/git-grep clean). PROVEN environmental: it fails identically on a
clean `master` (c61e0a6) checkout with zero of my changes. Root cause is the
local proto-go-shim `{{context.Compiler}}` bug degrading controller-gen's
go/packages load (controller-gen errors outright when run directly). CI is green
for master, so CI is unaffected. Do NOT "fix" RBAC — nothing is wrong.

PROCESS SLIP (recovered): a `cd` in a compound Bash command didn't persist, so a
later `git commit` landed the stage fix as `255b384` on LOCAL master instead of
feat. Caught immediately via `git -C <abs> log`. Recovered: cherry-picked to
feat (`b2dc24e`), `git reset --hard origin/master` on main (255b384 never
pushed; master back at c61e0a6, clean). LESSON: use `git -C <abs-path>` and
verify pwd; the session-046 garbled-state caution applies.

State: feat has 6 commits (sync cmd, controller rewire, sidecar-spec fix, lint,
chainsaw assert, genesis enrich). Targeted suites all green (cardanodbsync,
cardanonetwork, cardano-tools, stage). e2e #2 re-running in background from the
feat worktree (rebuilds cardano-tools from source). After green: push + PR.

## 2026-05-31 21:15 — PR-C green, ready for review
e2e #2 PASSED (chainsaw manager-smoke green, 0 db-sync errors — the genesis
enrichment fixed the ByronGenesisHash failure; my HTTP-transport assertion held).
Pushed PR #77 (https://github.com/meigma/yacd/pull/77).

CI: e2e + cardano-tools-image + Kusari green first try; the RBAC test PASSED in CI
(confirming the local failure is purely the proto-shim artifact — do not chase it).
Only red was the documented load-sensitive flake
`TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync` — failed 3x in a
row (each ~17-18s hitting tight 10s Eventually waits), exceeding the prior
"2x-then-green" pattern. Verified NOT my bug: passed 5x locally in isolation + 2x
in full suite; my change adds zero reconcile latency to that path. User chose
(AskUserQuestion) to BUNDLE the de-flake into PR-C. Committed `ee06df5`: bumped the
8 in-scope `10*time.Second` Eventually waits (the attachment test 668-868 + its two
deployment-assertion helpers 882-923) to `time.Minute`; left the other 46 in sibling
tests untouched. Fresh full CI after the push: **ALL GREEN** (ci 4m45s, e2e 8m17s,
cardano-tools-image, Kusari).

PR #77 = 7 commits, ready for review/merge. NOT merging (PR review is the
integration path; user merges). REMAINING F0 after this: PR-B (node reads PVC +
DELETE the ConfigMap = the mainnet unblock; also migrate db-sync identity off
Status.Artifacts.DataHash), then PR-D (remove report verb, pin manager
cardano-tools image to a published sync-capable digest, drop the e2e build+load
hack, DESIGN.md + chainsaw ConfigMap-shape rewrite). The flake is now de-flaked in
PR-C, so the TECH_NOTES KNOWN-FLAKE entry can be retired once #77 merges.
