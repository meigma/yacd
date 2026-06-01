---
id: 048
title: F0 redesign — PR-B1 + PR-B2 (remove ConfigMap/custom-public, delete publisher, cut slimmer image)
date: 2026-06-01
status: complete
repos_touched: [yacd]
related_sessions: [046, 047]
---

## Goal

Continue the F0 redesign from session 047's banked, approved PR-B1 plan. Land
**PR-B1** (remove the `<net>-network-artifacts` ConfigMap concept and the
`custom-public` profile — the mainnet unblock), then plan and land **PR-B2**
(delete the now-dead cardano-testnet artifact publisher and cut a slimmer tools
image). PR-D was explicitly excluded; PR-B2 was to be the last slice before
concluding.

## Outcome

**Goal met in full.** Four PRs merged to `master` and the slimmer image is
released and verified end-to-end:

- **#78** `feat(cardanonetwork)!:` — PR-B1: removed the network-artifacts
  ConfigMap + `custom-public`; curated-public node+ogmios now read config from
  the PVC; re-sourced ArtifactsReady / sync-timing / db-sync identity; db-sync
  is single-path serve (the F0 mainnet unblock).
- **#79** `build(cardano-testnet)!:` — PR-B2: deleted the publisher binary,
  nested module, and `artifactpublisher` package; removed the Dockerfile
  publisher stage; pruned all publisher references from CI/dev tooling; bumped
  the manager default image revision to `yacd.5`.
- **#80** `build(cardano-testnet):` — versioning follow-up (user-approved):
  pinned the next release to `11.0.1-yacd.5` via a `Release-As:` footer and
  documented the cardano-testnet versioning contract in a new README.
- **#34** `chore(master): release cardano-testnet 11.0.1-yacd.5` — the
  release-please PR; merged → tag `cardano-testnet/v11.0.1-yacd.5` → image
  built/published. **Verified by pulling the published image: the publisher
  binary is absent; cardano-node/cli/testnet + `yacd-cardano-testnet-init` are
  present.** This is exactly the tag the manager `yacd.5` default targets, so a
  production install now resolves the slimmer, publisher-free image.

## Key Decisions

- **Remove `custom-public` entirely rather than migrate it** (carried from 047)
  → the ConfigMap was the only `custom-public` materialization path and it was
  the F0 root cause (1 MiB cap silently breaking mainnet). Deleting the concept
  beat reworking it behind a reference.
- **Keep a vestigial empty `containers/cardano-testnet/go.mod`** after deleting
  all its Go code → lowest-risk; leaves the release-please `go` component and
  the image build context intact, no need to retype the component to `simple`.
