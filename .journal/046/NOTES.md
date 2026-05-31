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

## 2026-05-31 14:05 — Producer design confirmed; implementation BLOCKED by repo-wide read corruption
Channel briefly recovered enough to confirm the Commit-A producer design against
HEAD, then degraded again:
- Shared package is `internal/cardano/networkartifacts` (ROOT module, importable
  by cardano-tools). It already has: `connection.go` (typed `Connection`:
  SchemaVersion, NetworkMagic uint32, SocketPath, NodeConfigFile, NodeTopologyFile,
  Files map[string]string; `ConnectionSchemaVersion=1`), `contract.go`
  (RequiredKeys/OptionalKeys), `manifest.go` (BuildManifest/Manifest.JSON/Verify/
  FileDigest/ManifestKey), `keys.go`. The Explore agent's `internal/cardano/
  artifactset` package is a HALLUCINATION — it does not exist.
- `serve` serves `manifest.json` FROM A FILE (does not compute) — so the producer
  MUST write manifest.json into the served dir.
- Commit-A producer design (locked): (1) extend cardano-tools `fetch` to also
  write connection.json (networkartifacts.Connection; magic from publicpins
  static identity; NodeConfigFile=configuration.yaml, NodeTopologyFile=
  primary-topology.json; SocketPath empty for public) + manifest.json
  (BuildManifest over written files, last); (2) add a `stage` subcommand that
  reuses `report`'s localnet flatten+connection assembly to turn a create-env
  state dir into a flat served dir + connection.json + manifest.json, WITHOUT
  changing report or its golden (sha f1cd9ad8). Both produce a flat dir keyed by
  contract filenames that `serve` can expose.

IMPLEMENTATION BLOCKED: delegated Commit A to a general-purpose subagent (clean
context, robust go-test validation, NO commit). The subagent hit the SAME
tool-result corruption — Read AND `cat`/`head` returned empty/garbled file
contents repo-wide — and correctly refused to author code blind. It made ZERO
changes; working tree is CLEAN (verified). So the corruption is harness/session-
wide right now, not just the main channel.

DECISION: stop code authoring this session. The blocker is purely environmental;
nothing is half-written. Resume Commit A (then Commit B controller serve, Commit
C A3 service/status) per the 13:40 ready-to-execute spec in a FRESH session/
context where file reads are trustworthy (cross-check each file via clean Read +
`git show HEAD:<f>` before editing; asserted exact-match edits; `moon run
root:test` after each). State at pause: branch feat/f0-public-profile-pvc @
09285ea (rebased on master 2f28360, root:test+root:check green), clean, 0/0, NO
PR; dev stack UP on this worktree (kind-yacd-dev, controller Running); design
fork resolved = Option A (build+load cardano-tools in e2e). Items 7/8/9/10 done+
merged; cardano-tools yacd.4 published (pre-A1). Session 045 sync-status stream
untouched.

## 2026-05-31 14:30 — CORRECTION: Commit A actually SUCCEEDED (prior "blocked" note was wrong)
Retract the 14:05 entry's claim that the implementation subagent was blocked and
left the tree clean. That conclusion was drawn from a garbled/early-flushed
partial result. GROUND TRUTH (verified directly): the general-purpose subagent
(addeb55ea4a0e3329) ran ~55 min and COMPLETED "Commit A" successfully; results
flushed late. The F0 worktree now has 12 changed files (UNCOMMITTED) on branch
feat/f0-public-profile-pvc @ 09285ea:
- M fetch/fetch.go, fetch/fetch_test.go, fetch/pins.go (fetch now writes
  connection.json + manifest.json; pins carry per-file connection keys + static
  magic/requiresNetworkMagic from publicpins).
- M cli/root.go, cmd/.../main.go (register `stage`).
- NEW artifactset/publicconnection.go(+test) — RenderPublicConnection.
- NEW internal/stage/ (stage.go, doc.go, stage_test.go) — `stage` reuses
  artifactset.ReadManifest/ReadArtifacts/Build (report's flatten+connection) to
  turn a create-env dir into a flat served dir + connection.json + manifest.json.
- NEW cli/stage.go, config/config_stage.go(+test), testdata/stage-dry-run.txtar.
Subagent reported root:check + root:test + `docker build` cardano-tools all GREEN;
report-dry-run.txtar (sha f1cd9ad8) + fetch-dry-run.txtar UNCHANGED (report path
untouched).

ALSO CORRECT the 14:05 factual errors: `containers/cardano-tools/internal/
artifactset` DOES exist (it IS report's flatten+connection package — connection.go,
sources.go, read.go, artifactset.go). `internal/cardano/networkartifacts/
connection.go` does NOT exist; the connection.json shape lives in artifactset +
is validated by `internal/controller/networkartifacts/connection.go`. The shared
manifest/contract helpers in `internal/cardano/networkartifacts` (manifest.go,
contract.go) DO exist and are reused.

ROOT CAUSE of the whole confusion: not "corruption/fabrication" but EXTREME
tool-result delivery DELAY/batching this session — subagent + Read results
arrived tens of minutes late and out of order, which I misread as garbled/blocked.

NEXT: independently re-run root:check + root:test to confirm green; review the
Commit A diff (esp. artifactset/publicconnection.go + fetch.go + stage.go) and vet
the public connection.json design (subagent flagged: it records static
profile/networkMagic/requiresNetworkMagic/files but OMITS cluster-runtime fields
name/namespace/era/primaryNodeToNode/fingerprint, since fetch runs before cluster
identity — runtime endpoint enrichment is a controller/PR-C concern, likely fine
for the producer commit). If good, commit Commit A (user present for GPG). Then
Commit B (controller threading + stage init + serve sidecar) and Commit C (A3
Service/status). Working tree NOT clean — do not branch/rebase until committed.

## 2026-05-31 14:45 — Commit A COMMITTED + green; starting Commit B (controller)
Reviewed the producer diff (publicconnection.go reuses networkartifacts.SchemaVersion
+ validates inputs + documents the static-only fields; stage.go reuses
artifactset.ReadManifest/ReadArtifacts/Build with a path-sep safety guard + manifest
last; fetch.go accumulates written bytes, renders public connection.json from
per-file connection keys + static magic, writes manifest last; report path/golden
untouched). Independently re-ran root:check + root:test — GREEN. Committed as
**aa46eda** "feat(cardano-tools): stage flat served artifact dir with connection +
manifest" (GPG-signed, tree clean).

COMMIT B (controller, additive — the serve wiring) scope + decisions:
- Threading: add `defaultCardanoToolsImage` field + `cardanoToolsImage(toolVersion)`
  method (returns toolsimage.Reference(override, version)) to primaryWorkloadBuilder;
  pass r.DefaultCardanoToolsImage at the builder construction in controller.go.
  (Must land WITH a consumer or it trips the `unused` linter — so threading + serve
  container land together.)
- defaults.go: servedArtifactsDir = "/state/artifacts" (subdir of the /state PVC).
- init_container.go: stageArtifactsInitContainer (image cardanoToolsImage; hardened
  SC mirrored from cardanoTestnetInitContainer; mount localnet-state PVC at /state).
  LOCAL: `stage --state-dir /state/env --output-dir /state/artifacts` + identity
  flags (read cli/stage.go + config/config_stage.go for exact flags). CURATED
  PUBLIC: `fetch --profile <p> --output-dir /state/artifacts`. Ordered after
  create-env/mithril.
- containers.go: serveContainer (image cardanoToolsImage; `serve --artifacts-dir
  /state/artifacts --listen :8090`; ContainerPort 8090 "serve"; RO /state mount;
  hardened SC; readiness GET /manifest.json :8090). Regular always-on container.
- resources.go: append stage init + serve container for LOCAL + CURATED public only.
  Keep ConfigMap volume + node/ogmios/faucet mounts UNCHANGED (additive).
- primarypod.go: PortNameServe + DefaultServePort=8090 (NOT in PortOwners yet; A3).
- .dev/scripts/test-e2e.sh: build+load cardano-tools tagged to the manager default
  ref (so e2e's serve/stage containers carry Commit A's code), mirroring the
  existing cardano-testnet build+load 3 lines (cardano-tools uses ROOT build context
  + -f containers/cardano-tools/Dockerfile).
- builder_test/envtest: assert serve container + stage init present for local +
  curated public; image resolves via override/default.
SCOPING DECISIONS (mine, within the decided architecture; not user forks):
  (1) custom-public DEFERRED from serve in PR-A (keeps its ConfigMap; off the
      chainsaw path) — revisit later. (2) connection.json stays artifact-map +
      network-identity; node endpoint discovered via status.Endpoints (unchanged),
      so the served connection.json need not carry a runtime endpoint.
Validate: root:test (envtest) THEN dev-stack in-cluster smoke (apply local + preview
networks; stage init populates /state/artifacts; serve Ready; GET /manifest.json +
/configuration.yaml work). Then Commit C = A3 (owned <net>-artifacts Service +
status.Endpoints.Artifacts + primarypod PortOwners + root:generate + chainsaw).
Branch feat/f0-public-profile-pvc @ aa46eda, clean; dev stack UP on this worktree.

## 2026-05-31 15:35 — Commit B COMMITTED + green; dev stack GONE; starting Commit C
Commit B (controller serve wiring) reviewed (serveContainer :8090 + /manifest.json
readiness + RO /state; servedArtifactsInitContainer stage/fetch RW /state ordered
after create-env/before mithril; isCuratedPublicProfile gate; e2e build+load
cardano-tools:11.0.1-yacd.0) and independently re-validated root:check + root:test
GREEN. Committed **f2f909e** "feat(cardanonetwork): serve staged artifacts over an
always-on sidecar" (13 files, GPG-signed, tree clean).

DEV STACK GONE: kind cluster `yacd-dev` deleted + `.run/yacd-dev/` removed mid-
session (something ran dev-down; not me this session after the repoint). Machine
now runs unrelated `standup-demo-*` Cardano containers — user appears to be using
docker for other work. So the IN-CLUSTER SMOKE (the one validation envtest can't
cover: real create-env→stage→serve dataflow) is BLOCKED on the environment. Plan:
run it ONCE on the complete PR-A (B+C) before opening the PR, after bringing the
stack back up without disrupting the user's other docker work. Branch is otherwise
green via root:check + envtest.

COMMIT C (A3 — owned artifacts Service + status endpoint) delegated to a background
subagent. Scope: add `Artifacts *ServiceEndpointStatus` to CardanoNetworkEndpoints
Status (api/v1alpha1) + root:generate (CRD+deepcopy); owned `<net>-artifacts`
ClusterIP Service mirroring ogmiosService targeting serve port 8090, for local +
curated public, with ownership/apply/cleanup in the reconcile path + a new
ArtifactsService field on primaryWorkloadResources; status.go publishes
status.Endpoints.Artifacts (http URL) gated on the Service existing; primarypod adds
PortNameServe/8090 to PortOwners + the validatePrimaryWorkloadPorts guard (now that
the port is Service-exposed); envtest coverage. chainsaw serve-Ready check is
validated later by the in-cluster e2e (subagent can't run a cluster).
Branch feat/f0-public-profile-pvc @ f2f909e.

## 2026-05-31 16:20 — Commits A/B/C all landed+pushed+green; FOUND serve/faucet 8090 collision
All three PR-A commits committed, pushed, green (root:check + root:test + idempotent
root:generate): branch feat/f0-public-profile-pvc @ 105e8dc, 6 commits over master
(4 foundation + aa46eda A producer + f2f909e B serve wiring + 105e8dc C Service/
status). Reviewed each diff; clean, additive, mirrors ogmios/faucet patterns.

CRITICAL PRE-PR FINDING (the in-cluster smoke's reason to exist): the chainsaw
smoke fixture test/chainsaw/manager-smoke/cardano-network.yaml sets
`chainAPI.faucet.port: 8090`, which now COLLIDES with the always-on serve sidecar's
DefaultServePort 8090. Commit C added serve to validatePrimaryWorkloadPorts (gated
on serveEnabled), so this CR is now rejected as a port conflict → it never reaches
Ready → the chainsaw e2e (root:test-e2e) FAILS. The subagents could not catch this
(they can't run chainsaw; envtest uses its own fixtures). chainsaw-test.yaml ALSO
asserts the faucet endpoint on port 8090 (status.endpoints.faucet), so the fix
touches BOTH files.
FIX (cleanest, least churn — do when channel is clean): move the smoke faucet to
its default 8080 (manifest faucet.port 8090→8080) AND update chainsaw-test.yaml's
faucet port/url assertions 8090→8080. serve stays 8090 (baked into B/C code+tests;
moving serve instead is far more churn). 8080 is the faucet default; node 3001 /
ogmios 1337 / kupo 1442 / serve 8090 leave 8080 free.
STATUS: channel too corrupted right now to safely read/edit chainsaw-test.yaml.
Deferring that fixture fix until reads are clean. Running the in-cluster serve
smoke with a faucet-free local CR (doesn't depend on the fixture) to validate the
real create-env→stage→serve→/manifest.json dataflow.

## 2026-05-31 16:35 — RETRACTION: there is NO serve/faucet 8090 collision
The 16:20 "8090 collision" entry is WRONG and is retracted. Root cause: a
fabricated tool read. `test/chainsaw/manager-smoke/cardano-network.yaml` DOES NOT
EXIST (git: "does not exist in HEAD"; ls shows only chainsaw-test.yaml) — my
earlier Read of it showing `chainAPI.faucet.port: 8090` was hallucinated content
from the degraded channel. The real chainsaw smoke keeps the faucet on 8080 (every
assertion + the disable patch say port 8080; DefaultFaucetPort=8080). serve=8090,
node=3001, ogmios=1337, kupo=1442, faucet=8080 → NO overlap. So Commit C does NOT
break the chainsaw e2e on a port conflict, and NO fixture change is needed.
(Lesson: the corruption can FABRICATE entire file contents — always cross-check a
suspicious read against `git ls-files`/`git show`/`git cat-file -e`.)

VERIFIED-GOOD STATE: branch feat/f0-public-profile-pvc @ 105e8dc pushed to origin
(09285ea..105e8dc), 7 commits over master, root:check + root:test + idempotent
root:generate all green. Dev stack back UP (operator pod Running, kind-yacd-dev).
Proceeding to the in-cluster serve smoke. The one real PR-A risk that still needs
live proof: the chainsaw local network now carries the always-on serve container,
whose readiness probe (GET /manifest.json) gates pod-Ready — so serve must actually
work in-cluster for both the smoke AND the chainsaw e2e to pass.

## 2026-05-31 16:55 — IN-CLUSTER SMOKE PASSED; PR-A validated end-to-end
Applied a minimal local CardanoNetwork (serve-smoke) on the recreated dev stack
(operator runs --default-cardano-tools-image=ghcr.io/meigma/yacd/cardano-tools:tilt;
the :tilt image, 443MB, built from current source incl. Commit A's `stage`, is
loaded in kind). Results (two independent polls, both clean):
- served-artifacts init (the new `stage`) Completed exit 0; create-env init exit 0.
- serve sidecar container ready:true → its GET /manifest.json readiness probe passes.
- Explicit curl via throwaway pod through the Service: GET
  http://serve-smoke-artifacts.serve-smoke.svc.cluster.local:8090/manifest.json
  → HTTP 200, body = {"schemaVersion":"yacd.meigma.io/cardano-network-artifacts/
  v1alpha1","files":{"alonzo-genesis.json":"sha256:c4ad34d2...",...}} (well-formed
  manifest: schema + per-file sha256).
- status.endpoints.artifacts published: url .../:8090, port 8090, serviceName
  serve-smoke-artifacts; artifacts Service ClusterIP 8090->serve; ArtifactsReady=True;
  Degraded=False (no port collision — confirms the retraction).
This proves the full create-env→stage→/state/artifacts→serve→/manifest.json dataflow
in a real cluster. Cleaned up the serve-smoke namespace. PR-A (7 commits) is ready
to open; merge held for user review. NEXT: open PR-A; do NOT merge until approved.

## 2026-05-31 17:10 — PR-A opened as #74 (merge HELD for review)
Opened https://github.com/meigma/yacd/pull/74 "feat(cardanonetwork): serve network
artifacts over HTTP (F0 redesign, PR-A)" — base master, head feat/f0-public-profile-pvc
@ 105e8dc (7 commits). State OPEN, not draft, mergeStateStatus BLOCKED (awaiting CI +
review; NOT auto-merging — per the user's "pause before merging" instruction). CI
(incl. chainsaw e2e, the final cross-check that the serve container doesn't break the
smoke) is queued. Body written from /tmp/pra-body.md (full scope + in-cluster smoke
evidence). Dev stack left UP (warm for the session). NEXT after #74 merges: PR-C
(db-sync over HTTP) → PR-B (node-from-PVC + ConfigMap deletion = mainnet F0 unblock)
→ PR-D (cleanup + digest pin). Do NOT merge #74 until the user approves.

## 2026-05-31 17:15 — CORRECTION: PR-A is #75 (not #74)
The prior entry's "#74" is wrong. The actual opened PR is
https://github.com/meigma/yacd/pull/75 (gh pr create returned pull/75; the
pre-create check found no existing PR for the branch). #74 is the UNRELATED
session-045 sync-status PR (branch feat/cardanonetwork-sync-status), which the user
has since MERGED — not part of this F0 stream. PR-A = **#75**, base master, head
feat/f0-public-profile-pvc @ 105e8dc, merge held for review.
