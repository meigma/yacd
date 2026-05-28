---
id: 030
title: TBD
started: 2026-05-28
---

## 2026-05-28 07:38 — Kickoff
Goal for the session: pending — the user has not yet stated this session's concrete request.
Current state of the world:
- `master` is at `69a87d1` (PR #48 merged: public non-mainnet CardanoDBSync primary-sidecar support).
- Sessions 026-028 closed: primary-sidecar manual pass (PR #46), public CardanoNetwork profiles plus mainnet bootstrap (PR #47), public non-mainnet db-sync primary-sidecar (PR #48).
- Session 029 was kicked off this morning (2026-05-28 07:03) for "Continue dbsync work" and recorded research notes through 07:37 on db-sync snapshot/restore behavior, ledger-vs-node snapshot terminology, and a snapshot-manifest direction, but was never closed; no SUMMARY.md exists and INDEX.md still ends at 028. The journal branch is ahead of origin by 1 commit that captures the 029 notes mutation.
- Public mainnet db-sync remains unsupported. Open thread from session 028 is a managed-Postgres-only snapshot restore prototype for public mainnet `primarySidecar`.
- Local dev stack is not running (no implementation worktree selected yet).
Plan: wait for the user's concrete request, decide whether to resume session 029 or proceed in 030, then select or create the implementation worktree and bring up the dev stack before substantive work.