- **Removed the flaky `...AttachesPrimarySidecarDBSync` manager envtest instead
  of hardening it** (user: "Remove it for now. We will refactor all tests in a
  future session.") → the test was non-deterministic under CI load; a proper
  deterministic refactor is deferred. The other two manager tests were fixed in
  PR-B1 by wiring a `syncedNodeTimingProber()` override so they stop doing real
  failing HTTP timing probes per reconcile.
- **Fixed release versioning via a per-release `Release-As:` footer** (user
  chose "follow-up PR to fix it now") → release-please was deriving the
  cardano-testnet version from Conventional-Commit semver, drifting the base off
  the packaged cardano-node version (it proposed `12.0.0-yacd.4`). The base
  *must* equal the cardano-node version the release workflow downloads and the
  operator computes its ref from. Footer scopes the override to the component
  whose paths the commit touches, leaving root #7 / cardano-tools #76 alone.

## Changes

PR-B1 (#78):
- `api/v1alpha1/cardanonetwork_types.go` — removed `PublicNetworkProfileCustom`,
  `NetworkConfigSource`, `PublicNetworkSpec.ConfigSource` (+CEL), and
  `Status.Artifacts` / `CardanoNetworkArtifactsStatus`.
- `internal/controller/cardanonetwork/sync_probe.go` — serve-fetch sync-timing
  (`cardanoNetworkTimingProber` interface + default prober that joins
  `artifactsURL`+`ShelleyGenesisKey` and reuses `parseShelleyGenesisTiming`).
- `internal/controller/cardanonetwork/{status.go,readiness.go}` —
  `primaryArtifactsReadyCondition` derived from serve-container readiness.
- `internal/controller/cardanonetwork/init_container.go` — publisher env/volume
  removed; create-env init kept.
- envtest: wired `syncedNodeTimingProber()` into the manager tests (flake fix).

PR-B2 (#79):
- Deleted `containers/cardano-testnet/{publisher/**,
  cmd/yacd-cardano-testnet-publisher/**, internal/artifactpublisher/**}`.
- `containers/cardano-testnet/Dockerfile` — removed the publisher stage, COPY,
  and `YACD_PUBLISHER_*` ARGs; `YACD_CARDANO_TESTNET_VERSION=11.0.1-yacd.5`.
- `containers/cardano-testnet/yacd-cardano-testnet-init` — removed
  `publish_artifacts()` + both call sites (keeps the create-env pass-through).
- `internal/controller/cardanonetwork/init_container.go`
  (`cardanoTestnetImageRevision`) + `internal/controller/cardanodbsync/defaults.go`
  (`defaultFollowerNodeImageRevision`) → `yacd.5`.
- Pruned publisher refs in `moon.yml`, `.dev/scripts/check.sh`,
  `.dev/scripts/test-e2e.sh`, `.dev/build-cardano-testnet.sh`,
  `.github/workflows/{release-cardano-testnet.yml,release-dry-run.yml}`.
- Removed the flaky `...AttachesPrimarySidecarDBSync` envtest + its helpers.

PR-B2 versioning (#80):
- Added `containers/cardano-testnet/README.md` documenting the versioning
  contract (base = packaged cardano-node version, `-yacd.N` = packaging
  revision, set each release via a `Release-As:` footer).

## Open Threads

See the dedicated PR-D section below — it is the only remaining F0 slice.
Other carried work: the deterministic primary-sidecar manager envtest refactor
(the removed flaky test); TEST_REPORT F2/F4; test-harness Phases 3–5. A broader
concern surfaced by #80: cardano-testnet now relies on a per-release
`Release-As:` footer to hold its base; other components may eventually want the
same treatment or a config-level guard.

Separately, session **049** (yacd CLI all-in-one local cluster lifecycle / k3d
design) is a distinct concurrent session and remains `in-progress` — untouched
by this close-out.

## Next session — PR-D (the last remaining F0 PR)

PR-D is the final slice of the F0 redesign. After PR-A (serve over HTTP, #75),
PR-C (db-sync consumes over HTTP, #77), and PR-B1/B2 (this session) the runtime
data path is fully HTTP/PVC-based and the publisher is gone — but several
cleanup/hardening items were deliberately deferred into PR-D. It is **cleanup +
docs, not new runtime behavior**, so it should be a low-risk close to the series.

Scope (all four were explicitly carried out of this session):

1. **Remove the cardano-tools `report` verb.** It was the publisher's
   genesis-hash enrichment mechanism; `cardano-tools stage` (PR-C) took that
   over, so `report` is now dead. Grep `containers/cardano-tools` for the
   `report` subcommand + its `internal/` impl and tests, and confirm nothing in
   the operator or init wrappers still invokes it before deleting.
2. **Pin the manager's cardano-tools image to a published digest.** Today the
   manager defaults to a `yacd.N` *tag* (same pattern cardano-testnet uses).
   The sequencing gap is documented and accepted pre-1.0 (between an image PR
   merging and its release-please PR merging, a no-override `helm install` would
   reference a not-yet-published tag). Digest pinning closes that. Note
   release-please cardano-tools PR **#76 is open** — coordinate the digest with
   whatever it publishes. Check `internal/controller/cardanodbsync/defaults.go`
   and the cardanonetwork init/sidecar wiring for the cardano-tools image ref.
3. **Drop the e2e build+load hack.** The e2e currently builds the
   cardano-testnet/tools images from source and loads them into Kind because the
   manager defaulted to not-yet-published tags. Once the images are published +
   digest-pinned, the e2e can pull instead of build-and-load. Look at
   `.dev/scripts/test-e2e.sh` and the chainsaw setup.
4. **Rewrite the DESIGN.md ConfigMap prose.** `DESIGN.md` still describes the
   old artifact-ConfigMap transport that PR-B1 deleted. Update it to the
   serve-over-HTTP / PVC-staged model that now ships.

Practical starting points: the F0 status block and the cardano-testnet
versioning convention are recorded in `.journal/TECH_NOTES.md`; the original
multi-PR F0 plan and the custom-public-removal decision are in
`.journal/047/SUMMARY.md` and `.journal/046/SUMMARY.md`. PR-D needs a release
of cardano-tools (for the `report` removal + the digest to pin to), so expect a
release-please cardano-tools PR to be part of landing it — apply the same
`Release-As:` base-pinning discipline documented in
`containers/cardano-tools` / the cardano-testnet README if its base drifts.

## References

- PRs: #78 (PR-B1), #79 (PR-B2), #80 (versioning fix), #34 (release
  cardano-testnet 11.0.1-yacd.5).
- Released image: `ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.5`
  (tag `cardano-testnet/v11.0.1-yacd.5`; release run 26781069769).
- Prior F0 sessions: `.journal/046/SUMMARY.md` (PR-A),
  `.journal/047/SUMMARY.md` (PR-C + PR-B plan).
- Versioning contract: `containers/cardano-testnet/README.md`;
  `.journal/TECH_NOTES.md`.
- Open release-please PRs at close: #7 (root), #76 (cardano-tools).
