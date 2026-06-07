# yacd CLI UX Audit

## 1. Architecture overview

The yacd developer CLI lives under `cli/` inside the single root Go module (`github.com/meigma/yacd`, `go 1.26.3`, `go.mod:1`). It is built with Cobra/Viper. Entry point `cli/cmd/yacd/main.go` wires `Out=os.Stdout`, `Err=os.Stderr` (`main.go:46-47`) and prints errors once (`main.go:51-55`); the command tree is built via `NewRootCommand(Options{...})` (`root.go`).

Key seams already in place:
- **Injected streams.** `Options{In,Out,Err}` (`options.go:107-109`) flow into `commandContext{in,out,err}` (`options.go:155-157`) and onto cobra via `root.SetIn/SetOut/SetErr` (`root.go:120-122`). Commands write to `commandContext.out`/`.err` directly, never through `cmd.OutOrStdout()`.
- **Instance Viper.** A fresh `*viper.Viper` is threaded through `commandContext`; config is initialized in root `PersistentPreRunE` (`root.go:107-117`) — current best practice over `cobra.OnInitialize`.
- **Factory ports.** `Options` exposes seven factory seams (`KubeClientFactory`, `TxSubmitterFactory`, `EndpointProber`, `ClusterProvisionerFactory`, etc., `options.go:105-148`) for dependency injection in tests.
- **Centralized exit policy.** `ResolveExit` (`exit.go:40-51`) → `main.go:51-53`: nil→0 silent; `exitError` carries a child exit code (silent if msg empty); else→1 printed once.
- **Progress seam.** `lifecycle.Reporter` (`Step`/`Substep`/`Done`, `lifecycle/lifecycle.go:124-133`); sole impl `stepReporter` writes `"==> "`/indent lines to stderr (`devnet.go:171-185`).

These seams are sound and mostly should be preserved. The gaps are UX surface, not core correctness.

## 2. Command & flag inventory

Root command: `Use: "yacd"`, `Short: "YACD developer CLI"` (`root.go:102-103`); custom version template `"yacd %s (%s) built %s\n"` (`root.go:104,119`); `SilenceUsage`/`SilenceErrors: true` (`root.go:105-106`); no root `RunE` (bare `yacd` prints help, exit 0).

Persistent flags (`root.go:124-128`): `--kubeconfig` (""), `--context` (""), `-n/--namespace` ("", only shorthand in tree), `--log-level` ("info"), `--log-format` ("text"). Config init binds these plus all local flags into Viper, `SetEnvPrefix("YACD")`, `AutomaticEnv`, `-`/`.`→`_` replacer (`config.go:40-70`). Note `--context` binds to Viper key `kube-context` (`config.go:53`), so its env var is `YACD_KUBE_CONTEXT`, not `YACD_CONTEXT`. Logging always goes to stderr (`root.go:98,115`; `newLogger` `config.go:122-141`).

Streams: O=stdout, E=stderr. "JSON" = has a `--json` flag.

