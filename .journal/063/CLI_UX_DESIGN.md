# yacd CLI UX Design

## 1. Overview & principles

This document is the single, authoritative contract for the `yacd` CLI's user experience. It collapses five per-concern designs (global UX flags, IO/UX core package, long-running status/progress, verbose logging, JSON output) plus the command-architecture standard into one coherent specification, resolving every cross-concern conflict the critic raised. Where two concerns disagreed, exactly one decision is made here and the rejected option is recorded in section 11.

The eight goals: (1) consistent UX across all commands; (2) default color/interactive interface with `--non-interactive`/`--quiet` to disable rich output; (3) a default status display for long-running commands; (4) loading icons for long-running commands; (5) verbosity-controlled logging (`debug|info|warn|error`) with `-vvv`-style control, default `info`; (6) all non-copyable output to stderr, only script-useful output to stdout; (7) JSON output where it makes sense; (8) clean, consistent code/architecture.

### Principles (hard constraints, enforced structurally)

- **Data to stdout; everything else to stderr.** Diagnostics, progress, prompts, logs, spinners, banners, and confirmations go to stderr. Only script-useful payloads go to stdout. This is enforced by a single output type (`ui.IO`), not per-call-site discipline.
- **Machine output is invariant.** JSON output MUST NOT vary with TTY, color, or verbosity. It is written straight to the data writer and never touches the styling layer.
- **The non-interactive path is first-class.** Every command runs without a TTY and never hangs on a prompt. Interactivity is a one-way latch: a non-TTY can never be re-enabled into prompting.
- **Color/spinners/progress auto-disable off-TTY and honor `NO_COLOR`.** Stripping happens at the write boundary (charm v2 contract), not via a non-existent per-style toggle.
- **Destructive actions gate on TTY with explicit `--yes`/`--force`** and refuse (never hang) non-interactively.
- **Precedence: flags > env > config file > defaults**, read through the instance viper, with one documented exception class (command-local behavior flags read directly off `cmd.Flags()` to avoid cross-command env bleed) and one two-key composition (`-v` is additive over the resolved `--log-level`).
- **Charm is confined to `cli/internal/ui`** and must never enter the manager import graph (`go list -deps ./cmd`).
- **`run`/`exec` are byte-transparent.** Their child stdio passes through untouched; they propagate child exit codes verbatim via `exitError`/`ResolveExit` and are never wrapped by `ui.IO`, `--json`, `--quiet`-rich treatment, or spinners.

### Single ownership (resolving the critic's dominant risk)

There is **one** `cli/internal/ui` package with **one** config type, **one** constructor, and **one** reporter implementation. There is **one** flag table (section 2). There is **one** merged `commandContext`/`RuntimeConfig` field set (section 4). The duplicated/contradictory package designs (`ui.UI` vs `ui.IO` vs `ui.Policy`; three `New()` signatures) are collapsed into the spec in section 4. The work is sequenced as a single **foundation phase** followed by independently-shippable layers (section 8).

---

## 2. Global flag & precedence contract

### 2.1 The authoritative flag table

This is the single reconciled flag table. It supersedes every per-concern flag claim.

| Flag | Short | Scope | Type | Default | Viper key / env | Read via |
|---|---|---|---|---|---|---|
| `--kubeconfig` | — | persistent | string | `""` | `kubeconfig` / `YACD_KUBECONFIG` | `cc.runtimeConfig.Kubeconfig` |
| `--context` | — | persistent | string | `""` | `kube-context` / `YACD_KUBE_CONTEXT` | `cc.runtimeConfig.KubeContext` |
| `--namespace` | `-n` | persistent | string | `""` | `namespace` / `YACD_NAMESPACE` (intentional mirror) | `cc.runtimeConfig.Namespace` |
| `--log-level` | — | persistent | string | `info` | `log-level` / `YACD_LOG_LEVEL` | `cc.runtimeConfig.LogLevel` (base) |
| `--log-format` | — | persistent | string `text\|json` | `text` | `log-format` / `YACD_LOG_FORMAT` | `cc.runtimeConfig.LogFormat` |
| `--verbose` | `-v` | persistent | count | `0` | **flag-only, no env** | `cc.runtimeConfig.Verbosity` |
| `--quiet` | `-q` | persistent | bool | `false` | **flag-only, no env** | `cc.runtimeConfig.Quiet` / `cc.io.Quiet()` |
| `--non-interactive` | — | persistent | bool | `false` | **flag-only, no env** | `cc.runtimeConfig.NonInteractive` |
| `--color` | — | persistent | string `auto\|always\|never` | `auto` | **flag-only, no env** | `cc.runtimeConfig.Color` |
| `--no-color` | — | persistent | bool | `false` | **flag-only, no env** (honors `NO_COLOR`) | folded into `Color` |
| `--output` | `-o` | persistent | string `text\|json` | `text` | `output` / `YACD_OUTPUT` | `cc.io.JSON()` / `cc.runtimeConfig.OutputFormat` |

### 2.2 Three conflict resolutions baked into the table

**`-v` ownership and semantics (resolves the `-v` vs `--version` collision and the additive-vs-floor split).** Goal 5 explicitly requests `-vvv`. The only model that delivers it is the **additive** one. We therefore:

1. **Reassign the `--version` shorthand.** Cobra auto-binds `-v` to `--version` when `Version` is set (verified: `yacd -v` prints version + exit 0 today). The foundation phase explicitly frees `-v` by registering version without a shorthand and claiming `-v` for `--verbose`. This is a deliberate, tested breaking change: `root_test.go` asserts (a) `yacd --version` still prints version to stdout and exits 0 (the exact `root_test.go:28` string), and (b) `yacd -v` now increments verbosity (and, alone, runs the bare root → help, since no subcommand). A CHANGELOG note records the reassignment.
2. **`-v` is additive over the resolved `--log-level`** along `error < warn < info < debug`; it never lowers. `--verbose` is flag-only (a count has no sane env representation), so no `YACD_VERBOSE` exists and nothing leaks into the child env.

