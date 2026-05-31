---
id: 044
title: CLI review fixes
date: 2026-05-31
status: complete
repos_touched: [yacd]
related_sessions: [041]
---

## Goal
Review `cli/` for architectural consistency, correctness against the operator contract, bugs, UX, and Go style/testing practices, then implement the accepted fixes.

## Outcome
Met. The review found focused CLI issues rather than a broad hexagonal violation, and PR #73 merged the fixes into `master`. The local dev stack was stopped, the local `master` checkout was fast-forwarded to the merge commit, and the feature worktree was removed.

## Key Decisions
- Keep the CLI runtime preflight local to `cli/internal/devconfig` instead of importing controller internals, preserving package boundaries while failing unsupported configs before render/apply.
- Keep the default `connect` endpoint path compatible for `namespace == name`, but namespace-qualify override paths to avoid cross-namespace collisions.
- Remove endpoint state files on clean disconnect/drop instead of leaving advisory stale URLs for host tools.

## Changes
- `cli/internal/devconfig` - added runtime-support preflight validation for deterministic controller rejections that are knowable from developer config, plus table coverage.
- `cli/internal/cli` - validated `topup --await --kupo-url` before cluster/faucet side effects, namespace-qualified `connect` endpoint files, removed stale endpoint files, and cleaned up `httptest` handler assertions.
- `docs/host-access.md` - documented namespace-qualified endpoint paths and endpoint file cleanup behavior.

## Open Threads
- The CLI runtime preflight intentionally mirrors current controller support constants. If the controller support matrix changes, update the CLI preflight in the same slice.
- `feat/f0-public-profile-pvc` remains an unrelated active branch from the F0 follow-up and was left alone.

## References
- PR: https://github.com/meigma/yacd/pull/73
- Prior host-access implementation context: `.journal/041/SUMMARY.md`
