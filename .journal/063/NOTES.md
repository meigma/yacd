---
id: 063
title: New session
started: 2026-06-06
---

## 2026-06-06 15:14 — Kickoff
Goal for the session: not yet stated — session started via `session-new`,
awaiting the user's actual request.

Current state of the world:
- master is at `f2501b7` (PR #116, external-access P3 — CLI resolver). The
  3-phase external-access design is COMPLETE and released as **v0.2.1**
  (`ghcr.io/meigma/yacd:v0.2.1`, `chart:0.2.1`); GitHub release left as a draft.
- Faucet removal is COMPLETE (sessions 059/061, v0.2.0): no in-cluster faucet
  service; the controller surfaces a genesis-funded `faucet` wallet (local mode
  only) and the CLI owns all wallet management + funding via direct Ogmios/Kupo
  txns (`yacd wallet {list,add,topup,export,remove}`). The CLI installs the
  operator by appVersion tag.
- Known open threads carried from recent sessions:
  - Docs are stale: `README.md` + `DESIGN.md` + the MkDocs **PR #91**
    (`docs/mkdocs-site` branch, +13 ahead) still describe the pre-faucet-removal
    model and pre-session-057 CLI; they need a rewrite to the genesis-faucet-wallet
    + `yacd wallet` + external-access (NodePort/externalURL) model.
  - ogmigo ws-1006 non-fatal genesis-config warning during wallet funding —
    issue #110 (move off ogmigo / suppress at source).
  - GitHub releases for v0.2.1 (root), `cardano-tools`, `cardano-testnet` are
    drafts (GHCR artifacts already live) — publish if desired.
  - TEST_REPORT F2/F4; the `yacd-env` GitHub Action (test-harness Phase 4).
  - Stale `in-progress` INDEX rows 051 + 052 (052 = the docs PR #91 stream).

Plan: await the user's request, then load task-relevant skills and set up an
implementation worktree + dev stack if the work calls for it.

## 2026-06-06 17:04 — CLI UX overhaul: plan produced (design-only, no code)

Goal received: a full UX overhaul of the `yacd` CLI against 8 stated goals
(consistent UX; default color/interactive + `--non-interactive`/`--quiet`;
default long-running status display; loading bars/icons; `-vvv` verbosity
logging default info; stdout=data / stderr=everything-else; `--json` everywhere
sensible; clean consistent command architecture). User asked for a multi-stream,
adversarially-verified, multi-phase/multi-PR PLAN using the `charmbracelet`,
`cli`, and `cobra-viper-cli` skills, with the journal as scratchpad.

Approach: loaded the three skills + `git`/`worktrunk`; scouted the CLI
(`cli/internal/cli`, single root module, factory-per-command, `commandContext`
with slog logger, persistent `--kubeconfig/--context/-n/--log-level/--log-format`,
per-command `--json` only on list/info/wallet, progress only via `stepReporter`
→ stderr, no TTY detection/color/spinners/quiet/non-interactive/-v). Then ran a
dynamic Workflow (`wf_0641bf73-f55`, 46 agents, ~3.56M tok, ~30m): Understand
(6 parallel auditors) → Design→Verify→Revise (6 concerns × 4-lens adversarial
panel: cli-scriptability / charm-correctness / cobra-viper / scope-regression)
→ completeness Critic → Synthesize (audit + design + phased plan).

Adversarial value: the panel + critic caught that the per-concern designs
genuinely contradicted each other — dominant find was a FALSE claim that `-v`
was free (cobra auto-binds `-v`→`--version` when `Version` is set, so `yacd -v`
prints version today). Also: two incompatible JSON-flag designs, three
incompatible `cli/internal/ui` package shapes, `--quiet`-vs-logger disagreement,
the `connect` stdout-pollution mislabel, and the wallet `--ogmios-url`/`--kupo-url`
+ `timeout`/`wait` `YACD_*` env-bleed (real, verified). The synthesis reconciled
ALL of them into one authoritative contract.

Artifacts written to `.journal/063/`:
- `CLI_UX_AUDIT.md` — current-state findings (command/flag inventory, stdout/stderr
  violations, cobra-viper gaps, long-running ops, deps/module boundary, tests).
- `CLI_UX_DESIGN.md` — the single authoritative design: §2.1 flag table, §2.2 the
  three conflict resolutions (reassign `-v` from `--version` w/ test+CHANGELOG;
  `--output`/`-o` not `--json`; `--quiet` doesn't touch the logger), §2.4
  interaction matrix, §3–4 one `ui` pkg + merged `commandContext`/`RuntimeConfig`,
  §5 Reporter widening, §6 logging, §7 JSON per-command table, §8 command-arch
  standard + routing table §8.4 + DoD, §9 boundary guards, §10 phasing, §11
  rejected alternatives.
- `CLI_UX_PLAN.md` — phased multi-PR plan: P0 Reporter widening → P1 Foundation
  (flags+ui+merged ctx+verbose logging+collision fixes+CI guards) → P2 RunE
  thinning → P3 routing + additive JSON → P4 spinners+charm styling+destructive
  gating → P5 (optional) completion. Goal→phase matrix, dependency graph,
  per-PR files/tests/risks/DoD, named moving test assertions, and §6 the 8
  maintainer decisions.
- `CLI_UX_CRITIQUE.json` — completeness critic output (per-goal coverage,
  boundary risks, gaps, conflicts) — the rationale record for the design's calls.

Boundaries enforced throughout: charm deps confined to `cli/internal/ui`, kept
out of `./cmd` via a `go list -deps` guard; `run`/`exec` byte-transparency +
`exitError` passthrough; stable `list`/`info` JSON shapes; `YACD_*` host-access
env contract not shadowed/leaked; deterministic tests (non-TTY buffers, no ESC
bytes); no Bubble Tea full-screen TUI.

Next: get maintainer decisions on `CLI_UX_PLAN.md` §6 (esp. the breaking `-v`
reassignment and `--json`→`--output` deprecation), then start implementation at
P0/P1 in an implementation worktree (dev stack not needed for plan; will be for
implementation). No code changed yet; master untouched.
