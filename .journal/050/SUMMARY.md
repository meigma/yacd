---
id: 050
title: F0 redesign — PR-D (remove report verb, digest-pin cardano-tools, drop e2e build hack)
date: 2026-06-01
status: complete
repos_touched: [yacd]
related_sessions: [048, 047, 046]
---

## Goal

Land **PR-D**, the final slice of the F0 redesign. Per session 048's plan, PR-D
was four cleanup/hardening items (no new runtime behavior): remove the dead
cardano-tools `report` verb, pin the manager's cardano-tools image to a published
digest, drop the e2e build+load hack, and rewrite the DESIGN.md ConfigMap prose.

## Outcome

**Goal met.** Review collapsed the scope from four items to three, and PR-D
landed as a **code → release → digest-pin chain** of three merged PRs. The F0
redesign series is now **complete** (PR-A #75, PR-C #77, PR-B1 #78, PR-B2 #79,
PR-D #81+#82): the runtime data path is fully HTTP/PVC-based, the publisher and
the `report` verb are gone, and the manager's cardano-tools default is a
published, digest-pinned image that the e2e pulls (no build+load).

- **#81** `build(cardano-tools)!:` — removed the `report` subcommand, its config
  loader (`config.go` was *entirely* report-only), the orphaned `internal/kube`
  package, its tests + `report-dry-run.txtar` golden; added
  `containers/cardano-tools/README.md` (versioning contract); dropped the
  cardano-testnet build+load from the e2e (published tag, Kind pulls it).
- **#76** `chore(master): release cardano-tools 11.0.1-yacd.5` — the
  release-please PR, base fixed off the drifted `11.1.0-yacd.4`. Merged → tag
  `cardano-tools/v11.0.1-yacd.5` → image built+published.
- **#82** `build(cardanonetwork):` — pinned the built-in `toolsimage` default to
  the published digest, dropped the cardano-tools e2e build+load, and repinned
  the root release back to `1.0.0` (leak fix, below).

**DESIGN.md (item 4) was dropped** — it has no stale artifact-ConfigMap prose
(only a generic K8s-resource-type list), contrary to the 048 scope note.

Pinned image:
`ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.5@sha256:d3283ca5fc925f6ec01f61a54371e5ad1934088614b7cde1e1e1915424662fc4`.
`docker run` on the digest confirmed the verbs: no `report`,
generate/fetch/serve/stage/sync present. Each stage was verified green incl.
`root:test-e2e` (the digest-pin run proves Kind pulls the published image and the
operator reaches Ready with it).

## Key Decisions

- **Digest-pin requires a NEW cardano-tools release first** (the crux). The only
  previously published image (`11.0.1-yacd.4`) predated PR-A/PR-C and lacked the
  `stage`/`serve`/`sync` verbs the operator now requires — the real reason the
  e2e built from source and the manager defaulted to the unpublished `yacd.0`
  tag. So the pin could not reuse an existing image; PR-D had to be a
  code→release→pin chain. The user chose to drive the full chain with a pause
  before the release merge (the image publish is outward-facing).
- **Removed `config.go` wholesale, not trimmed.** Discovery showed `LoadStage`
  derives its own plan-manifest filename, so nothing in `config.go` was shared —
  the whole file (and `config_test.go`, and the orphaned `internal/kube`) was
  report-only. Rewrote `config/doc.go` to describe the surviving stage loader.
- **Assert against `toolsimage.Reference(...)`, not a literal**, in the
  cardanonetwork builder/init-container tests. The 6 hardcoded `:11.0.1-yacd.0`
  expectations would churn on every digest/revision bump; the literal now lives
  only in `toolsimage_test.go`, which owns the "default is digest-pinned"
  invariant (`TestDigestPin`).
- **Reference form = `<repo>:<toolVersion>-<revision>@<digest>`** (not
  digest-only). Keeps a human-readable version in the pod spec while the registry
  resolves by digest. The chart helper's digest path stays `repo@digest`; both
  are valid digest pins, no chart change needed.
- **`Release-As` MUST be component-scoped on multi-component commits** (mistake +
  fix). #81's unscoped `Release-As: 11.0.1-yacd.5` leaked into the root `yacd`
  release PR #7 (was `1.0.0`) because the commit also touched
  `.dev/scripts/test-e2e.sh`, a root-component path. #82 (root paths only)
  carried scoped `Release-As: yacd@1.0.0` and release-please recomputed #7 back
  to `1.0.0`. Use `Release-As: <package-name>@<version>` whenever a commit spans
  components.
- **Root release PR #7 (operator `1.0.0`) left open/unmerged** — releasing the
  operator is a deliberate decision outside PR-D's scope.

## Changes

PR #81 (`2b9bf84`), all under `containers/cardano-tools/` + e2e:
- Deleted `internal/cli/report.go` (+ `root.go` registration), `internal/kube/**`,
  `internal/config/config.go`, `internal/config/config_test.go`, and the
  `cmd/yacd-cardano-tools/testdata/report-dry-run.txtar` golden.
- Rewrote `internal/config/doc.go` (stage loader); refreshed stale "report verb"
  comments in `internal/stage/{stage.go,doc.go}` and the Dockerfile `LABEL`.
- Added `containers/cardano-tools/README.md` (versioning contract).
- `.dev/scripts/test-e2e.sh` — dropped cardano-testnet build+load.

PR #82 (`bd8e0bf`), root-component paths only:
- `internal/cardano/toolsimage/toolsimage.go` — `Revision`→`yacd.5`, added
  `Digest`, `Reference()` emits the digest-pinned ref; doc refreshed.
- `internal/cardano/toolsimage/toolsimage_test.go` — updated expectations +
  `TestDigestPin` guard.
- `internal/controller/cardanonetwork/{builder_test.go,init_container_test.go}` —
  assert via `toolsimage.Reference("", "11.0.1")`.
- `.dev/scripts/test-e2e.sh` — dropped cardano-tools build+load (now only
  manager+faucet built+loaded; both tools images pulled).

## Open Threads

- **Root release PR #7 (`yacd 1.0.0`)** open and correct — awaiting a deliberate
  operator-release decision.
- **cardano-testnet digest-pin parity** — still a published tag, not digest
  pinned; optional future follow-up mirroring PR-D's cardano-tools work.
- Carried from prior sessions: the deterministic primary-sidecar manager-envtest
  refactor (the flaky test removed in 048); TEST_REPORT F2/F4; test-harness
  Phases 3–5.
- Session **049** (CLI all-in-one local cluster / k3d) remains separately
  `in-progress` — untouched by this close-out.

## References

- PRs: #81 (report removal, `2b9bf84`), #76 (cardano-tools 11.0.1-yacd.5
  release), #82 (digest pin + e2e drop, `bd8e0bf`).
