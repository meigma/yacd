---
id: 053
title: Local lifecycle Phase 1 — cut operator v0.1.0
started: 2026-06-01
---

## 2026-06-01 18:07 — Kickoff

Goal for the session: begin executing the `LOCAL_LIFECYCLE_PLAN.md` (session 049),
starting with **Phase 1 — cut `v0.1.0` of the operator + Helm chart**. Per the
plan, Phase 1 is pure release plumbing (no operator behavior changes): drive the
existing release-please + `release.yml` flow to a real published `v0.1.0` — images
on `ghcr.io/meigma/yacd` and the Helm chart at `0.1.0` (appVersion `v0.1.0`) — so
the CLI lifecycle work has a reliable, versioned install target. User will give
the next concrete instructions after priming.

Current state of the world:
- Primed against the session protocol. Journal root = the `journal/jmgilman`
  Worktrunk worktree at `.wt/journal-jmgilman`; journal clean and up to date.
- Read session 049's `LOCAL_LIFECYCLE_DESIGN.md` + `LOCAL_LIFECYCLE_PLAN.md` (the
  approved, design-only plan) and the 049/050 summaries. Plan dependency graph:
  P1 (release v0.1.0) → P4 (funded wallet, v0.2.0) → P5 (operator install, embeds
  v0.2.0); P2 (toolbin) → P3 (cluster); P3,P5 → P6 (devnet) → P7 (hardening).
- The F0 redesign series is **COMPLETE** as of session 050 (PR-A/B/C/D merged);
  master is a coherent local + curated-public operator — the Phase-1 "don't cut
  mid-redesign" precondition appears satisfied.
- **Tension to resolve before doing Phase 1 work:** the plan says cut `v0.1.0`,
  but session 050 left **root release PR #7 open targeting `yacd 1.0.0`** (the
  release-please-computed version, deliberately unmerged awaiting a release
  decision). Phase 1's target version (`v0.1.0`) conflicts with the in-flight
  `1.0.0` release PR. Need a user decision on which version the operator's first
  published release should carry before driving release-please.
- Sessions 051 and 052 are stale `in-progress` rows in INDEX (started, never
  used; "awaiting the user's request"). Left untouched.

Plan: confirm priming complete, surface the v0.1.0-vs-1.0.0 version tension to the
user, and wait for concrete Phase 1 instructions before any release-please/CI work.
No implementation worktree created yet (Phase 1 is release plumbing; will create
one from fetched master once the approach is confirmed).
