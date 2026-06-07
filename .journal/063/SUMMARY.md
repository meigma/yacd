---
id: 063
title: CLI UX overhaul — design & multi-phase plan (no code)
date: 2026-06-06
status: complete
repos_touched: []
related_sessions: [049, 057, 058, 062]
---

## Goal

Plan a full UX overhaul of the `yacd` developer CLI against 8 stated goals
(consistent UX; default color/interactive + `--non-interactive`/`--quiet`;
default long-running status display; loading bars/icons; `-vvv` verbosity logging
default info; stdout=data / stderr=everything-else; `--json` everywhere sensible;
clean consistent command architecture). The user asked for a **design-only**,
multi-stream, adversarially-verified, multi-phase/multi-PR PLAN built with the
`charmbracelet`, `cli`, and `cobra-viper-cli` skills — no implementation this
session. Future sessions consume the plan and execute it.

## Outcome

**Met.** Produced four reviewed artifacts in `.journal/063/` and locked all
maintainer decisions into them. **No product code changed; `master` untouched; no
implementation branch/PR; no dev stack started.** The plan is execution-ready
starting at P0.

Method: loaded the three named skills (+ `git`/`worktrunk`), scouted the CLI
inline, then ran a dynamic Workflow (`wf_0641bf73-f55`, 46 agents, ~3.56M tokens,
~30m): **Understand** (6 parallel auditors) → **Design → Verify → Revise** (6
concerns × a 4-lens adversarial panel: cli-scriptability / charm-correctness /
cobra-viper / scope-regression) → **Critique** (completeness) → **Synthesize**
(audit + design + phased plan). The adversarial pass earned its keep: it caught
that the per-concern designs genuinely contradicted each other (the dominant find
was a FALSE claim that `-v` was free — cobra auto-binds `-v`→`--version` when
`Version` is set), plus two incompatible JSON-flag designs, three incompatible
`cli/internal/ui` package shapes, a `--quiet`-vs-logger disagreement, the
`connect` stdout-pollution mislabel, and the real, verified wallet
`--ogmios-url`/`--kupo-url` + `timeout`/`wait` `YACD_*` env-bleed. The synthesis
reconciled all of them into one authoritative contract; the maintainer then made
the 8 calls and the docs were revised to match.

## The documents we produced — and how to use them

All live in `.journal/063/`. A future implementing session should read them in
this order:

1. **`CLI_UX_PLAN.md` — START HERE. The execution playbook.** Phased,
   multi-PR. Section 6 ("Locked decisions") and the §4 phase blocks are
   authoritative. Phases: **P0** charm-free `lifecycle.Reporter.Run` widening →
   **P1** Foundation (flag table + `cli/internal/ui` package + merged
   `commandContext`/`RuntimeConfig` + verbose logging on slog + `YACD_*`
   collision fixes + CI guards) → **P2** RunE thinning + skeleton → **P3**
   routing table + additive JSON shapes → **P4** spinners + charm v2 styling +
   destructive gating. (Phase 5 / shell completion was DROPPED.) Each phase
   lists its PRs, files created/touched, new deps + the guard keeping them out of
   `./cmd`, test strategy with **named moving test assertions**, risk/rollback,
   and a Definition of Done. Also has a dependency graph and a goal→phase/PR
   matrix. **Execute PR-by-PR in order; each PR is independently shippable and
   must keep CI green.**
2. **`CLI_UX_DESIGN.md` — the authoritative contract.** The single source of
   truth for *what* to build: §2.1 the flag table, §2.2 the conflict
   resolutions, §2.4 the flag×effect interaction matrix, §3–4 the one `ui`
   package + merged context (with Go signatures), §5 the Reporter, §6 logging,
   §7 the per-command JSON table + shapes, §8 the command-architecture standard +
   routing table (§8.4) + per-command refactor checklist + Definition of Done,
   §9 the boundary guards, §10 phasing, §11 rejected alternatives. The
   "Locked decisions" block at the top OVERRIDES any conflicting prose below it.
   **Consult this whenever the plan references a contract detail.**
3. **`CLI_UX_AUDIT.md` — the current-state map.** Reference, not action: the
   command/flag inventory, the stdout/stderr-pollution findings, cobra-viper
   gaps, long-running-ops inventory, deps/module boundary, and test/determinism
   landscape — all with `file:line` evidence. **Use it to locate code and
   understand why a change is needed; re-verify any `file:line` before relying
   on it (master moves).**
4. **`CLI_UX_CRITIQUE.json` — the rationale record.** The completeness critic's
   per-goal coverage, boundary risks, gaps, and the cross-concern conflicts the
   synthesis had to resolve. **Read it to understand *why* the design made a
   given call** (e.g. why `--output`/`-o` instead of `--json`); not needed for
   day-to-day execution.

