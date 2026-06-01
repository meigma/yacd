---
id: 050
title: F0 redesign — PR-D (remove report verb, pin cardano-tools digest, drop e2e build hack, DESIGN.md rewrite)
started: 2026-06-01
---

## 2026-06-01 14:05 — Kickoff

Goal for the session: land **PR-D**, the final slice of the F0 redesign. Per
session 048's SUMMARY, PR-D is cleanup + docs (no new runtime behavior), a
low-risk close to the F0 series. Four carried items:

1. **Remove the cardano-tools `report` verb** — it was the publisher's
   genesis-hash enrichment mechanism; `cardano-tools stage` (PR-C) took over, so
   `report` is dead. Grep `containers/cardano-tools` for the `report` subcommand
   + its `internal/` impl/tests; confirm nothing in the operator or init
   wrappers still invokes it before deleting.
2. **Pin the manager's cardano-tools image to a published digest** — today the
   manager defaults to a `yacd.N` tag (same pattern cardano-testnet uses). The
   sequencing gap (no-override `helm install` between an image PR merging and its
   release-please PR merging) is documented/accepted pre-1.0; digest pinning
   closes it. release-please cardano-tools PR **#76 is open** — coordinate the
   digest with whatever it publishes. Check
   `internal/controller/cardanodbsync/defaults.go` and the cardanonetwork
   init/sidecar wiring for the cardano-tools image ref.
3. **Drop the e2e build+load hack** — e2e currently builds cardano-testnet/tools
   images from source and loads them into Kind because the manager defaulted to
   not-yet-published tags. Once images are published + digest-pinned, e2e can
   pull instead of build-and-load. See `.dev/scripts/test-e2e.sh` + chainsaw
   setup.
4. **Rewrite the DESIGN.md ConfigMap prose** — DESIGN.md still describes the old
   artifact-ConfigMap transport that PR-B1 deleted. Update it to the
   serve-over-HTTP / PVC-staged model that now ships.

