---
id: 047
title: F0 redesign PR-C — db-sync consumes configs over HTTP
started: 2026-05-31
---

## 2026-05-31 18:21 — Kickoff
Goal for the session: continue the F0 redesign series. PR-A is merged
(session 046, PR #75, `c61e0a6`). The next slice per the agreed order
(A → C → B → D) is **PR-C: CardanoDBSync consumes network configs over HTTP**.
Replace the CardanoDBSync ConfigMap mount with a cardano-tools `fetch` init
→ emptyDir + manifest verify, pointed at the primary network's serve endpoint
(`status.endpoints.artifacts.url`). PR-C MUST land before PR-B, which deletes
the `<net>-network-artifacts` ConfigMap that db-sync currently GETs by name.

Current state of the world:
- `master` at `c61e0a6` (PR-A merged, additive: serve sidecar + producer +
  `<net>-artifacts` Service + `status.endpoints.artifacts` alongside the
  existing ConfigMap path; node/ogmios/faucet containers unchanged).
- PR-C reworks ~6 `internal/controller/cardanodbsync` files plus a
  cross-controller edit to
  `internal/controller/cardanonetwork/dbsync_sidecar.go` (~lines 103, 113).
- Each F0 PR must keep the chainsaw e2e green (runs on every PR). Branch fresh
  off master.
- Carried non-F0 threads: TEST_REPORT F2/F4 open; test-harness Phases 3–5
  remain; KNOWN FLAKE
  `TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync`
  (load-sensitive envtest); e2e Docker Hub 429 jitter on ogmios/kupo.
- Dev stack was left UP but ORPHANED on `kind-yacd-dev` at the end of session
  046 (the F0 worktree that owned it was removed at merge; `.run/yacd-dev`
  ownership dangles). Tear down with `moon run root:dev-down` from the main
  checkout, or repair before starting implementation.

Plan (rough):
1. Wait for the user's actual request / confirm PR-C is the target.
2. Create a fresh implementation worktree off master via `wt`.
3. Start the dev stack (`moon run root:dev-up`) once after selecting the
   worktree — note the orphaned-stack cleanup may be needed first.
4. Implement PR-C, keep `root:check`/`root:test`/chainsaw green, PR + squash.
