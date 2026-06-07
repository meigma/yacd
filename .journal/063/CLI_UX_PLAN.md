# yacd CLI UX Plan

## 0. Orientation

This plan turns the **yacd CLI UX Design** into a sequence of independently shippable phases and PRs. Every phase keeps CI green, holds the verified repo invariants, and touches the smallest possible surface. The work lives entirely under `cli/internal/cli/` (package `cli`) and the new `cli/internal/ui/` package; the manager (`./cmd`) and `cli/internal/lifecycle` stay charm-free.

**Verified ground truth (read before trusting any claim below):**
- CLI package is `cli`, rooted at `cli/internal/cli/`; root construction in `cli/internal/cli/root.go::NewRootCommand`; streams injected via `Options{In,Out,Err}`.
- `loadRuntimeConfig(vp)` is called in **11** places: `root.go:111` (the keystone) plus 10 re-calls in `connect.go:50`, `down.go:21`, `exec.go:69`, `info.go:22`, `install.go:52`, `list.go:26`, `run.go:39`, `target.go:70`, `up.go:28`, `wallet.go:79`.
- No `-v` shorthand is registered, so Cobra auto-binds `-v`→`--version` (root has `Version:` set at `root.go:104`). `root_test.go` only tests `--version` (long form) today.
- `--json` is a per-command `cmd.Flags().Bool("json", ...)` bound to viper key `json` in `list.go:65` / `info.go:62` (+ wallet); under `SetEnvPrefix("YACD")+AutomaticEnv()` this exposes `YACD_JSON` everywhere and leaks into `run.go:109` `child.Env = append(os.Environ(), connected.env...)`.
- `connect.go:112` writes the `KEY=URL` block **and** banners to `commandContext.out` (stdout) via one `infoWriter`; `connect.go:92/123/130` already write retry/disconnect banners to `commandContext.err`.
- `exec.go:161` defines a local `isTerminalWriter`; `wallet_fund.go:193` swaps `os.Stdout` to `os.Stderr` around funding submit.
- JSON encoding is `json.MarshalIndent(items,"","  ")` (`list.go:51`), matching `ui.IO.Encode`'s frozen contract.
- `lifecycle.Reporter` (`lifecycle.go:124`) has `Step/Substep/Done` only; `NopReporter` implements those three; no `Run`. No `cli/internal/ui` package exists; no charm/lipgloss/huh/colorprofile in `go.mod`; no `go list -deps ./cmd` guard exists.
- Env contract keys live in `envcontract.go` (`YACD_NETWORK`, `YACD_NAMESPACE`, `YACD_NETWORK_MAGIC`, `YACD_OGMIOS_URL`, `YACD_KUPO_URL`, faucet token).

The design's phasing (its section 10) is correct and I adopt it with one renumbering: the design's "Phase 0" (charm-free Reporter widening) becomes **P0** here because it is the only genuinely independent, byte-neutral, zero-dep change and it unblocks both the reporter selection in the foundation and the spinner work later. The remainder maps to the prompt's suggested order with two deliberate deviations, justified in §1.

---

## 1. Phase sequence and why this order

The prompt's suggested order is `(P0) IO/UX core + flags + determinism + import-guard; (P1) logging; (P2) stdout/stderr routing; (P3) status/spinners; (P4) JSON; (P5) architecture sweep`. The design implies a **better** order, and I adopt it with justification:

1. **Split the prompt's P0 into P0 (Reporter widening) + P1 (foundation).** The foundation is large and must reconcile three contradictory concern-specs into one flag table, one `ui` package, one merged `commandContext`. Landing the charm-free `Reporter.Run` widening **first** (no flags, no deps, byte-neutral) shrinks the foundation diff and lets the foundation's `Reporter()` selector return real types. This is the design's Phase 0 and is uncontroversially independent.

2. **Fold logging INTO the foundation, not after it (deviation from prompt's P1).** Verbosity is not separable from the flag table: `-v` ownership requires moving version to a `yacd version` subcommand (dropping cobra's `Version:`/`--version`), `effectiveLevel = raise(resolve(log-level), verbose-count)` is computed in `loadRuntimeConfig`, and the logger is built in the one `PersistentPreRunE`. Shipping logging as a later phase would mean wiring `-v`/`raise()` twice. The design (§6, §10) puts slog-based verbose logging in the foundation; I follow it. Charm `log/v2` styling is deferred to the styling phase.