**`--output`/`-o`, not `--json` (resolves goal 7's two-flag conflict).** A separate `--json` boolean cannot be made env-inert under the repo's `SetEnvPrefix("YACD") + AutomaticEnv()`: binding key `json` exposes `YACD_JSON` on every command, making machine output vary with ambient environment (a boundary-A violation) and injecting `YACD_JSON` into children via `run.go:109`. We ship a single persistent `--output`/`-o` enum (`text|json`) bound to one key (`output`) / one env (`YACD_OUTPUT`). The legacy `YACD_JSON` and the per-command `--json` flags are **retired with a one-release deprecation alias**: `--json` is registered as a hidden persistent boolean that, when `Changed`, maps to `--output json` and prints a one-line stderr deprecation warning; `YACD_JSON` (when `YACD_OUTPUT` is unset) maps to `output=json` with the same warning. Both are removed in the next minor. `-o` is free (only `-n`, `-f`, `-v`, `-q` are claimed).

**`--quiet` does not touch the logger (resolves the `-q` logger-effect conflict).** `--quiet` suppresses `ui.IO`'s human/progress channel (Info/Status/Reporter spinners) on stderr. It does **not** raise the slog level: a `--quiet` pipeline still emits diagnostics per `--log-level`/`-v`. This keeps the diagnostic stream a single-writer concern owned only by verbosity. Warnings and errors are always shown; data is always shown.

### 2.3 Precedence

`--output` and the existing kube/log keys: `flag > YACD_* env > config-file (future) > default`, read through viper. `--verbose`/`--quiet`/`--non-interactive`/`--color`/`--no-color` are flag-only (no env tier by design — they are session/TTY decisions). `-v`'s additive interaction with `--log-level` is the one documented two-key composition: `effectiveLevel = raise(resolve(log-level), verbose-count)`. Explicit-vs-derived detection uses `cmd.Flags().Changed(...)`, never `viper.IsSet(...)` (which is true for both `SetDefault` and env and therefore cannot mean "the user typed it").

There is no config-file reading today (`ReadInConfig` is absent); the config-file tier is documented-for-future. Only `viper.ConfigFileNotFoundError` would be ignored; parse/permission/type errors surface.

### 2.4 Interaction matrix (flags × effect)

`D` = data plane (stdout, incl. `--output json`); `H` = human Info/Status/progress (stderr); `W/E` = warnings/errors (stderr); `L` = slog logs (stderr); `P` = prompts (huh); `S` = spinner/color (stderr).

| Mode | D (stdout) | H (info/progress) | W/E | L (slog level) | P (prompts) | S (color/spinner) |
|---|---|---|---|---|---|---|
| **default, TTY** | data | shown | shown | per level | allowed | animated + color |
| **`--output json`** | one JSON doc, byte-clean | suppressed | shown | per level | n/a (no prompt path) | none on stdout, ever |
| **`--quiet` / `-q`** | data | **suppressed** | shown | **unchanged** | allowed | per color |
| **`--non-interactive`** | data | shown (plain) | shown | per level | **disabled** | **no animation; plain** |
| **non-TTY (piped)** | data | shown (plain) | shown (plain) | per level | **disabled** (latch) | **none (auto)** |
| **`NO_COLOR` / `--no-color` / `--color=never`** | data | shown (plain) | shown (plain) | per level | allowed | **no color, no animation** |
| **`--color=always`, non-TTY** | data | ANSI (by request) | ANSI | per level | disabled (latch) | color on stderr; **never stdout** |
| **`-vvv`** | data | shown | shown | debug | allowed | per color |
| **`run`/`exec`, any mode** | child stdout passthrough | only yacd's own pre-status line, `-q`-gated | child stderr passthrough | yacd's own, per level | n/a | n/a |

Invariants the matrix encodes:

- **JSON never receives styling.** Color is never applied to stdout under any setting, including `--color=always`. JSON output is invariant under `-v`/`--color`/TTY because data never flows through `ui.IO`'s styled path.
- **`NO_COLOR`/`--color=never` disable animation, not just ANSI** — a `NO_COLOR` TTY run gets a plain reporter with zero cursor-control escapes.
- **The non-interactive latch is one-way:** `nonInteractive = flag || !errTTY || !inTTY`. No flag/env re-enables prompts on a non-TTY.
- **`run`/`exec` rows are byte-transparent.** `--output`/`--color`/`-v` are no-ops on the child; `-q` may suppress only yacd's own pre-status line, never the child streams. Explicit `-o json` on `run`/`exec`/`init`/`connect`/`up --dry-run` is a clear error; ambient `YACD_OUTPUT=json` is silently ignored on those verbs.

---

## 3. IO/UX core package

One CLI-only package, `cli/internal/ui`, owns all terminal IO and is the **only** package permitted to import `charm.land/*`. It exposes a single value type `ui.IO`, constructed once in root `PersistentPreRunE` and threaded by value on `commandContext`.

### 3.1 Color model (charm v2, verified)

lipgloss v2 removed `NewRenderer`/`SetColorProfile`/`renderer.NewStyle`; `Style.Render()` always emits full-fidelity ANSI. Downsampling/stripping happens **at the writer** via `charm.land/colorprofile`. `ui.IO` therefore owns one `color bool`, wraps the stderr stream in a `colorprofile.Writer`, and forces `Profile = colorprofile.NoTTY` when color is off. Styles are plain `lipgloss.NewStyle()` values. A `bytes.Buffer` test stream is non-TTY, so output is plain deterministically. `ui.IO` additionally skips styling outright when `!color`, so byte-cleanliness never depends on the writer alone.

### 3.2 File paths

```
cli/internal/ui/
  io.go         // IO type, New(Config, in, out, err), capability detection, Out/Err/Data, JSON/Quiet/Interactive
  config.go     // Config struct, ConfigFromRuntime(RuntimeConfig)
  palette.go    // shared lipgloss v2 styles (plain value styles) + charm/log styles
  message.go    // Info/Warn/Success/Detail/Status human helpers (stderr, quiet-gated, color-gated)
  reporter.go   // Reporter (lifecycle.Reporter impl): plain + rich (huh spinner) + nop selection
  data.go       // Data() io.Writer, Encode(v) — the data plane (stdout), never styled
  log.go        // NewSlogLogger(level, jsonFormat) — slog now; charm/log v2 swap deferred to styling phase
  confirm.go    // Confirm(prompt) — huh confirm behind Interactive(); accessible mode (added in destructive phase)
  guard_test.go // go list -deps ./cmd import-graph tripwire
```

### 3.3 Signatures (real charm v2 APIs)

```go
// cli/internal/ui/config.go
package ui

type ColorMode int

const (
	ColorAuto ColorMode = iota
	ColorAlways
	ColorNever
)

// Config is the resolved UX runtime, computed once and final.
type Config struct {
	OutputJSON     bool   // --output json (the data-format selector)
	Quiet          bool   // -q: suppress human/progress
	NonInteractive bool   // effective one-way latch
	Color          bool   // final on/off (after auto|always|never + NO_COLOR + TTY)
	Accessible     bool   // huh accessible mode (ACCESSIBLE env)
	Verbosity      int    // resolved -v count
	LogLevel       string // resolved base level after -v raise
	LogFormat      string // text | json
}

// ConfigFromRuntime derives ui.Config from the already-validated RuntimeConfig
// plus the resolved color/interactive booleans. Single derivation path.
func ConfigFromRuntime(rc RuntimeConfig, color, nonInteractive bool) Config
```

```go
// cli/internal/ui/io.go
package ui

import (
	"io"
	"os"

	"charm.land/colorprofile"
	"golang.org/x/term"
)

// IO is the single terminal-IO surface. out is the data plane (stdout, never
// styled); err is the human plane (stderr). cw wraps err for ANSI-correct
// styled writes; it is forced to NoTTY when color is off.
type IO struct {
	out      io.Writer
	err      io.Writer
	cw       *colorprofile.Writer
	cfg      Config
	outIsTTY bool
	errIsTTY bool
	color    bool
	styles   styles
}

// New builds IO from the resolved Config and the injected streams. outIsTTY/
// errIsTTY come from the concrete writers (a bytes.Buffer is non-TTY). color is
// the single source of truth; the colorprofile.Writer is forced to NoTTY when
// color is off so even a real TTY honors --no-color / NO_COLOR.
func New(cfg Config, in io.Reader, out, errw io.Writer) IO

// IsTerminal is the ONE TTY definition for the whole CLI; exec.go's local
// isTerminalWriter is removed in favor of this.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (i IO) Color() bool    { return i.color }
func (i IO) JSON() bool     { return i.cfg.OutputJSON }
func (i IO) Quiet() bool    { return i.cfg.Quiet }
func (i IO) ErrIsTTY() bool { return i.errIsTTY }

// Interactive reports whether blocking prompts (huh) are permitted: only when
// !NonInteractive AND stdin AND stderr are TTYs. False whenever a prompt could
// hang a script.
func (i IO) Interactive() bool
```

```go
// cli/internal/ui/data.go — the data plane (stdout). NEVER styled.
func (i IO) Data() io.Writer { return i.out }

// Encode writes v as json.MarshalIndent(v, "", "  ") + "\n" — byte-identical to
// the existing list/info/wallet --json paths. SetEscapeHTML(true) reproduces
// MarshalIndent exactly, so frozen contracts need no re-pinning. Output never
// depends on TTY/color/verbosity.
func (i IO) Encode(v any) error
```

```go
// cli/internal/ui/message.go — human plane (stderr). Quiet-gated; color-gated.
func (i IO) Info(format string, a ...any)    // status line; no-op in Quiet
func (i IO) Status(format string, a ...any)  // alias group used by long-running commands
func (i IO) Warn(format string, a ...any)    // always shown
func (i IO) Error(format string, a ...any)   // always shown
func (i IO) Success(format string, a ...any) // no-op in Quiet
func (i IO) Detail(format string, a ...any)  // indented detail; no-op in Quiet
```

All human helpers funnel through `cw` when `color`, else write plain to the raw `err` stream — never `fmt.Fprintf` of `Render()` output (which would leak ANSI into pipes). A unit test asserts buffer output contains no `0x1b` (ESC) byte.

```go
// cli/internal/ui/log.go
func (i IO) NewSlogLogger(level slog.Level, jsonFormat bool) *slog.Logger
```

Foundation phase: `NewSlogLogger` returns stdlib `slog` handlers (text/JSON) on stderr — no charm/log dependency. The styling phase swaps the text path to `charm.land/log/v2` (TTY-gated styling) behind the same signature; the JSON path stays stdlib JSON for log-shape stability.

### 3.4 Construction and threading

Streams come from `Options` (already injected; `main.go` unchanged). `ui.New` is called once in `PersistentPreRunE` from the **raw injected writers captured in `NewRootCommand`**, before any `os.Stdout` swap (`wallet_fund.go`) can run, so the TTY verdict and target are correct. No global `os.Stdin`/`os.Stdout` probing. Dependencies live on `commandContext`, never in `context.Context`.

---

## 4. Merged `commandContext` & `RuntimeConfig`

One merged field set (resolves the four-field-name conflict):

```go
// options.go
type commandContext struct {
	in  io.Reader
	out io.Writer // raw stdout writer; run/exec child passthrough writes here
	err io.Writer // raw stderr writer; passthrough fallback only

	viper *viper.Viper
	// ... existing factory fields ...

	runtimeConfig RuntimeConfig // resolved once in PersistentPreRunE (replaces 11 re-calls)
	io            ui.IO         // the single renderer/output seam
	logger        *slog.Logger  // single resolved logger
	outputExplicit bool         // cmd.Flags().Changed("output") on the active command
}
```

```go
// config.go
type RuntimeConfig struct {
	Kubeconfig  string
	KubeContext string
	Namespace   string

	LogLevel  string // resolved base AFTER -v raise
	LogFormat string // text | json
	Verbosity int    // raw -v count

	Quiet          bool
	NonInteractive bool
	Color          ui.ColorMode // requested mode; folded with NO_COLOR + TTY in resolveUX
	OutputFormat   string       // text | json
}
```

`PersistentPreRunE` is the single resolution point:

```go
PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
	if err := initializeConfig(cmd, ctx.viper); err != nil {
		return err
	}
	rc, err := loadRuntimeConfig(cmd, ctx.viper) // cmd needed for Changed/Count reads
	if err != nil {
		return err
	}
	ctx.runtimeConfig = rc
	color, nonInteractive := resolveUX(rc, ctx.err, ctx.in) // TTY + NO_COLOR + latch
	ctx.io = ui.New(ui.ConfigFromRuntime(rc, color, nonInteractive), ctx.in, ctx.out, ctx.err)
	ctx.logger = ctx.io.NewSlogLogger(slogLevel(rc.LogLevel), rc.LogFormat == "json")
	ctx.outputExplicit = cmd.Flags().Changed("output")
	return nil
},
```

`loadRuntimeConfig` resolves precedence-sensitive values through viper and reads the flag-only knobs off `cmd.Flags()`:

```go
func loadRuntimeConfig(cmd *cobra.Command, vp *viper.Viper) (RuntimeConfig, error) {
	// kube/log/output via viper (precedence). verbose/quiet/non-interactive/color via cmd.Flags().
	// LogLevel = raise(resolve(log-level), verboseCount).
	// Validate log-level ∈ {debug,info,warn,error}, log-format ∈ {text,json}, output ∈ {text,json}.
}

func raise(base string, v int) string {
	order := []string{"error", "warn", "info", "debug"}
	i := indexOf(order, base) // default info → 2
	return order[min(i+v, len(order)-1)]
}

func resolveUX(rc RuntimeConfig, errOut io.Writer, in io.Reader) (color, nonInteractive bool) {
	errTTY := ui.IsTerminal(errOut)
	inTTY := isTerminalReader(in)
	noColorEnv := os.Getenv("NO_COLOR") != ""
	switch {
	case noColorEnv:
		color = false // NO_COLOR is user-supreme, beats --color=always
	case rc.Color == ui.ColorAlways:
		color = true
	case rc.Color == ui.ColorNever:
		color = false
	default: // ColorAuto
		color = errTTY
	}
	nonInteractive = rc.NonInteractive || !errTTY || !inTTY
	return color, nonInteractive
}
```

`--no-color` (flag-only) folds into `Color`: when `--color` was not explicitly `Changed` and `--no-color` is set, `Color = ColorNever`. `NO_COLOR` env is applied last in `resolveUX` and always wins. `--output` is **not** registered with `vp.SetDefault` (so `Changed` stays meaningful); the flag's pflag default `text` plus a normalize-empty step supplies the value.

The **11** existing `loadRuntimeConfig(commandContext.viper)` re-calls (`connect.go:50`, `down.go:21`, `exec.go:69`, `info.go:22`, `install.go:52`, `list.go:26`, `run.go:39`, `target.go:70`, `up.go:28`, `wallet.go:79`) are deleted; each reads `cc.runtimeConfig`. `root.go:111` stays the one caller.

`-v`↔`--version` reassignment and the `--output` registration both land here. `initializeConfig` is extended only to `bindFlag(vp, "output", …)`; it does **not** bind `verbose`/`quiet`/`non-interactive`/`color`/`no-color` (flag-only by design).

---

## 5. Long-running status & progress

One progress abstraction: the existing `lifecycle.Reporter` interface, widened by one charm-free method, backed by three stderr-only implementations chosen once at construction.

### 5.1 Interface (charm-free, in `cli/internal/lifecycle`)

```go
type Reporter interface {
	Step(format string, args ...any)
	Substep(format string, args ...any)
	Done(format string, args ...any)
	Run(ctx context.Context, title string, action func(context.Context) error) error
}
```

`NopReporter.Run` calls the action. `Run` carries only `context.Context` + a func, so the interface stays charm-free and reachable from non-charm packages.

### 5.2 Implementations (in `cli/internal/ui/reporter.go`)

- **`richReporter`** (TTY ∧ interactive ∧ ¬quiet ∧ ¬json ∧ color): per-`Run` animated spinner via `charm.land/huh/v2/spinner` (verified API: `New().Title().Type(spinner.Dots).Context(ctx).WithOutput(stderr).WithAccessible(acc).ActionWithErr(fn).Run()`; inline `tea.NewProgram`, no `WithAltScreen`; cancellation via `tea.WithContext`). `WithOutput` is stderr. Step/Substep/Done print styled standalone lines.
- **`plainReporter`** (non-TTY, `--non-interactive`, `NO_COLOR`, `--color=never`): byte-identical `"==> "`/`"    "` line printer (the promoted `stepReporter`).
- **`nopReporter`** (`--quiet`): discards progress, still runs the action.

```go
func (i IO) Reporter() lifecycle.Reporter // selects rich/plain/nop from cfg
```

Animation predicate: `animate = errIsTTY && interactive && !quiet && !json && color`. `NO_COLOR`/`--color=never` fold into `color=false` ⇒ no spinner constructed at all (no cursor-control escapes). A JSON-emitting command's `Reporter()` returns plain/nop, so a machine-output run never animates regardless of TTY.

### 5.3 Symmetric completion lines (resolves the "is ready."/"is gone." contradiction)

Neither `richReporter.Run` nor `plainReporter.Run` emits a completion line. The command layer always calls `report.Done(<verbatim success line>)` **after** `report.Run(<wait>)`. So `up_test.go:217,250` (`"…is ready."`) and `down_test.go:38,62` (`"…is gone."`) assert byte-identical strings on both TTY and non-TTY; the `NotContains` guards (`up_test.go:276`, `down_test.go:102`) hold because `Done` is inside the wait branch.

### 5.4 Mapping & teardown

All waits are indeterminate (boolean polls / subprocess waits) → spinners only; no fake percentage bars. The wait helpers (`kube.WaitReady`, `kube.WaitGone`, `operator.WaitForReady`, `awaitConfirmation`) stay charm-free; the command layer wraps them in `report.Run`. On cancel, the spinner restores the cursor on program exit, plus a defensive trailing newline. `wallet add/topup --await` narrows `redirectStdoutToStderr` to bracket only `submitter.Submit`, restoring `os.Stdout` before the await spinner runs.

| Op | File | Indicator |
|---|---|---|
| devnet up (sequence) | `manager.go` | sequential spinners (one per phase) |
| `up --wait` | `up.go` | spinner; `Done("…is ready.")` |
| `down --wait` | `down.go` | spinner; `Done("…is gone.")` |
| `install --wait` | `install.go` / `manager.go` | spinner |
| `wallet add/topup --await` | `wallet_fund.go` | spinner, json-gated to plain/nop |
| port-forward resolve | `forward_resolve.go` | none (sub-second) |
| `connect` (supervisor) | `connect.go` | none (distinct shape) |
| `run`/`exec` | `run.go`/`exec.go` | none (passthrough) |

---

## 6. Logging

Logging stays on `log/slog` for the foundation phase (no color needed for level control; zero new deps; deterministic ANSI-free text output). The charm `log/v2` swap is deferred to the styling phase (where lipgloss genuinely enters the graph) behind the unchanged `NewSlogLogger` signature.

- **Levels:** `debug|info|warn|error`, default `info`, on stderr.
- **`-vvv`:** `--verbose` is a count; effective level = `raise(resolve(--log-level), verbose-count)`. Additive, never lowers. `-v`→debug, and a higher base (e.g. `--log-level=warn`) steps `warn→info→debug`.
- **Precedence:** a typed `--log-level` flag (`Changed`) is authoritative over `-v`; an ambient `YACD_LOG_LEVEL` env does **not** outrank the `-v` flag (so `YACD_LOG_LEVEL=warn yacd up -v` → debug). `YACD_LOG_LEVEL` without `-v` is respected.
- **`--quiet`:** does not change the logger level (see 2.2).
- **`--output json` vs logs:** orthogonal. Logs stay on stderr always; `--output json` changes only stdout data. Structured logs are an independent opt-in via `--log-format=json` (stderr). The logger never writes to stdout.
- **No `ReportCaller`/`file:line`** (nondeterministic) and **no `logfmt`** format (gold-plating; enum stays `text|json`).
- **The single existing structured log call** (`up.go:96` `logger.Debug(...)`) is unchanged.

| | stdout (data) | stderr (logs) |
|---|---|---|
| default | data / `--output json` payload | plain text, level=info |
| `--output json` | stable JSON | unchanged plain text logs |
| `--log-format=json` | unchanged | structured JSON logs |
| `-v` / `-q` / `NO_COLOR` / TTY | **byte-identical** | level threshold only (logs); `-q` does not change level |

---

## 7. JSON output contract

One global `--output`/`-o` enum (`text|json`), default `text`, bound to `output`/`YACD_OUTPUT` (section 2). One shared encoder `ui.IO.Encode` (= `json.MarshalIndent(v, "", "  ")` + `\n`, `SetEscapeHTML(true)`) reproduces frozen shapes byte-for-byte. One choke point makes the contract enforceable:

```go
func (c *commandContext) emit(value any, human func(io.Writer) error) error {
	if c.io.JSON() {
		return c.io.Encode(value) // stdout only, byte-clean
	}
	return human(c.io.Data()) // text-mode renderer; honors c.io.Color()
}
```

Contract: in JSON mode, stdout carries exactly one stable JSON document and nothing else; all human text/progress/logs go to stderr; errors are never wrapped into the data document (failure = stderr message + non-zero exit, stdout empty/unparseable-as-success). Output is invariant to TTY/color/verbosity.

### 7.1 Per-command JSON table

| Command | JSON? | Shape | Notes |
|---|---|---|---|
| `list` | yes (exists) | `[]listItem` | frozen field names |
| `info` | yes (exists) | `infoOutput` | frozen; Chainsaw e2e parses it |
| `wallet list` | yes (exists) | `[]walletListItem` | preserved |
| `wallet add` | yes (exists) | `walletAddResult` | preserved |
| `wallet topup` | yes (exists) | `fundResult` | preserved |
| `devnet status` | NEW | `devnetStatusOutput` | additive (no existing stdout text moves) |
| `up` | NEW | `upResult` | additive (emits nothing on stdout today) |
| `down` | NEW | `actionResult` | additive |
| `install` | NEW | `installResult` | plan + apply share one shape |
| `devnet` / `devnet up` / `devnet down` | DEFERRED | `devnetUpOutput` / `actionResult` | land after routing moves banners off stdout |
| `wallet remove` / `wallet export` | DEFERRED | `actionResult` / `walletExportOutput` | land after routing |
| `up --dry-run` | NO JSON (YAML) | raw manifest | ambient-immune; explicit `-o json` + `--dry-run` errors |
| `connect` | EXCLUDE | — | long-running; `endpoints.json` file is the machine interface |
| `init` | EXCLUDE | — | stdout payload is the template |
| `run` / `exec` | EXCLUDE | — | byte-transparent passthrough |

Excluded verbs: explicit `-o json` on the command line fails fast with a clear error (one shared helper `rejectExplicitJSON(verb)` reading `c.outputExplicit && c.io.JSON()`); ambient `YACD_OUTPUT=json` is silently ignored.

New shapes (frozen on introduction):

```go
type devnetStatusOutput struct {
	Exists   bool                  `json:"exists"`
	Cluster  *devnetClusterOutput  `json:"cluster,omitempty"`
	Operator *devnetOperatorOutput `json:"operator,omitempty"`
	Networks []networkRefOutput    `json:"networks"`
}
type upResult struct {
	Name, Namespace string `json:"name"` // (Namespace tagged separately)
	Applied, Ready, Waited bool
}
type actionResult struct {
	Action, Resource, Name string
	Namespace string `json:"namespace,omitempty"`
	Removed   bool
}
type installResult struct {
	Action, Namespace, TargetVersion string
	InstalledVersion string `json:"installedVersion,omitempty"`
	Ready, DryRun bool
}
```

---

## 8. Command architecture & consistency standard

### 8.1 The skeleton (Goals 1 & 8)

Every leaf command compiles to: a declarative `newXCommand(cc *commandContext) *cobra.Command` constructor (builds the command, registers flags via shared registrars, sets a thin `RunE`), and a thin `RunE` (3–8 lines: read cached config, parse operands, call one unexported `runX(ctx, cc, params)`). All logic lives in `runX`. No `loadRuntimeConfig` in `RunE`; no formatting; no port construction beyond what the orchestrator needs; an `xParams` struct carries parsed inputs.

```go
func newUpCommand(cc *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up NAME",
		Short: "Create or update a YACD environment and wait for readiness",
		Long:  longUp, Example: exampleUp,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, namespace, err := cc.resolveIdentity(args[0])
			if err != nil {
				return err
			}
			file := cc.viper.GetString("file")
			if strings.TrimSpace(file) == "" {
				return fmt.Errorf("set --file or YACD_FILE: a developer environment file is required")
			}
			return runUp(cmd.Context(), cc, upParams{
				name: name, namespace: namespace, file: file,
				dryRun:       cc.viper.GetBool("dry-run"),
				allowMainnet: cc.viper.GetBool("allow-mainnet"),
				wait:         mustBool(cmd.Flags().GetBool("wait")),
				timeout:      mustDur(cmd.Flags().GetDuration("timeout")),
			})
		},
	}
	flagFile(cmd); flagDryRun(cmd, "Render the manifest without applying it")
	cmd.Flags().Bool("allow-mainnet", false, "Allow applying a mainnet CardanoNetwork")
	flagWait(cmd, 12*time.Minute)
	return cmd
}
```

### 8.2 Shared registrars & validators (`flags.go`, `identity.go`)

`flagWait`, `flagDryRun`, `flagFile` (`-f`), `flagAwait`, `flagYes`. Args validators: `cobra.ExactArgs/NoArgs/MinimumNArgs` plus `requireNameAndCommand` for `exec` (`NAME -- cmd...`). Required inputs: CLI-only flags use `MarkFlagRequired`; env/config-participating flags (`--file`/`YACD_FILE`) validate the resolved value with an actionable message (never `MarkFlagRequired`, which checks only `flag.Changed`); operands use arity validators. Help: `Short` required; `Long`/`Example` (package-level consts) where they add value. Leaf commands MUST NOT define `PersistentPreRunE` (cobra runs only the root's); local validation goes in `PreRunE`.

### 8.3 YACD_* collision audit (Goal 6, repo invariant D)

| Flag / key | Maps to env | Verdict | Rule |
|---|---|---|---|
| `--namespace` / `namespace` | `YACD_NAMESPACE` | intentional | keep (mirrors contract; `info_test.go:19` relies on it) |
| `--ogmios-url` (wallet) | `YACD_OGMIOS_URL` | **collision — FIX** | read via `cmd.Flags().GetString` (rename `chainOverridesFromViper`→`chainOverridesFromFlags`) |
| `--kupo-url` (wallet) | `YACD_KUPO_URL` | **collision — FIX** | read via `cmd.Flags().GetString` |
| per-command `timeout`/`wait` | `YACD_TIMEOUT`/`YACD_WAIT` | **cross-command bleed — FIX** | read via `cmd.Flags().GetDuration/GetBool`, not shared viper |
| `--output`, `--verbose`, `--quiet`, `--color`, `--non-interactive` | `YACD_OUTPUT` only (others flag-only) | safe | only `--output` is env-bound; it does not collide with host-access keys |

`install`'s `--values/--set/--set-string` keep `cmd.Flags().GetStringArray` (documented exception: viper StringArray mangles Helm `--set a=1,b=2`). Tests assert `YACD_OGMIOS_URL`/`YACD_KUPO_URL` do not shadow wallet flags and `YACD_TIMEOUT` does not bleed across commands.

### 8.4 Output routing table (the single reconciled stdout/stderr decision — resolves the `list` empty-state and `connect` contradictions)

| Message | Stream | Replacement |
|---|---|---|
| `up` dry-run manifest | **stdout** | `cc.io.Data(...)` |
| `up` status (Applied/Waiting/is ready/Dry run/mainnet warn) | stderr (already) | `cc.io.Status(...)` |
| `list`/`info` table + JSON | stdout | `cc.io.Data(...)` / `emit` |
| `list` empty-state "No CardanoNetworks found." | **stdout** (text-mode result) | `cc.io.Data(...)` — **kept on stdout** (it is the text result; in JSON mode the result is `[]`). `list_test.go:134/152` unchanged. |
| `install` dry-run "Plan: …" | **stdout** (scriptable preview) | `cc.io.Data(...)` |
| `install` success "Operator … ready/installed" | **stderr** (MOVED) | `cc.io.Status(...)` |
| `devnet` banner + endpoints + "Try:" | **stderr** (MOVED) | `cc.io.Status(...)`/`Detail(...)` |
| `devnet down`/`devnet status` text | **stderr** (MOVED) | `cc.io.Status(...)` |
| `connect` banners ("Forwarding…", "Wrote… Ctrl-C") | **stderr** (MOVED) | `cc.io.Status(...)` |
| `connect` `KEY=URL` pairs | **stdout** (eval-able data contract) | `cc.io.Data(...)`; `endpoints.json` remains durable contract |
| `wallet remove` "Removed wallet…" | **stderr** (MOVED) | `cc.io.Success(...)` |
| `run`/`exec` child stdio | child's own | **EXCLUDED** (`//ui-passthrough-ok`) |
| `wallet_fund.go` `os.Stdout` swap | named exception | `//ui-passthrough-ok`; IO built from raw writer before swap |

The `list` empty-state stays on stdout (it is the command's text result, with a `[]` JSON equivalent). `connect` is split: banners→stderr, `KEY=URL`→stdout. `connect` is **not** byte-transparent (correcting the global-UX matrix).

### 8.5 Destructive-action gating

Genuinely destructive verbs — `down` (deletes a CardanoNetwork + GC'd children) and `devnet down` (deletes the k3d cluster) — gate via `flagYes`: TTY + no `--yes` → huh confirm on stderr (behind `cc.io.Interactive()`, accessible-mode supported); non-TTY + no `--yes` → refuse with an actionable error and non-zero exit (never an unconditional prompt that would hang CI); `--yes`/`--force` bypasses. `up`/`devnet`/`install` are create/upgrade → no gate.

### 8.6 Error convention

`runX` returns errors; `RunE` returns them; `main.go` + `ResolveExit` print once. Wrap with `fmt.Errorf("<verb phrase>: %w", err)` (lowercase, no trailing punctuation). Validation errors name the input. This concern does **not** introduce sentinel/typed exit-code errors (that is the exit-code concern's deliverable); the `exitError`/`ResolveExit`/`newExitError` contract is verbatim.

### 8.7 Per-command refactor checklist

For each leaf command:

1. [ ] Constructor is declarative (flags via shared registrars; no logic).
2. [ ] `RunE` is ≤8 lines, calls one `runX(ctx, cc, params)`.
3. [ ] No `loadRuntimeConfig` in `RunE`; reads `cc.runtimeConfig`.
4. [ ] Args validator set; required env/config inputs validated on resolved value (not `MarkFlagRequired`).
5. [ ] No raw `fmt.Fprintf(cc.out/cc.err)` / `os.Std*` / `fmt.Print*` / `lipgloss.Print*` — route through `cc.io` (or tag `//ui-passthrough-ok` for the run/exec passthrough lines).
6. [ ] Data → `cc.io.Data()`/`cc.io.Encode()`/`emit`; human → `cc.io.Status/Info/Warn/Success/Detail`.
7. [ ] Long-running waits wrapped in `cc.io.Reporter().Run(...)`; success line via `Done(...)` after `Run(...)`.
8. [ ] `--output json` path produces only the frozen/new JSON shape on stdout.
9. [ ] No leaf `PersistentPreRunE`; local validation in `PreRunE`.
10. [ ] Command-local behavior flags (`timeout`/`wait`/`ogmios-url`/`kupo-url`) read via `cmd.Flags()`, not shared viper.
11. [ ] Destructive verbs gate via `flagYes` (TTY confirm / non-TTY refuse).
12. [ ] `Short` set; `Long`/`Example` where they add value.

### 8.8 Definition of Done (new command)

A new command is done when: it follows the skeleton (8.7 fully checked); it has a `runX` unit test via `NewRootCommand(Options{...})` + `viper.New()` + buffer streams (deterministic: buffers are non-TTY → color off → plain); data lands only on stdout and only via `Data/Encode/emit`; if it emits `--output json`, the shape is frozen and tested for byte-stability; if destructive, both a TTY-refusal (non-TTY, no `--yes`) and a `--yes`-bypass test exist; no new viper key collides with a host-access `YACD_*` name; `go list -deps ./cmd` stays charm-free; no new raw stream writes survive the CI grep.

---

## 9. How boundaries are upheld

- **stdout/stderr (Goal 6):** the single routing table (8.4) decides every message's stream; `emit`/`Data`/`Encode` are the only stdout writers; all human helpers and the Reporter write to stderr. CI grep (9, guard #3) bans raw `Fprintf(cc.out/cc.err)`, `os.Std*`, `fmt.Print*`, `lipgloss.Print*` outside `cli/internal/ui`, with line-level `//ui-passthrough-ok` pragmas (not whole-file allowlists) for run/exec passthrough and the `wallet_fund` swap.
- **TTY/NO_COLOR (Goal 2):** decided once in `resolveUX` from the raw injected writers + `NO_COLOR` (direct `os.Getenv`, supreme) + `--color`/`--no-color`; folded into `ui.IO.color`; the `colorprofile.Writer` strips ANSI at the boundary (forced `NoTTY` when off); `--json` bypasses color entirely. Buffers are non-TTY → deterministic plain output.
- **Manager-import guard (repo invariant D):** charm confined to `cli/internal/ui`. Net-new CI tripwire (`cli/internal/ui/guard_test.go::TestManagerImportGraphHasNoCharm`) fails if `go list -deps ./cmd` matches `charm|lipgloss|huh|bubble|ultraviolet|colorprofile|charmbracelet|ogmigo|kugo|internal/cardano/tx`. A v2-path grep fails on any `github.com/charmbracelet/(lipgloss|huh|log)` v0/v1 import under `cli/`. Ships in the foundation PR that first adds charm to `go.mod`. The slog→charm/log swap is deferred to the styling phase so the "first charm PR" story stays coherent.
- **JSON stability (repo invariant):** `Encode` reproduces `MarshalIndent` byte-for-byte (`SetEscapeHTML(true)`); `listItem`/`infoOutput`/wallet shapes untouched; Chainsaw `info --json` (`chainsaw-test.yaml:545`) and `list_test.go:177`/`info_test.go` stay byte-clean. New shapes are additive.
- **Env contract (repo invariant D):** only `--output` is env-bound among the new flags (`YACD_OUTPUT`), no host-access collision; `--verbose`/`--quiet`/`--color`/`--non-interactive` are flag-only (no `YACD_*`, no child leak). `run`/`exec` keep `append(os.Environ(), connected.env...)`; a guard test asserts the CLI-injected set (`connected.env`) contains only the documented host-access keys even when `YACD_OUTPUT`/`YACD_VERBOSE`/etc. are exported in the test process. `--ogmios-url`/`--kupo-url` read off `cmd.Flags()` so injected `YACD_OGMIOS_URL`/`YACD_KUPO_URL` no longer shadow them.
- **Test determinism (repo invariant E):** buffers → non-TTY → color off → plain; `ui` no-ESC-byte test; `plainReporter` keeps `"==> "`/`"    "` prefixes so live + Chainsaw phrase asserts hold. **Named moving assertions:** `install_test.go:159-162,183-186,259-260` → stderr (`285-289,306-309` stay stdout); `devnet_test.go:104,128,142,159,172,186` → stderr; `devnet_live_test.go:48,49,67,73` → stderr; `connect_test.go` banners → stderr (`KEY=URL` stay stdout); `wallet_test.go` remove line → stderr; `exec_test.go:79` reworded to the `requireNameAndCommand` message; `list_test.go:134/152` and `up_test.go`/`down_test.go` unchanged. **New tests:** `-v`↔`--version` reassignment guard; `--color`×`--no-color`×`NO_COLOR` precedence; `raise()` verbosity table incl. env-base rows; `YACD_JSON`/`YACD_OUTPUT` deprecation-alias behavior; `YACD_OGMIOS_URL`/`YACD_KUPO_URL`/`YACD_TIMEOUT` non-bleed; child-env guard; destructive TTY-refuse + `--yes`-bypass.

---

## 10. Phasing

Sequenced so each phase is independently shippable with CI green and minimal blast radius (resolves the inconsistent cross-concern sequencing).

- **Phase 0 — charm-free Reporter widening (independent, first, byte-neutral):** widen `lifecycle.Reporter` with `Run`; add `Run` to `NopReporter`, `stepReporter`, and test doubles; change `runInstall`'s parameter to `lifecycle.Reporter`. No flags, no deps, no behavior change.
- **Phase 1 — foundation (the mandatory reconciliation pass):** the single `cli/internal/ui` package (one `Config`, one `New`, one `Reporter`, `Encode`, slog logger); the merged `commandContext`/`RuntimeConfig`; the single flag table incl. `-v`↔`--version` reassignment, `--output`/`-o` (+ `--json`/`YACD_JSON` deprecation alias), `--verbose`/`--quiet`/`--non-interactive`/`--color`/`--no-color`; `resolveUX`/`raise`; the 11 `loadRuntimeConfig` re-call deletions + caching; the YACD_* collision fixes (wallet URLs, per-command timeout/wait); the import-graph + v2-path + stdout-purity CI guards. Verbose logging ships here on slog (interim, no charm).
- **Phase 2 — RunE thinning + skeleton:** extract `runX` orchestrators, shared flag registrars, Args validators (incl. `requireNameAndCommand`), help text. Byte-neutral except the exec arity error (named test).
- **Phase 3 — routing + additive JSON:** apply the single routing table (8.4) with all named assertion moves; route everything through `cc.io`; add `up`/`down`/`install`/`devnet status` JSON shapes via `emit`. The deferred JSON shapes (`devnet up/down`, `wallet remove/export`) land here once their text moves to stderr.
- **Phase 4 — spinners + styling + destructive gating:** `richReporter` huh spinner; charm/log v2 swap (text path) — the first introduction of `log/v2`; lipgloss palette; huh confirm + `flagYes` for `down`/`devnet down`.
- **Phase 5 (optional) — completion:** live-kube `ValidArgsFunction`s with hang-safe rules (timeout-bounded, no-op logger during `__complete`, `NoFileComp` on error). Not part of goals 1–8; its own PR.

---

## 11. Rejected alternatives

1. **Keep `-v` bound to `--version`; ship `--verbose` shorthand-less.** Rejected: goal 5 demands `-vvv`, deliverable only via an additive count on `-v`. We reassign `--version`'s shorthand with explicit tests instead.
2. **Binary "floor to debug" `-v` semantics.** Rejected: no `-vvv` stepping from a higher base; goal 5 asks for it. Additive `raise()` chosen.
3. **Separate `--json` boolean (local or persistent).** Rejected: cannot be env-inert under `AutomaticEnv`+`BindPFlags` (`YACD_JSON` flips it on every command — boundary-A violation and child-env leak). One `--output`/`-o` enum bound to `YACD_OUTPUT`; `--json`/`YACD_JSON` retired via one-release alias.
4. **`--quiet` raises the slog level to warn.** Rejected: makes the diagnostic stream depend on a cosmetic flag and adds a second writer of the level. `--quiet` gates only the human/progress channel.
5. **Three `cli/internal/ui` package designs (`ui.UI`/`ui.IO`/`ui.Policy`, three `New()` signatures).** Rejected: would not compile as a union and contradicts goal 8. Collapsed into one `ui.IO` + one `New` + one `Reporter`.
6. **Per-style "disable when Color==false" lipgloss mechanism.** Rejected: does not exist in v2; `Render()` always emits ANSI. Strip at the `colorprofile.Writer` boundary.
7. **Derive TTY/color from global `os.Stdout`.** Rejected: tests inject buffers and `wallet_fund` swaps `os.Stdout`. Derive from the raw injected writers captured before any swap.
8. **`MarkFlagRequired("file")`.** Rejected: `--file`/`YACD_FILE` participates in env precedence; `MarkFlagRequired` checks only `flag.Changed` and would reject a legitimate env value. Validate the resolved value.
9. **Fake percentage progress bars / Bubble Tea full-screen TUI.** Rejected: all waits are indeterminate (no honest denominator); TUI is out of scope. Inline huh spinners only.
10. **Adopt charm `log/v2` in the foundation phase.** Rejected: front-loads lipgloss into the graph and couples logging to styling. slog interim in foundation; charm/log swap deferred to the styling phase, which owns the first charm/log import.
11. **Filter `os.Environ()` in `run.go`.** Rejected: changes the whole-environment passthrough (out of scope). Guard the narrower true invariant (CLI-injected `connected.env` excludes UX keys) with a test.
12. **`connect` as byte-transparent / `connect -o json`.** Rejected: `connect` writes interleaved banners + `KEY=URL` to stdout (not eval-safe) and is a long-running supervisor (a half-stream JSON object is not pipe-composable). Split: banners→stderr, `KEY=URL`→stdout; `endpoints.json` is the machine interface; `connect` excludes JSON.
13. **`--output table|json|yaml`.** Rejected for scope: `yaml` has no consumer; the stable contracts are JSON-shaped. The enum can add `yaml` later without breaking.
14. **Mandatory `Long`/`Example` on every leaf.** Rejected: bloats the diff for trivial verbs; `Short` required, `Long`/`Example` where they add value (verified no test asserts help text).
15. **Introduce sentinel/typed exit-code errors here.** Rejected: belongs to the exit-code concern; this design leaves a clean `runX` seam and keeps `exitError`/`ResolveExit` verbatim.
16. **Per-command subpackages / a `Command` interface registry.** Rejected: the repo keeps one `package cli`; cobra is the registry. `runX` extraction gives testability without export churn.
