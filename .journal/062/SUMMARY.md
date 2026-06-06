---
id: 062
title: External-access P2 (devnet plumbing) + P3 (CLI resolver), released as v0.2.1
date: 2026-06-05
status: complete
repos_touched: [yacd]
related_sessions: [060, 061, 059, 058]
---

## Goal

Continue session 060's external-access design (`.journal/060/EXTERNAL_ACCESS_DESIGN.md`)
beyond the already-shipped P1 (operator API/status). Implement **P2** (make a
`yacd devnet` cluster's Ogmios/Kupo reachable from the host on stable ports) and
**P3** (a CLI resolver that prefers a directly-reachable URL over a port-forward).
This completes the 3-phase design.

## Outcome

**Met — the full external-access design is complete and live-proven.** Four PRs
merged to master:

- **P2 — #114** (`feat(cli): route devnet ogmios/kupo over pinned host ports`):
  the k3d cluster is created with `--port "1337:30137@loadbalancer"` /
  `--port "1442:30442@loadbalancer"`; the embedded devnet spec exposes Ogmios/Kupo
  as NodePort Services (30137/30442) with localhost externalURLs; the devnet
  banner advertises the host-reachable externalURL.
- **Tripwire root-cause — #115** (`test(operator): track chart appVersion …`):
  the six `operator/ssa` version tripwires now read the chart appVersion
  dynamically instead of a hardcoded literal, so releases no longer red-light
  master.
- **Release — #113**: cut **v0.2.1** (bundling P1 #112 + P2 #114); published
  `ghcr.io/meigma/yacd:v0.2.1` + chart + CLI binaries to GHCR. master CI stayed
  green (the tripwire fix worked). GitHub release left as a draft.
- **P3 — #116** (`feat(cli): prefer reachable external URLs over port-forwards`):
  a shared per-endpoint resolver (`flag > YACD_*_URL env > probed externalURL >
  port-forward`) wired into `run` and the wallet-funding path.

Each implementation PR was adversarially reviewed (multi-agent workflows) and
live-validated on k3d before merge; the user approved every merge.

## Key Decisions

- **P2: edit the embedded `devnet.yaml` (not inject programmatically).** Forced by
  `devconfig.validateExplicitFields` — a present-but-zero `OgmiosSpec` would
  serialize `enabled:false`/`image:""` and override CRD defaults, so the chainAPI
  block must be fully spelled out (pinning the Ogmios/Kupo images in the yaml).
- **P2: `devnet.yaml` diverges from `examples/local`.** A localhost externalURL is
  correct only for devnet's host-mapped k3d cluster; `examples/local` stays
  ClusterIP so `yacd up -f` deploys anywhere.
- **Cut a v0.2.1 release (user's call).** `yacd devnet` installs the operator by
  appVersion tag, and P1 merged *after* v0.2.0 — so P2's payoff only activates
  once a P1-containing operator is released. Discovered live (the v0.2.0 operator
  rendered ClusterIP and didn't mirror externalURL). See Lessons.
- **Fixed the version-tripwire papercut at the root (user's call), not reactively.**
  Made the assertions track the chart appVersion (the `embeddedChartAppVersion`
  helper already existed) so no future release red-lights master. Had to rebase
  the release-please PR onto the tripwire fix (release-please doesn't regenerate
  for `test:` commits, so its branch was stale-red).
- **P3 probe = a 2s scheme-agnostic TCP dial** (the design's "short connect
  timeout"); no `/health` assumption, honours ctx, fails closed → fall back to
  forwarding.
- **P3 funding override needs no trust gate.** Funding submits a locally-signed
  tx (the signing key never leaves the CLI) and Kupo is read-only, so
  `--ogmios-url`/`--kupo-url` carry no token-leak risk; the deleted faucet trust
  gate was NOT reintroduced.
- **P3: `connect` stays forward-only**; `run` + funding use the resolver. `run`'s
  `chainAccess.session` is nillable with nil-safe `Close/Done/Err` so it reports
  no false "lost connection" when nothing was forwarded.

## Changes

P2 (#114, CLI):
- `cli/internal/cluster/cluster.go` — `PortMapping` + host/node port consts +
  `DefaultPortMappings` on `Spec`/`DefaultSpec`.
- `cli/internal/cluster/k3d/ensure.go` — render `--port` mappings + friendly
  host-port-collision error.
- `cli/internal/cli/devnet.yaml` + `embed.go` — NodePort chainAPI block + localhost
  externalURLs (divergence noted).

#115 (test): `cli/internal/operator/ssa/{apply_test,install_envtest_test}.go` →
`embeddedChartAppVersion(t)`.

#113 (release): `charts/yacd/Chart.yaml` (v0.2.1) + CHANGELOG + manifest.

P3 (#116, CLI):
- `cli/internal/cli/forward_resolve.go` (new) — `resolveChainAccess`, the prober,
  `chainOverrides`, `connectChain`.
- `cli/internal/cli/forward.go` — `chainAccess` (nillable session, nil-safe
  lifecycle) replaces `connectedSession`; `forwardAll` for connect.
- `cli/internal/cli/envcontract.go` — URL-based `hostEnvFromURLs`/`documentFromURLs`
  + `loopbackURLs`; `chainEndpoints` always returns both entries.
- `cli/internal/cli/{run,wallet,wallet_fund}.go` — wire the resolver + funding flags.

## Open Threads

- **GitHub release v0.2.1 is a draft** — publish if desired (GHCR artifacts are
  already live).
- **ogmigo ws-1006** non-fatal genesis-config warning still prints during wallet
  funding (issue #110) — move off ogmigo / suppress at source.
- **P3 is CLI-only** and works against the released v0.2.1 operator; a CLI release
  to ship the resolver to users is a separate decision (not taken this session).
- **Stale in-progress INDEX rows 051 + 052** predate this session (052 = docs
  PR #91, a separate stream); left untouched.
- Carried: docs (README/DESIGN + MkDocs #91) still describe pre-faucet-removal
  model; TEST_REPORT F2/F4; `yacd-env` GitHub Action.

## References

- PRs: #114 (P2, `dfa20b8`→ squashed), #115 (tripwire, `13122b6`), #113 (release
  v0.2.1, `d5c0b92`), #116 (P3, `f2501b7`). Follow-up issue #110.
- Release: `v0.2.1` — `ghcr.io/meigma/yacd:v0.2.1`, `chart:0.2.1`.
- Design: `.journal/060/EXTERNAL_ACCESS_DESIGN.md`. Prior: `.journal/060/SUMMARY.md`
  (P1), `.journal/061/SUMMARY.md` (faucet removal / v0.2.0).
- Review workflows: `wf_d4ae6b22-ea6` (P2), `wf_6c4ec7de-af2` (P3).

## Lessons

- **A feature touching BOTH the operator and the CLI's devnet path only works
  end-to-end once the operator half is RELEASED**, because `yacd devnet` installs
  the operator by appVersion tag. P1 was merged but not released; a devnet from
  master installed the *v0.2.0* operator (no P1) and silently rendered ClusterIP +
  no externalURL. Verifying against the released operator (not just master) is
  essential; sequence the release before declaring the CLI half "done".
- **release-please does not rebase its PR for non-releasable (`test:`) commits.**
  After merging the tripwire fix, the open release PR's branch was stale (old
  appVersion-vs-old-tripwire), so its CI was red. Rebase the release-please branch
  onto the new master (or it can't merge green); the squash-merge still tags
  correctly.
- **Prefer dynamic version assertions over hardcoded tripwires.** The hardcoded
  `v0.2.0` literals re-broke master on every release (v0.2.0 did, v0.2.1 would
  have); reading the chart appVersion ends the recurring papercut while still
  catching template-wiring drift.
- **A test-determinism trap in the P3 refactor:** routing `run`/funding tests
  through the new resolver made them flow through the default *real-TCP* prober,
  and the shared `readyNetwork` stub carried an externalURL — so a running devnet
  on localhost:1337 would have flaked them. Fix: keep the shared stub
  externalURL-free; set it only where the probe path is under test.
