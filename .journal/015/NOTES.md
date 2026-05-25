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