- Pinned image: `ghcr.io/meigma/yacd/cardano-tools:11.0.1-yacd.5@sha256:d3283ca5fc925f6ec01f61a54371e5ad1934088614b7cde1e1e1915424662fc4`
  (tag `cardano-tools/v11.0.1-yacd.5`).
- Prior F0 sessions: `.journal/048/SUMMARY.md` (PR-B1/B2 + PR-D plan),
  `.journal/047/SUMMARY.md` (PR-C), `.journal/046/SUMMARY.md` (PR-A).
- Versioning + Release-As-scoping contract: `containers/cardano-tools/README.md`,
  `containers/cardano-testnet/README.md`, `.journal/TECH_NOTES.md`.

## Lessons

- **release-please `Release-As` is per-component, and an unscoped footer hits
  EVERY component the commit touches.** The root `yacd` component excludes only
  the two `containers/*` dirs, so any commit touching repo tooling (e.g.
  `.dev/scripts/`) counts toward root. Scope the footer
  (`Release-As: <package-name>@<version>`) on any multi-component commit. Verify
  the open release PRs after a release-please run, not just the component you
  intended to bump.
- **A "moving tag" default that points at an unpublished/old revision is a
  latent trap.** The manager defaulted to `cardano-tools:11.0.1-yacd.0` (never
  published) while the real published image was `yacd.4` and too old to work —
  invisible because the e2e built from source. Digest-pinning to a verified
  published image closes both the sequencing gap and the silent-skew.
