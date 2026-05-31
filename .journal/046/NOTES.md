---
id: 046
title: F0 redesign — PR-A continuation (serve sidecar + manifest)
started: 2026-05-31
---

## 2026-05-31 13:08 — Kickoff
Goal for the session: continue the F0 redesign work begun in session 043 (the
"manager is not an authoritative config source / remove the network-artifacts
ConfigMap entirely" redesign). The session-043 NOTES end with a full
"HANDOFF — START HERE" section; the immediate next step is **PR-A / A2** (wire
the always-on cardano-tools `serve` native sidecar into the primary Deployment
and get a `manifest.json` written into the served dir).

Current state of the world:
- `master` is clean at `2f28360 fix(cli): harden review findings (#73)` (session
  044). The personal journal branch `journal/jmgilman` is clean/up to date after
  the session-045 checkpoint.
- Implementation worktree already exists: `.wt/feat-f0-public-profile-pvc`,
  branch `feat/f0-public-profile-pvc` @ `41def22`, attached and clean. It carries
  4 commits on top of master and is **1 commit BEHIND** current `master`
  (it was last rebased onto `dbaa886`; `#73`/`2f28360` landed afterward) — rebase
  onto `2f28360` before resuming.
- The 4 banked commits (all behavior-additive / golden-locked, no PR opened):
  `0f00ad0` publicpins registry, `83cdb7f` fetch→publicpins adapter, `cd87128`
  publicpins static per-profile identity, `41def22` **A1** served-artifact
  manifest contract (`internal/cardano/networkartifacts/manifest.go` + `ManifestKey`
  added to optional contract keys + golden fix). `moon run root:test` and
  `root:check` were green at `41def22`.
- Items 7/8/9/10 of the original F0 plan are DONE+merged on master (cardano-tools
  image seam/PR-CI/static-musl guard; published
  `ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.4`
  `@sha256:9ca9e03348c3f9d22408be36f1525c3ef518ab6e0b0053b0a05f2b8401a6039e`).
- Session 045 (CardanoNetwork node sync status, branch
  `feat/cardanonetwork-sync-status`) remains `in-progress`/dormant and is a
  SEPARATE work-stream — not touched by this session.
- Dev stack: `.run/yacd-dev` is owned by the OLD PR1 worktree
  (`.wt/feat-cardano-tools-image-foundation`), context `kind-yacd-dev`. For an
  in-cluster smoke from this branch it must be repointed (`dev-down` then
  `dev-up` from `.wt/feat-f0-public-profile-pvc`). Not started yet this session.

Redesign (DECIDED in 043 — do not relitigate): manager holds no configs, only
`publicpins` metadata; local generates / public fetches configs onto the node
state PVC at `/state/profile`; `cardano-node` reads from the PVC (no ConfigMap);
every other consumer (db-sync, CLI, external) fetches over HTTP from an always-on
cardano-tools `serve` sidecar + owned ClusterIP Service; integrity/discovery via
a served `manifest.json` (schemaVersion + per-file sha256). PR order is
**A → C → B → D** (verified: A→B→C→D bricks db-sync). PR-A is additive (ConfigMap
stays) so build + chainsaw stay green throughout.

Key gotchas carried from 043: (1) commits are GPG-signed and need the user present
for pinentry, else they cancel silently — verify HEAD moved; (2) intermittent
Read-tool corruption was reported all session — cross-check suspicious reads with
`git show HEAD:<path>` before editing; (3) use `moon run root:test` (not plain
`go test`, which lacks KUBEBUILDER_ASSETS); (4) re-check `git symbolic-ref HEAD`
around rebase/amend (detach risk).

Plan: review complete; summarize current state + next steps for the user and
await their go-ahead before bringing up the dev stack and resuming PR-A / A2.
References: `.journal/043/SUMMARY.md` + the "HANDOFF — START HERE" section at the
end of `.journal/043/NOTES.md`; plan
`.claude/plans/ok-please-propose-a-curious-toucan.md` (A→C→B→D section).

## 2026-05-31 13:25 — Setup done; A2 surface mapped; release/e2e fork found
Setup: rebased `feat/f0-public-profile-pvc` onto current master `2f28360`
(clean; new HEAD `09285ea`, 4 commits), `root:test` + `root:check` both green,
force-pushed. Dev stack repointed from the session-045 sync-status worktree to
this F0 worktree (`dev-down` then `dev-up`); controller Running in yacd-system
on `kind-yacd-dev`.

Ran an 8-agent surface-map + adversarial-verify workflow (`wpq65h8v0`, ~970k
tokens) on the A2 implementation surface, then cross-checked the load-bearing
facts directly. VERIFIED facts (each confirmed against HEAD):
- Image-threading GAP is real: builder lacks `defaultCardanoToolsImage` field +
  `cardanoToolsImage()` method; reconciler field (`controller.go`), Kong flag
  (`cmd/options.go`), and `cmd/setup.go` wiring all already exist. Only the
  builder hop is missing.
- `serve` serves ONE FLAT dir via a default-deny allowlist built from
  `networkartifacts.RequiredKeys()+OptionalKeys()` (compiled into the binary);
  nested paths rejected. A1 added `ManifestKey` to OptionalKeys → only a
  cardano-tools image built from A1+ source exposes `GET /manifest.json`.
- No flat+complete served dir exists today: local `/state/env` is NESTED
  (topology at node-data/node1/topology.json) and has NO connection.json on disk
  (the cardano-testnet publisher synthesizes connection.json only INTO the
  ConfigMap); public artifact bytes live ONLY in the `<net>-network-artifacts`
  ConfigMap mounted read-only at `/profile` (the `public-profile` volume IS that
  ConfigMap, added only when `plan.isPublic()`), never on the PVC.
- Nothing writes `manifest.json` yet (only manifest_test.go calls BuildManifest).
- **Image revision mismatch (key blocker):** `cardano-testnet` default revision
  = `yacd.4` (published, pulled in e2e). `cardano-tools` default = `yacd.0`
  (`internal/cardano/toolsimage/toolsimage.go:23`) — NEVER published (first
  release was `yacd.4`). So the default ref `cardano-tools:11.0.1-yacd.0` is not
  pullable.
- **CI e2e pulls non-manager images from ghcr:** `.dev/scripts/deploy.sh` builds
  ONLY the manager (ko) + `kind load`s it; helm install overrides only `image.*`.
  cardano-tools/cardano-testnet/ogmios/kupo/faucet are pulled by Kind using
  manager/chart defaults. Dev is the exception (Tilt builds cardano-tools `:tilt`
  from source).

CONSEQUENCE / DESIGN FORK (surfacing to user, "pause for design issues"):
PR-A introduces the FIRST cardano-tools runtime container (the serve sidecar).
To serve the LOCAL chainsaw network green, serve needs a flat source dir, which
local cannot provide without new cardano-tools staging code; and even the public
ConfigMap-mount path can't expose manifest.json on the published `yacd.4`. So
PR-A needs a cardano-tools image BUILT FROM CURRENT SOURCE available in CI e2e —
which isn't true today (default `yacd.0` unpublished; `yacd.4` pre-A1). The
handoff's "additive, defer image pin to PR-D" is therefore not achievable as-is.
Decision needed: how to get a current-source cardano-tools image into CI e2e —
(A) build+load it in `deploy.sh` like the manager (no release coupling, mirrors
manager, +build time; my lean), or (B) cut a new cardano-tools release first +
pin the manager default (e2e stays fast, but a release round-trip per change and
release-please may not auto-bump on shared-`internal/cardano` changes), or
(C) minimal: serve existing keys on `yacd.4`, defer manifest serving + the pin
(can't make LOCAL serve green without new code → effectively unworkable for the
chainsaw local network). Recommending A.

PROCESS NOTE: tool-result delivery corruption is ACTIVE this session (duplicated
lines, mangled paths, and FABRICATED prose injected into Bash/Read results).
Confirmed by a clean round-trip test passing while file dumps corrupt. Using
`git show HEAD:<path>` as ground truth + asserted exact-match edits + build
verification before trusting any read.

## 2026-05-31 13:40 — Decision locked (Option A) + ready-to-execute PR-A spec; STOPPED on channel corruption
DECISION (user-confirmed via AskUserQuestion): resolve the cardano-tools image
gap by **building + kind-loading cardano-tools in CI e2e**, mirroring how
`test-e2e.sh` ALREADY build+loads the manager, faucet, and cardano-testnet
images. (Delayed tool delivery finally surfaced `test-e2e.sh`: it `docker build`s
manager + faucet + cardano-testnet and `kind load`s all three, tagging
cardano-testnet as its default `11.0.1-yacd.4`. `deploy.sh` then `helm upgrade
--install`s and only overrides `image.*`/`faucet.image.*`.) So e2e tests
source-built first-party images; cardano-tools just needs the same 3-line
treatment. No release coupling.

EXPANDED PR-A SCOPE (necessary, end-state-aligned): there is no flat+complete
served dir today, so serve needs a real producer. Bring forward the PVC-staging
producer: a stage init writes a flat artifact dir + manifest.json onto the PVC
(`/state/artifacts`) for BOTH modes; serve reads it. ConfigMap stays (additive);
node mounts unchanged.

READY-TO-EXECUTE PR-A COMMIT PLAN (all facts verified vs HEAD before the channel
degraded; RE-VERIFY each file with a trustworthy read before editing):
- Commit 1 (mechanical, low-risk): image threading + revision + e2e load.
  * builder.go: add `defaultCardanoToolsImage string` field to
    primaryWorkloadBuilder (next to defaultCardanoTestnetImage); add method
    `cardanoToolsImage(toolVersion string) string` returning
    `toolsimage.Reference(b.defaultCardanoToolsImage, toolVersion)`. (NOTE: the
    builder uses an options pattern — `newPrimaryWorkloadBuilder` + `applyOptions`;
    find the option/setter that sets defaultCardanoTestnetImage and add the
    parallel one, OR set the field at the construction site. VERIFY the real
    builder.go: it has a struct at ~14-23 and a `newPrimaryWorkloadBuilder`
    constructor; the earlier read was corrupted and even contradicted itself
    re: the runtime import.)
  * controller.go: thread `r.DefaultCardanoToolsImage` into the builder at its
    construction site (field exists on the reconciler already; flag in
    cmd/options.go + cmd/setup.go already wired).
  * internal/cardano/toolsimage/toolsimage.go: bump `Revision = "yacd.0"` ->
    `"yacd.5"` (the upcoming release w/ A1+serve). Update toolsimage_test.go
    goldens (two `11.0.1-yacd.0` -> `11.0.1-yacd.5`).
  * .dev/scripts/test-e2e.sh: add cardano-tools build+load mirroring the
    cardano-testnet 3 lines — `docker build -f containers/cardano-tools/Dockerfile
    -t ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.5 .` (ROOT context, unlike
    cardano-testnet which builds from its own dir) + `kind load docker-image ...`.
    (Pull policy IfNotPresent is set in deploy.sh only under LOCAL_IMAGE; check
    whether sidecar images need the same — manager default pullPolicy may be
    Always; if so the loaded cardano-tools must be referenced so Kind uses it. The
    cardano-testnet load already works in e2e, so copy its exact pattern incl. any
    pullPolicy implication.)
  * Validate: root:test, root:check. No deployment behavior change yet.
- Commit 2 (cardano-tools producer, container module): make the staged dir
  producible.
  * fetch (containers/cardano-tools/internal/fetch): also write connection.json
    + manifest.json into --output-dir (use networkartifacts.BuildManifest +
    Manifest.JSON; connection.json synthesis like the publisher's
    buildConnectionJSON). Keep fetch-dry-run.txtar golden in sync.
  * local staging: cardano-tools `report` already reads a localnet artifact dir
    and builds flat keyed data + connection.json (targets a ConfigMap). Add a
    dir-output path (new `stage`/`assemble` subcommand OR a report flag) that
    writes the flat keyed files + connection.json + manifest.json to an
    --output-dir on the PVC. Reuse the existing flatten+connection logic; add
    unit/txtar coverage. (This is the local producer; end-state-aligned.)
  * docker build (static guard) must stay green.
- Commit 3 (controller: stage init + serve sidecar — additive):
  * defaults.go: add `servedArtifactsDir = "/state/artifacts"` (subdir of the
    existing localnet-state PVC mounted at /state).
  * init_container.go: add stageArtifactsInitContainer (image
    b.cardanoToolsImage(version); hardened SC mirrored from
    cardanoTestnetInitContainer; mount localnet-state PVC at /state). LOCAL: run
    the cardano-tools stage-from-/state/env step -> /state/artifacts. PUBLIC: run
    `fetch --profile <p> --output-dir /state/artifacts`. Order it AFTER the
    create-env/mithril inits.
  * containers.go: add serveContainer (image b.cardanoToolsImage(version); args
    `serve --artifacts-dir /state/artifacts --listen :8090`; ContainerPort 8090;
    ReadOnly /state mount; hardened SC; readiness probe GET /manifest.json :8090).
  * resources.go: append serveContainer to the containers slice (regular
    always-on container, NOT a native sidecar init); append stage init to the
    initContainers slice; ensure /state PVC mount is present (it is). Keep
    node/ogmios/faucet/ConfigMap mounts UNTOUCHED (additive).
  * primarypod.go: add `PortNameServe="serve"` + `DefaultServePort int32 = 8090`
    (8080 collides with DefaultFaucetPort). Do NOT add to PortOwners yet (that
    feeds db-sync placement collision checks; A3/PR-C territory).
  * Validate: root:test, then dev-stack in-cluster smoke (apply a local + a
    preview-public CardanoNetwork; confirm stage init populates /state/artifacts,
    serve Ready, GET /manifest.json + GET /configuration.yaml work via the
    sidecar). builder_test/envtest updates.
- Commit 4 = A3 (Service + status endpoint):
  * resources.go: owned `<net>-artifacts` ClusterIP Service (mirror ogmiosService),
    targets serve port 8090.
  * api/v1alpha1/cardanonetwork_types.go: add `Artifacts *ServiceEndpointStatus`
    to CardanoNetworkEndpointsStatus; root:generate (CRD + deepcopy).
  * status.go: publish status.Endpoints.Artifacts gated on the Service existing
    (mirror ogmios nil-gate). status.Artifacts.DataHash = sha256 of manifest.
  * primarypod.go: now add 8090/PortNameServe to PortOwners + a
    validatePrimaryWorkloadPorts guard.
  * chainsaw: confirm the always-on serve container reaches Ready on the local
    smoke network (no existing assertion should need changing; A is additive).
  * Validate: root:generate, root:check, root:test, root:test-e2e; then open PR-A.

CHANNEL STOP (why I paused before writing code): tool-result delivery is actively
corrupting THIS session — large `git show|cat -n` rendered ~5000 blank lines for
an 80-line file; a small Read of builder.go came back truncated with fabricated
trailing fragments and an internal contradiction (struct uses runtime.Scheme but
the shown imports omit it); the harness injected "previous Read result may be
corrupted" warnings. Small outputs (commit hashes, grep -c, moon summaries, the
round-trip test) remain reliable; large/structured reads do not. Authoring a
multi-file controller + image-pin change from corrupted reads is exactly the
previous session's documented disaster (wrong digest / broken stored-identity).
So I banked this spec and stopped. RESUME when reads are trustworthy: cross-check
every file via a clean Read AND `git show HEAD:<f> | sha256sum` (small output)
before editing; prefer asserted exact-match edits; re-run root:test after each.
Branch feat/f0-public-profile-pvc @ 09285ea, clean, 0/0, rebased on master
2f28360, dev stack UP on this worktree (kind-yacd-dev, controller Running).
