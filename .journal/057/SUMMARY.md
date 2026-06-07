---
id: 057
title: CLI one-offs — all-namespaces list, self-forwarding topup, init command
date: 2026-06-03
status: complete
repos_touched: [yacd]
related_sessions: [041, 053, 055, 056]
---

## Goal
A session of small, user-driven CLI UX one-offs, all on one shared branch/PR off
master. No operator/API changes. The three landed: fix `yacd list`'s namespace
default, make `yacd topup` usable without a `yacd run` wrapper, and add a `yacd
init` that scaffolds a commented config.

## Outcome
**Met.** Three changes shipped as one squash-merged PR (**#93**, `b611645`),
CLI-only, all CI green (`ci`, `e2e`, `cardano-tools-image`, Kusari). Live-validated
end-to-end on a throwaway k3d devnet with the published operator v0.1.1 (then torn
down clean):
- `yacd init > yacd.yaml` → `yacd up` reached **Ready** with a funded wallet (the
  generated batteries-included config is usable out of the box).
- `yacd list` (no `-n`) listed the network cross-namespace.
- standalone `yacd topup NAME 5000000 --address … --await` (no `run`, no
  `--faucet-url`, no `--kupo-url`) **confirmed on-chain** — the self-forward path
  that unit tests can only mock.

## Key Decisions
- **`list` defaults to all namespaces, `-A` removed** (not: point devnet's context
  at its namespace). The one-env-per-namespace identity model makes a
  namespace-scoped default structurally empty for every env, not just devnet;
  fixing list's scope fixes the root cause. `-n` still scopes.
- **topup always self-forwards when no override; no reuse of an open `connect`
  session.** Self-forward (reusing the connect/run machinery) covers the
  "connect-open" case too and avoids state-file coupling + staleness. Honor
  explicit `--faucet-url` and ambient `YACD_FAUCET_URL` (inside `yacd run`) as
  overrides. Secret read stays after the trust gate (no-token-leak invariant).
- **topup `LOVELACE` positional; `--address` stays required** (not defaulted to the
  wallet address) — user's call. Breaking, safe pre-1.0.
- **`init` prints to stdout** (not file-writing) and emits a **batteries-included**
  local config. Embedded `init.yaml`; uncommented sections must be complete
  (UnmarshalStrict + validateExplicitFields), optional sections commented wholesale.
- **Extracted `forwardEndpoints`** from `connectNetwork` so topup and connect/run
  share one forwarding path; topup gained `resolveFaucetTransport`/
  `printTopUpResult` to stay under the gocyclo lint threshold.

## Changes (all under `cli/`, plus master-tracked docs; PR #93 `b611645`)
- `cli/internal/cli/list.go` (+`list_test.go`) — all-namespaces default, drop `-A`,
  derive empty-result message from the namespace.
- `cli/internal/cli/topup.go` (+`topup_test.go`, `topup_await_test.go`) — self-forward
  transport, positional LOVELACE, `resolveFaucetTransport`/`printTopUpResult`.
- `cli/internal/cli/forward.go` — extract `forwardEndpoints` (connect delegates).
- `cli/internal/cli/init.go` + `init.yaml` + `init_test.go` + `embed.go` + `root.go` —
  the new `init` command + embedded template + drift-guard test + registration.
- `README.md`, `docs/host-access.md` — updated `topup`/`list`/`init` examples.

## Open Threads
- **Docs follow-up on `docs/mkdocs-site` (PR #91, session 052, still open):** stale
  `yacd list -A` in `docs/reference/cli.md` + `docs/developer/{getting-started,
  networks}.md`, and `topup`-under-`run` / `--lovelace` examples need the new
  standalone form. Must be fixed before #91 merges. Master docs already updated.
- Sessions 051 and 052 remain `in-progress` in INDEX (pre-existing, not this session).
- Carried from prior: operator GitHub draft releases v0.1.0/v0.1.1 await a human
  Publish; release-please root PR #7; local-lifecycle **P7** (hardening/UX);
  deterministic primary-sidecar manager-envtest refactor; TEST_REPORT F2/F4;
  test-harness `yacd-env` Action + examples/how-to.

## References
- PR #93 (`b611645`, squash-merged) — the three changes.
- Prior: `.journal/056/SUMMARY.md` (devnet KUBECONFIG fix), `.journal/055/SUMMARY.md`
  (devnet P6). The `forwardEndpoints`/connect machinery traces to session 041.

## Lessons
- **A namespace-scoped default fights a one-namespace-per-entity model.** When each
  entity lives in its own namespace, a current-namespace-default list is almost
  always empty; align the command's scope with where things actually live rather
  than patching one flow (e.g. the context-namespace alternative).
- **Reusing existing forward machinery beat a bespoke transport.** Extracting
  `forwardEndpoints` from `connectNetwork` gave topup self-forwarding for free and
  kept one code path; the secret-read-after-trust-gate ordering preserved the
  security invariant without special-casing.
- **`gh pr merge --squash --delete-branch` still trips on the worktree layout**
  (session-055 lesson held): the remote squash-merge succeeded but the local
  branch-delete failed ("master is already used by worktree"). Verify
  `gh pr view --json state` = MERGED, then ff the main checkout and
  `git push origin --delete` the remote branch manually.
- **Mock-only paths earn a live check.** topup's self-forward (Forward/PrimaryPodName
  need a real kubelet) was unprovable in unit tests; the k3d run confirmed it before
  merge — and the `init`→`up`→`list`→`topup` smoke validated all three at once.
