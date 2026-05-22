---
id: 008
title: Session kickoff
started: 2026-05-21
---

## 2026-05-21 20:55 — Kickoff
Goal for the session: Start a fresh YACD journal session and wait for the actual implementation or research request.
Current state of the world: Session 007 closed after the Ogmios chain API slice landed in PR #12 and `master` was fast-forwarded to `fe8b4fd`. The journal branch `journal/jmgilman` is up to date, the main checkout is clean, and recent technical notes identify the next likely threads as documentation drift, public networks, db-sync/follower services, faucet/topup, and CLI connection/status UX.
Plan: Keep the session open, select or create an implementation worktree once the requested task is known, then start the repo dev stack if the work is implementation-oriented.

## 2026-05-21 22:50 — Phase 4 CLI architecture review
Reviewed `.journal/PLAN.md` phase 4, `DESIGN.md`, the current `CardanoNetwork` API/status contract, controller readiness behavior, Chainsaw smoke manifests, release packaging, and current manager CLI entrypoint. The phase 4 proposal should stay prototype-first: build a local developer config plus render/apply/wait/status/connection-info flow against the existing `CardanoNetwork` CR before broadening into faucet, db-sync, or final CLI packaging details.

## 2026-05-22 08:28 — CLI surface feedback
The CLI proposal was narrowed after user feedback: avoid a separate `apply` command if `render | kubectl apply` is enough, prefer one deployment command with `--dry-run` and optional `--wait`, and collapse status plus connection details into `yacd info`. Candidate phase-4 surface is now two commands: `yacd deploy` (or `create`) and `yacd info`.

## 2026-05-22 08:46 — CLI implementation kickoff
Created implementation worktree `feat/cli-foundation` at `.wt/feat-cli-foundation` and started the required dev stack with `moon run root:dev-up`. The stack is ready on Kind context `kind-yacd-dev`, with Tilt logs under `.run/yacd-dev/tilt.log`; implementation will now add the phase-4 CLI under `cli/` while keeping the manager image entrypoint on `./cmd`.

## 2026-05-22 09:07 — CLI foundation implemented
Implemented the first developer CLI under `cli/` with a Cobra/Viper root command, linker-injected version metadata, global Kubernetes/logging flags backed by `YACD_*` env vars, `deploy -f` with `--dry-run`, `--wait`, and timeout support, and `info NAME --json` backed by a command-owned DTO. The CLI uses a checked-in developer config envelope that reuses `api/v1alpha1.CardanoNetworkSpec`, renders one `CardanoNetwork`, applies it with server-side apply field owner `yacd-cli`, and polls status conditions for readiness/degraded outcomes.

Validation completed from `.wt/feat-cli-foundation`: `moon run root:test --summary minimal`, `moon run root:check --summary minimal`, `moon run root:test-e2e --summary minimal`, `git diff --check`, `go run ./cli/cmd/yacd --version`, and `goreleaser release --snapshot --clean --skip=publish`. The Chainsaw smoke now runs `go run ./cli/cmd/yacd deploy -f examples/local/yacd.yaml --namespace yacd-smoke --wait --timeout 10m`, verifies `info --json`, and still proves the Ogmios endpoint through an in-cluster JSON-RPC query.

## 2026-05-22 09:25 — CLI review fixes
Addressed review findings on `feat/cli-foundation`: `deploy --wait` now ignores stale `Ready`/`Degraded` conditions whose observed generation is older than the current `CardanoNetwork` generation; dry-run namespace rendering resolves the same kubeconfig default namespace as real deploy when no flag or config namespace is provided; and developer config loading rejects omitted CRD-defaulted concrete fields instead of rendering zero values. Added targeted unit/envtest coverage and reran `go test ./cli/...` with envtest assets, `moon run root:test --summary minimal`, `moon run root:check --summary minimal`, `moon run root:test-e2e --summary minimal`, and `git diff --check`.
