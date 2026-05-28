---
id: 029
title: Continue dbsync work
started: 2026-05-28
---

## 2026-05-28 07:03 — Kickoff
Goal for the session: Continue work from the last few sessions, specifically focusing on dbsync.
Current state of the world: Sessions 026-028 closed the primary-sidecar manual pass, public CardanoNetwork profiles and mainnet bootstrap, and public non-mainnet CardanoDBSync primary-sidecar support. Public mainnet db-sync remains unsupported; the likely next slice is an agile managed-Postgres snapshot restore prototype for public mainnet primary-sidecar db-sync.
Plan: Prime the session context first, then wait for the concrete dbsync request before selecting or creating an implementation worktree and starting the dev stack.

## 2026-05-28 07:12 — DB Sync snapshot investigation
Investigated upstream CardanoDBSync snapshot/restore behavior before implementing anything. Upstream state snapshots bundle PostgreSQL plus db-sync ledger state, are created after stopping db-sync with `cardano-db-tool prepare-snapshot` followed by `scripts/postgresql-setup.sh --create-snapshot`, and are restored with `scripts/postgresql-setup.sh --restore-snapshot <snapshot.tgz> <state-dir>`. Restore recreates the database by default, requires an empty ledger-state directory, and restores both ledger state and LSM data when applicable. Current official release `13.7.1.0` links only mainnet snapshot directories, with schema `13.7` and `13.6` compatibility. The public bucket is discoverable via the S3 list API under `cardano-db-sync/`; current `13.7` has two mainnet snapshots and sha256 sidecars. Takeaway for YACD: public mainnet db-sync support should treat snapshot restore as managed-Postgres-only bootstrap first, with snapshot URL/schema/block/arch/hash/backend as accepted identity inputs.
