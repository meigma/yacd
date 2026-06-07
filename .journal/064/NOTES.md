---
id: 064
title: CLI UX overhaul — execution (P0+)
started: 2026-06-07
---

## 2026-06-07 11:39 — Kickoff
Goal for the session: begin EXECUTING the session-063 CLI UX overhaul plan,
starting with the first phase (P0). Session 063 was design-only; this session
turns it into shipped code.

Current state of the world:
- `master` at `f2501b7` (external-access P3, #116). v0.2.1 released. No CLI UX
  overhaul code exists yet — 063 changed no product code.
- The execution-ready artifacts live in `.journal/063/`:
  - `CLI_UX_PLAN.md` — START HERE; phased multi-PR playbook (P0→P4). §6 locked
    decisions + §4 phase blocks authoritative.
  - `CLI_UX_DESIGN.md` — authoritative contract (flag table, ui package, merged
    context, Reporter, logging, JSON `--output`/`-o`, command-arch standard,
    boundary guards). "Locked decisions" block overrides conflicting prose.
  - `CLI_UX_AUDIT.md` — current-state map with file:line evidence (re-verify
    before relying; master moves).
  - `CLI_UX_CRITIQUE.json` — rationale record.
- 8 maintainer decisions locked: version→subcommand only (drop `--version`/`-v`
  shadow); `--json`→`--output`/`-o` clean cut; `-q`=global mute incl logger;
  Phase 5 (shell completion) DROPPED; pre-1.0 ⇒ clean cuts (no shims/aliases).
- P0 = charm-free `lifecycle.Reporter.Run` widening (byte-neutral, zero-dep);
  later phases bring the `cli/internal/ui` package, verbose logging, JSON
  `--output`, RunE thinning, routing table, spinners/charm v2.

Plan:
1. Prime session, then review the 063 artifacts (esp. CLI_UX_PLAN.md) in depth
   and report readiness to the user before touching code (user asked to be told
   when ready to continue).
2. On the user's go-ahead: re-fetch master, create an implementation worktree,
   `moon run root:dev-up` from P1 onward, execute P0 first PR-by-PR.