How a future session should proceed: re-fetch `master`, create an implementation
worktree, read `CLI_UX_PLAN.md` §6 + the P0 block, implement **P0** first
(byte-neutral, zero-dep), open a PR, then proceed through P1→P4 PR-by-PR, keeping
`CLI_UX_DESIGN.md` open as the contract. Bring up the dev stack
(`moon run root:dev-up`) from P1 onward. Honor the boundary guards (charm
confined to `cli/internal/ui`, kept out of `./cmd`; `run`/`exec`
byte-transparency; deterministic non-TTY tests).

## Key Decisions

- **Design-only via an adversarial Workflow, not a single pass** — the user
  asked for multiple streams + adversarial agents to keep every design inside the
  cli/charm/cobra-viper boundaries; the 4-lens panel + critic surfaced real
  inter-concern contradictions a single designer would have shipped.
- **Maintainer locked all 8 open questions** (pre-1.0 ⇒ clean cuts, no
  shims/deprecations/aliases):
  1. **Version → `yacd version` subcommand only** — drop cobra's `Version:`
     field AND the `--version` flag entirely; `-v` is always verbosity.
  2. **JSON → clean cut** — remove per-command `--json` + all `YACD_JSON`
     handling outright (no alias, no warning); `--output`/`-o` (`YACD_OUTPUT`) is
     the only surface; existing JSON byte shapes unchanged.
  3. `list` empty-state stays on stdout.
  4. Tests/Chainsaw assertions move as needed (incl. `info --json`→`info -o json`).
  5. `connect` excluded from JSON; `endpoints.json` is its machine surface.
  6. **`-q`/`--quiet` = global mute** — silences info/warn/progress/spinners AND
     forces the logger off (overrides `-v`/`--log-level`); data (incl. `-o json`)
     still prints and the final returned error reason still prints via
     `ResolveExit`/main.go (errors NOT suppressed).
  7. JSON shapes approved as designed (§7.1).
  8. **Phase 5 (shell completion) DROPPED** — plan ends at P4.
- The design+plan were revised in place to match these (decisions 1, 2, 6 and
  the P5 drop inverted/replaced the synthesis's original calls); both docs carry
  a "Locked decisions" override block and were grepped clean of stale
  `--version`/alias/`-q`-logger references.

## Changes

- No source/repo changes. Journal artifacts only (on `journal/jmgilman`):
  - `.journal/063/CLI_UX_AUDIT.md`, `CLI_UX_DESIGN.md`, `CLI_UX_PLAN.md`,
    `CLI_UX_CRITIQUE.json`, `NOTES.md`.
  - `.journal/INDEX.md` row for 063; `.journal/TECH_NOTES.md` pointer bullet.

## Open Threads

- **Execute the plan.** Future session: implement P0 → P4 PR-by-PR per
  `CLI_UX_PLAN.md`. Nothing is started yet.
- **One implementation-time confirmation (not a blocker):** the charm v2 module
  path — the skill cites `charm.land/lipgloss/v2` etc.; if the proxy serves
  `github.com/charmbracelet/lipgloss/v2`, author the PR-1.6 v2-path guard regex
  to whichever resolves (intent unchanged).
- Pre-existing carried items unaffected by this session: stale docs
  (README/DESIGN + MkDocs PR #91); ogmigo ws-1006 (issue #110); draft GitHub
  releases (v0.2.1, cardano-tools, cardano-testnet); TEST_REPORT F2/F4; the
  `yacd-env` GitHub Action.

## References

- Artifacts: `.journal/063/CLI_UX_{AUDIT,DESIGN,PLAN}.md`, `CLI_UX_CRITIQUE.json`.
- Workflow run: `wf_0641bf73-f55` (46 agents). Skills used: `charmbracelet`,
  `cli`, `cobra-viper-cli`, `git`, `worktrunk`.
- Prior: `.journal/062/SUMMARY.md` (external-access/v0.2.1), `.journal/049/`
  (devnet lifecycle design — the CLI's prior big design effort),
  `.journal/057/`/`058/` (CLI one-offs / `yacd install`).

## Lessons

- **An adversarial design panel catches contradictions a single pass ships.**
  The dominant defect — a confidently-stated but FALSE "`-v` is free" claim — was
  only exposed because a skeptic lens was told to refute and verified it against
  cobra's behavior. For design work where sub-designs are produced independently,
  a verify+critic stage before synthesis is worth its cost.
- **Synthesis ≠ final.** The synthesized design still encoded reasonable-but-
  wrong defaults (keep `--version`, `--quiet` doesn't touch the logger,
  deprecation aliases) that the maintainer overruled. Surfacing the genuine forks
  as explicit "decisions for the maintainer" (plan §6) — rather than silently
  picking — is what let those calls be made cleanly.