| Command | Use / Args | Local flags (default) | stdout (data) | stderr (diag) | --json | Exit |
|---|---|---|---|---|---|---|
| `devnet` | `devnet` / `NoArgs` (`devnet.go:32,34`) | `--bare` (false), `--timeout` (12m) (`devnet.go:69-70`) | `printDevnetUp` report (`devnet.go:66,191`) | step progress (`devnet.go:165,175-185`) | no | 0/1; rejects explicit target (`:36`); `--timeout<=0`→err (`:41`) |
| `devnet down` | `down` / `NoArgs` (`devnet.go:81,84`) | `--timeout` (5m) (`devnet.go:108`) | `"devnet cluster removed."` (`devnet.go:104`) | step progress | no | 0/1 |
| `devnet status` | `status` / `NoArgs` (`devnet.go:116,119`) | none | `printDevnetStatus` (`devnet.go:139,240`) | — | no | 0/1; clears stale record (`:135-137`) |
| `up` | `up NAME` / `ExactArgs(1)` (`up.go:24,26`) | `-f/--file` ("", required at runtime), `--dry-run` (false), `--allow-mainnet` (false), `--wait` (true), `--timeout` (12m) (`up.go:117-121`) | dry-run manifest only (`up.go:70`) | apply/wait/warn status (`up.go:78,97,102,108,150`) | no | 0/1; missing `-f`→err (`:39`); `--wait`+`timeout<=0`→err (`:46`); mainnet w/o `--allow-mainnet`→err (`:143`) |
| `down` | `down NAME` / `ExactArgs(1)` (`down.go:17,19`) | `--wait` (true), `--timeout` (5m) (`down.go:63-64`) | (none) | delete/gone status (`down.go:43,54`) | no | 0/1; idempotent; `--wait`+`timeout<=0`→err (`:31`) |
| `list` | `list` / `NoArgs` (`list.go:21,24`) | `--json` (false) (`list.go:65`) | table or JSON array (`list.go:55,61`) | — | yes | 0/1 |
| `info` | `info NAME` / `ExactArgs(1)` (`info.go:17,20`) | `--json` (false) (`info.go:62`) | text or JSON (`info.go:53,59`) | — | yes | 0/1 |
| `connect` | `connect NAME` / `ExactArgs(1)` (`connect.go:39,48`) | none | forwarded URLs + file path (`connect.go:112,186`) | reconnect/drop/disconnect notices (`connect.go:92,123,130`) | no | 0 on Ctrl-C; 1 on first-connect failure (`:89`); long-running; writes `.yacd/.../endpoints.json` |
| `run` | `run NAME [-- command...]` / `MinimumNArgs(1)` (`run.go:24,37`) | none (has `Example`) | child stdout, inherited (`run.go:111`) | "Connected... running" (`run.go:62`); child stderr | no | propagates child exit code (`run.go:125`,`exit.go`); 128+sig; lost-forward→1 (`run.go:118`) |
| `exec` | `exec NAME -- command...` / `MinimumNArgs(1)` (`exec.go:42,67`) | none (has `Long`+`Example`) | in-pod stdout (`exec.go:110`) | "command required" if none (`:79`); TTY notices | no | propagates remote exit code (`exec.go:123,289`); interactive TTY only when stdin+stdout TTY (`:103`) |
| `init` | `init` / `NoArgs` (`init.go:25`) | none | embedded YAML template (`init.go:27`) | — | n/a | 0/1 |
| `install` | `install` / `NoArgs` (`install.go:24,50`) | `--wait` (true), `--timeout` (5m), `--dry-run` (false), `-f/--values` (StringArray), `--set` (StringArray), `--set-string` (StringArray) (`install.go:121-126`) | plan / ready/installed (`install.go:156,192,196`) | step progress (`install.go:104,174,184`) | no | 0/1; refusal→1 w/ guidance (`:177,218`); `--wait`+`timeout<=0`→err (`:114`) |
| `wallet` | `wallet` / `NoArgs` (`wallet.go:46,49`) | none (parent; prints help) | help | — | n/a | 0 |
| `wallet list` | `list NET` / `ExactArgs(1)` (`wallet.go:114,117`) | `--json` (false) (`wallet.go:136`) | table or JSON (`wallet.go:130,133`) | target announce (`wallet.go:92`) | yes | 0/1 |
| `wallet add` | `add NET` / `ExactArgs(1)` (`wallet.go:146,149`) | `--name` (""), `--topup` (""), `--await` (false), `--await-timeout` (2m), `--json` (false), `--ogmios-url` (""), `--kupo-url` ("") (`wallet.go:214-219`) | created wallet + funding or JSON (`wallet.go:209,211`) | target announce; "Waiting..." (`wallet_fund.go:146`) | yes | 0/1; `--await` w/o `--topup`→err (`:162`); bad `--await-timeout`→err (`:165`) |
| `wallet topup` | `topup NET WALLET LOVELACE` / `ExactArgs(3)` (`wallet.go:229,232`) | `--from` (""), `--await` (false), `--await-timeout` (2m), `--json` (false), `--ogmios-url` (""), `--kupo-url` ("") (`wallet.go:277-281`) | funding result or JSON (`wallet.go:271,274`) | target announce; "Waiting..." | yes | 0/1; bad LOVELACE→err (`:387`); bad `--await-timeout`→err (`:242`) |
| `wallet export` | `export NET WALLET` / `ExactArgs(2)` (`wallet_export.go:25,28`) | `--out` (""), `--force` (false) (`wallet_export.go:64-65`) | (none) — keys never on stdout | written paths (`wallet_export.go:61,183`) | no | 0/1; existing file w/o `--force`→err (`:141`) |
| `wallet remove` | `remove NET WALLET` / `ExactArgs(2)` (`wallet.go:289,292`) | none | "Removed wallet ..." (`wallet.go:318`) | target announce | no | 0/1; faucet reserved→err (`:303`); not-found→err (`:310`) |

