---
id: 043
title: Session start — awaiting direction
started: 2026-05-30
---

## 2026-05-30 15:39 — Kickoff
Goal for the session: not yet specified — opened via `session-new`; awaiting the
user's task.

Current state of the world:
- `master` is at `ad46e82` (PR #64 merged): new `containers/cardano-tools`
  container + `yacd-cardano-tools` binary (generate/fetch/serve/report) on a
  distroless/static slim image — the **foundation** for the TEST_REPORT F0 fix.
  The controller rewiring that actually closes F0 was intentionally deferred.
- Session 042 is closed. Session 041 (`feat/cli-connect-verb`, Test-Harness
  Phase 2 PR5 `yacd connect`) remains `in-progress` and dormant; its worktree
  still exists under `.wt/feat-cli-connect-verb`.
- Open threads carried forward (see `.journal/042/SUMMARY.md` Next Steps):
  F0 controller rewiring (mainnet public node reads config/genesis from a
  PVC staged by a generate/fetch init container; replace the small-profile
  ConfigMap with a metadata manifest; drop the manager `//go:embed`; serve
  sidecar + advertised URLs; CardanoDBSync consumer rewiring; public `report`
  path; manager flag/Helm value to point containers at the cardano-tools image;
  enable the cardano-tools image build on PR CI; cut the first cardano-tools
  release via the auto-opened release-please PR; re-verify the static-musl
  assumption on cardano-node bumps). TEST_REPORT findings F2/F4 are also still
  open.
- Local dev stack is NOT started. Per `.session.md`, start `moon run root:dev-up`
  from the implementation worktree only after a task is chosen and an
  implementation worktree is selected/created.

Plan: wait for the user's actual goal, then select/create an implementation
worktree off the fetched `master`, bring up the dev stack, and proceed.

## 2026-05-30 16:49 — Re-entry via session-new (reusing 043)
`session-new` was run again. Session 043 was an empty, same-day "awaiting
direction" placeholder that was never given a task, so reusing it rather than
opening a duplicate 044. Refreshing the stale world snapshot above:

- `master` has advanced to `e45ad76` (PR #67 merged) — the original 043 kickoff
  was captured at `ad46e82` (PR #64). Sessions 041 and 042 are now both closed.
- Session 041 (Test-Harness Phase 2: run/connect/exec verbs, `topup --await`,
  the `YACD_*` contract, host-access docs) was resumed and closed after 043 was
  first opened (PRs #59-63, #66, #67 all merged). Its worktree should be cleaned
  up if still present.
- Open threads carried forward (see `.journal/042/SUMMARY.md` Next Steps):
  the F0 controller rewiring (mainnet public node reads config/genesis from a
  PVC staged by a generate/fetch init container using the new `cardano-tools`
  image; replace the small-profile ConfigMap with a metadata manifest; drop the
  manager `//go:embed`; serve sidecar + advertised URLs; CardanoDBSync consumer
  rewiring; public `report` path; manager flag/Helm value pointing containers at
  the cardano-tools image; enable cardano-tools image build on PR CI; cut the
  first cardano-tools release; re-verify the static-musl assumption on
  cardano-node bumps). TEST_REPORT findings F2/F4 remain open. Test-harness
  Phases 3 (release), 4 (the `yacd-env` Action), and 5 (examples + how-to)
  remain.
- KNOWN FLAKE to watch:
  `TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync` is a
  load-sensitive envtest that has intermittently blocked merges; rerun the `ci`
  job if it fails, and consider a standalone de-flake PR.
- Local dev stack is NOT started. Per `.session.md`, start `moon run root:dev-up`
  from the implementation worktree only after a task is chosen and an
  implementation worktree is selected/created.

Still awaiting the user's actual goal.

## 2026-05-30 17:10 — Session-041 code review (workflow)
Ran a multi-agent review workflow over the session-041 CLI code (host-access
verbs + the YACD_* contract, PRs #59-63, #66, #67; base c7825f8..head e45ad76,
path-scoped to exclude the interleaved PR #64 cardano-tools work). Script saved
at `.journal/043/session-041-review.workflow.js` (resumable). 9 dimension
finders → 30 raw findings → 3 adversarial verifiers each → **26 confirmed
(0 critical, 0 high, 5 medium, 12 low, 9 nit), 1 uncertain, 3 refuted**. 101
agents, ~20 min.

Verdict: solid, shippable work; no critical/high defects. Token-handling
discipline and the hexagonal boundary held well. The weak spots are the newest
resilience/long-wait features:
- MEDIUM `tests-1`: the `connect` reconnect/backoff supervision loop (PR #63's
  headline feature) is entirely untested (`runConnect` ~47-53% cov).
- MEDIUM `correctness-kube-1`: `Forward` doesn't promptly honor ctx cancellation
  during the SPDY dial (no Dial/Timeout on the port-forward rest.Config) —
  `access.go:153-168`. No unit or e2e coverage.
- MEDIUM `ux-1`: interactive `exec` requests a remote TTY but never enters raw
  mode / sends a window size (`exec.go:91-101,128-137`).
- Plus low/nit: `topup --await` polls silently & swallows the submitted TxID on
  the failure path; Ctrl-C misreported as timeout; stale token-free
  `endpoints.json` after `connect` exit; `cli/doc.go` omits the new
  `UTxOConfirmer` export; `host-access.md:17` `$SHELL` wording inverted.

Top recommended fixes (in priority order): test the connect reconnect loop;
bound Forward's cancellation; fix exec TTY raw-mode (or document non-interactive);
pin the await-at-requested-address security invariant in a test; make
`topup --await` observable/parseable; tidy connect's stale endpoints.json;
batch the doc/contract drift fixes. Full report delivered in-session; not yet
acted on — awaiting user direction on which (if any) to implement.

## 2026-05-30 18:10 — PR1 opened (items 7, 8, 10)

Goal locked in after planning: complete all 11 session-042 next steps across
~5 PRs with human review before each merge. Plan file:
`.claude/plans/ok-please-propose-a-curious-toucan.md`.

Key planning decisions (from the user via AskUserQuestion):
- Profile staging (F0): reuse node state PVC at `/state/profile`, idempotent
  cardano-tools fetch init.
- serve: always-on sidecar + owned ClusterIP Service + always-advertised
  status.Endpoints (PR3).
- Fewer/larger PRs (~5-6).
- First cardano-tools release (item 9) cut mid-sequence; user merges the
  release-please PR, then the manager default pins the published digest.
- Dependency correction: image foundation (7/8/10) lands BEFORE the F0 redesign
  because the fetch init needs the image to exist/be buildable/be released.

PR mapping: PR1=7+8+10; release step=9; PR2=1+2+3+6; PR3=4; PR4=5; PR5=11.

Implementation worktree: `.wt/feat-cardano-tools-image-foundation` (off master).
Dev stack: `moon run root:dev-up` succeeded; operator Running in yacd-system on
kind-yacd-dev. Stack kept warm for the session.

PR1 (branch feat/cardano-tools-image-foundation, commit 1c1f31a) -> **PR #65**:
- New `internal/cardano/toolsimage` shared package (Repository/Revision/Reference
  + unit tests). Both controllers consume the same default; existing
  cardano-testnet seam left as-is (retiring it is out of scope — cardano-tools
  lacks the `yacd-cardano-testnet-init` entrypoint, uses `generate`).
- `--default-cardano-tools-image` flag (cmd/options.go + options_test.go) ->
  cmd/setup.go -> exported field on BOTH reconcilers. Builder method/field
  deliberately NOT added yet (would trip the `unused` linter; lands in PR2 when
  fetch/serve consume it). So PR1 is genuinely no-behavior-change.
- Helm cardanoTools.image.{repository,tag,digest}: values.yaml,
  values.schema.json, _helpers.tpl `yacd.cardanoToolsImage` (digest>tag>repo,
  empty omits flag), controller-deployment.yaml arg. helm lint + template OK.
- Dev seam: `.dev/build-cardano-tools.sh` (ROOT build context, unlike
  cardano-testnet which builds from its own dir) + Tiltfile `cardano-tools-image`
  local_resource + chart set values + resource_deps.
- Item 8: single-arch amd64 `cardano-tools-image` job in ci.yml (buildx gha
  cache scope cardano-tools-ci-amd64, distroless smoke version + generate
  --dry-run). Pinned to the same action SHAs as release-dry-run.yml.
- Item 10: static-linkage guard in containers/cardano-tools/Dockerfile fetch
  stage (binutils=2.40-2; readelf PT_INTERP + NEEDED checks; FATAL on dynamic).
  Scoped to cardano-tools only (distroless/static = load-bearing); cardano-testnet
  is debian-slim so a guard there would be advisory — left out to keep PR focused.

Validation: root:check OK, root:test OK (cmd/toolsimage/cardanonetwork/
cardanodbsync envtest green), local `docker build` of cardano-tools rebuilt the
fetch stage (binutils invalidated cache) so the guard genuinely ran against the
real IOG 11.0.1 binaries and passed.

Process notes / gotchas this session:
- Heavy tool-result batching/delivery lag again; several Edit calls reported
  "string not found" against stale buffers — re-grepped exact bytes before each
  retry. One phantom "stray file" (cardanonetwork/toolsimage.go) was a misread of
  a delayed diff; the file never existed (git rm failed "did not match").
- gopls shows false `malformed import path "{{context.Compiler}}"` diagnostics
  (proto go-shim word-split) — ignore; direct `go build` with
  PATH=~/.proto/tools/go/<ver>/bin is the source of truth.

Next: wait for PR #65 review/merge. Then item 9 (user merges release-please PR;
record published cardano-tools @sha256 digest), then PR2 (F0 transport redesign).

## 2026-05-30 18:14 — PR #68 e2e flake (Docker Hub 429), re-running

PR #68 CI: `ci` + ALL release dry-runs green (including the new cardano-tools
amd64/arm64 dry-runs and the Cardano Tools Image Dry Run smoke). The `e2e`
(KinD+Chainsaw manager-smoke) job FAILED — root cause is a Docker Hub
unauthenticated pull rate limit (HTTP 429 / `toomanyrequests`) on the
third-party `cardanosolutions/ogmios:v6.14.0` and `cardanosolutions/kupo:v2.11.0`
images, NOT anything in PR1:
- the operator deployed fine; cardano-node + create-env init + faucet all pulled
  and ran (ghcr/example images), only the Docker Hub sidecars 429'd.
- PR1 touches no ogmios/kupo image references; it's no-runtime-behavior.
- This is the exact "Docker Hub rate-limit jitter" flake TECH_NOTES already
  flags for the e2e / future yacd-env action (preload ogmios/kupo).
Action: `gh run rerun --failed` on the e2e job. If it recurs, it's still infra,
not the PR. Durable fix (later, not PR1): authenticated Docker Hub pulls or
mirror/preload ogmios+kupo in the e2e job — candidate add to the e2e-hardening
backlog.

Correction to an earlier worry: there was NO stray `cardanonetwork/toolsimage.go`
file — that was a misread of a delayed/batched diff; the file never existed.
Final PR1 changeset is 15 files (confirmed via numstat), whitespace clean.

## 2026-05-30 18:30 — PR #68 fully green, review-ready

Both flakes cleared on re-run; PR #68 is now green end-to-end:
- ci: PASS (attempt 2). Attempt 1 flaked on the pre-existing envtest
  TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync
  ("Condition never satisfied", 17.65s) — present on master, NOT in PR1's
  changeset, and the same commit 7e8f609 passed ci in the sibling run + twice
  locally. Classic envtest eventual-consistency timeout under CI load. Re-run
  green (all packages ok, cardanonetwork 34.967s).
- e2e: PASS (re-run). Attempt 1 flaked on Docker Hub 429 (anonymous pull-rate
  limit) for cardanosolutions/ogmios + /kupo — third-party images PR1 doesn't
  touch. The TECH_NOTES-flagged jitter.
- cardano-tools-image (new PR-CI job): PASS.
- Kusari Inspector: PASS. Release dry-runs: correctly skipped (release-please
  branch gate).

PR #68 is READY TO MERGE pending human review. Everything downstream is
user-blocked:
  1. human reviews/merges PR #68
  2. item 9 first cardano-tools release (user merges the release-please PR;
     record the published @sha256 digest)
  3. PR2 (F0 transport redesign) — can't start until #68 merges AND the digest
     exists, so not starting it speculatively.

Two flake patterns to fold into the e2e-hardening backlog (NOT PR1):
- Docker Hub 429 on ogmios/kupo → authenticated pulls or preload/mirror in the
  e2e job (also relevant to the future yacd-env action, per TECH_NOTES).
- Flaky TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync envtest
  timeout → worth a longer Eventually timeout / poll interval.

Loop standing down to a long heartbeat — nothing autonomous to advance until
the user acts on #68.

## 2026-05-30 (later) — User LGTM on PR #68; enqueued for merge

User reviewed PR #68 and said "LGTM. Proceed." Repo requires the GitHub merge
queue, so plain `gh pr merge --squash` is rejected; used
`gh pr merge 68 --squash --auto` → auto-merge enabled, PR will merge via the
merge queue once queue CI passes (~15 min, e2e dominates). Not merged yet.

Next once it lands (blocked on the queue):
- Item 9 (user-gated): the release-please PR for cardano-tools should
  open/update on the merge-to-master push. Surface it for the user to merge;
  do NOT merge it myself (their explicit decision: "you merge it"). After the
  release workflow publishes, record the `@sha256` digest.
- PR2 (F0 transport redesign, items 1/2/3/6): branch fresh off post-merge
  master (PR2 needs PR1's `internal/cardano/toolsimage` + builder wiring to
  compile, so it must wait for the squash merge to land on master).
- Dev stack still up on the PR1 worktree; will need to repoint at the PR2
  worktree when PR2 reaches in-cluster testing.

## 2026-05-30 (later) — PR #68 MERGED to master; item 9 = release PR #65 (user gate)

PR #68 squash-merged to master (commit f11486d); `internal/cardano/toolsimage`
confirmed present on origin/master. PR1 (items 7/8/10) is DONE.

Item 9 release PR is **#65** (`chore(master): release cardano-tools
11.0.1-yacd.4`). Inspected:
- Files: creates `containers/cardano-tools/CHANGELOG.md` (release-please-authored
  — the no-pre-seed-CHANGELOG constraint held ✓) + bumps manifest
  `containers/cardano-tools` 11.0.1-yacd.0 → 11.0.1-yacd.4.
- Version surprise: plan expected first tag `.0/.1`; release-please computed
  `.4`. Config for cardano-tools is a clone of cardano-testnet
  (`versioning: prerelease`, `prerelease-type: yacd`); the generated CHANGELOG
  compare link references a nonexistent `cardano-tools-v11.0.1-yacd.3`. Root
  cause is release-please's prerelease counter, not a collision. HARMLESS:
  `ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.4` is a fresh image repo, valid
  tag, no collision with cardano-testnet (different component/repo). The broken
  compare link would be broken at ANY first-release number (no prior tag), so
  forcing `.1` gains nothing functional.
- Recommendation to user: merge #65 as-is (`.4`). Forcing `.1` needs a manifest
  edit / Release-As + force-push to the release branch for purely cosmetic gain.

Decision: HOLD PR2 until #65 merges and the release workflow publishes, because
PR2's `toolsimage.Revision` default (currently `yacd.0`) must match the
published tag — i.e. it becomes `yacd.4` (or a digest pin). Building PR2's bulk
before that is fine in theory but the Revision is the one real coupling, so I'm
waiting for the user's item-9 outcome. Item 9 is explicitly user-gated
("you merge it"), so I will NOT merge #65.

When #65 merges: release-cardano-tools.yml fires on tag
`cardano-tools/v11.0.1-yacd.4` → publishes multi-arch manifest + attests. Grab
the `@sha256` digest from the release job output, then start PR2 (worktree off
fresh master) and set toolsimage.Revision/digest accordingly.

## 2026-05-31 (later) — Release PR #65 merged; PR2 started

User approved ("LGTM. Please proceed."). Merged release PR #65 (squash). Tag
`cardano-tools/v11.0.1-yacd.4` pushed; `release-cardano-tools.yml` run
26702435140 in progress (~8 min to publish multi-arch manifest + attest digest).
Item 9 effectively DONE pending the workflow publishing the image; need to grab
the `@sha256` digest from the release job output for PR2's toolsimage pin.

PR2 (F0 transport redesign, items 1/2/3/6) worktree created:
`.wt/feat-f0-public-profile-pvc` off fresh master (has PR1's toolsimage).
Approach per plan: public profiles staged on node state PVC at /state/profile
via idempotent cardano-tools fetch init; manifest-only ConfigMap (connection.json
+ yacd-public-profile.json w/ per-file sha256); drop manager //go:embed + share
pins via internal/cardano/publicpins; public report path. Building the bulk now
(independent of the digest); will set toolsimage.Revision=yacd.4 (or digest pin)
at the end once the release publishes.

Open decision to resolve during PR2 (flagged in plan): whether to promote
genesis/checkpoints to pinned digests in publicpins (complete manifest
integrity) vs rely on cardano-node config.json hash verification. Will pick the
pinned-digest route for a complete integrity contract unless it proves
impractical.

## 2026-05-31 (later) — Item 9 DONE: cardano-tools published; PR2 slicing

cardano-tools release workflow (run 26702435140) SUCCESS. Published:
- tag `11.0.1-yacd.4`
- manifest digest `sha256:9ca9e03348c3f9d22408be36f1525c3ef518ab6e0b0053b0a05f2b8401a6039e`
  (multi-arch amd64+arm64, attested).
PR2 will pin the manager default to this digest (toolsimage: Revision yacd.4 +
optional @sha256 digest pin).

Decision RESOLVED (was flagged open): publicpins will pin genesis/checkpoints
per-file digests too (complete manifest integrity), since all digests are now
computed. Captured profile file sha256s in /tmp/profile_hashes.txt; config/
topology/mithril match existing pins.go constants (cross-check passed). NOTE:
peer-snapshot stays UNPINNED/optional (advances with chain) — manifest marks it
digest-exempt; fetch verify must not fail on it. SOURCE.md is not an artifact
(skip).

PR2 build order (committed slices on feat/f0-public-profile-pvc):
  1. internal/cardano/publicpins (shared pins+metadata+digests+contentHash) [foundation]
  2. publicnet BuildPlan: drop //go:embed, source from publicpins; manifest gains per-file sha256
  3. cardano-tools fetch: pins.go -> thin adapter over publicpins; add --verify-manifest
  4. controller: manifest-only public ConfigMap + /state/profile fetch init + node mount repoint
  5. mode-aware dataContract (public requires connection.json+yacd-public-profile.json)
  6. public report path + goldens; toolsimage digest pin; tests + chainsaw preview

## 2026-05-31 (later) — PR2 coding DEFERRED: tool-result delivery corruption

Started PR2 reads to build internal/cardano/publicpins but BOTH Read and
`git show` results came back garbled this turn (truncated <64-hex digests,
phantom lines like `mainnetConfigSHA256Hash = ""` and `func pinsFor( placeholder`,
non-rendering grep counts). The on-disk files are the merged PR #64 code (CI
passed) and are fine — the corruption is in tool-result DELIVERY, intermittent
all session. Refusing to author publicpins (must reproduce exact 64-char
digests + pinsFor structure) from unreliable reads.

State is clean to resume: nothing written to the PR2 branch yet (only journal
commits on journal branch). Resume plan unchanged — start at slice 1
(publicpins) per the build order above, re-reading pins.go + publicnet/{plan,
types,fingerprint}.go cleanly first. Published digest for the toolsimage pin:
sha256:9ca9e03348c3f9d22408be36f1525c3ef518ab6e0b0053b0a05f2b8401a6039e
(tag 11.0.1-yacd.4). Profile file digests captured in /tmp/profile_hashes.txt
(ephemeral — recompute with shasum -a 256 on the profiles/ tree if gone).

## 2026-05-31 (later) — PR2 slice 1 committed: publicpins foundation

Reads came back clean this tick; earlier "delivery corruption" was overcautious
(intermittent display garble, not file corruption). Proceeded with PR2 slice 1.

Committed on feat/f0-public-profile-pvc (17025c3, pushed to origin; NO PR yet —
push to a non-master branch w/o PR doesn't trigger ci.yml):
- internal/cardano/publicpins/publicpins.go — shared curated profile registry
  (File{ArtifactKey,SourceName,ConnectionKey,Source,SHA256,Optional,Pinned},
  Profile, Mithril; Lookup/Known/MithrilAggregatorEndpoint). Pins EVERY chain
  file digest (complete-integrity route), peer-snapshot unpinned/optional.
- internal/cardano/publicnet/publicpins_crosscheck_test.go — recomputes every
  pinned digest against the still-embedded profile bytes; PASSED, so the
  hand-transcribed digests are proven correct vs ground truth before slice 2
  deletes the embed. (Defends against the delivery flakiness.)
Validated: go build + vet + test + golangci-lint all green. No behavior change
(publicpins has no consumers yet; embed + fetch pins untouched).

Remaining PR2 slices (resume here, fresh context):
  2. publicnet BuildPlan: source from publicpins, DROP //go:embed + delete
     profiles/*/ tree (keep SOURCE.md? no — provenance now in publicpins.SourceURL);
     manifest per-file sha256 from publicpins (not contentHash of bytes).
     Update .dockerignore (drop the profiles re-include). Keep cross-check test
     working: it relies on the embed — must convert it to assert publicpins
     digests another way (e.g. pin a golden, or move the byte check into a
     fetch integration test) BEFORE deleting the embed, else the safety net and
     the embed vanish together. Plan: in slice 2, replace the embed-based
     cross-check with a frozen-golden test of the publicpins digests.
  3. cardano-tools fetch: pins.go -> thin adapter over publicpins; add
     --verify-manifest; keep fetch-dry-run.txtar (URLs/pins identical).
  4. controller: manifest-only public ConfigMap (connection.json +
     yacd-public-profile.json) + /state/profile fetch init (cardanoToolsImage,
     before mithril) + node mount repoint to /state/profile.
  5. mode-aware dataContract (public requires only connection + public-profile
     manifest; local unchanged so report-dry-run golden sha256:f1cd9ad8 stays).
  6. public report path + goldens; pin manager default to
     sha256:9ca9e03348c3f9d22408be36f1525c3ef518ab6e0b0053b0a05f2b8401a6039e
     (toolsimage Revision yacd.4 + digest); builder_test + chainsaw preview.

Dev stack still up (PR1 worktree). Will repoint to PR2 worktree at slice 4+
in-cluster testing.

## 2026-05-31 (later) — PR2 HELD at slice 1 pending genesis-pinning decision

Stopped advancing PR2 autonomously. Reasoning: slices 2-3 activate publicpins in
the fetch path, which forces plan open-item #1 (genesis pinning) — a decision the
approved plan explicitly flagged "confirm during implementation," and which I had
resolved unilaterally toward the riskier "pin everything" option in slice 1.

The risk that stopped me: pinning genesis means `fetch` verifies each downloaded
genesis file's sha256 against my pin at DOWNLOAD time (new failure mode). I pinned
against the repo's embedded bytes (cross-check PASSED), but cannot validate against
the LIVE operations-book source without a network fetch while the user is away. If
the book serves genesis with different bytes than the checked-in copies, public
fetches break. Reaching for "the embedded copies probably came from the book" was
the signal to wait, not push.

Recommendation surfaced to user: switch to the BEHAVIOR-PRESERVING model — pin only
config.json + topology + Mithril (what fetch pins today), let cardano-node verify
genesis transitively via config.json inline hashes (proven model, no new failure
mode). If accepted, slice 1 needs a small revision (un-pin genesis in publicpins +
adjust the cross-check test to only the pinned set). If user keeps complete-
integrity, validate genesis pins against the live book FIRST.

State: items 7/8/9/10 done+merged. PR2 slice 1 (publicpins, commit 17025c3) pushed
to origin/feat/f0-public-profile-pvc, NO PR opened, inert/tested/safe to leave or
revise. Embed removal (item #3, destructive but git-recoverable) deferred until
after the pinning decision. Dev stack still up on the PR1 worktree.

Loop -> long heartbeat; PR2 activation slices are now user-blocked on the pinning
decision (like the earlier PR-review waits).

## 2026-05-31 (later) — Genesis-pinning decision RESOLVED: trust remote source

User decided: "divert more trust to the remote source... intended for test
environments... what most node operators do." => behavior-preserving model.

Revised PR2 slice 1 accordingly (amended commit 464a960, force-pushed to
origin/feat/f0-public-profile-pvc, branch in sync, clean):
- publicpins now pins ONLY config.json + topology.json + Mithril keys (8 pinned
  digests total: 3 config + 3 topology + 2 mithril). genesis + checkpoints are
  downloaded but UNPINNED — authenticated downstream by cardano-node against
  config.json's inline hashes. peer-snapshot unpinned/optional. This matches the
  already-merged fetch/pins.go posture exactly.
- File.Pinned now means "verify at fetch time"; doc comment rewritten to explain
  the trust anchor model.
- Cross-check test (publicnet) still recomputes the pinned digests vs embedded
  bytes and PASSES — now covers only the pinned set, which is the correct scope.
- build/test/vet/golangci-lint all green.

This unblocks the PR2 activation slices (no new genesis failure mode; faithful
refactor of existing fetch behavior). Resume at slice 2:
  2. publicnet BuildPlan -> source file list/metadata from publicpins; DROP
     //go:embed + delete profiles/*/ tree; manifest fingerprint still over the
     pinned-file digests from publicpins (NOT contentHash of genesis bytes the
     manager no longer has). BEFORE deleting embed, convert the cross-check test
     to a frozen-golden of the publicpins pinned digests (else safety net dies
     with the embed). Update .dockerignore (drop profiles re-include).
  3. cardano-tools fetch: pins.go -> adapter over publicpins; add
     --verify-manifest; fetch-dry-run.txtar URLs/pins unchanged.
  4. controller manifest-only ConfigMap + /state/profile fetch init + node mount.
  5. mode-aware dataContract.
  6. public report + manager digest pin (sha256:9ca9e033...) + tests + chainsaw.

## 2026-05-31 (later) — User LGTM to continue; slice 3 deferred (degraded delivery)

User: "LGTM. Please continue." Genesis-pinning fork resolved (trust remote);
slice 1 (publicpins, commit 464a960) is clean on origin/feat/f0-public-profile-pvc.

Read the slice-2/3 source cleanly this tick (captured below), but tool-result
DELIVERY degraded mid-tick (corrupted grep echo + a "<system_warning> delivery
degraded" banner). Did NOT author slice 3 — the pins.go adapter + fetch_test.go
rewrite encode exact digests/URLs and a corrupted read could land a wrong pin.
Worktree verified clean (0 dirty, HEAD 464a960). Nothing partially written.

Decision: chose slice 3 (cardano-tools fetch -> publicpins adapter) as the next
slice rather than slice 2 (drop embed), because slice 2 has an unresolved design
question I should NOT decide solo: with the embed gone, the manager has no genesis
bytes, so (a) the public fingerprint can no longer be contentHash(genesis bytes)
— must switch to fingerprinting over the publicpins pinned digests + networkMagic
+ requiresNetworkMagic; and (b) CUSTOM profiles (user-supplied bytes via
ConfigMap/Secret, no fetch) still need their own artifact+fingerprint path. Slice
3 is pure behavior-preserving refactor (golden-locked), so it's the safe next
autonomous step; slice 2's fingerprint/custom design may warrant a quick user
check.

FACTS captured for slice 3 (verified from clean reads this tick):
- fetch.go depends on pins.go: `pinsFor(profile) ([]pinnedFile, bool)`,
  `knownProfiles []string`, and pinnedFile fields {dest, url, expectedSHA256,
  optional}. writeDryRun prints "<dest>\t<url>\t(sha256:<hex>|unpinned)".
- fetch_test.go refs package idents: bookBase, previewConfigSHA256,
  previewTopologySHA256 (helpers previewBodies/pinnedPreviewConfig/
  embeddedProfileFile). It keys fake-doer bodies by `bookBase+"preview/<src>"`
  and asserts pin==digest(embedded). Rewrite to source URL+digest from
  publicpins (publicpins.Lookup("preview"); File.URL(profile); File.SHA256),
  add helper bookSourceURL(t,profile,sourceName)->File.URL.
- golden testdata/fetch-dry-run.txtar must stay byte-identical: preview prints
  configuration.yaml + primary-topology.json as sha256:<64hex>, byron-genesis as
  unpinned; mainnet prints mithril-genesis.vkey/ancillary.vkey pinned; unknown
  profile errors "unknown profile". (publicpins already reproduces all these.)
- profile data extracted from embedded files BEFORE any deletion:
  preview networkMagic=2 RequiresMagic; preprod=1 RequiresMagic;
  mainnet=764824073 RequiresNoMagic.

Slice 3 adapter shape (pins.go): keep pinnedFile struct + pinsFor + knownProfiles
but derive from publicpins.Lookup/Known; pinned files copy SHA256 only when
File.Pinned. Drop the per-profile SHA consts + bookBase/bookFile/chainArtifacts
(now in publicpins). Then update fetch_test.go. Validate: go test ./...fetch/...
+ the txtar golden + golangci-lint, commit, push (no PR yet).

Resume: re-read pins.go + fetch_test.go cleanly, then write slice 3.

## 2026-05-31 (later) — PR2 slice 3 landed (fetch -> publicpins adapter)

Delivery recovered; proceeded with slice 3 (behavior-preserving, golden-locked).
Committed 3c3a253 on origin/feat/f0-public-profile-pvc (branch 0/0, clean):
- containers/cardano-tools/internal/fetch/pins.go collapsed to a thin adapter
  over publicpins (pinsFor + knownProfiles derive from publicpins.Lookup/Known;
  dropped the duplicated digest consts + bookBase/bookFile/chainArtifacts).
  pinnedFile shape + Run/writeDryRun unchanged.
- fetch_test.go sources URLs/digests from publicpins (single source of truth);
  fixed an unparam lint (helpers are preview-only -> previewProfile const).
Validated: whole-module go build, cardano-tools tests incl. fetch-dry-run.txtar
golden (byte-identical), vet, golangci-lint — all green. publicpins now has its
first consumer; slice 1 is no longer inert.

PR2 progress: slice 1 (publicpins) + slice 3 (fetch adapter) DONE on branch, no
PR opened yet. Remaining: slice 2 (drop //go:embed + reshape BuildPlan), 4
(controller manifest-only ConfigMap + /state/profile fetch init), 5 (mode-aware
dataContract), 6 (public report + manager digest pin + tests + chainsaw).

NEXT = slice 2, but it has TWO design questions I should put to the user rather
than decide solo (both flow from the manager losing genesis bytes when the embed
is removed):
  Q1 fingerprint: today the public fingerprint = sha256 over per-file
     contentHash(genesis+config+topology bytes). With the embed gone the manager
     no longer has genesis/checkpoints bytes, so it can only fingerprint over the
     files it still knows digests for (config+topology+mithril, from publicpins)
     plus networkMagic + requiresNetworkMagic. That CHANGES the fingerprint value
     + algorithm (a stored-identity input). Need to confirm that's acceptable
     (it is pre-1.0, no persisted public networks yet) vs. keeping a byte-level
     fingerprint sourced differently.
  Q2 custom profiles: custom public profiles are user-supplied bytes via
     ConfigMap/Secret (no fetch, no pins). Their artifact+fingerprint path must
     stay byte-based and is orthogonal to the curated-fetch redesign — confirm
     custom keeps mounting its bytes (likely still via a ConfigMap since custom
     bundles are small + already size-bounded), i.e. the PVC-fetch model applies
     to CURATED public profiles only.
Also slice 2 deletes the embed which kills the publicpins cross-check test's
ground truth -> must convert that test to a frozen-golden of the publicpins
pinned digests in the SAME slice.

Dev stack still up (PR1 worktree). Will surface Q1/Q2 to user next.

## 2026-05-31 (later) — Slice 2 design Qs ANSWERED

Q1 fingerprint -> "Pinned digests + magic": curated public fingerprint becomes
sha256(json{schemaVersion, profile, networkMagic, requiresNetworkMagic,
files:[config, topology, mithril vkeys -- pinned set only]}). Genesis/checkpoints
EXCLUDED (manager no longer has bytes; node verifies downstream). Fingerprint
value+algorithm changes; OK pre-1.0 (no persisted public networks). plan_test.go
golden fingerprints WILL change intentionally -> recompute + update goldens.
Q2 custom -> "Curated-only; custom unchanged": PVC-fetch + manifest-only ConfigMap
applies to curated preview/preprod/mainnet ONLY. Custom keeps byte-based path:
user-supplied bytes stay in the artifacts ConfigMap mounted directly (no fetch,
no pins). So BuildPlan keeps two paths: curated (metadata/digests from publicpins,
no embed) vs custom (bytes from CustomBundle, fingerprint over those bytes as
today).

Slice 2 concrete plan (this is the F0 core, controller wiring follows in slice 4):
- publicnet/plan.go: delete //go:embed + profileAssets; loadCuratedArtifacts no
  longer reads bytes. Curated path now produces, from publicpins.Lookup(profile):
  the file list (ArtifactKey/ConnectionKey/SourceName/Pinned/SHA256/Optional),
  the manifest (Files map + per-pinned-file sha256), and fingerprint over the
  pinned-set digests + magic. networkMagic/requiresNetworkMagic are no longer
  parsed from genesis bytes the manager lacks -> move them into publicpins as
  static per-profile facts (preview magic=2 RequiresMagic; preprod=1 RequiresMagic;
  mainnet=764824073 RequiresNoMagic -- captured from embed before deletion).
  Mithril vkeys: manager no longer has the .vkey bytes; MithrilPlan.{Genesis,
  Ancillary}VerificationKey were sourced from artifacts[...] -> now must come
  from publicpins (the vkeys are tiny + pinned; safe to carry their CONTENT in
  publicpins? NO -- publicpins has digests, not bytes). DECISION NEEDED in-slice:
  either (a) vkeys move to fetch-staged files + MithrilPlan references the staged
  path, or (b) keep the two .vkey files embedded (they are 223B/221B, trivial)
  while dropping the big genesis embed. Lean (b): keep mithril vkeys embedded
  (tiny, and Mithril bootstrap needs their content as env/args), drop only the
  large genesis/config/topology/checkpoints embed. Re-confirm against init
  container usage in slice 4.
- Custom path unchanged (still reads CustomBundle.Files, fingerprints over bytes).
- Manifest gains per-file sha256 for pinned files (yacd-public-profile.json).
- Convert publicpins_crosscheck_test.go: it currently reads embedded bytes; after
  embed removal of genesis it can still read config/topology (if those stay
  embedded?) -- but plan says drop the whole curated embed. So convert the
  cross-check to a frozen golden of publicpins pinned digests in THIS slice.
- .dockerignore: drop the profiles re-include for the deleted tree (but KEEP it
  if mithril vkeys remain embedded -> adjust to the narrower path).
- plan_test.go: update golden fingerprints (intentional change).

OPEN micro-decision for slice 2 (vkeys embed-or-fetch) noted above; will resolve
by reading init_container.go mithril usage in slice 4 prep, leaning keep-vkeys-
embedded. This does not need a user round-trip (reversible, internal).

## 2026-05-31 (later) — Slice 2 NOT started: corrupted source reads

Both design answers are in (fingerprint=pinned-digests+magic; custom=unchanged),
so slice 2 is unblocked in principle. But the reads needed to author it came back
corrupted this tick: plan_test.go showed garbled line-number prefixes (33/32/31
where 53/82/93/101 belong) and init_container.go mithril read truncated. Slice 2
is the F0 core — deletes //go:embed, CHANGES the public fingerprint goldens
(stored-identity input), and reworks Mithril vkey sourcing — too high-blast-radius
to write from corrupted reads. Held.

State is clean: slices 1 (publicpins, 464a960) + 3 (fetch adapter, eb96db9) on
origin/feat/f0-public-profile-pvc, branch 0/0, worktree clean, no PR yet. Nothing
partial written. Resume slice 2 with fresh reads of: publicnet/plan.go,
plan_test.go (golden fingerprints to recompute), internal/controller/cardanonetwork/
init_container.go (mithril vkey usage -> resolve the embed-vkeys-or-fetch micro-
decision, leaning keep-vkeys-embedded since they're 223B/221B and Mithril needs
their content), and .dockerignore.

Captured-before-deletion facts (safe to reuse): per-profile static identity is
preview{magic=2,RequiresMagic}, preprod{magic=1,RequiresMagic},
mainnet{magic=764824073,RequiresNoMagic}; profile file digests in
/tmp/profile_hashes.txt (recompute via shasum if gone). Manager cardano-tools
digest pin (slice 6): sha256:9ca9e03348c3f9d22408be36f1525c3ef518ab6e0b0053b0a05f2b8401a6039e.
