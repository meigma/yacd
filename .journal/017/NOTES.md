---
id: 017
title: ctrlkit foundation
started: 2026-05-25
---

## 2026-05-25 13:19 — Kickoff
Goal for the session: implement the standalone `internal/ctrlkit` foundation with subpackages for controller naming, metadata/ownership, conditions, readiness, and generic artifact ConfigMap validation, without modifying existing controllers or other repo surfaces.
Current state of the world: `master` is at `de42f99` with `CardanoNetwork` and `CardanoDBSync` controllers already implemented; prior closed sessions through `015` are complete, and `.journal/016` already exists as an active tracked session. The new work should stay scoped to `internal/ctrlkit/**`.
Plan: prime this session on `journal/jmgilman`, create an isolated Worktrunk branch for implementation, start the repo dev stack, add the ctrlkit packages and focused tests, then run `moon run root:test`, `moon run root:check`, and `git diff --check`.

## 2026-05-25 13:21 — Dev stack ready
Created implementation worktree `/Users/josh/code/meigma/yacd/.wt/feat-ctrlkit-foundation` on branch `feat/ctrlkit-foundation`. Ran `direnv allow` and `moon run root:dev-up`; the Kind/Tilt dev stack reported `YACD dev stack is ready` with Tilt UI on `http://localhost:10350/`.

## 2026-05-25 13:28 — ctrlkit foundation pushed
Implemented only `internal/ctrlkit/**`: `names`, `metadata`, `conditions`, `readiness`, and `artifacts`, each with package docs and focused table-driven tests. Validation passed with `go test ./internal/ctrlkit/...`, `moon run root:test`, `moon run root:check`, and `git diff --check`. Committed `abb9747` (`feat(ctrlkit): add controller utility foundation`) and pushed `feat/ctrlkit-foundation` to origin.

## 2026-05-25 14:29 — ctrlkit controller integration pushed
Integrated `internal/ctrlkit` into the `CardanoNetwork` and `CardanoDBSync` controllers as a refactor-only slice. Added shared `ctrlkit/apply` and `ctrlkit/storage` helpers, extended artifact/name contracts, removed duplicated db-sync network artifact helpers, and rewired controller naming, metadata merge, ownership, conditions, readiness predicates, artifact validation, and requested storage-class handling through ctrlkit while preserving controller-specific wrappers and messages. Validation passed with `go test ./internal/ctrlkit/...`, `moon run root:test`, `moon run root:check`, and `git diff --check`. Committed `6482867` (`refactor(ctrlkit): share controller helper logic`) and pushed `feat/ctrlkit-foundation` to origin.

## 2026-05-25 15:08 — review feedback cleanup pushed
Addressed review feedback that `ctrlkit` should not own CardanoNetwork domain contracts. Added `internal/cardano/networkartifacts` for the operator-side CardanoNetwork artifact schema/key contract, removed the unused `ctrlkit/artifacts` `Result` validator and string-based reason classification, and renamed `metadata.MergeStringMap` to `OverlayStringMap` to make preserve-existing-key semantics explicit. Validation passed with `go test ./internal/ctrlkit/... ./internal/cardano/networkartifacts`, `moon run root:test`, `moon run root:check`, and `git diff --check`. Committed `c62c213` (`refactor(ctrlkit): move network artifact contract`) and pushed `feat/ctrlkit-foundation` to origin.

## 2026-05-25 17:25 — controller contract cleanup pushed
Addressed the second architecture review pass. Moved producer and consumer artifact validation workflows into `internal/cardano/networkartifacts`, routed `CardanoNetwork` sidecar deployment gates through a local helper backed by `ctrlkit/readiness.DeploymentAvailable`, made `ctrlkit/names.DNSLabelWithSuffix` sanitize and hash unsafe or truncated suffixes, and centralized requested storage-class drift comparison in `ctrlkit/storage`. Validation passed with `go test ./internal/ctrlkit/... ./internal/cardano/networkartifacts`, `moon run root:test`, `moon run root:check`, and `git diff --check`. Committed `3a69672` (`refactor(ctrlkit): centralize controller contract checks`) and pushed `feat/ctrlkit-foundation` to origin.