Current state of the world:
- PR-A (#75), PR-C (#77), PR-B1 (#78), PR-B2 (#79), versioning follow-up (#80),
  and the cardano-testnet release (#34 → tag `cardano-testnet/v11.0.1-yacd.5`)
  are all merged. Runtime data path is fully HTTP/PVC-based; the publisher is
  gone. master @ `ca24030`.
- Open release-please PRs: **#76** (`release cardano-tools 11.1.0-yacd.4`),
  **#7** (`release 1.0.0`, root). Also open: dependabot #43, #44.
- PR-D needs a release of cardano-tools (for the `report` removal + the digest
  to pin to), so expect a release-please cardano-tools PR as part of landing it.
  Apply the same `Release-As:` base-pinning discipline (base = packaged
  cardano-node version) if its base drifts. NOTE the cardano-tools versioning:
  base should equal the cardano-node version; #76 currently proposes
  `11.1.0-yacd.4` which looks drifted off `11.0.1` — verify.

Plan: review PR-D scope in the code, confirm with the user, then execute on a
fresh implementation worktree off master. Dev stack startup deferred until the
implementation worktree is selected and the user gives the go-ahead.

User asked me to review PR-D and report readiness before further instructions.

## 2026-06-01 14:15 — PR-D review findings

Reviewed all four items against master (`ca24030`). Two things diverge from the
session-048 PR-D scope:

1. **`report` verb removal — clean/ready.** Lives at
   `containers/cardano-tools/internal/cli/report.go` (`newReportCommand`),
   registered `internal/cli/root.go:106`, config in `internal/config/config.go`
   (`ReportConfig` + tests), golden
   `cmd/yacd-cardano-tools/testdata/report-dry-run.txtar`. NO remaining
   invocations anywhere (operator, init wrappers, .dev scripts, chainsaw,
   moon.yml). Its only purpose (publisher sha256 compat) is moot since the
   publisher was deleted in PR-B2. Straight dead-code removal.

2. **Digest pinning — the crux, with a latent bug + chicken-and-egg.**
   - `internal/cardano/toolsimage/toolsimage.go:23` `Revision = "yacd.0"` →
     manager defaults to `ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.0`.
   - **But the ONLY published cardano-tools tag is `v11.0.1-yacd.4`** (git tag
     list). So the manager default points at an UNPUBLISHED tag — this is
     exactly why the e2e build+load hack exists, and digest-pinning fixes it.
   - Chicken-and-egg: removing `report` changes the image, so a NEW cardano-tools
     release must be cut before there's a final digest to pin to.
   - **Open release-please PR #76 proposes `11.1.0-yacd.4` — base DRIFTED**
     (feat commits drove minor bump). Per the versioning contract base must equal
     the cardano-node version `11.0.1`; needs a `Release-As:` fix like #80 did for
     cardano-testnet.
   - Helm value `cardanoTools.image.digest` already exists
     (`charts/yacd/values.yaml`), and `--default-cardano-tools-image` is the
     override flag — but the built-in `Reference()` is tag-only; pinning means
     teaching it a digest constant.

3. **e2e build+load hack — ready, partial.** `.dev/scripts/test-e2e.sh:36-45`
   builds+kind-loads manager, faucet, cardano-testnet, cardano-tools. Only
   cardano-testnet + cardano-tools can switch to pull-by-digest; manager + faucet
   MUST stay built-and-loaded (they're the code under test, not published
   per-commit).

4. **DESIGN.md — NO stale ConfigMap prose.** Contrary to the 048 note, `DESIGN.md`
   does NOT describe the old artifact-ConfigMap transport (only mention is a
   generic K8s-resource-type list at line 49). So item 4 is either a no-op or
   should be reframed as ADDING serve-over-HTTP/PVC transport prose. Flagged for
   the user.

Implied landing order: (a) remove report + drop e2e build/load for tools images
+ teach digest pinning mechanism in one code PR → (b) merge → release-please
cardano-tools PR (fix base via Release-As) → (c) merge release → published digest
→ (d) pin manager to that digest. Steps b–d span a release, so digest pinning
tails the release. Awaiting user direction on sequencing + the DESIGN.md question.

## 2026-06-01 14:35 — Plan approved; Stage 1 implemented

User decisions: **skip item 4** (DESIGN.md already clean), **full chain with a
pause before merging the release**, **Release-As: 11.0.1-yacd.5** + add a
cardano-tools README. Plan saved; PR-D is a 3-stage chain (code PR → release →
digest-pin PR).

Worktree `f0-pr-d-report` off `origin/master`. `moon run root:dev-up` ran clean.

**Stage 1 done + committed** (`95cf748`,
`build(cardano-tools)!: remove the dead report verb (F0 PR-D)`):
- Discovery refined the plan: `config.go` was ENTIRELY report-only — `LoadStage`
  derives its own plan-manifest filename, so `planManifestFilename` was NOT
  shared. Deleted the whole `config.go` (kept `config_stage.go`) and rewrote
  `config/doc.go` to describe the stage loader.
- Deleted: `cli/report.go`, the `report` registration in `root.go`, the orphaned
  `internal/kube/` package (imported only by report), `config_test.go` (all
  report-only), and the `report-dry-run.txtar` golden.
- Refreshed stale "report verb" doc comments in `stage/{stage.go,doc.go}` and the
  Dockerfile LABEL verb list (now generate/fetch/serve/stage/sync).
- Added `containers/cardano-tools/README.md` (versioning contract; mirrors
  cardano-testnet; points at `toolsimage.Revision`/`Digest`).
- `.dev/scripts/test-e2e.sh`: dropped cardano-testnet build+load (published tag,
  Kind pulls it); kept cardano-tools build+load until Stage 3.

Verified: `go build ./...`, all cardano-tools tests, `--help` shows no `report`,
`root:generate` no-op, `root:check` green (incl. test/chart RBAC, which passed
locally this run), `root:test` green. `root:test-e2e` running in background to
confirm the cardano-testnet pull works.

## 2026-06-01 15:05 — Stage 1 merged; Stage 2 in flight; Release-As LEAK

Stage 1: `root:test-e2e` green (`Passed tests 1`, 0 reconcile errors, fresh
cluster created+deleted — proves the cardano-testnet PULL works). All CI on
**PR #81** green (ci/e2e/cardano-tools-image/Kusari). Squash-merged → master
`2b9bf84`. Worktree removed.

**MISTAKE + fix (recorded in TECH_NOTES):** my Stage-1 squash commit carried an
UNSCOPED `Release-As: 11.0.1-yacd.5`. The root `yacd` release-please component
excludes only the two `containers/*` dirs, and the commit also touched
`.dev/scripts/test-e2e.sh` (a root path) — so the footer LEAKED into root release
PR **#7**, flipping it from `1.0.0` → `11.0.1-yacd.5`. cardano-tools #76 was
correct (`11.0.1-yacd.5`). Root cause: unscoped footer on a multi-component
commit. Fix (user-approved): root stays unmerged/latent; restore to `1.0.0` in
Stage 3 via the proven component-scoped form `Release-As: yacd@1.0.0` (Stage 3
legitimately touches root). Use scoped footers for any multi-component commit.

Stage 2 (user OK'd the publish): squash-merged release-please **#76** → master
`046f979` → release-please created tag `cardano-tools/v11.0.1-yacd.5` → "Release
cardano-tools Image" workflow building+publishing (polling). NOTE: a master CI
run flaked on `cardano-tools-image` "Booting builder" pulling
`moby/buildkit:buildx-stable-1` (`context deadline exceeded` — Docker Hub
jitter, env not code).

## 2026-06-01 15:30 — Stage 2 done; Stage 3 implemented + e2e green; #82 open

**Stage 2 complete.** "Release cardano-tools Image" workflow succeeded; image
published. **Pinned digest (multi-arch index):**
`ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.5@sha256:d3283ca5fc925f6ec01f61a54371e5ad1934088614b7cde1e1e1915424662fc4`.
Verified by `docker run ... --help` on the digest: **no `report`**, has
generate/fetch/serve/stage/sync. Exactly the image to pin.

**Stage 3 implemented** (worktree `f0-pr-d-pin` off post-release master, commit
`feef834`):
- `internal/cardano/toolsimage/toolsimage.go`: `Revision`→`yacd.5`, added
  `Digest` const, `Reference()` emits `<repo>:<ver>-<rev>@<digest>` for the
  built-in default; `TestDigestPin` guard added.
- `cardanonetwork` builder_test/init_container_test: 6 hardcoded
  `:11.0.1-yacd.0` literals → `toolsimage.Reference("", "11.0.1")` (track the
  source; literal now lives only in toolsimage_test.go). Refreshed a stale doc
  comment. cardanodbsync tests only assert the `:tilt` override → untouched.
- `.dev/scripts/test-e2e.sh`: dropped the cardano-tools build+load entirely; now
  only manager+faucet are built+loaded, cardano-testnet+cardano-tools are pulled.
- Carries scoped **`Release-As: yacd@1.0.0`** footer (commit touches ONLY
  root-component paths) to repin root #7 back to 1.0.0.

Verified: `root:check`, `root:test` green (after updating the 6 builder-test
expectations), `root:test-e2e` GREEN (`Passed tests 1`, 0 reconcile errors) —
proves Kind pulls the digest-pinned cardano-tools image and the operator reaches
Ready with it. **PR #82** open; polling CI. After merge: confirm root #7
recomputes 1.0.0 and cardano-tools is undisturbed.

## 2026-06-01 15:45 — PR-D COMPLETE; F0 series closed; leak fixed

#82 CI green (ci/e2e/cardano-tools-image/Kusari) → squash-merged to master
`bd8e0bf` with the scoped `Release-As: yacd@1.0.0`. release-please ran:
- **Root #7 recomputed `0.0.0` → `1.0.0`** (leak FIXED; the scoped footer
  overrode the earlier unscoped one in root's window).
- cardano-testnet stays `11.0.1-yacd.5`; **no new cardano-tools release PR**
  (#82 didn't touch `containers/cardano-tools`). Only #7 (root, 1.0.0) open.
- **#7 left OPEN/unmerged on purpose** — releasing the operator 1.0.0 is not
  part of PR-D.

Cleanup: both PR-D worktrees removed; local+remote `f0-pr-d-{report,pin}`
branches deleted. Pre-existing unrelated `feat/f0-dbsync-http` left alone.

**PR-D done. The F0 redesign series is COMPLETE** (PR-A #75, PR-C #77, PR-B1 #78,
PR-B2 #79, PR-D #81+#82). The runtime data path is fully HTTP/PVC-based, the
publisher + report verb are gone, and the manager's cardano-tools default is a
published, digest-pinned image (e2e pulls it, no build+load).

Merged this session: **#81** (report removal), **#76** (cardano-tools
11.0.1-yacd.5 release), **#82** (digest pin + e2e hack drop). Dev stack
(kind-yacd-dev) still running — leave warm until session close.

Carried/untouched: root #7 (operator 1.0.0, awaiting a deliberate release
decision); deterministic primary-sidecar manager-envtest refactor; TEST_REPORT
F2/F4; test-harness Phases 3–5; cardano-testnet digest-pin parity (optional
follow-up). Session 049 (CLI/k3d) remains separately in-progress.
