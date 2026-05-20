---
id: 002
title: YACD foundation branding pass
date: 2026-05-20
status: complete
repos_touched: [yacd]
related_sessions: [001]
---

## Goal
Continue from the initial design bootstrap and complete the first implementation
slice: brand the repository as YACD, remove the template `NginxDeployment`
surface, keep the useful operator foundation, and prove the empty manager shell
still builds, tests, and deploys.

## Outcome
The goal was met. PR #2 was reviewed, CI passed, and it was squash-merged into
`master` as `9680952` (`refactor: brand repository as YACD foundation (#2)`).

## Key Decisions
- Removed the sample CRD entirely rather than replacing it with a fake YACD API
  because this pass was meant to avoid inventing product API shape before the
  first real environment prototype.
- Kept controller-runtime manager startup, secure metrics, Helm, Moon,
  envtest, Chainsaw, release, and dev-stack wiring so the repo remains a usable
  operator foundation.
- Treated `.journal/PLAN.md` phase 1 as complete in intent, with its "first
  YACD API group/version" bullet deferred to phase 2 because the approved scope
  explicitly avoided introducing actual APIs.

## Changes
- `go.mod`, `PROJECT`, `README.md`, `AGENTS.md`, release workflows, scripts,
  and dev-stack files now use YACD identity and defaults.
- `charts/yacd` replaces the template chart path and renders an installable
  manager-only chart with empty manager RBAC while no controllers exist.
- `api/v1alpha1`, `internal/controller`, generated CRDs, Nginx fixtures, and
  template cleanup guidance were removed.
- `cmd/setup.go` now registers only core Kubernetes types and logs that no
  controllers are registered yet.
- `cmd/foundation_test.go`, chart tests, and Chainsaw smoke now verify the
  no-custom-API foundation, manager readiness, and protected metrics.

## Open Threads
- Start phase 2 by introducing the first real YACD primary environment CRD and
  controller; do not add a placeholder API just to satisfy scaffolding.
- Update `.journal/PLAN.md` when the next session starts if the phase 1 API
  bullet should be reworded or marked complete.
- The release dry-run workflow remains intentionally skipped on ordinary PRs;
  use Release Please branches or manual dispatch to exercise it.

## References
- PR #2: https://github.com/meigma/yacd/pull/2
- Merged commit: `9680952` (`refactor: brand repository as YACD foundation (#2)`)
- Prior design session: `.journal/001/SUMMARY.md`
- Design: `DESIGN.md`
- Prototype plan: `.journal/PLAN.md`
