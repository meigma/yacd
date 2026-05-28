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

## 2026-05-28 07:22 — Node ledger snapshot terminology
Clarified that "ledger snapshot" is not the right top-level YACD product boundary. Cardano node ChainDB has immutable, volatile, and ledger snapshot subdirectories; the ledger snapshot is enough for restart/replay within a ChainDB, but not a complete portable environment restore by itself. Public node bootstrap should keep using Mithril's Cardano DB snapshot flow because the client discovers, downloads, verifies, and unpacks a full node DB plus ancillary ledger data. For local/YACD-created restore, the simple first slice should snapshot the full node DB/PVC plus generated network material, not only `db/ledger`. The CRD can still stay universal by pointing at a snapshot descriptor URL with component entries (`node`/`dbsync`) and metadata, while the operator dispatches each component to existing restore tools.

## 2026-05-28 07:37 — Snapshot manifest direction
Design leaning: make a small standardized YACD snapshot manifest the common contract, and have the CLI produce it. Support two packaging modes instead of forcing one: a self-contained YACD bundle for snapshots YACD creates, and an external-artifacts manifest for existing tooling outputs such as Mithril Cardano DB snapshots plus upstream db-sync `.tgz` snapshots. The CRD should reference the manifest URL/checksum and selected restore components, while the manifest records artifact URLs/checksums/formats/tool metadata. This keeps the operator universal without making users repackage large public artifacts.

## 2026-05-28 07:53 — Snapshot design draft
Created `.journal/SNAPSHOT_DESIGN.md` as the first proposal draft. The document uses the agreed outline: introduction, manifest specification, YACD-native snapshotting create/consume, public snapshot consumption, and open design questions. It recommends a CLI-produced manifest as the common contract, supports both self-contained YACD bundles and external public artifacts, and keeps the CRD restore surface small.

## 2026-05-28 07:38 — Session refocus: break-the-operator manual pass
Pivot for the rest of session 029. The user redirected from snapshot design to a focused adversarial manual test pass against the operator on the local Kind/Tilt stack. Scope:
- Goal: get the operator into "unexpected" states — infinite reconcile loops, changes that silently break the underlying node/db-sync, or unrecoverable error conditions. Explicitly out of scope: legitimate declared error states where the controller correctly says "I am failing because <X>" through events/status conditions.
- Bonus angle: evaluate whether legitimate error states are sufficiently observable — does status/events let a user reasonably infer what is wrong?
- Constraint: no CLI in this pass. All probing is via direct CRD edits or Kubernetes API manipulation (kubectl, raw resource mutations, child resource tampering).
- Approach the user asked for:
  1. Build a firm operator-architecture mental model.
  2. Spawn parallel subagents to theorize ways the operator could be broken.
  3. Synthesize, dedupe, and present a final candidate list for review before turning it into a test plan.
Cleanup: an erroneously created `.journal/030/` was removed; session 029 stays open and absorbs this work. Erroneous start commit `526aad3` remains in the journal history for accuracy; this NOTES entry records the pivot.

## 2026-05-28 07:38 — Erroneous session 030 cleanup
Removed `.journal/030/` from disk. The earlier `docs(journal): start session 030` push (`526aad3`) is preserved in history rather than rewritten — the next journal commit records the cleanup and refocus instead.