Strengths: args validators are consistent and DNS-1123-validated (`identity.go:24-34`); NAME defaults the namespace to NAME (`identity.go:28-31`), so identity is positional+`-n`, never read from the env file (`doc.go:7-9`).

Inventory-level inconsistencies:
- **`--json` is per-command and incomplete** — 5 of ~19 leaf commands (`list`, `info`, `wallet list/add/topup`). `up`, `down`, `devnet status`, `install`, `connect`, `run`, `exec`, `wallet remove/export` have no machine mode despite `devnet status`/`install`/`connect` emitting structured-ish text. It is re-declared on each command (six `Bool("json", false, …)`), not shared/persistent.
- **`-f` is overloaded** — `up -f`/`--file` is a developer environment file (`up.go:117`); `install -f`/`--values` is a repeatable Helm values file (`install.go:124`). Same shorthand, different semantics/arity across siblings.
- **Required positional vs required flag mismatch** — `up` enforces `--file` via a runtime error (`up.go:39`), not `MarkFlagRequired`, so `--help` is not truthful; same for the manual "command is required" in `exec` (`:79`).
- **No completion / sparse examples** — only `run` and `exec` set `Example`/`Long`. No `ValidArgsFunction`/`RegisterFlagCompletionFunc` anywhere, so no dynamic completion for the obvious operands (network names, wallet names, namespaces).
- **`devnet` rejects explicit target; siblings accept it** — `devnet*` errors on `--kubeconfig/--context` (`target.go:69-80`), while `install` and the network verbs accept them. A discoverability cliff for the same global flags.
- **Destructive ops have no confirmation/`--yes`** — `down`, `devnet down`, `wallet remove` delete resources with no prompt and no `--dry-run`, while `up`/`install` are previewable. Teardown asymmetry.

## 3. Output routing & stdout/stderr violations

Emission mechanisms: direct `fmt.Fprintf/Fprintln` to `ctx.out`/`ctx.err`; `infoWriter` (sticky-error writer, `info_print.go:12-33`, constructed per call site against out or err); `stepReporter` (always bound to stderr, `devnet.go:165`, `install.go:104`). The `slog` logger always writes to stderr (`config.go:122-141`); its only real call site is `up.go:96`.

Correctly routed today (representative): `init.go:27` template→out; `up.go:70,74` dry-run manifest→out; `list.go:55,170-179` data→out; `info.go:53,59`→out; `connect.go:188-194` `KEY=URL` lines→out; all of `up.go`/`down.go` progress/status→err; `wallet_export.go:184-187` written-paths notice→err (keeps keys off stdout); `target.go:101`, `orphan.go:77`, both `stepReporter` sites→err.

Defensive guard worth preserving: `redirectStdoutToStderr()` (`wallet_fund.go:193-198`) temporarily repoints process `os.Stdout`→`os.Stderr` during tx submit because Apollo's `OgmiosChainContext` does a hardcoded `fmt.Printf` to real stdout that would corrupt `--json`. Evidence the codebase already treats stdout as sacred.

Actual violations — human chatter on STDOUT (all write `commandContext.out`):

1. **`devnet.go:191-224` `printDevnetUp` — entire block is human prose on stdout.** "devnet is ready.", the Cluster/Operator/Ogmios/Kupo/Wallet summary, the `--bare` hint (`:198`), and the "Try:" hint block (`:219-221`) printing `yacd exec …` / `yacd devnet down`. No `--json` mode at all.
2. **`devnet.go:240-275` `printDevnetStatus` — human status on stdout,** including the empty-state hint (`:243`) and decorated `(running)`/`(healthy)`/`(ready)` flags. No `--json`.
3. **`devnet.go:104` "devnet cluster removed." on stdout** — confirmation chatter; the parallel `down.go` confirmations correctly go to stderr.
4. **`install.go:192,196` "Operator … ready/installed …" on stdout** — success confirmation, not data; no `--json`.
5. **`install.go:156` "Plan: install operator …" on stdout** — human-formatted plan; arguably the dry-run output, but prose with no machine shape.
6. **`wallet.go:318` "Removed wallet … " on stdout** — confirmation chatter; nothing script-useful and no `--json`.
7. **`list.go:160-167` "No CardanoNetworks found[ in namespace x]." on stdout** — empty-result prose interleaved with the data stream; a script doing `yacd list | wc -l` gets a bogus line (the JSON path correctly emits `[]`).
8. **`wallet.go:439` "No managed wallets." on stdout** — same empty-result-as-prose problem.
9. **`connect.go:188,195` header/footer prose on stdout** — "Forwarding NET (namespace X):" and "Wrote PATH — Ctrl-C to disconnect." frame the eval-able `KEY=URL` lines on the same stream, so `eval "$(yacd connect …)"` cannot work.

