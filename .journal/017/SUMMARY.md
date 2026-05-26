---
id: 017
title: ctrlkit foundation
date: 2026-05-25
status: complete
repos_touched: [yacd]
related_sessions: [012, 013, 015]
---

## Goal
Establish a small internal controller utility library that makes the existing `CardanoNetwork` and `CardanoDBSync` controller mechanics easier to reason about without swallowing Cardano/YACD domain rules. Start from a standalone `internal/ctrlkit` foundation, then integrate it into both controllers where real call sites proved the shape.

## Outcome
The goal was met. PR #33 merged as `d8b610e` with a squash commit titled `refactor(ctrlkit): share controller foundations`; CI and Kusari Inspector passed before merge. The local dev stack was shut down, local `master` was fast-forwarded to the merge commit, and the feature worktree was removed.

## Key Decisions
- Keep `ctrlkit` generic and domain-free -> review feedback repeatedly flagged Cardano/YACD constants in shared helpers as a boundary smell, so artifact contracts and annotation keys now live in `internal/cardano/*` or controller-side packages.
- Extract owned-child apply only after controller call sites proved it -> `ctrlkit/apply.ApplyOwnedObject` now owns the common get/create/owner/validate/mutate/patch-update skeleton while mutation callbacks keep per-resource field ownership explicit.
- Split artifact validation by layer -> pure CardanoNetwork artifact schema/key contracts live in `internal/cardano/networkartifacts`; Kubernetes/status producer and consumer checks live in `internal/controller/networkartifacts`; generic hash/key validation remains in `ctrlkit/artifacts`.
- Preserve controller-specific status language -> shared helpers return generic mechanics, while controller packages still map conflicts, storage drift, readiness, and artifact failures into the existing condition reasons/messages.

## Changes
- `internal/ctrlkit/*` - added shared packages for naming, metadata/ownership, apply mechanics, artifact data validation, readiness, resources, status errors, and storage drift helpers with focused unit tests.
- `internal/controller/cardanonetwork/*` - replaced duplicated naming, metadata merge, ownership, condition, readiness, artifact, storage, and owned-child apply code with `ctrlkit` helpers while preserving behavior.
- `internal/controller/cardanodbsync/*` - adopted the same shared helpers for db-sync workload and managed Postgres reconciliation, removed the local network artifact validation duplicate, and tightened readiness/status flows.
- `internal/cardano/networkartifacts/*` - added the Kubernetes-free CardanoNetwork artifact schema/key contract used by producer and consumer validation.
- `internal/controller/networkartifacts/*` - added controller-side producer/consumer ConfigMap and status validation, including explicit unsupported schema rejection for dependent controllers.
- `internal/controller/annotations/*` and `internal/controller/storage/*` - centralized YACD controller-owned annotation keys and controller-specific storage drift message mapping outside `ctrlkit`.

## Open Threads
- No blocking follow-up from this session. Future controller work should prefer `ctrlkit` for shared mechanics, but should keep product/domain contracts in `internal/cardano` or controller-side packages unless a truly generic boundary emerges.

## References
- PR #33: https://github.com/meigma/yacd/pull/33
- Merge commit: `d8b610e31b6584c755d6df14b03a1d8074439cb1`
- Session notes: `.journal/017/NOTES.md`