3. **Architecture sweep (RunE thinning) comes BEFORE routing/JSON (deviation from prompt's P5-last).** The design (§8, §10) makes RunE-thinning Phase 2, before routing (Phase 3). Reason: routing and JSON edits land in `runX` orchestrators; doing the thinning first means the routing/JSON phases touch one clean seam per command instead of editing fat `RunE` closures twice. The prompt's "consistency sweep last" would force routing edits into un-refactored closures, then re-touch them. The design's order is lower-blast-radius. The "sweep" is therefore split: the **structural** sweep (thin RunE, shared registrars, validators, collision fixes already done in foundation) is P2; the **routing/output** sweep is P3.

4. **Routing + additive JSON ship together (merges prompt's P2 and P4).** The design (§10 Phase 3) couples them because both edit the same `runX` output statements and both depend on the `emit`/`Data`/`Encode` seam. The deferred JSON shapes (`devnet up/down`, `wallet remove/export`) can only land once their banners move off stdout, so JSON-shape work is gated on routing in the same phase. Splitting them would mean two passes over identical lines.

5. **Spinners + styling + destructive gating last (prompt's P3, but after routing/JSON).** This is the first PR to add `lipgloss`/`huh`/`log/v2` to the import graph. Deferring it keeps "the first charm PR" coherent and means the import-guard regex (shipped in foundation) is already proven green on a charm-free graph before charm enters. Spinners need the routing already correct (success lines via `Done` after `Run`, stderr-only) — so they come after P3.

**Resulting order:** P0 Reporter widening → P1 Foundation (flags + ui package + merged context + verbose logging on slog + collision fixes + all CI guards) → P2 RunE thinning + skeleton → P3 Routing + additive JSON → P4 Spinners + charm styling + destructive gating. Logging-as-a-distinct-phase and a separate stdout-routing-cleanup phase from the prompt are **absorbed** (logging into P1, routing into P3) for the reasons above. (The prompt's Phase 5 / shell completion is **DROPPED** per maintainer decision — the plan ends at P4.)

---

## 2. Dependency graph

```
P0 (Reporter.Run widening, charm-free, byte-neutral)
 │   unblocks: real Reporter selection + spinner backing
 ▼
P1 (Foundation: flag table, ui pkg, merged commandContext/RuntimeConfig,
     resolveUX/raise, verbose logging on slog, YACD_* collision fixes,
     CI guards: import-graph + v2-path + stdout-purity)
 │   hard prerequisite for everything below
 ├──────────────► P2 (RunE thinning + shared registrars + validators + help)
 │                 │
 │                 ▼
 └──────────────► P3 (Routing table applied + additive JSON shapes via emit;
                       deferred JSON shapes land here after their text moves)
                   │   needs P2's runX seams to be clean (lower blast radius);
                   │   strictly needs P1's ui.IO/emit/Data/Encode
                   ▼
                  P4 (richReporter huh spinner + charm/log v2 swap +
                      lipgloss palette + huh confirm + flagYes destructive gate)
                   │   needs P3 routing correct (Done-after-Run, stderr-only)
                   ▼
                  (P5 / shell completion DROPPED — not a goal, out of scope)
```

Edges that are HARD (compile/contract): P1→{P2,P3,P4}, P3→P4 (spinner success-line contract), P2→P3 (soft: cleanliness/blast-radius, not compile). P0 is a leaf prerequisite with no upstream.

---

## 3. Goal → Phase/PR coverage matrix

| # | Goal | Phase(s) | PR(s) |
|---|---|---|---|
| 1 | Consistent UX across commands/subcommands/flags | P1 (single flag table), P2 (skeleton), P3 (routing) | PR-1.2, PR-1.3, PR-2.1, PR-2.2, PR-3.1 |
| 2 | Default color/interactive; `--non-interactive`/`--quiet` disable rich output | P1 (flags + `resolveUX` + `ui.IO` color model), P4 (animation gating) | PR-1.1, PR-1.4, PR-4.1 |
| 3 | Default status display on long-running commands | P0 (Reporter.Run), P3 (Done-after-Run lines), P4 (rich) | PR-0.1, PR-3.2, PR-4.2 |
| 4 | Loading bars/icons for long-running commands | P4 (huh spinner, indeterminate-only) | PR-4.2 |
| 5 | Verbose logging debug/info/warn/error with `-vvv`, default info | P1 (`version` subcommand, `-v` always verbose, `raise()`, slog logger), P4 (charm/log styling swap) | PR-1.2, PR-1.5, PR-4.3 |
| 6 | Non-copyable output to stderr; only script-useful to stdout | P1 (`ui.IO` data/human split + stdout-purity guard), P3 (routing table) | PR-1.1, PR-1.6, PR-3.1 |
| 7 | JSON output where it makes sense | P1 (`--output`/`-o` + `Encode`; `--json`/`YACD_JSON` removed), P3 (additive shapes) | PR-1.3, PR-3.3 |
| 8 | Clean, consistent code/architecture | P0, P1 (merged context, dedupe), P2 (runX skeleton), all guards | PR-0.1, PR-1.4, PR-2.1, PR-2.2 |

---

## 4. Phases

### P0 — Charm-free Reporter widening

**Goals addressed:** 3 (status display foundation), 8 (clean shared interface).

**PRs:**
- **PR-0.1** — Widen `lifecycle.Reporter` with `Run(ctx, title, action) error`; implement on `NopReporter`, the existing `stepReporter`, and test doubles; change `runInstall`'s reporter parameter type to `lifecycle.Reporter`.

**Files touched:**
- `cli/internal/lifecycle/lifecycle.go` (add `Run` to the `Reporter` interface at line 124; add `NopReporter.Run` calling the action).
- `cli/internal/lifecycle/manager.go` (any internal Reporter use stays compatible; `Manager.Report` unchanged).
- `cli/internal/lifecycle/manager_test.go` + any test double implementing `Reporter` (add `Run`).
- `cli/internal/cli/install.go` (`runInstall` parameter → `lifecycle.Reporter`) and the promoted `stepReporter` location (currently the command layer's plain reporter) gains `Run`.

**New deps:** none. **Guard:** N/A (no new imports). The `Run` signature is deliberately `(context.Context, string, func(context.Context) error)` so the interface stays in the charm-free `lifecycle` package.

**Test strategy:** Pure compile + behavior-preservation. `NopReporter.Run` must call the action and return its error (unit test). No golden/substring assertions move; output is byte-identical (P0 emits no completion line — that is added in P3). Existing `install_test.go`/`up_test.go`/`down_test.go` unchanged.

**Determinism:** unaffected; no IO change.

**Risk/rollback:** Minimal. Risk = a test double missing `Run` fails compile (caught in CI). Rollback = revert single PR; nothing depends on it yet except future phases.

**DoD:** `lifecycle.Reporter` has `Run`; all implementers compile; `runInstall` takes `lifecycle.Reporter`; `moon run root:test` green; `git diff --check` clean; zero behavior/output change verified by unchanged golden tests.

---

### P1 — Foundation (flags + ui package + merged context + verbose logging + collision fixes + guards)

This is the mandatory reconciliation pass (design §1 single-ownership, §2 flag table, §3–4 ui+context, §6 logging, §8.3 collisions, §9 guards). It is the largest phase; PRs are sequenced so each is independently green.

**Goals addressed:** 1, 2, 5, 6 (the data/human split + purity guard), 7 (the flag surface), 8.

**Contract changes (user-visible / test-visible) — called out explicitly:**
- **Version becomes a `yacd version` subcommand; cobra's `Version:` field and the `--version` flag are removed.** `-v`/`--verbose` is verbosity, always (additive count). `root_test.go` guards `yacd version` (stdout, exit 0) and `-v` (verbosity); asserts no `--version` flag exists.
- **`--json` and `YACD_JSON` are removed outright (pre-1.0 clean cut — no alias, no warning).** `--output`/`-o` (`text|json`, env `YACD_OUTPUT`) is the only surface; existing JSON byte shapes are unchanged (only the toggle moves).
- **`-q`/`--quiet` is a global mute** — suppresses info/warn/progress/spinners and forces the logger off (overrides `-v`/`--log-level`); data (incl. `-o json`) and the final returned error reason still print.
- **`YACD_OGMIOS_URL`/`YACD_KUPO_URL` no longer shadow wallet `--ogmios-url`/`--kupo-url` flags;** `YACD_TIMEOUT`/`YACD_WAIT` no longer bleed across commands. (Bug fix; new tests assert non-shadowing.)

**PRs (in landing order):**

- **PR-1.1 — `cli/internal/ui` package skeleton + color model (charm-free interim).**
  - Creates `cli/internal/ui/{config.go,io.go,data.go,message.go,log.go}` with `Config`, `ConfigFromRuntime`, `ui.New`, `ui.IsTerminal`, `IO.{Data,Encode,Info,Status,Warn,Error,Success,Detail,JSON,Quiet,Color,Interactive,NewSlogLogger}`.
  - Color decided by a single `color bool`; the human plane writes plain when `!color` (no `0x1b`). **Interim:** wrap stderr via a thin local writer; do NOT import `colorprofile` yet (no charm in graph until P4). `NewSlogLogger` returns stdlib slog handlers.
  - Files: new `cli/internal/ui/*`. New deps: **none** (charm deferred). Guard: the import-graph guard test ships in PR-1.6, but PR-1.1 keeps `ui` charm-free so the graph stays clean.
  - Tests: `ui` unit tests — `Encode` byte-matches `json.MarshalIndent(v,"","  ")+"\n"` with `SetEscapeHTML(true)`; no-ESC-byte assertion on every human helper into a `bytes.Buffer`; `Data()` is stdout, helpers are stderr; `Interactive()` false for non-TTY streams.

- **PR-1.2 — `version` subcommand (drop cobra `Version:`/`--version`; `-v` claims verbosity) + `--verbose`/`--quiet`/`--non-interactive`/`--color`/`--no-color` persistent flags + `raise()` + verbose logging on slog.**
  - `root.go`: do NOT set cobra's `Version:` field and do NOT register a `--version` flag (so `-v` is free); add a `yacd version` subcommand (prints the version template to stdout, exit 0); add persistent `--verbose -v` (count), `--quiet -q` (bool), `--non-interactive` (bool), `--color` (`auto|always|never`), `--no-color` (bool). `config.go`: `raise(base, count)`, `resolveUX(rc, errOut, in)`.
  - `loadRuntimeConfig` signature changes to `loadRuntimeConfig(cmd, vp)` (needs `cmd.Flags().Changed/Count`); `LogLevel = raise(resolve(log-level), verboseCount)`.
  - Files: `cli/internal/cli/root.go`, new `cli/internal/cli/version.go`, `cli/internal/cli/config.go`, `cli/internal/cli/config_test.go`, `cli/internal/cli/root_test.go`.
  - New deps: none. Tests: **new** `root_test.go` cases — `yacd version`→stdout+exit0; no `--version` flag exists (asserts unknown-flag error); `-v` increments verbosity (and bare-root `-v` → help); `raise()` table incl. env-base rows (`YACD_LOG_LEVEL=warn yacd up -v` → debug; `-vvv` → debug; never lowers); `YACD_LOG_LEVEL` without `-v` respected. `-q` global mute: assert NO debug/info/warn line emits under `-q` even with `-vvv`, and that a returned error reason still prints under `-q`.

- **PR-1.3 — `--output`/`-o` enum + clean removal of `--json`/`YACD_JSON` + `Encode` adoption.**
  - Add persistent `--output -o` (`text|json`, bound to viper key `output`/`YACD_OUTPUT`, **not** `SetDefault` so `Changed` stays meaningful). **Remove** the per-command `--json` flags from `list.go:65`/`info.go:62`/wallet and all `YACD_JSON` handling outright (no alias, no warning); route their JSON branch through `cc.io.JSON()`.
  - Files: `cli/internal/cli/root.go`, `config.go`, `list.go`, `info.go`, `wallet.go`, new `output_test.go`.
  - Tests: `list -o json` produces the frozen `[]listItem` byte-identical to today's `--json` output; `info -o json` the frozen `infoOutput`; `YACD_OUTPUT` precedence; `--json` is no longer registered (asserts unknown-flag error). **Moving assertion:** existing `list_test.go`/`info_test.go` tests that invoke `--json` are rewritten to `-o json` (same bytes). Chainsaw's `info --json` step moves to `info -o json`.

- **PR-1.4 — Merge `commandContext`/`RuntimeConfig`; single `PersistentPreRunE` resolution; delete the 10 re-calls.**
  - `options.go`: add `runtimeConfig RuntimeConfig`, `io ui.IO`, `outputExplicit bool` to `commandContext` (keep `logger *slog.Logger`). `config.go`: extend `RuntimeConfig` with `Verbosity,Quiet,NonInteractive,Color (ui.ColorMode),OutputFormat`.
  - `root.go` `PersistentPreRunE`: resolve once → `ctx.runtimeConfig`, `ctx.io = ui.New(ui.ConfigFromRuntime(...))`, `ctx.logger`, `ctx.outputExplicit`. Built from the **raw injected writers captured in `NewRootCommand`** (before any `os.Stdout` swap).
  - Delete `loadRuntimeConfig(...)` re-calls in `connect.go:50`, `down.go:21`, `exec.go:69`, `info.go:22`, `install.go:52`, `list.go:26`, `run.go:39`, `target.go:70`, `up.go:28`, `wallet.go:79`; each reads `cc.runtimeConfig`. `root.go:111` stays the one caller (now `loadRuntimeConfig(cmd, vp)`).
  - Files: `options.go`, `config.go`, `root.go`, and all 10 re-call sites + their tests.
  - Tests: existing per-command tests stay green (same resolved values); add a test that resolution happens exactly once (e.g., via a viper read counter or behavior parity).

- **PR-1.5 — YACD_* collision + cross-command bleed fixes.**
  - `wallet.go`: rename `chainOverridesFromViper`→`chainOverridesFromFlags`; read `--ogmios-url`/`--kupo-url` via `cmd.Flags().GetString` (not shared viper) so `YACD_OGMIOS_URL`/`YACD_KUPO_URL` no longer shadow them.
  - `up.go`,`down.go`,`install.go`,`devnet.go`: read `timeout`/`wait` via `cmd.Flags().GetDuration/GetBool` (not `commandContext.viper`), killing `YACD_TIMEOUT`/`YACD_WAIT` cross-command bleed. `install`'s `--values/--set/--set-string` keep `cmd.Flags().GetStringArray` (documented exception).
  - Files: `wallet.go`, `up.go`, `down.go`, `install.go`, `devnet.go`, `wallet_test.go`, plus new bleed tests.
  - Tests: **new** — `YACD_OGMIOS_URL`/`YACD_KUPO_URL` set in env do NOT become wallet flag defaults; `YACD_TIMEOUT` set does NOT change `up`/`down`/`install` timeout; child-env guard asserts `connected.env` (the CLI-injected set in `run.go:109`) excludes `YACD_OUTPUT`/`YACD_VERBOSE`/etc. even when exported in the test process.

- **PR-1.6 — CI guards (import-graph + v2-path + stdout-purity).**
  - New `cli/internal/ui/guard_test.go::TestManagerImportGraphHasNoCharm`: `go list -deps ./cmd` must not match `charm|lipgloss|huh|bubble|ultraviolet|colorprofile|charmbracelet|ogmigo|kugo|internal/cardano/tx`.
  - New v2-path grep test: no `github.com/charmbracelet/(lipgloss|huh|log)` (v0/v1) import under `cli/`.
  - New stdout-purity grep test: no raw `fmt.Fprintf(cc.out/cc.err)`, `os.Std*`, `fmt.Print*`, `lipgloss.Print*` outside `cli/internal/ui`, except lines tagged `//ui-passthrough-ok`.
  - Files: new `cli/internal/ui/guard_test.go`, new `cli/internal/cli/purity_test.go`. New deps: none.
  - Note: ships in P1 (before any charm). The purity guard initially allows existing raw writes via `//ui-passthrough-ok` pragmas where they still exist; P3 removes most pragmas. To keep P1 green, P1 tags the not-yet-routed raw writes (`connect.go`, `install.go`, `devnet.go`, run/exec passthrough, `wallet_fund` swap) with `//ui-passthrough-ok`; P3 removes the pragmas it routes.

**New deps added in P1:** none (charm deferred to P4). **Guard keeping deps out of `./cmd`:** PR-1.6 import-graph test (the structural enforcement) — it is green on a charm-free graph in P1 and stays the tripwire when P4 adds charm.

**Test strategy / determinism harness:** All tests use `NewRootCommand(Options{In,Out:&bytes.Buffer{},Err:&bytes.Buffer{}, Viper: viper.New()})`. Buffers are non-TTY → `resolveUX` yields `color=false`, `nonInteractive=true` → plain, deterministic output. The `ui` no-ESC-byte test is the determinism backstop. **Assertions that move in P1:** none of the existing golden/substring assertions move (P1 is flag/plumbing/guard work, byte-neutral on stdout for existing commands); the collision/bleed tests are **net-new** stderr assertions.

**Risk/rollback:** Highest-blast phase. Top risk = removing the `--version` flag and claiming `-v` for verbosity (user-visible). Mitigated by explicit `root_test.go` guards + CHANGELOG. Second risk = `loadRuntimeConfig` signature change rippling through 10 sites — mitigated by landing PR-1.4 atomically with all call-site edits. Rollback = each PR is independently revertible; PR-1.2 (the breaking `-v` change) can be held while the rest lands if needed.

**DoD:** single flag table live; one `ui` package, one `Config`, one `New`, one merged `commandContext`/`RuntimeConfig`; resolution happens once in `PersistentPreRunE`; `yacd version` subcommand live (no `--version` flag), `-v` always verbose, tested; `--output`/`-o` live with `--json`/`YACD_JSON` removed; `-q` global-mute (logger off, error reason preserved) tested; wallet URL + timeout/wait collisions fixed+tested; child-env guard green; import-graph/v2-path/stdout-purity guards green and charm-free; `moon run root:test` + `root:test-e2e` green; Chainsaw migrated to `info -o json` (byte-clean).

---

### P2 — RunE thinning + skeleton

**Goals addressed:** 1, 8.

**PRs:**
- **PR-2.1 — Shared registrars + validators + help.** Add `flagWait`, `flagDryRun`, `flagFile`(`-f`), `flagAwait`, `flagYes` to `flags.go`; `requireNameAndCommand` for `exec` to `identity.go`; package-level `Long`/`Example` consts where they add value. No leaf `PersistentPreRunE`; local validation in `PreRunE`. Files: new `cli/internal/cli/flags.go`, `identity.go`, per-command constructors.
- **PR-2.2 — Extract `runX` orchestrators.** For each leaf, constructor becomes declarative; `RunE` ≤8 lines calling one `runX(ctx, cc, params)` with an `xParams` struct; required env/config inputs (e.g. `--file`/`YACD_FILE`) validated on the resolved value, never `MarkFlagRequired`. Files: `up.go`, `down.go`, `list.go`, `info.go`, `install.go`, `connect.go`, `run.go`, `exec.go`, `wallet*.go`, `devnet.go`, `init.go` + their tests.

**Files created:** `cli/internal/cli/flags.go`. **Files touched:** every command file + `identity.go`.

**New deps:** none. **Guard:** unchanged P1 guards stay green.

**Test strategy / determinism:** Byte-neutral except one named change. **Moving assertion:** `exec_test.go:79` is reworded to the new `requireNameAndCommand` error message (named). All other golden/substring assertions unchanged. Determinism harness identical to P1 (buffers → plain). Add `runX` unit tests where extraction created a newly testable seam.

**Risk/rollback:** Low-medium; mechanical refactor. Risk = behavioral drift in a fat closure during extraction — mitigated by keeping existing per-command tests as the regression net and refactoring one command per commit. Rollback = revert per-command commits.

**DoD:** every leaf passes the §8.7 checklist items achievable without routing (declarative constructor, ≤8-line `RunE`, no `loadRuntimeConfig` in `RunE`, args validators, no leaf `PersistentPreRunE`, command-local flags read via `cmd.Flags()`); `exec` arity error reworded+tested; CI green; no new raw stream writes introduced (purity guard green).

---

### P3 — Routing table applied + additive JSON shapes

**Goals addressed:** 6 (the routing moves), 7 (additive shapes), 3 (Done-after-Run success lines).

**Contract changes (test-visible) — the single reconciled routing table (design §8.4) is applied. Named assertion moves below.**

**PRs:**
- **PR-3.1 — Apply routing table; route everything through `cc.io`.** Stdout keeps only script-useful data: `up` dry-run manifest → `Data`; `list`/`info` table+JSON → `Data`/`emit`; `list` empty-state "No CardanoNetworks found." **stays stdout** (it is the text result); `install` dry-run "Plan: …" → stdout; `connect` `KEY=URL` pairs → stdout. Everything else → stderr via `Status/Info/Warn/Success/Detail`: `install` success line, `devnet` banner/endpoints/"Try:", `devnet down`/`status` text, `connect` banners ("Forwarding…","Wrote… Ctrl-C"), `wallet remove` "Removed wallet…". `run`/`exec` child stdio stays passthrough (`//ui-passthrough-ok`); `wallet_fund` swap narrowed to bracket only `submitter.Submit`. Remove `exec.go`'s local `isTerminalWriter` in favor of `ui.IsTerminal`. Split `connect`'s single `infoWriter` (`connect.go:112`) into stdout `KEY=URL` + stderr banners. Files: `install.go`, `devnet.go`, `connect.go`, `wallet.go`, `wallet_fund.go`, `exec.go`, `up.go`, `list.go`, `info_print.go`.
- **PR-3.2 — Wrap long-running waits in `Reporter().Run(...)` + Done-after-Run success lines.** Command layer wraps `kube.WaitReady`/`WaitGone`/`operator.WaitForReady`/`awaitConfirmation` in `report.Run`, then calls `report.Done(<verbatim line>)` after. Reporter is `nopReporter` under buffers (non-TTY) so output stays plain/deterministic. Files: `up.go`, `down.go`, `install.go`, `wallet_fund.go`, `manager.go`, `cli/internal/ui/reporter.go` (plain/nop selection only; rich deferred to P4).
- **PR-3.3 — Additive JSON shapes via `emit`.** Add `devnetStatusOutput`, `upResult`, `actionResult`, `installResult` (frozen on introduction) wired through `commandContext.emit`. Deferred shapes (`devnet up/down`, `wallet remove/export`) land here since their text moved to stderr in PR-3.1. Excluded verbs (`run`/`exec`/`init`/`connect`/`up --dry-run`) fail fast on explicit `-o json` via shared `rejectExplicitJSON(verb)` reading `cc.outputExplicit && cc.io.JSON()`; ambient `YACD_OUTPUT=json` silently ignored. Files: `up.go`, `down.go`, `install.go`, `devnet.go`, `wallet*.go`, new `emit.go`.

**Files created:** `cli/internal/ui/reporter.go` (plain/nop), `cli/internal/cli/emit.go`. **New deps:** none.

**Test strategy / determinism — named moving assertions (design §9):**
- → **stderr** (MOVED): `install_test.go:159-162,183-186,259-260`; `devnet_test.go:104,128,142,159,172,186`; `devnet_live_test.go:48,49,67,73`; `connect_test.go` banner lines; `wallet_test.go` remove line.
- **stay stdout** (unchanged): `install_test.go:285-289,306-309` (dry-run Plan); `list_test.go:134,152` (empty-state); `up_test.go:217,250` ("…is ready."); `down_test.go:38,62` ("…is gone."); `connect_test.go` `KEY=URL` pairs.
- `up_test.go:276`/`down_test.go:102` `NotContains` guards hold because `Done` is inside the wait branch.
- **New tests:** frozen byte-stability for `upResult`/`actionResult`/`installResult`/`devnetStatusOutput`; `rejectExplicitJSON` on each excluded verb; ambient `YACD_OUTPUT=json` ignored on excluded verbs.
- Determinism: buffers→non-TTY→plain reporter; `plainReporter` keeps `"==> "`/`"    "` so Chainsaw phrase asserts hold.

**Chainsaw:** `info --json` (chainsaw `info --json` parse) stays byte-clean (frozen `infoOutput` untouched). If any Chainsaw step asserts an `install`/`devnet` line on stdout, it moves to the stderr assertion in the same PR — **call out** to the maintainer (open question Q4).

**Risk/rollback:** Medium. Top risk = a downstream script/Chainsaw step parsing a now-moved banner from stdout. Mitigated by the named-assertion audit and the `connect`/`list` reconciliation already decided in the design. Rollback = revert per-command routing commits; JSON shapes are additive so removing them is safe.

**DoD:** routing table fully applied; stdout carries only data + the kept text results; all named assertions moved/verified; new JSON shapes frozen + byte-stable-tested; excluded verbs reject explicit `-o json`; purity guard green with pragmas removed where routed; `run`/`exec` byte-transparency preserved; Chainsaw green.

---

### P4 — Spinners + charm styling + destructive gating

**Goals addressed:** 4 (spinners), 2 (animation gating), 5 (charm/log styling), 3 (rich status), plus CLI-skill-mandated destructive gating for `down`/`devnet down`.

**This is the first PR to add charm to the import graph.** The P1 import-graph guard now proves charm is confined to `cli/internal/ui`.

**PRs:**
- **PR-4.1 — Add charm v2 deps + adopt `colorprofile` writer in `ui`.** Add `charm.land/lipgloss/v2`, `charm.land/colorprofile`, `charm.land/huh/v2`, `charm.land/log/v2` to root `go.mod`. Swap `ui.io.go`'s interim writer for `colorprofile.Writer` forced to `NoTTY` when color off; add `palette.go` (plain lipgloss value styles). Files: `go.mod`, `go.sum`, `cli/internal/ui/io.go`, new `cli/internal/ui/palette.go`. **Guard:** import-graph test must stay green (`go list -deps ./cmd` charm-free); v2-path grep enforces `charm.land/*` (not `github.com/charmbracelet/*`).
- **PR-4.2 — `richReporter` huh spinner.** `cli/internal/ui/reporter.go` gains `richReporter` (predicate `animate = errIsTTY && interactive && !quiet && !json && color`) using `huh/v2/spinner` with `WithOutput(stderr)`, `Context(ctx)`, accessible-mode, no alt-screen. `Reporter()` selects rich/plain/nop. Files: `cli/internal/ui/reporter.go`, `config.go` (`Accessible`).
- **PR-4.3 — charm/log v2 swap (text path).** `ui/log.go` `NewSlogLogger` text path → `charm.land/log/v2` (TTY-gated styling) behind the unchanged signature; JSON path stays stdlib JSON for log-shape stability. Files: `cli/internal/ui/log.go`.
- **PR-4.4 — huh confirm + `flagYes` destructive gating.** `down` and `devnet down` gate via `flagYes`: TTY+no-`--yes` → huh confirm on stderr behind `cc.io.Interactive()` (accessible-mode); non-TTY+no-`--yes` → refuse with actionable error + non-zero exit (never hang); `--yes`/`--force` bypass. `up`/`devnet`/`install` ungated. Files: `down.go`, `devnet.go`, new `cli/internal/ui/confirm.go`.

**Files created:** `cli/internal/ui/palette.go`, `cli/internal/ui/confirm.go`. **New deps:** `charm.land/lipgloss/v2`, `charm.land/colorprofile`, `charm.land/huh/v2`, `charm.land/log/v2`. **Guard keeping them out of `./cmd`:** the P1 import-graph test — this phase is exactly when it earns its keep.

**Test strategy / determinism:** Buffers→non-TTY→`animate=false`→nop/plain reporter→no spinner→no ESC bytes; the no-ESC-byte `ui` test now also covers the charm-backed path under buffers. **New tests:** destructive TTY-refusal (non-TTY, no `--yes` → error + non-zero exit, stdout empty) and `--yes`-bypass for `down`/`devnet down`; `--color`×`--no-color`×`NO_COLOR` precedence through the `colorprofile` writer (assert plain under each); JSON-mode run with a TTY still emits no stdout styling and uses plain/nop reporter. No existing golden assertion moves (animation is stderr-only and off under tests).

**Risk/rollback:** Medium. Top risk = charm leaking into `./cmd` (caught by import-graph guard) or a v0/v1 path slipping in (caught by v2-path grep). Second risk = destructive gate hanging CI — explicitly designed out (non-TTY refuses, never prompts) and tested. Rollback = revert PR-4.x; `Reporter()` falls back to plain/nop, logger falls back to slog (PR-4.3 revert), confirm reverts to no-gate (note: reverting the gate re-enables un-confirmed destruction, so revert PR-4.4 only by re-adding `--yes`-or-refuse).

**DoD:** charm v2 confined to `cli/internal/ui`, import-graph + v2-path guards green; spinners animate only on interactive TTY non-json non-quiet color; `NO_COLOR`/`--color=never`/non-TTY → zero cursor-control escapes; charm/log text styling on TTY, JSON log shape stable; `down`/`devnet down` confirm on TTY / refuse non-TTY / bypass on `--yes`, all tested; `moon run root:test` + `root:test-e2e` green.

---

### P5 — DROPPED (maintainer decision)

Shell completion is **out of scope** for this overhaul; it addresses none of goals 1–8. The plan ends at P4. (Original sketch retained below for reference only, should it ever be revisited as separate work.)

> ~~**PR-5.1** — live-kube `ValidArgsFunction`s with hang-safe rules (timeout-bounded, no-op logger during `__complete`, `NoFileComp` on error). Per-command constructors. No new deps. Completion-request unit tests with a stubbed client.~~

---

## 5. Cross-cutting determinism harness (applies to every phase)

- Tests construct the root via `NewRootCommand(Options{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Viper: viper.New(), ...mocks})`. Buffers are non-TTY ⇒ `resolveUX` ⇒ `color=false`, `nonInteractive=true` ⇒ plain output, no spinner, no prompt.
- The `ui` no-ESC-byte test (`!bytes.Contains(out, []byte{0x1b})`) is the single backstop for "JSON/data never styled" and "human plane plain off-TTY".
- `plainReporter` preserves the exact `"==> "` / `"    "` prefixes so live Go tests and Chainsaw phrase assertions remain valid.
- `NO_COLOR` is read via direct `os.Getenv` in `resolveUX` and is supreme (beats `--color=always`).

---

## 6. Locked decisions (2026-06-06)

The maintainer answered all eight open questions. These are authoritative and OVERRIDE any conflicting text above (pre-1.0: clean cuts, no shims / deprecations / aliases):

1. **Version → subcommand only.** Add a `yacd version` leaf command; do NOT set cobra's `Version:` field and do NOT register a `--version` flag. `-v`/`--verbose` is verbosity, always. (PR-1.2.)
2. **JSON → clean cut.** Remove the per-command `--json` flags and all `YACD_JSON` handling outright — no alias, no deprecation warning. `--output`/`-o` (`YACD_OUTPUT`) is the only surface; existing JSON byte shapes unchanged. (PR-1.3.)
3. **`list` empty-state stays on stdout.** As designed. (PR-3.1.)
4. **Tests / Chainsaw assertions move as needed.** Approved; P3 adjusts banner assertions and the `info` JSON invocation. (PR-1.3 / PR-3.1 / PR-3.3.)
5. **`connect` excluded from JSON.** `endpoints.json` is its machine surface. As designed.
6. **`-q`/`--quiet` is a global mute** — suppresses info/warn/progress/spinners AND forces the logger off (overrides `-v`/`--log-level`). Data (incl. `-o json`) still prints; the final returned error reason still prints via `ResolveExit`/main.go (not the logger). (PR-1.2.)
7. **JSON shapes** — approved as designed (design §7.1). (PR-3.3.)
8. **Shell completion (Phase 5) — DROPPED.** Plan ends at P4.

The remaining residual decisions are implementation-time confirmations, not blockers:
- **charm module path.** The design uses `charm.land/lipgloss/v2` etc. (the charmbracelet skill's v2 paths). If the canonical path is still `github.com/charmbracelet/lipgloss/v2` at implementation time, author the PR-1.6 v2-path guard regex to the path actually adopted (the guard's intent — v2-only, charm-confined — is unchanged either way).
