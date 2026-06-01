---
id: 046
title: F0 redesign PR-A — serve network artifacts over HTTP
date: 2026-05-31
status: complete
repos_touched: [yacd]
related_sessions: [043, 029, 042, 045]
---

## Goal
Continue the F0 redesign begun in session 043. F0 is the TEST_REPORT finding that
public **mainnet** `CardanoNetwork` cannot be created because the
`<net>-network-artifacts` ConfigMap exceeds etcd's ~1 MiB cap. The agreed redesign
(decided in 043, do not relitigate): the manager is **not** an authoritative config
source — local configs are *generated* and public configs are *fetched* onto the
node PVC, `cardano-node` reads from the PVC, and every other consumer (db-sync, CLI,
out-of-cluster) fetches over HTTP from an always-on cardano-tools `serve` sidecar,
with integrity/discovery via a served `manifest.json`. The redesign lands as
**PR-A → PR-C → PR-B → PR-D** (this order is deliberate; an 8-agent surface-map
workflow in 043 proved A→B→C→D would brick db-sync). This session's job was PR-A.

## Outcome
**Met.** PR-A (the additive first step) is **merged to master** as squash commit
`c61e0a6` (PR #75). PR-A keeps the artifact ConfigMap and leaves the
node/ogmios/faucet containers + mounts unchanged, so build and chainsaw stay green;
it adds the serve sidecar + producer + Service/status *alongside* the existing path.
Validated at every layer: `root:check`, `root:test` (envtest), idempotent
`root:generate`, a **live in-cluster smoke** on Kind (`GET /manifest.json` → HTTP
200 with a well-formed manifest), and full CI green incl. the e2e chainsaw smoke.

## Key Decisions
- **cardano-tools image into CI e2e by build+load, not release-first** (user choice
  via AskUserQuestion). PR-A introduces the first cardano-tools *runtime* container,
  but the built-in default revision is `yacd.0` (never published) and the published
  `yacd.4` predates A1's manifest support — so e2e needs a source-built image.
  `.dev/scripts/test-e2e.sh` already build+loads the manager/faucet/cardano-testnet
  images, so cardano-tools got the same 3-line treatment (no release coupling). The
  manager-default digest pin is deferred to PR-D.
- **serve reads a flat staged dir; the producer writes it.** `serve` exposes ONE
  flat directory with a default-deny allowlist of exact contract-key filenames
  (compiled into the binary), and serves `manifest.json` *from a file* (it does not
  compute it). Neither mode had a flat+complete dir on disk (local `/state/env` is
  nested + lacks `connection.json`; public bytes lived only in the ConfigMap). So
  PR-A brings forward the PVC-staging producer: a stage/fetch init writes
  `/state/artifacts` (flat keyed files + `connection.json` + `manifest.json`).
- **serve + stage are local + curated-public only; custom-public deferred.** Custom
  public keeps its byte-based ConfigMap (small, off the chainsaw path). Curated =
  `isPublic && profile != custom`.
- **Serve port 8090**, because 8080 is the faucet default. Registered in
  `primarypod.PortOwners` (so a db-sync metrics-port collision is rejected) but
  reserved in `validatePrimaryWorkloadPorts` only when serve actually runs (so
  custom-public can still use node port 8090).
- **public connection.json records static facts only** (profile, networkMagic,
  requiresNetworkMagic, files map) — the fetch verb runs before cluster identity
  exists; runtime endpoint enrichment is a later/controller concern.
- **Resolved a mid-session conflict with PR #74** (session-045 node sync status,
  merged to master while PR-A was open) in `status.go` — a clean union: both
  `artifactsService` (PR-A) and `syncStatus` (#74) params kept on
  `patchPrimaryWorkloadStatus`/`patchPrimaryWorkloadAppliedStatus`; both publish
  blocks retained. Rebased (not merged) onto master, fixed the one wrong-arity nil
  call git's auto-merge left, re-validated green.

## Changes
All in repo `yacd`, merged via PR #75 (`c61e0a6`). 7 commits:
- `internal/cardano/publicpins/*` — shared curated public-profile pin registry +
  static per-profile identity; `cardano-tools fetch` sources pins from it
  (foundation commits, golden-locked, behavior-preserving — banked from 043).
- `internal/cardano/networkartifacts/manifest.go` — typed `Manifest`
  (`schemaVersion` + per-file `sha256`) + `BuildManifest`/`Verify`/`JSON`;
  `ManifestKey` added to optional contract keys so `serve` exposes
  `GET /manifest.json` by construction (A1).
- `containers/cardano-tools/internal/{fetch,stage,artifactset,cli,config}/*` —
  `fetch` now writes `connection.json` + `manifest.json`; new `stage` subcommand
  flattens a create-env dir into the flat served dir, reusing `report`'s
  artifactset assembly (report + its golden unchanged).
- `internal/controller/cardanonetwork/{builder,controller,init_container,containers,resources,defaults}.go`
  — thread `--default-cardano-tools-image` into the builder; `servedArtifactsInitContainer`
  (stage local / fetch curated-public, → `/state/artifacts`); always-on
  `serveContainer` (:8090, `/manifest.json` readiness, RO `/state`); wired into the
  Deployment for local + curated-public.
- `internal/cardano/primarypod/primarypod.go` — `PortNameServe`/`DefaultServePort=8090`
  + PortOwners registration.
- `api/v1alpha1/cardanonetwork_types.go` (+ regenerated CRD + deepcopy) — new
  `status.endpoints.artifacts` (`ServiceEndpointStatus`).
- `internal/controller/cardanonetwork/{names,delete,status}.go` — owned
  `<net>-artifacts` ClusterIP Service (mirrors ogmios) + apply/delete cascade +
  `status.endpoints.artifacts` publishing.
- `.dev/scripts/test-e2e.sh` — build + kind-load the cardano-tools image.

## Open Threads — REMAINING F0 SERIES (resume here, fresh branch off master)
The redesign is ~1/4 done. **Order matters: PR-C → PR-B → PR-D.** Each must keep
chainsaw e2e green (it runs on every PR). Branch fresh off master each time.

- **PR-C (next): db-sync consumes configs over HTTP.** Replace the CardanoDBSync
  ConfigMap mount with a cardano-tools `fetch` init → emptyDir + manifest verify,
  pointed at the primary network's serve endpoint
  (`status.endpoints.artifacts.url`). Reworks ~6 `internal/controller/cardanodbsync`
  files + a cross-controller edit to
  `internal/controller/cardanonetwork/dbsync_sidecar.go` (~lines 103,113). **MUST
  land before PR-B** — PR-B deletes the ConfigMap, and db-sync currently GETs it by
  name; deleting it before C wedges every CardanoDBSync (`NetworkArtifactsPending`,
  won't compile). This dependency is why the order is A→C→B→D, not A→B→C→D.
- **PR-B: node reads from PVC + delete the ConfigMap (the mainnet F0 UNBLOCK).**
  Repoint the curated-public node config/topology reads from the ConfigMap
  `/profile` mount to `/state/artifacts` on the PVC; **delete** the
  `<net>-network-artifacts` ConfigMap, the artifact-publisher SA/Role/RoleBinding
  RBAC, the `containers/cardano-testnet/publisher` binary + its txtar goldens, the
  `networkartifacts` ProducerConfigMap path, and `Status.Artifacts.NetworkConfigMapName`.
  The RBAC marker drop MUST be mirrored in `charts/yacd/templates/rbac-manager.yaml`
  in the SAME PR (`TestManagerRBACMatchesControllerGen` enforces byte-equivalence).
  NOTE: the `//go:embed` in the manager is NOT the blocker — public node config
  comes from the ConfigMap *volume mount*, not the embed; the embed can stay (manager
  image isn't size-constrained, only the ConfigMap is). Custom-public keeps its
  byte-based ConfigMap path. This is the PR that actually lets public mainnet be
  created.
- **PR-D: cleanup + digest pin.** Remove the cardano-tools `report` verb + its
  golden + report-only packages; **pin the manager default cardano-tools image to a
  published digest** (bump `internal/cardano/toolsimage` `Revision` from `yacd.0`
  and/or add a digest — PR-A's merge already opened release-please PR #76
  `release cardano-tools 11.1.0-yacd.4`; cut a release that INCLUDES A1+serve, then
  pin to it, and drop the e2e build+load hack from PR-A); rewrite the `DESIGN.md`
  ConfigMap prose; rewrite the chainsaw `manager-smoke` assertions (~20 places that
  assert the old ConfigMap shape).

Other carried threads (pre-existing, not F0):
- TEST_REPORT F2/F4 still open. Test-harness Phases 3 (release), 4 (`yacd-env`
  Action), 5 (examples + how-to) remain.
- KNOWN FLAKE: `TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync`
  (load-sensitive envtest). e2e Docker Hub 429 jitter on ogmios/kupo.
- **Dev stack left UP** on `kind-yacd-dev` but ORPHANED (the F0 worktree that owned
  it was removed at merge; `.run/yacd-dev` ownership now dangles). Tear down with
  `moon run root:dev-down` from the main checkout when convenient.

## References
- Merged: PR #75 `https://github.com/meigma/yacd/pull/75` (squash `c61e0a6`).
- Conflicting PR merged mid-session: #74 (session-045 node sync status, `bfadcf6`).
- Published cardano-tools image (still pre-A1, PR-D will re-pin):
  `ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.4`
  `@sha256:9ca9e03348c3f9d22408be36f1525c3ef518ab6e0b0053b0a05f2b8401a6039e`.
- Release-please PR opened by this merge: #76 `release cardano-tools 11.1.0-yacd.4`.
- Prior: `.journal/043/SUMMARY.md` (F0 redesign decided + foundation banked),
  `.journal/029/` TEST_REPORT (F0 origin), `.journal/045/` (the sync-status PR #74).

## Lessons
- **Tool-result delivery was severely delayed/garbled all session** (results
  arriving tens of minutes late and out of order; in two cases entire file contents
  were fabricated — a non-existent `cardano-network.yaml` and a hallucinated
  `internal/cardano/artifactset` package). This caused multiple wrong intermediate
  conclusions (a "blocked Commit A" that had actually succeeded; an "8090 collision"
  read from a phantom file; an "auto-merged zero conflicts" rebase that had really
  stopped on a conflict). Every one was caught by cross-checking against
  `git show`/`git ls-files`/`git ls-remote` and small-output sentinels. RULE for
  future sessions if this recurs: never act on a structurally-suspicious large read;
  verify file existence/content with git plumbing first; verify git state with
  `git rev-parse`/`ls-remote` not narrative; prefer asserted exact-match edits + a
  compile after every edit.
- Delegating each commit's authoring to a fresh-context subagent (with explicit
  "no push, validate with moon, report back") worked well and kept large controller
  files off the flaky main channel; the parent reviewed the diff + re-ran the gates
  + committed.
