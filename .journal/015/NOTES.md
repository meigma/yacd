---
id: 015
title: New YACD session
started: 2026-05-24
---

## 2026-05-24 18:40 — Kickoff
Goal for the session: Start a new journal session and wait for the substantive implementation or research request.
Current state of the world: `journal/jmgilman` is clean and up to date. The last closed session, `014`, merged managed Postgres support for `CardanoDBSync` in PR #24. Durable notes say YACD now has `CardanoNetwork`, Kupo, faucet, `CardanoDBSync`, external Postgres, and managed Postgres foundations in place. No implementation worktree has been selected yet, and the dev stack has not been started for this session.
Plan: Wait for the user's actual request. For implementation work, select or create the Worktrunk implementation worktree first, load task-relevant skills such as `k8s-operator` when touching APIs/controllers/tests, then run `moon run root:dev-up` once before making substantive code changes unless the user waives that startup step.