Net pattern: the stdout=data / stderr=diagnostics contract is followed by `up`/`down`/`wallet export` but broken by `devnet`/`install`/`list`/`wallet list`/`wallet remove`/`connect`. "Did it succeed" lands on stdout for some verbs and stderr for others — inconsistent for scripts.

## 4. Config, flags & cobra-viper gaps

Core wiring matches the cobra-viper "Default Production Pattern": instance Viper, binding in `PersistentPreRunE`, `RunE` + `SilenceUsage`/`SilenceErrors` + `ExecuteContext` with `signal.NotifyContext`. Dependency versions are current (cobra `v1.10.2` `go.mod:15`, viper `v1.21.0` `go.mod:17`, pflag `v1.0.10` `go.mod:16`, `x/term v0.43.0` `go.mod:20`). Good practices: `bindFlag` nil-guard fails fast on a renamed flag (`config.go:75-83`); logger correctly sinks to `ctx.err`; `-n` is the only short flag.

Footguns:
1. **Resolved config is recomputed, never cached.** Every command re-calls `loadRuntimeConfig(commandContext.viper)` — 13 sites (connect.go:50, list.go:26, run.go:39, up.go:28, down.go:21, target.go:70, exec.go:69, wallet.go:79, info.go:22, install.go:52, plus root.go:111). `PersistentPreRunE` already computes it (`root.go:111`) to build the logger but discards the `RuntimeConfig` (only `ctx.logger` is retained; `commandContext` has no `runtimeConfig` field, `options.go:153-172`). Enum validation runs twice; the inverse of "load once."
2. **Shared-viper bare-key collisions.** `BindPFlags(cmd.Flags())` (`config.go:65`) binds each active subcommand's local flags into the process-shared Viper under bare keys. With `AutomaticEnv`, `YACD_JSON=true` silently flips machine output on for every command defining a `json` flag; the same latent aliasing applies to reused keys `wait`, `timeout`, `dry-run`, `force`, `await`, `bare`, `allow-mainnet`.
3. **No config-file support at all.** `initializeConfig` never calls `SetConfigName`/`AddConfigPath`/`ReadInConfig` (`config.go:40-70`). Precedence is only `flag > env > default`; the skill's `flag > env > config > default` chain is truncated. No `--config` flag.
4. **`--context` → `YACD_KUBE_CONTEXT` mapping is load-bearing but undocumented.** Flag name `context` (`root.go:125`), Viper key `kube-context` (`config.go:53`), reads `vp.GetString("kube-context")` (`config.go:93`); asserted only in `rejectExplicitTarget`'s error message (`target.go:77`). No test covers the env→key path.
5. **`install` reads `--values/--set/--set-string` directly off `cmd.Flags()`** (`install.go:64-72`) to dodge Viper's StringArray comma-mangling, so those three flags do not participate in env/precedence unlike every other flag. Deliberate but undocumented.
6. **Thin precedence test coverage.** `root_test.go` covers only `--version` (`root_test.go:12-30`); nothing exercises `YACD_*` binding, flag>env precedence, enum-validation rejections, or the `bindFlag` nil-guard.

Missing global UX capabilities:

| Capability | Status | Evidence |
|---|---|---|
| Global TTY detection | Absent (only local to `exec`) | exec.go:152-165; no global `isTerminal` on `commandContext` |
| `-v` verbosity counting | Absent | only string `--log-level` (root.go:127); no `Count`/`CountP` |
| `--quiet`/`-q` | Absent | grep = none |
| `--non-interactive` | Absent | interactivity inferred ad hoc only in exec (exec.go:103) |
| `--color`/`--no-color` | Absent | no color subsystem; no NO_COLOR handling |
| Config-file support | Absent | config.go has no ReadInConfig/AddConfigPath/--config |
| Global `--json` | Per-command only | 5 separate `Bool("json", …)` defs |
| Output writer respecting quiet/json | Absent | each command hand-rolls `fmt.Fprintf` (list.go:55, up.go:78, connect.go:92, devnet.go:66) — no central renderer |

