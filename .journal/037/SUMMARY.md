---
id: 037
title: TEST_REPORT issue follow-through
date: 2026-05-29
status: complete
repos_touched: [yacd]
related_sessions: [029, 031, 032, 033, 034, 035]
---

## Goal
Continue fixing issues found in `.journal/TEST_REPORT.md`, focusing this session on D1: faucet auth Secret recovery and workload revision visibility.

## Outcome
The goal was met. PR #54 fixed D1 by making owned faucet auth Secret create/update/delete events part of normal `CardanoNetwork` reconciliation and by stamping the primary Deployment pod template with a deterministic hash of the live faucet auth token. The PR merged into `master`, the local default checkout was fast-forwarded, the dev stack was shut down, and the feature worktree was removed.

## Key Decisions
- Keep token generation, live Secret reads, and token-hash stamping in the side-effecting reconciler path -> preserves the hexagonal boundary and leaves `primaryWorkloadBuilder` pure.
- Use `yacd.meigma.io/faucet-auth-token-hash` on the Deployment pod template -> Secret repair or valid token rotation now produces a normal Kubernetes rollout.
- Add a narrow owned Secret predicate for primary faucet auth Secrets -> owned Secret events enqueue their `CardanoNetwork` without mixing this path with custom public profile Secret watches.
- Keep live Secret reads in faucet auth apply/readiness behavior -> the controller must not publish readiness or workload revisions from stale cached Secret data.

## Changes
- `internal/controller/cardanonetwork/faucet_auth.go` - returns the reconciled Secret from apply, validates/repairs token state from live reads, and computes the stable token hash.
- `internal/controller/cardanonetwork/controller.go` - wires the reconciled Secret into Deployment apply and registers the owned faucet auth Secret watch.
- `internal/controller/cardanonetwork/faucet_auth_watch.go` - adds the scoped predicate for YACD primary-workload faucet auth Secrets.
- `internal/controller/cardanonetwork/annotations.go` and `artifacts.go` - define and stamp the faucet auth token hash pod-template annotation.
- `internal/controller/cardanonetwork/controller_test.go` - covers initial hash stamping, valid token rotation, invalid-token repair, missing-Secret repair, and annotation removal when faucet is disabled.
- `internal/controller/cardanonetwork/controller_envtest_test.go` - proves manager-backed immediate recovery after deleting the owned faucet auth Secret.
- `internal/controller/cardanonetwork/doc.go` - updates package boundary docs for auth token generation and hashing.

## Open Threads
- Remaining `.journal/TEST_REPORT.md` findings after this session include D2, D6, F0, and F2/F4.
- Session 036 remains separately in progress and was not closed here.

## References
- PR #54: https://github.com/meigma/yacd/pull/54
- TEST report: `.journal/TEST_REPORT.md`
- Prior adversarial test session: `.journal/029/SUMMARY.md`
