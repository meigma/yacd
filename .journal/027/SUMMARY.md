---
id: 027
title: Public CardanoNetwork profiles and mainnet bootstrap
date: 2026-05-27
status: complete
repos_touched: [yacd]
related_sessions: [024, 025, 026]
---

## Goal
Implement public-network support in small vertical slices: first passive preview
nodes with Ogmios, then curated/custom public profile sources, then public
CardanoDBSync dedicated followers, and finally a practical mainnet prototype
with explicit bootstrap and operator friction.

## Outcome
Goal met. PR #47 merged as squash commit `385718b`, bringing public
`CardanoNetwork` profiles for preview, preprod, mainnet, and custom
ConfigMap/Secret sources; public preview/preprod runtime smoke coverage;
public `CardanoDBSync` dedicated-follower support for preview/preprod/custom;
and gated mainnet `CardanoNetwork` support through Mithril bootstrap,
mainnet-sized defaults, and `yacd deploy --allow-mainnet`. CI and Kusari
Inspector passed. `master` was fast-forwarded, the session dev stack was
stopped, and the implementation worktree was removed.

## Key Decisions
- Prove public support with passive node plus Ogmios first -> avoided turning
  the first public slice into a full public service matrix.
- Vendor curated public profile files -> keeps profile fingerprinting,
  artifacts, tests, and controller behavior deterministic while still allowing
  custom ConfigMap/Secret sources.
- Keep public Kupo/faucet rejected -> those surfaces need separate chain-data
  and funding/trust decisions before being exposed on public networks.
- Support public db-sync only as `dedicatedFollower` for preview/preprod/custom
  -> preserves the already-proven follower model while public primary-sidecar
  semantics remain unvalidated.
- Keep public mainnet db-sync rejected -> mainnet needs either follower-node
  Mithril bootstrap or a deliberately validated public `primarySidecar` slice.
- Require Mithril for mainnet `CardanoNetwork` and add `--allow-mainnet` for
  real CLI applies -> mainnet should be explicit, expensive, and harder to run
  by accident.

## Changes
- `api/v1alpha1/cardanonetwork_types.go` and generated CRD/deepcopy - added
  `spec.public.bootstrap.mithril` with defaulted image/snapshot and profile
  validation.
- `internal/cardano/publicnet` - added the Kubernetes-free public profile
  planner, profile fingerprints, embedded preview/preprod/mainnet profile
  files, custom bundle support, and mainnet Mithril metadata.
- `internal/controller/cardanonetwork` - added public profile resolution,
  custom ConfigMap/Secret watches, public primary workload rendering, public
  artifact/status publication, mainnet storage/resource defaults, Mithril init
  container rendering, and public-mode validations.
- `internal/controller/cardanodbsync` and `internal/controller/networkartifacts`
  - added public artifact consumption through `connection.json`, public
  dedicated-follower support, and explicit public mainnet/public sidecar gates.
- `cli/internal/cli` and `cli/internal/devconfig` - added public developer
  config coverage, mainnet dry-run warning behavior, and real-apply
  `--allow-mainnet` enforcement.
- `examples/public-*` - added preview, preprod, custom, mainnet, and public
  preprod managed-Postgres db-sync examples.
- `moon.yml` - included the new generated API/CRD validation input.

## Open Threads
- Full live mainnet bootstrap was intentionally not run. Treat it as a
  long-running manual proof, not a default local acceptance check.
- Public `primarySidecar` does not "not exist"; it exists only for local
  networks today. Public support is a future slice to lift the local-only gate,
  validate public artifacts/socket behavior, and size the combined pod.
- Public mainnet `CardanoDBSync` remains unsupported until follower-node
  Mithril bootstrap or public primary-sidecar support is added.
- Public Kupo and faucet remain unsupported.
- Custom public profiles are accepted from ConfigMap/Secret bundles, but their
  operational compatibility remains the user's responsibility.

## References
- PR #47: https://github.com/meigma/yacd/pull/47
- Merge commit: `385718b`
- Session 024: `.journal/024/SUMMARY.md`
- Session 025: `.journal/025/SUMMARY.md`
- Session 026: `.journal/026/SUMMARY.md`
- Session notes: `.journal/027/NOTES.md`

## Lessons
- The phrase "until public primarySidecar exists" is misleading in this repo.
  `primarySidecar` already exists and is tested for local networks; the missing
  work is public-profile validation and operational sizing.
- Public-network support became tractable once split into rendering/artifact,
  db-sync consumption, and mainnet bootstrap slices instead of trying to design
  the full final public-network matrix up front.