`RuntimeConfig` (`config.go:17-35`) is the natural home for new global fields (Quiet, JSON, Color, Interactive, Verbosity); `commandContext` (`options.go:153-172`) is the natural home to cache the resolved config plus a TTY/color-aware writer — which also fixes footgun #1. The logger sink (`ctx.err`) is correct and should stay; `--quiet` should gate human status lines, not the structured logger; `--json` should gate the payload renderer.

## 5. Long-running operations & progress

No progress UI primitives exist: no spinner, no progress bar, no TTY-gated rendering. Every long-running op reports via plain line-buffered `fmt.Fprintf` to stderr. The only TTY detection in the whole CLI is `exec.go:153,164,176` (raw-mode interactive shells), orthogonal to output styling.

Two indeterminate wait engines underlie all polling: `wait.PollUntilContextTimeout` — `kube.WaitReady`/`WaitGone` (`kube/wait.go:34,96`, 2s) and `awaitConfirmation` (`wallet_await.go:63`, 1s); `wait.PollUntilContextCancel` — `operator.WaitForReady` (`operator/ready.go:23`, 3s). The k3d provisioner runs `k3d cluster create --wait` as a subprocess whose stdout/stderr are **captured into buffers, not streamed** (`exec/exec.go:30-32`; invoked from `cluster/k3d/ensure.go:72`), so during the single longest wait the user sees one static `"==> Ensuring local cluster …"` line then nothing.

Per-operation summary:

| Operation | Feedback today | Wait / seam | Progress type |
|---|---|---|---|
| devnet up (sequence) | `stepReporter` lines→stderr (`devnet.go:75-135`) | `lifecycle.Reporter` (the seam) | Indeterminate; **could be determinate** (3-4 known phases) |
| ↳ cluster ensure/create | one Step line then silent | k3d subprocess, output buffered (`exec.go:30`, `ensure.go:72`); Reporter not threaded in | Indeterminate; **longest blind wait** |
| ↳ operator install + ready | Step/Substep lines | `operator.WaitForReady` 3s (`ready.go:23`) | Indeterminate |
| ↳ network apply + ready | Step/Substep/Done lines | `kube.WaitReady` 2s (`wait.go:34`) | Indeterminate |
| devnet down | Step/Done + restore notes (`manager.go:217-237`) | k3d delete subprocess, buffered (`ensure.go:131`) | Indeterminate |
| up --wait | bare `fmt.Fprintf` "Waiting…"/"is ready." (no Reporter) | `kube.WaitReady` (`up.go:105`); default 12m | Indeterminate |
| install --wait | `stepReporter` (reuses seam, `install.go:104,174,184`) | `operator.WaitForReady` 3s; default 5m | Indeterminate |
| wallet add/topup --await | single "Waiting up to %s…" line then silent (`wallet_fund.go:146`) | `awaitConfirmation` 1s Kupo poll; default 2m | Indeterminate |
| connect | foreground supervised loop; endpoints + reconnect/backoff notices (`connect.go:82-135`) | infinite loop, `time.After` 1→15s backoff | Indeterminate, **long-lived/foreground** — wants a persistent status line, not a finite spinner |
| run / exec | one "Connected…" line; child/in-pod owns stdio | port-forward + child exec / SPDY stream | N/A (child/interactive owns terminal) |

Attach seams: (1) `lifecycle.Reporter` is the primary seam — a TTY-aware impl (spinner per `Step`, freeze-to-check on `Done`) upgrades all of devnet up/down and install with no call-site changes; its comment even anticipates this. (2) The **k3d provisioner is the biggest gap** — the Reporter is not threaded into `cluster.Provisioner` and subprocess output is buffered; streaming k3d's own stderr would require changing the `exec.Runner` contract (`([]byte,[]byte,error)`, `exec/exec.go:27`). (3) `up.go` hand-rolls raw `fmt.Fprintf` (`up.go:97-110`) and should adopt the shared seam. (4) wallet `--await` wants a spinner around its poll, kept off stdout. (5) connect wants a live refreshing status line tied to its supervise/backoff loop.

Determinacy: all current waits are indeterminate → spinner-only per individual wait. The one place determinate progress is achievable is the **devnet up sequence as a whole** (a fixed ordered phase set known up front from `UpOptions.Bare`, `lifecycle.go:51-65`, `manager.go:99-101`) → a "step N of M" / checklist render.

## 6. Dependencies & module boundary

