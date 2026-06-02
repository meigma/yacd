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

## 2026-06-01 19:23 — Redirect landed; PR #7 at 0.1.0; at the publish gate

Plan approved (pause-before-merge; redirect+execute only). Executed the redirect:

- Worktree `chore/release-v0.1.0` off fetched master under `.wt/`. Dev stack
  waived (pure release plumbing; verification uses a throwaway cluster + published
  chart).
- Redirect change: one-line clarification in the `.github/workflows/release.yml`
  header comment, carrying a component-scoped `Release-As: yacd@0.1.0` footer.
  `root:check` green, `git diff --check` clean.
- PR #83 → squash-merged to master as `925caec` with the scoped footer in the
  squash commit. (Non-publishing — only makes release-please recompute.)
- release-please run on `925caec` succeeded and **rewrote PR #7 to 0.1.0**:
  title `chore(master): release 0.1.0`, manifest `.`=`0.1.0`, Chart.yaml
  `version: 0.1.0` / `appVersion: "v0.1.0"`; container components untouched
  (`11.0.1-yacd.5`) — scoped footer worked exactly as designed.
- All 15 dry-run checks re-ran green on the rewritten PR #7 (binary, container
  x2 arch, faucet, helm, cardano-testnet/tools x2 arch, ci, e2e, kusari).

Pre-flight cleared:
- **Tag protection**: I have repo admin. Repo rulesets = `[]`, master has no
  branch protection, classic tag protection = 404 (none), and the org is below
  the tier that enables org rulesets (`orgs/meigma/rulesets` → 403 "Upgrade to
  Team"). So there is NO tag protection; the release-please.yml header comment
  about a "protected-tag bypass" is intent-only, nothing is configured. The app
  already creates `cardano-*/v*` tags, so creating `v0.1.0` will not be blocked.
  No admin action needed.
- Draft-release sequencing: low risk — component releases already produce draft
  GitHub releases via the same `draft:true`+`force-tag-creation:true` config.

**At the publish gate.** Refs that WILL publish when PR #7 merges (tag `v0.1.0`):
manager `ghcr.io/meigma/yacd:v0.1.0`, faucet `ghcr.io/meigma/yacd/faucet:v0.1.0`,
chart `oci://ghcr.io/meigma/yacd/chart` @ `0.1.0`, binaries
`yacd_0.1.0_{darwin,linux}_{amd64,arm64}` on a DRAFT GitHub release. GHCR has no
draft state, so the merge publishes images+chart immediately + attestations; only
the GitHub release stays draft for manual Publish. Awaiting explicit user go to
merge PR #7. This will be the first-ever real run of `release.yml` (dry-runs skip
publish) — will babysit and fix-forward if the publish/attest tail breaks.
