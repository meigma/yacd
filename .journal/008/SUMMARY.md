---
id: 008
title: Developer CLI foundation
date: 2026-05-22
status: complete
repos_touched: [yacd]
related_sessions: [003, 005, 006, 007]
---

## Goal
Move into phase 4 by designing and implementing the first developer-facing YACD
CLI. The CLI should stay prototype-sized, reuse the existing `CardanoNetwork`
API shape where practical, and prove the user workflow of deploying a local
environment and reading connection/status information.

## Outcome
The goal was met. PR #13 was squash-merged into `master` as `8bf1b26`, the
local default checkout was fast-forwarded, the development stack was stopped,
and the implementation worktree was removed. The repository now has a
same-module Cobra/Viper CLI under `cli/`, release packaging builds the CLI
binary from `./cli/cmd/yacd`, and the installed-operator smoke exercises the
developer workflow end to end.

## Key Decisions
- Keep the command surface to `deploy` and `info` because phase 4 is about a
  thin developer workflow, not a full kubectl replacement.
- Use `deploy` as an apply/upsert operation with `--dry-run`, `--wait`, and
  `--timeout` because it matches the current product workflow better than
  separate render/apply/wait commands.
- Keep `--dry-run` client-side for this slice so users can inspect the rendered
  manifest without requiring cluster write access.
- Reuse `api/v1alpha1.CardanoNetworkSpec` in the developer config to avoid
  duplicating the CRD schema in the first prototype.
- Reject omitted CRD-defaulted concrete fields in developer configs because
  decoding directly into the concrete API type cannot preserve unset-vs-zero
  semantics yet.
- Treat `CardanoNetwork` conditions as usable only when their
  `observedGeneration` is current, so `deploy --wait` cannot return success
  from stale status after an update.

## Changes
- `cli/cmd/yacd/main.go` - added the thin CLI entrypoint with signal-aware
  context handling and linker-injected build metadata.
- `cli/internal/cli` - added the Cobra root command, global Viper-backed
  Kubernetes/logging flags, `deploy`, `info`, injected IO streams, command DTOs,
  and command tests.
- `cli/internal/devconfig` - added the phase-4 local developer config loader for
  `yacd.meigma.io/devconfig/v1alpha1` `Environment` documents.
- `cli/internal/render` - added rendering from developer config to one
  namespaced `CardanoNetwork` plus YAML manifest output.
- `cli/internal/kube` - added Kubernetes client construction, server-side apply,
  `CardanoNetwork` fetch, default namespace resolution, and readiness polling.
- `examples/local/yacd.yaml` - added the first checked-in local environment
  sample used by docs, manual testing, and Chainsaw.
- `.goreleaser.yaml` - changed the release binary build to `./cli/cmd/yacd`
  while leaving the manager image path on `./cmd`.
- `moon.yml` and `.dev/scripts/check.sh` - included `cli/**/*.go` and example
  manifests in the maintained check/test inputs.
- `test/chainsaw/manager-smoke/chainsaw-test.yaml` - switched the installed
  smoke to deploy through `go run ./cli/cmd/yacd deploy ... --wait`, assert
  `info --json`, and keep the Ogmios protocol query proof.
- `README.md` - refreshed current-state and quickstart text for the first CLI
  workflow.

## Open Threads
- Replace the developer config's direct concrete API reuse with a dedicated CLI
  DTO if preserving unset CRD defaults becomes important.
- Add server-side dry-run later if users need API admission/defaulting feedback
  before applying.
- Public networks, faucet/topup, wallets, db-sync/follower services, and
  richer connection helpers remain later phases.
- The release snapshot still inherits the latest `cardano-testnet/...` tag when
  run locally; that is acceptable for the current smoke but may deserve release
  hygiene later.

## References
- PR #13: https://github.com/meigma/yacd/pull/13
- Merge commit: `8bf1b26` (`feat(cli): add developer environment CLI (#13)`)
- Prior session 003: `.journal/003/SUMMARY.md`
- Prior session 005: `.journal/005/SUMMARY.md`
- Prior session 006: `.journal/006/SUMMARY.md`
- Prior session 007: `.journal/007/SUMMARY.md`