Single root `go.mod` (`go.mod:1`); the CLI lives under `cli/` in the same module (no separate module, no `replace`). Two `main` packages share the module: `./cmd` (operator/manager, built by `ko`, uses Kong) and `./cli/cmd/yacd` (the CLI, built by GoReleaser, `.goreleaser.yaml:7`, uses Cobra/Viper). They share one dependency set and one `go.sum`. The Helm chart is embedded via `charts/embed.go:18` (`//go:embed all:yacd`, `all:` prefix is load-bearing) and consumed only by the CLI path (`root.go:9`); the manager does not embed it.

Current state: **no charm/TUI deps anywhere** — no lipgloss/huh/bubbletea/bubbles, no fatih/color/tablewriter/pterm. CLI output is stdlib-only: `fmt`, `text/tabwriter` (`list.go:8`), `encoding/json`, `log/slog` (`config.go:122-140`). No color, styling, or interactive prompts.

Manager import-graph baseline to preserve: `go list -deps ./cmd | grep -iE "charm|lipgloss|huh|bubble|ogmigo|kugo|internal/cardano/tx"` returns **zero matches**. The manager does legitimately import other cardano subpackages (`internal/cardano/{dbsync,networkartifacts,primarypod,toolsimage,localnet,publicnet,wallet}`) and apollo crypto/serialization subpackages, but NOT `internal/cardano/tx`, ogmigo, or kugo (those are CLI-only). So the boundary to protect is specifically: keep charm/TUI, ogmigo, kugo, and `internal/cardano/tx` out of `./cmd` — not all of apollo/cardano.

Adding charm implications: `charm.land/lipgloss/v2 v2.0.2`, `charm.land/huh/v2 v2.0.3`, `charm.land/log/v2 v2.0.0` all resolve on the proxy. lipgloss + log are light; **huh is the cost center** — it drags in the full Bubble Tea v2 stack (~20 modules: bubbletea/bubbles v2, ultraviolet, colorprofile, harmonica, the `charmbracelet/x/*` family, go-colorful, go-runewidth, cancelreader, uniseg, go-udiff). Because the module is shared, these land in the module's `require`/`go.sum` regardless of which binary uses them, but Go links only reachable packages per-binary, so the distroless **manager binary size is unaffected** as long as nothing reachable from `./cmd` imports them; the shared `go.sum`/supply-chain surface grows for both.

Containment plan: confine all charm imports to CLI-only packages under `cli/internal/` (e.g. a new `cli/internal/ui` / `cli/internal/render` and route CLI logging through `charm.land/log/v2`); never import charm from any package reachable by `./cmd` or from `charts`. Keep the manager on Kong + slog→logr untouched. huh's Bubble Tea dependency means interactive prompts must be opt-in / TTY-gated. Enforce with a CI tripwire asserting `go list -deps ./cmd` stays free of `charm|lipgloss|huh|bubble|ultraviolet|colorprofile` (and continues to exclude `ogmigo|kugo|internal/cardano/tx`), consistent with existing version tripwires (commit `13122b6`).

## 7. Tests & determinism

The suite builds the full tree via `NewRootCommand(Options{...})` with `bytes.Buffer` streams and a fresh `viper.New()` per test, then `SetArgs` + `ExecuteContext` (canonical: `list_test.go:69-76`, `info_test.go:37-44`, `up_test.go:23-33`, `wallet_test.go:98-105`; `devnet_test.go:32-57` adds a `newDevnetRoot` helper bundling four port mocks). The factory-on-`Options` design is the suite's main strength — exactly the testable seam the skill recommends. The suite is already deterministic by construction: instance Viper (no process-global), `bytes.Buffer` streams (no real TTY), `t.Setenv` for env paths (23 uses) with explicit isolation of ambient `YACD_*` vars, `t.TempDir` for filesystem. There is **no golden-file harness, no `TestMain`, and no color/spinner/TTY/TUI machinery**.

Assertion style: substring dominates — **42 `assert.Contains/NotContains`** on `*.String()` vs **only 4 exact-equality** stream assertions (`root_test.go:28` exact `--version` line + empty-stderr; `list_test.go:134,152` exact empty-state sentences; `run_test.go:99` exact `ws://localhost:1337`). A few decode rather than match (`list_test.go:177` `json.Unmarshal`; `info_test.go:47-55` asserts JSON *substrings*, brittle to `MarshalIndent` reformatting).

