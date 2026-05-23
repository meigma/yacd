---
id: 009
title: Phase 5 Kupo and faucet
date: 2026-05-23
status: complete
repos_touched: [yacd]
related_sessions: [007, 008]
---

## Goal
Move from the phase-4 CLI foundation into phase 5 by adding the chain API
pieces needed for a useful local developer faucet. Start with Kupo as a
first-class indexed chain API, then build a narrow faucet/top-up service that
keeps source signing material inside the primary Pod boundary while exposing a
controlled API and CLI command.

## Outcome
The goal was met. PR #14 added Kupo as a default `CardanoNetwork` chain API and
was squash-merged into `master` as `b52e923`. PR #15 added the opt-in
authenticated faucet vertical and was squash-merged into `master` as `14bbcc1`.
The local `master` checkout was fast-forwarded, the Kind/Tilt dev stack was
stopped, and the session implementation worktrees were removed.

## Key Decisions
- Treat Kupo as a YACD chain API, not just an Apollo implementation detail,
  because it gives developers a lightweight indexed endpoint alongside Ogmios.
- Keep the faucet opt-in rather than default-enabled, because enabling a
  signing endpoint just because Ogmios and Kupo exist is too surprising.
- Use the Pod as the faucet key boundary: source keys stay on the localnet state
  volume, and the faucet sidecar receives only the `utxo-keys` subPath.
- Protect mutable faucet requests with a controller-owned bearer-token Secret,
  while keeping health, readiness, and source discovery unauthenticated.
- Remove Secret watches/list RBAC and use live API reads plus periodic requeue,
  because cluster-wide Secret informers are too broad for this operator.
- Accept the Apollo/ogmigo prototype dependency despite Kusari's Gorilla
  WebSocket EOL policy finding, because `govulncheck` reports no called
  vulnerabilities and replacing the transaction stack is a larger follow-up.

## Changes
- `api/v1alpha1/cardanonetwork_types.go` and generated CRDs - added
  `spec.chainAPI.kupo`, `spec.chainAPI.faucet`, endpoint status, faucet auth
  Secret status, and `KupoReady`/`FaucetReady` condition documentation.
- `internal/controller/cardanonetwork` - injects Kupo and faucet sidecars,
  reconciles owned Services and the faucet auth Secret, derives readiness and
  aggregate `Ready`, revokes stale faucet exposure on unsupported/degraded
  specs, enforces port/image/dependency constraints, and periodically repairs
  faucet Secrets without Secret watches.
- `services/faucet` - added the `yacd-faucet` service with Cobra/Viper
  configuration, source discovery and validation for `utxo-keys`, authenticated
  JSON `POST /v1/topups`, Apollo transaction building/signing, Ogmios submit
  rejection checking, stale UTxO protection, exact-lovelace and fee/source-spend
  invariants, HTTP timeouts, and source/key validation tests.
- `cli/internal/cli` and `cli/internal/kube` - extended `yacd info` for Kupo and
  faucet, added `yacd topup`, read the faucet auth Secret before calling the
  service, and added explicit trust flags for custom non-loopback faucet URLs.
- `charts/yacd`, `cmd`, `Tiltfile`, `.dev`, `.github/workflows/release.yml`,
  and `services/faucet/Dockerfile` - wired the faucet image, manager default
  image option, chart values/schema/RBAC, Kyverno image refs, Tilt build/load,
  release dry-run, and e2e support.
- `test/chainsaw/manager-smoke/chainsaw-test.yaml` - extended installed
  operator smoke coverage for Kupo and faucet, including an authenticated live
  top-up through the deployed faucet sidecar.
- `README.md` and `examples/local/yacd.yaml` - refreshed current-state and
  local workflow documentation for Kupo, faucet, and host-side `yacd topup`.

## Open Threads
- Kusari Inspector still flags transitive `github.com/gorilla/websocket`
  through `github.com/SundaeSwap-finance/ogmigo/v6`. The accepted near-term
  posture is a prototype exception; a cleaner future path is a maintained
  YACD-owned Ogmios client or upstream `ogmigo` migration to a maintained
  WebSocket library.
- The faucet has bearer-token auth but no per-caller quota, rate limit,
  idempotency key, or confirmation polling.
- Kupo storage remains ephemeral and intentionally narrow. Persistent/index
  tuning fields can follow once the prototype proves where they matter.
- The developer config still reuses the concrete CRD spec and requires explicit
  fields for present chain API blocks.
- db-sync/follower-node supporting services remain future phase work.

## References
- PR #14: https://github.com/meigma/yacd/pull/14
- PR #15: https://github.com/meigma/yacd/pull/15
- Kupo merge commit: `b52e923` (`feat(cardanonetwork): add kupo chain api (#14)`)
- Faucet merge commit: `14bbcc1` (`feat(faucet): add authenticated top-up service (#15)`)
- Prior session 007: `.journal/007/SUMMARY.md`
- Prior session 008: `.journal/008/SUMMARY.md`
