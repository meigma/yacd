---
id: 028
title: Public db-sync follow-up
started: 2026-05-27
---

## 2026-05-27 20:51 — Kickoff
Goal for the session: continue work around the new db-sync logic, especially db-sync running next to the node and the new support for public networks.
Current state of the world: sessions 025-027 landed local-network `primarySidecar` placement, manual primary-sidecar functional proof, post-acceptance placement immutability, public `CardanoNetwork` profiles for preview/preprod/mainnet/custom, and public `CardanoDBSync` support only through `dedicatedFollower` for preview/preprod/custom. Public mainnet db-sync remains rejected; public primary-sidecar support is a future slice that needs artifact/socket validation and combined pod sizing rather than a brand-new placement concept.
Plan: stay agile and start from the recent behavior rather than designing the whole public db-sync matrix up front; pick a narrow next slice after reviewing the current code and runtime constraints.

## 2026-05-27 21:24 — Implementation worktree
Created implementation worktree `feat/public-dbsync-primary-sidecar` at `.wt/feat-public-dbsync-primary-sidecar` and started the local dev stack with `moon run root:dev-up`. The stack reported ready; Tilt UI is on port 10350 and logs are under `.run/yacd-dev/tilt.log`.

## 2026-05-27 21:36 — Public sidecar proof
Implemented public non-mainnet `primarySidecar` support on branch `feat/public-dbsync-primary-sidecar` and committed it as `2f94569`. Validation passed with focused controller tests, `moon run root:test`, `moon run root:check`, and `git diff --check`. Manual preview functional proof in Kind/Tilt passed: `preview-smoke` reached Ready, `preview-dbsync-sidecar` reached `PostgresReady=True`, `NodeSocketReady=True`, `SidecarMaterialReady=True`, `DBSyncReady=True`, and `DBSyncAttachmentReady=True`; the primary Deployment contained `cardano-db-sync`, the dedicated db-sync Deployment and follower PVC were absent, Ogmios returned `queryNetwork/tip`, and a psql probe found `66162` block rows.
