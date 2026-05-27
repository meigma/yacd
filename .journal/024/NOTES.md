---
id: 024
title: Post-refactor manual functional test pass
started: 2026-05-26
---

## 2026-05-26 16:22 — Kickoff
Goal for the session: pending — user has not stated a goal yet.
Current state of the world:
- Repo `meigma/yacd` on `master` at `939219c` (refactor(cli): split packages,
  tighten godoc bar, typed conditions, mockery migration — PR #41). Working
  tree clean; no implementation worktree active.
- Journal worktree `journal/jmgilman` at `cc1c0c6` (`docs(journal): close
  session 023`), clean and up to date with origin after rebase.
- Last three closed sessions (021, 022, 023) were the readability /
  hexagonal / typed-vocabulary refactor sweep: PRs #38, #39 (cardanonetwork),
  #40 (cardanodbsync), #41 (cli). The natural follow-up surfaced in 023's
  open threads is mockery + Testify migration in the controller / ctrlkit
  packages.
- Other open thread carried across recent sessions: INDEX.md is missing a
  row for session 016 (pre-existing gap, not introduced by recent sessions).
- Dev stack is down; no implementation Worktrunk worktree is selected. Will
  start `moon run root:dev-up` from the implementation worktree once one is
  created for this session's actual work, per `.session.md`.
Plan: wait for the user's actual request before priming further.

## 2026-05-26 18:39 — Close
Session complete. All ten manual functional test phases passed. One
real bug surfaced and was fixed mid-pass: the published
`cardano-testnet:11.0.1-yacd.4` tools image lags PR #31's
`EnrichGenesisHashes`, breaking CardanoDBSync on `moon run root:dev-up`
because db-sync requires `ByronGenesisHash` keys that the published
publisher doesn't write. Fixed by plumbing `--default-cardano-testnet-image`
through the manager flag, chart value, and Tilt's
`cardano-testnet-image` `local_resource` (two commits: one for
cardanonetwork, one for cardanodbsync; same PR because they share the
plumbing). CI green; PR #42 merged as squash commit `f5bbfbb`. Master
fast-forwarded; `test/post-refactor-validation` worktree and branch
removed; dev stack stopped via `moon run root:dev-down`.

Phase 3 produced one observation worth surfacing: disabling `kupo`
while `faucet` is enabled is rejected as `UnsupportedSpec` AND the
controller calls `revokePrimaryFaucetExposure` to tear down the
faucet Service/auth Secret/sidecar container. Intentional security
behavior per `controller.go:93` + `delete.go:124-138`; documented in
TECH_NOTES.md for future agents.

Open threads worth picking up later: cut `cardano-testnet/v11.0.1-yacd.5`
so the published image catches up to PR #31; revisit the 10-minute
faucet auth Secret repair latency if external Secret deletion becomes
an operational concern; consider re-ordering CardanoDBSync's
resolve-database vs. resolve-network so the managed Postgres auth
Secret isn't pre-created when the network reference is missing.

PRs merged: #42 (https://github.com/meigma/yacd/pull/42).