Restyle fault lines:
1. **Exact-match human lines** — `list_test.go:134,152` (full-buffer equality, most fragile), `root_test.go:28-29`.
2. **Phrase-coupled substrings** — `up` "Dry run: rendered…"/"Applied CardanoNetwork…"/"is ready."/"Warning: rendering mainnet…" (`up_test.go:43,65,87-88,217,250,275`); `devnet` "devnet is ready."/"No network applied"/"devnet cluster removed."/"Run \`yacd devnet\`"/"cleared stale state" (`devnet_test.go:104,128,142,172,185`); `wallet` "Removed wallet"/"Confirmed on-chain." (`wallet_test.go:604,714`); the embedded "Try:" hint `cardano-cli query tip --testnet-magic 42` (`devnet_test.go:112` from `devnet.go:219-220`).
3. **Stream-routing asserts** — `up` chatter on stderr (`up_test.go:43,65,217`); `wallet export` keys must NOT hit stdout (`wallet_test.go:776`); `devnet status` "cleared stale state" on stderr while hint on stdout (`devnet_test.go:185-186`).
4. **Table headers/columns** — `list` `NAME/NAMESPACE/MODE/READY/ENDPOINTS` + cell tokens (`list_test.go:78-89`); plain `text/tabwriter` today (`list.go:170`, `wallet.go:451`).
5. **JSON contamination** — any color/spinner bytes on stdout break `list_test.go:177` unmarshal and the Chainsaw e2e.

Live + e2e contracts: `TestDevnetLifecycleLive` (`devnet_live_test.go:30-33`, gated on `YACD_DEVNET_LIVE`) runs real production factories and asserts on real stdout phrases ("devnet is ready.", "addr_test" prefix, "devnet cluster removed."); isolates state via `t.Setenv("KUBECONFIG"/"XDG_STATE_HOME", tempdir)`. Two Chainsaw steps shell out to the CLI: `manager-smoke/chainsaw-test.yaml:193` (`yacd up …`, **depends only on exit 0**, so restyling stderr is safe; a non-zero exit or prompt-hang is not) and `chainsaw-test.yaml:545-557` (`yacd … info … --json` → `json.loads` asserting `name`/`namespace`/`endpoints.ogmios.url`/`endpoints.kupo.url`/Ready condition — **strict machine contract: stdout must be pure JSON**; `--json` field names declared stable at `info.go:67-69`).

Determinism gaps an overhaul must close: no NO_COLOR/TTY-off discipline (none needed yet); the `bytes.Buffer` streams are non-`*os.File`, so a writer-derived TTY check reports non-TTY in tests — **but only if the renderer derives color/TTY from the injected writer, not `os.Stdout`** (lipgloss/charm default to a global stdout-derived profile, so `lipgloss.NewRenderer(out)` plus honoring `NO_COLOR`/`--no-color` is required; tests should also set `t.Setenv("NO_COLOR","1")`). Preserve the `redirectStdoutToStderr` guard (`wallet_fund.go:193`). No structured output seam beyond `--json` on `info`/`list`, which is why ~50 assertions substring-match prose; adding stable machine modes everywhere lets assertions decode rather than match. No `TestMain` exists — add one (or a `t.Cleanup` helper) if a global style/profile reset is needed.

## 8. Cross-cutting overhaul implications

