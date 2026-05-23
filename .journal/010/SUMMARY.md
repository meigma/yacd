---
id: 010
title: Faucet E2E assessment and dev image fix
date: 2026-05-23
status: complete
repos_touched: [yacd]
related_sessions: [009]
---

## Goal
Assess the faucet-related code after the large phase-5 change and verify that a
user can fund an account through the `yacd` CLI talking to the custom faucet
service in the local development environment.

## Outcome
The goal was met. The faucet product path was sound, and manual Kind/Tilt E2E
testing proved a fresh address could be topped up through `yacd topup` and then
observed on chain with the requested lovelace amount. The assessment found one
local-dev wiring regression: Tilt did not build/load the faucet image because
the image reference only appeared in a manager flag, and the workload had also
started overriding the image entrypoint in a way that broke ko-built images.
PR #16 fixed those issues, passed CI, and was squash-merged into `master`.

## Key Decisions
- Preserve ko for local faucet image builds because that is the intended dev
  workflow; the incompatible workload command override was the bug.
- Make the faucet image an explicit Tilt local resource because Tilt cannot
  infer an image that only appears in `--default-faucet-image`.
- Let the faucet container use the image entrypoint so both ko-built dev images
  and release Dockerfile images remain valid.

## Changes
- `Tiltfile` - added the explicit `faucet-image` local resource that runs the
  ko faucet build helper and loads `ghcr.io/meigma/yacd/faucet:tilt` into
  `kind-yacd-dev`, with the controller resource depending on it.
- `internal/controller/cardanonetwork/workload_builder.go` - removed the
  hardcoded `/yacd-faucet` container command so the image entrypoint is the
  runtime contract.
- `internal/controller/cardanonetwork/workload_builder_test.go` - adjusted the
  faucet workload assertion to require an empty container command.

## Open Threads
- The faucet transaction stack still uses Apollo/ogmigo and inherits the
  session-009 Kusari follow-up around the transitive Gorilla WebSocket policy
  finding.
- The faucet remains a narrow prototype without caller quotas, rate limits,
  idempotency keys, or confirmation polling.
- Phase 6 can move on to the db-sync supporting-service model.

## Lessons
- Tilt image discovery only covers image references in rendered Kubernetes
  objects. Images passed as operator defaults or flags need explicit build/load
  resources in the dev stack.

## References
- PR #16: https://github.com/meigma/yacd/pull/16
- Merge commit: `7b6dc37` (`fix(dev): preserve ko faucet entrypoint (#16)`)
- Prior session 009: `.journal/009/SUMMARY.md`
