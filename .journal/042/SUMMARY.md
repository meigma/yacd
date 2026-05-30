---
id: 042
title: cardano-tools artifact utility container
date: 2026-05-30
status: complete
repos_touched: [yacd]
related_sessions: [029, 030, 040]
---

## Goal
Opened without a fixed goal. It became: understand how genesis/config artifacts
differ between local and public `CardanoNetwork` modes (the root of TEST_REPORT
finding F0 — mainnet's embedded public profile exceeds the ~1 MiB etcd ConfigMap
cap), design a redesign, and build its foundation: a single in-cluster utility
container that owns all artifact acquisition so the controller can stop carrying
network data.

## Outcome
Met. PR #64 (squash `ad46e82`) merged a new `containers/cardano-tools` container
and the `yacd-cardano-tools` binary, slimmed to a distroless/static image, after
three rounds of review fixes. This is the **foundation** for the F0 fix; the
controller rewiring that actually closes F0 was intentionally deferred (see Next
Steps). A `release-please` component PR for `cardano-tools` opened automatically
on merge and is unreleased.

## Key Decisions
- Root-module placement (no `go.mod` under `containers/cardano-tools/`, like
  `cli/`) -> imports the shared artifact contract directly instead of
  duplicating it the way the separate-module `cardano-testnet/publisher` does.
  The `report-dry-run` testscript reproduces the publisher's exact
  `sha256:f1cd9ad8…`, locking byte-compatibility with the controller's verifier.
- distroless/static base + copy only the 3 used binaries -> the IOG 11.0.1
  binaries are fully static musl (verified by ldd + ELF headers), so no glibc
  base is needed; the tarball ships ~14 binaries (~1.2 GB) but the tool uses 3
  (~370 MB). Result ~1.3 GB -> 442 MB. uid 10001 kept numerically to avoid a
  `/state` PVC-ownership change.
- Pin `config.json` + `topology.json` digests; leave `peer-snapshot.json`
  unpinned -> config transitively pins genesis/checkpoints (cardano-node verifies
  them); pinning topology turns silent peer-selection drift into a loud failure;
  peer-snapshot is a continuously-moving, optional, non-chain-critical hint.
- `serve` is a default-deny allowlist of known artifact keys (request + resolved
  path) -> for an HTTP-facing server, allowlisting beats a denylist; backup or
  future secret-shaped files are refused by construction.
- `generate` is idempotent (match -> re-enrich only; conflict -> refuse; absent
  -> generate) -> preserves the init wrapper's behavior so a pod restart on a
  populated PVC can't re-run create-env and wedge.
- `report` is localnet-only and `--kubernetes-api-url` must be https -> honest
  scoping (public report is downstream) and the projected bearer token never
  goes cleartext.
- mockery skipped for the two single-method interfaces (inline fakes are
  clearer); annotation keys imported from the existing
  `internal/controller/annotations` rather than moved (zero controller changes).

## Changes
- `containers/cardano-tools/` (new) - `cmd/yacd-cardano-tools` + `internal/{cli,
  config,generate,fetch,serve,artifactset,kube}`; subcommands generate/fetch/
  serve/report/version; root-context distroless `Dockerfile`.
- `release-please-config.json`, `.release-please-manifest.json` - new
  `cardano-tools` component (tracks the cardano-node version as
  `cardano-tools/vX.Y.Z-yacd.N`).
- `.github/workflows/release-cardano-tools.yml` (new) + dry-run jobs in
  `release-dry-run.yml` - signed/attested release mirroring cardano-testnet.
- `moon.yml`, `.dev/scripts/check.sh` - include cardano-tools in goSources,
  check inputs, and gofmt roots.
- `.journal/TECH_NOTES.md` - recorded the static-musl finding and the slim-image
  approach.

## Open Threads / Suggested Next Steps
The controller-side work that actually closes F0 was intentionally left for
future sessions. In rough dependency order:

1. **Minimal F0 fix (mainnet public node reads from PVC).** Switch the public
   mainnet primary node to read config/genesis from a PVC staged by a
   `generate`/`fetch` init container instead of the >1 MiB `<net>-network-artifacts`
   ConfigMap. Smallest slice that unblocks mainnet; breaks nothing (mainnet
   db-sync is still gated off).
2. **Replace the small-profile ConfigMap with a metadata manifest.** Publish a
   small ConfigMap carrying `schemaVersion` + per-file `sha256` + `DataHash`
   (the reconcilable source of truth / integrity handshake), with bulk bytes on
   the PVC. Keep `status.Artifacts` gating.
3. **Remove the manager's `//go:embed` of public profiles** once the tool owns
   acquisition; the manager keeps only profile metadata + pinned digests.
4. **`serve` sidecar wiring + advertise URLs.** Add the serve sidecar (native
   sidecar ordered before the Mithril init) for out-of-cluster/CLI consumers;
   advertise artifact URL(s) under `status.Endpoints` only if a real Service
   exists.
5. **CardanoDBSync consumer rewiring.** Have consumers obtain artifacts via their
   own init container (public: re-materialize/verify against the manifest;
   local: copy the small ConfigMap) instead of mounting the network-artifacts
   ConfigMap volume — needed before mainnet db-sync can be unblocked.
6. **Public `report` path.** `report` is localnet-only today; add a public
   manifest/connection path when a public consumer needs it.
7. **Manager flag + Helm value** to point localnet init / source-address /
   db-sync follower containers at the cardano-tools image; eventually retire or
   repoint the `cardano-testnet` image and give it the same distroless slimming.
8. **Enable the cardano-tools image build on PR CI.** Its dry-run jobs are gated
   to `release-please--*` branches, so the Dockerfile is not built per-PR (it was
   validated locally each round). Drop that gate (or add a light build to `ci`).
9. **Cut the first cardano-tools release** via the auto-opened release-please PR
   once ready.
10. **Re-verify the static-musl assumption** on every cardano-node version bump,
    on both arches; if IOG ever ships glibc-dynamic, switch the base to
    distroless/cc.
11. **Remaining TEST_REPORT findings** F2/F4 are still open (this session's work
    addresses F0 foundationally, not the others).

Also: session **041** remains `in-progress` and dormant — its
`feat/cli-connect-verb` worktree (Test-Harness Phase 2 PR5, `yacd connect`) was
left untouched throughout, as the user directed when 042 was opened.

## References
- PR #64 (merged, squash `ad46e82`): https://github.com/meigma/yacd/pull/64
- Plan: `.claude/plans/that-s-fine-can-you-enchanted-kay.md`
- F0 origin: `.journal/029/SUMMARY.md` (adversarial break pass),
  `.journal/040/SUMMARY.md` (F0 assessment), `.journal/TEST_REPORT.md`
- Test-harness design (related): `.journal/030/SUMMARY.md`
- TECH_NOTES: the static-musl / distroless slim-image entry

## Lessons
- The premise that the IOG Haskell binaries are glibc-dynamic was false — they
  are fully static musl (GHC 9.6.7). Empirical `ldd`/ELF checks overturned the
  design assumption and made near-scratch trivially viable; always verify the
  link model before reasoning about a base image.
- `cardano-testnet create-env` runs shell-free and finds `cardano-cli` via the
  `CARDANO_CLI` env var (no `/bin/sh`), which is what makes distroless viable for
  `generate`.
- Direct `go`/`go test` needs `PATH=/Users/josh/.proto/tools/go/<ver>/bin:$PATH`;
  the proto `go` shim word-splits a templated arg, so gopls shows false
  `malformed import path "{{context.Compiler}}"` diagnostics — ignore them.
