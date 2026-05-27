---
id: 026
title: Primary sidecar manual functional testing
started: 2026-05-27
---

## 2026-05-27 13:25 — Kickoff
Goal for the session: continue from session 025 by manually function-testing the new `CardanoDBSync.spec.placement.primarySidecar` feature.
Current state of the world: `master` is at `8e77d3d` (`feat(cardanodbsync): support primary sidecar placement (#45)`). Session 025 closed with the implementation merged, the implementation worktree removed, and the dev stack stopped. Recent context from sessions 023-025 covers the CLI refactor, the post-refactor manual validation pass, and the primary-sidecar implementation.
Plan: prime the session first, then select or create an implementation worktree, start the local dev stack with `moon run root:dev-up`, and run focused manual tests around primary-sidecar attach, readiness, conflict, and handoff behavior.