- **Centralize output routing.** Replace ad-hoc `ctx.out`/`ctx.err` + per-call-site `infoWriter`/`stepReporter` with a small printer/UI abstraction (data-writer vs message-writer vs progress-reporter) so the stdout=data / stderr=diagnostics contract is enforceable, not per-call-site discipline.
- **Promote `--json` (likely `--output=table|json|yaml`) to a persistent root flag** with one shared renderer; give every read/result command a machine mode (`devnet up/status`, `install`, `connect`, `wallet remove/export`). This also closes the `YACD_JSON` collision (footgun #2) and lets tests decode instead of substring-match.
- **Move all human chatter off stdout uniformly** — `printDevnetUp`/`printDevnetStatus`/"Try:" hints, `devnet`/`install`/`wallet remove` confirmations, and the "No … found." empty-result sentences to stderr; separate `connect`'s eval-able `KEY=URL` data from its human frame (or add an explicit `--export`/eval mode).
- **Cache resolved `RuntimeConfig` on `commandContext`** in `PersistentPreRunE`; delete the 13 `loadRuntimeConfig` re-calls. Add global persistent `--quiet/-q`, `--non-interactive`, `--color/--no-color`, `-v` count as `RuntimeConfig` fields; lift the `exec.go` TTY helper to a shared seam so color/interactive defaults derive once.
- **Add config-file support** (`--config`, `SetConfigName`/`AddConfigPath`/`ReadInConfig` ignoring only `ConfigFileNotFoundError`) to restore the full `flag > env > config > default` chain.
- **Implement a TTY-aware `lifecycle.Reporter`** (spinner per `Step`, persisted on `Done`, plain line-printer fallback for non-TTY/CI); thread it into the k3d provisioner (or stream its stderr — the one structural change at the `cluster.Provisioner`/`exec.Runner` boundary) since cluster-create is the longest blind wait; unify `up` onto the same seam; render the devnet up sequence as a determinate checklist. connect gets a distinct live refreshing status line.
- **Normalize the `-f` overload, declare runtime "required" checks** (`up --file`, `exec` command) so `--help` is truthful, add `Example`/`Long` and shell completion for network/wallet/namespace operands, add confirmation + `--yes` (TTY-gated) to destructive verbs, and introduce richer exit codes (not-found / not-ready / refused / timeout) so the tool is scriptable beyond 0/1.
- **Keep styling strictly TTY-gated and off the stdout data path.** Confine charm deps to CLI-only packages; preserve `redirectStdoutToStderr`, the `slog`→stderr diagnostic channel, the exit-code policy, and the writer-injection seam; add a CI tripwire keeping `./cmd` free of charm/ogmigo/kugo/`internal/cardano/tx`. Force styling off deterministically in tests (writer-derived profile + `NO_COLOR`).

## Gaps vs the 8 goals (prioritized)

| # | Goal | Status | Severity | Key evidence | Required work |
|---|---|---|---|---|---|
| 1 | Clean stdout/stderr split (data vs diagnostics) | Partial — broken for 9 sites | **P0** | devnet.go:191-224, 240-275, 104; install.go:156,192,196; list.go:160-167; wallet.go:318,439; connect.go:188,195 | Move all human prose/confirmations/empty-result lines to stderr; isolate connect `KEY=URL` data |
| 2 | Stable machine-readable mode everywhere | Partial — 5 of ~19 leaves | **P0** | list/info/wallet list-add-topup only; devnet/install/connect/run/exec/wallet remove-export have none | Promote persistent `--json`/`--output`, single renderer; cover all read/result verbs |
| 3 | Consistent flags/args & truthful help | Partial | **P1** | `-f` overload (up.go:117 vs install.go:124); runtime-required `up --file` (up.go:39), `exec` cmd (exec.go:79); no completion/examples except run/exec; devnet rejects target (target.go:69-80) | Resolve `-f`, `MarkFlagRequired`, add completion + Example/Long, reconcile target handling |
| 4 | Config precedence & global UX flags | Partial — chain truncated | **P1** | no config file (config.go:40-70); no `--quiet`/`-v`/`--color`/`--non-interactive`; `--context`→`YACD_KUBE_CONTEXT` undocumented; install direct-flag-read (install.go:64-72) | Add config-file tier + `--config`; add global verbosity/quiet/color/interactive on RuntimeConfig |
| 5 | Progress UX for long-running ops | Absent | **P1** | no spinner/bar/TTY-gating; k3d output buffered (exec.go:30, ensure.go:72); only stepReporter lines | TTY-aware `lifecycle.Reporter`; thread into k3d provisioner; unify `up`; checklist for devnet up; live line for connect |
| 6 | Config caching & viper hygiene | Footguns present | **P2** | 13 `loadRuntimeConfig` re-calls; shared-viper bare-key collisions (YACD_JSON etc.); RuntimeConfig discarded (root.go:111) | Cache config on commandContext; namespace/per-command viper keys |
| 7 | Test determinism vs restyle | Fragile — ~50 phrase asserts | **P2** | 4 exact-equality + 42 Contains; Chainsaw `info --json` strict (chainsaw-test.yaml:545); TestDevnetLifecycleLive phrases | Writer-derived color profile + NO_COLOR; migrate asserts to decode structured modes; consider golden harness/TestMain |
| 8 | Module boundary & dependency hygiene | Clean baseline to defend | **P2** | `./cmd` free of charm/ogmigo/kugo/internal-cardano-tx (zero matches); single shared go.mod; huh = heavy (~20 modules) | Confine charm to cli/internal/*; CI import tripwire on `./cmd`; decide huh vs lipgloss+log only |
