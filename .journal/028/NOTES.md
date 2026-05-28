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
