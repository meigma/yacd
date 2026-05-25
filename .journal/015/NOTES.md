---
id: 015
title: New YACD session
started: 2026-05-24
---

## 2026-05-24 18:40 — Kickoff
Goal for the session: Start a new journal session and wait for the substantive implementation or research request.
Current state of the world: `journal/jmgilman` is clean and up to date. The last closed session, `014`, merged managed Postgres support for `CardanoDBSync` in PR #24. Durable notes say YACD now has `CardanoNetwork`, Kupo, faucet, `CardanoDBSync`, external Postgres, and managed Postgres foundations in place. No implementation worktree has been selected yet, and the dev stack has not been started for this session.
Plan: Wait for the user's actual request. For implementation work, select or create the Worktrunk implementation worktree first, load task-relevant skills such as `k8s-operator` when touching APIs/controllers/tests, then run `moon run root:dev-up` once before making substantive code changes unless the user waives that startup step.

## 2026-05-24 18:48 — Phase 6 assessment
Goal for the checkpoint: Assess phase 6 progress against `.journal/PLAN.md` and the current db-sync-adjacent code.
Current state of the world: Phase 6 has moved well past the CRD/API stub. `CardanoDBSync` has an API, controller registration, planner, external Postgres path, managed Postgres path, follower-node/db-sync Deployment rendering, owned child reconciliation, artifact validation, identity guards, examples, envtest/controller coverage, and a Chainsaw managed-Postgres smoke. The remaining prototype proof is still significant: no CLI/devconfig surface for db-sync, no real Postgres connectivity/schema probe, no db-sync sync-progress probe, no populated `status.sync`, no aggregate `Ready=True`, and no live Kind proof that db-sync indexes blocks from the local chain.
Validation: `moon run root:test --summary minimal` passed the db-sync-related Go packages but failed overall in `test/chart` because the RBAC comparison still sees controller-gen rules for legacy `example.meigma.io/nginxdeployments` and core `events` that are not in the chart manager role.

## 2026-05-24 21:09 — Implementation worktree
Goal for the checkpoint: Begin implementation of efficient `CardanoDBSync` progress probing.
Current state of the world: Created Worktrunk implementation branch `feat/dbsync-progress-probes` at `.wt/feat-dbsync-progress-probes`. Ran `moon run root:dev-up` from that worktree; it created/attached the `kind-yacd-dev` stack, started Tilt in the background, and reported the YACD dev stack ready with Tilt UI on port `10350`.
Plan: Add a bounded controller runtime probe for Postgres progress plus Ogmios tip comparison, update status behavior and tests, then validate through focused Go tests before attempting broader checks or e2e smoke.

## 2026-05-24 22:29 — DBSync progress probe implementation
Goal for the checkpoint: Implement the phase-6 runtime progress probe for `CardanoDBSync` and validate it locally.
Current state of the world: The implementation worktree now has a bounded `pgx`/Ogmios runtime probe, status wiring for `status.sync`, `PostgresReady`, `Synced`, and aggregate `Ready`, controller requeue behavior, probe and controller tests, db-sync pod security/pgpass fixes, cardano-testnet artifact config hash enrichment, and an extended Chainsaw phase-6 smoke. The Kind smoke proves managed Postgres connectivity, `status.sync` publication, and an in-cluster `psql` query showing db-sync inserted at least one `block` row. The strict `Synced=True` Kind assertion was intentionally relaxed after live validation showed db-sync 13.7.1.0 stalls in near-tip client-index migration on the tiny localnet after the first block; `Synced=True` remains covered by controller tests with a near-tip probe result.
Validation: `moon run root:check --summary minimal`, `moon run root:test --summary minimal`, `moon run root:test-e2e --summary minimal`, and `git diff --check` all passed from `.wt/feat-dbsync-progress-probes`.

## 2026-05-25 10:49 — Review fixes
Goal for the checkpoint: Address the review findings against the db-sync progress probe branch without expanding the phase-6 scope.
Current state of the world: The implementation worktree now bumps default `cardano-testnet` tools image references and the local e2e image build to `11.0.1-yacd.4`, splits Postgres-only probing from the full Postgres/Ogmios sync probe so DB connectivity failures surface before db-sync containers are ready, and tightens the Chainsaw fallback check so phase 6 still requires node-tip and lag status publication. The implementation branch remains uncommitted; the tools image tag still needs a real GHCR publication before real installs can rely on that default pull.
Validation: `moon run root:test --summary minimal`, `moon run root:check --summary minimal`, `moon run root:test-e2e --summary minimal`, and `git diff --check` all passed from `.wt/feat-dbsync-progress-probes` after the review fixes.

## 2026-05-25 12:53 — Close
Goal for the checkpoint: Close session 015 after the reviewed db-sync progress probe work landed.
Current state of the world: PR #31 merged as `de42f99 feat(cardanodbsync): probe dbsync progress (#31)`, after the prerequisite `cardano-testnet` release path landed through PRs #25, #27, #29, and #30 with `cardano-testnet/v11.0.1-yacd.4`. The primary checkout `/Users/josh/code/meigma/yacd` is fast-forwarded to `master` at `de42f99`, the implementation worktree `.wt/feat-dbsync-progress-probes` has been removed, the session release branches and feature branch were deleted from origin, and `moon run root:dev-down` completed successfully from the primary checkout.
Validation: Before merge, `moon run root:test --summary minimal`, `moon run root:check --summary minimal`, `moon run root:test-e2e --summary minimal`, and `git diff --check origin/master` all passed. The e2e smoke built/loaded `ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.4`, proved db-sync inserted at least one block row, and published db/node/lag sync status.
