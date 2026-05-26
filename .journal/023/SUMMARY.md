---
id: 023
title: cli package refactor (readability + hexagonal + mockery)
date: 2026-05-26
status: complete
repos_touched: [yacd]
related_sessions: [018, 019, 020, 021, 022]
---

## Goal
Bring every package under `cli/` (cmd/yacd, internal/cli, internal/devconfig,
internal/kube, internal/render) up to the readability /
maintainability / architectural-purity rubric applied in sessions 020
(ctrlkit), 021 (cardanonetwork), and 022 (cardanodbsync). Strictly
behavior-preserving. Introduce mockery + Testify as a side effect since the
go-testing skill mandates them and the repo had zero prior usage.

## Outcome
Goal met. PR #41 merged as `939219c` with squash title
`refactor(cli): split packages, tighten godoc bar, typed conditions,
mockery migration`. CI green and Kusari Inspector clean before merge. Master
fast-forwarded; `refactor/cli-packages` worktree and branch removed; dev
stack stopped via `moon run root:dev-down`.

Diff: 35 files changed, +2146 / -1456. The `cli/` LOC total went up slightly
(roughly +700 net, mostly godocs and split-file headers + the new mocks
package), but per-file mass dropped: `root.go` 234→90, `info.go` 317→181,
`topup.go` 301→184, `kube/client.go` 164→133. Eight new focused files in
`cli/internal/cli` plus two in `cli/internal/kube`.

## Key Decisions
- **Single PR, not two.** Session 021 split into PRs #38 + #39; session 022
  did it as one. This pass followed 022 because the typed condition
  vocabulary cascades through the file split, mockery introduction, AND
  the Rule 7 constructor change — intermediate states would not have
  compiled cleanly. The diff is large but every commit is
  behavior-preserving.
- **Port stays in `kube/`, constructor returns concrete.** User picked the
  pragmatic shape over the strict "port in higher-up package" reading of
  Rule 7. `kube.NewClient` now returns `*kube.Adapter` (concrete);
  `runtimeClient` was renamed `Adapter`; the cli factory wrapper bridges
  to the `kube.Client` interface. The interface stays in `kube/` next to
  its sole adapter — standard Go layout, less churn at callers.
- **Mockery introduced + migrated.** First mockery and first programmatic
  Testify usage in the repo. Pinned at v3.7.0 through proto
  (`.moon/proto/mockery.toml` + `.prototools`). `.mockery.yml` at repo
  root targets `kube.Client` and `cli.HTTPDoer` with output at
  `cli/internal/mocks/`. The Moon task `root:generate` was extended.
  Hand-rolled `fakeKubeClient` / `fakeHTTPClient` deleted; tests use
  `mocks.Client` / `mocks.HTTPDoer` with the `EXPECT()` builder.
- **Test-file split mirrors the source split.** `root_test.go` (741 LOC)
  decomposed into `root_test.go` (TestVersion only), `deploy_test.go`,
  `info_test.go`, `topup_test.go`, and `testhelpers_test.go`. Each behavioral
  case in the prior matrix carried over without mutation.
- **`staticClient` kept hand-rolled in `kube/wait_test.go`.** The pure
  WaitReady tests poll repeatedly without setting up per-call mock
  expectations; a generated mock would error on the duplicate get.
  Documented inline.
- **`HTTPDoer` exported (was `httpDoer`).** Required for mockery to
  generate against it. The CLI's public surface gains one type, which is
  intentional — it's already part of the public `Options` field anyway.
- **Typed condition vocabulary in `kube/`.** New `ConditionType` alias
  plus `ConditionReady` / `ConditionDegraded` / `ConditionFaucetReady`
  constants in `kube/conditions.go`. `FreshCondition` retyped;
  `topup.go`'s three bare strings replaced. Mirrors the cardanonetwork
  and cardanodbsync controller patterns.
- **Sticky-error writer for `info` printers.** The six `print*` helpers
  used to carry a 4-line `if _, err := fmt.Fprintf(...); err != nil
  { return fmt.Errorf("write info: %w", err) }` ladder per write. Replaced
  with a small `infoWriter` (10 LOC) that captures the first error and
  no-ops subsequent calls. The six helpers shrank to their actual
  formatting logic.
- **Security comments on `validateFaucetURLTrust`.** Per session 022's
  lesson about security-load-bearing godocs, the function now carries a
  paragraph naming three attack vectors (token exfiltration to
  attacker-supplied URL host, accidental non-loopback exposure, plaintext
  eavesdropping) plus per-check inline annotations. The test
  `TestTopUpRequiresTrustForRemoteCustomFaucetURLBeforeReadingSecret`
  preserves the no-token-leak invariant via `mock.AssertNotCalled` (was
  `secretReadCount` counter on the hand-rolled fake).
- **Explicit non-goals.** Rejected from this pass and noted in the plan:
  renaming `FreshCondition`/`Options`/`RuntimeConfig`, unexporting
  `Environment.Metadata`/`Spec`, typed `LogLevel`/`LogFormat` enums,
  replacing function-typed `KubeClientFactory`/`KubeNamespaceResolver`
  with typed interface ports.

## Changes
- `cli/internal/cli/doc.go` - NEW. Package contract naming the
  command-tree + commandContext separation and the pure/side-effect
  split.
- `cli/internal/cli/root.go` - SLIM. Now only `NewRootCommand` + tree
  wiring + persistent-flag declarations. Default `KubeClientFactory` is
  the wrapper that bridges `kube.NewClient`'s `*Adapter` return to the
  `kube.Client` port.
- `cli/internal/cli/options.go` - NEW. `BuildInfo` + `withDefaults`,
  `Options`, `commandContext`, `HTTPDoer` (exported, was `httpDoer`),
  `KubeClientFactory` and `KubeNamespaceResolver` function-typed seams.
- `cli/internal/cli/config.go` - NEW. `RuntimeConfig`,
  `initializeConfig`, `bindFlag`, `loadRuntimeConfig`, `newLogger`.
  Carries a "why" comment on the viper env-key replacer.
- `cli/internal/cli/deploy.go` - GODOC. Adds the "why" comment for the
  dual-namespace branch (dry-run vs apply).
- `cli/internal/cli/info.go` - SLIM. Subcommand factory + `newInfo`
  decoder + `endpointInfo` + the six `*Output` DTO types. Six print
  helpers moved out.
- `cli/internal/cli/info_print.go` - NEW. `infoWriter` sticky-error
  helper + `printInfo` orchestrator + six section helpers.
- `cli/internal/cli/topup.go` - SLIM. Subcommand factory +
  `requireFaucetReady` + `publishedFaucetURL`. Pure trust validation
  and HTTP transport moved out. The bare `"Ready"` / `"Degraded"` /
  `"FaucetReady"` strings became `kube.ConditionReady` /
  `kube.ConditionDegraded` / `kube.ConditionFaucetReady`. Carries a
  "security-relevant default" inline comment on the `faucetURL == ""`
  fallback.
- `cli/internal/cli/topup_trust.go` - NEW. Pure `validateFaucetURLTrust`,
  `parseHTTPURL`, `sameFaucetURL`, `isLoopbackHost`. Paragraph-level
  security comment naming the three attack vectors; per-check inline
  annotations naming which vector each check defends.
- `cli/internal/cli/topup_transport.go` - NEW. `postTopUp`,
  `decodeFaucetError`, `topUpHTTPPayload`, `topUpHTTPResult`,
  `faucetErrorResponse`, `faucetAuthTokenKey`.
- `cli/internal/cli/root_test.go` - SLIM. Only `TestVersionFlagPrintsBuildMetadata`.
- `cli/internal/cli/deploy_test.go` - NEW. Deploy cases on mockery + Testify.
- `cli/internal/cli/info_test.go` - NEW. Info case on mockery + Testify.
- `cli/internal/cli/topup_test.go` - NEW. All topup cases on mockery + Testify,
  with `mock.AssertNotCalled` preserving the no-token-leak invariant.
- `cli/internal/cli/testhelpers_test.go` - NEW. `writeTempConfig`,
  `readyNetwork`, `kubeClientFactory`, `newKubeMock`, `newHTTPMock`.
- `cli/internal/devconfig/doc.go` - NEW. Package contract.
- `cli/internal/devconfig/config.go` - GODOC. Every exported type / field
  / function / constant has a godoc; `Load` carries a "why" comment
  explaining the two-pass validation; `validateExplicitFields` has a
  godoc naming the UnmarshalStrict-vs-Validate-vs-explicit-fields
  contract.
- `cli/internal/devconfig/config_test.go` - Testify migration.
- `cli/internal/kube/doc.go` - NEW. Package contract.
- `cli/internal/kube/client.go` - REFACTOR. `Client` interface (port) +
  `Adapter` struct (renamed from `runtimeClient`) + adapter methods.
  `NewClient` returns `*Adapter` per Rule 7. Every method has a godoc.
- `cli/internal/kube/config.go` - NEW. `Config` + `newClientConfig` +
  `restConfig` (unexported, single caller) + `DefaultNamespace` +
  `defaultNamespace` const + `fieldOwner` const.
- `cli/internal/kube/conditions.go` - NEW. `ConditionType` typed alias +
  `ConditionReady` / `ConditionDegraded` / `ConditionFaucetReady` typed
  constants + `FreshCondition` retyped against the alias.
- `cli/internal/kube/wait.go` - REFACTOR. Uses the typed condition
  constants; carries a "why" comment on the observed-generation
  staleness check.
- `cli/internal/kube/client_envtest_test.go` - Testify migration;
  `Adapter` rename absorbed; `staticClient` carries a godoc explaining
  why it stays hand-rolled.
- `cli/internal/render/doc.go` - NEW. Package contract naming the
  pure-no-I/O contract.
- `cli/internal/render/render.go` - GODOC. Every exported function +
  constant has a godoc.
- `cli/internal/render/render_test.go` - Testify migration.
- `cli/internal/mocks/client.go` - NEW (generated). Mockery v3 `testify`
  template; `Client` mock with `EXPECT()` builder.
- `cli/internal/mocks/http_doer.go` - NEW (generated). Mockery v3
  `testify` template; `HTTPDoer` mock.
- `cli/cmd/yacd/main.go` - GODOC. Package doc; godocs on `run` and the
  linker-injected vars.
- `.mockery.yml` - NEW. Repo-root mockery v3 config targeting the two
  ports with output at `cli/internal/mocks/`.
- `.moon/proto/mockery.toml` - NEW. Proto plugin for mockery (community
  plugin from `crashdump/proto-tools`).
- `.prototools` - mockery `=3.7.0` pinned.
- `moon.yml` - `root:generate` extended to run mockery, with the
  PATH-shim workaround for the proto go-shim arg-mangling bug.
- `go.mod` - `github.com/stretchr/objx v0.5.2` added as indirect (mock
  dependency); `k8s.io/utils` promoted from indirect to direct.

## Open Threads
- **Mockery + Testify migration in the controller packages** is the
  natural follow-up. This PR sets the precedent (`.mockery.yml` at repo
  root, mocks under `<pkg>/mocks/`, EXPECT() builder pattern in tests),
  but the controller and ctrlkit tests still use hand-rolled fakes. A
  future sweep will likely follow the same shape per package.
- **Proto go-shim arg-mangling bug**: `golang.org/x/tools/go/packages`'
  internal `go list -f "{{context.GOARCH}} {{context.Compiler}}" -- unsafe`
  call gets word-split by the proto go shim, breaking mockery and any
  tool that uses x/tools to load packages. The Moon `root:generate` task
  works around it by prepending the direct toolchain bin to PATH. If
  proto fixes the shim, the workaround can be deleted. Reported neither
  upstream nor recorded — open thread on tooling stability.
- **Pre-existing INDEX.md gap for session 016** still present (carried
  over from sessions 020-022). Not material to this session; flagged.

## References
- PR #41: https://github.com/meigma/yacd/pull/41 (squash commit `939219c`)
- Plan file: `/Users/josh/.claude/plans/we-re-going-to-do-crystalline-curry.md`
- Reference precedents: `.journal/020/SUMMARY.md` (ctrlkit pass, PR #37),
  `.journal/021/SUMMARY.md` (cardanonetwork controller, PRs #38+#39),
  `.journal/022/SUMMARY.md` (cardanodbsync controller, PR #40)
- Session notes: `.journal/023/NOTES.md`

## Lessons
- **Proto's go shim breaks `golang.org/x/tools/go/packages`.** Symptom:
  `mockery` (or any package-loading Go tool) errors with `malformed
  import path "{{context.GOARCH}}"` and `"{{context.Compiler}}"`. Cause:
  the shim word-splits the templated `-f` argument that x/tools passes
  to `go list -f "{{context.GOARCH}} {{context.Compiler}}"`, so each
  fragment becomes a positional arg interpreted as a package path. Fix:
  invoke the tool with the direct Go toolchain bin prepended to PATH
  (`PATH="$(dirname $(proto bin go)):$PATH" mockery`). Worth knowing
  before the next mockery user lands on this.
- **Mockery v3's testify template needs a sticky-error-friendly mindset
  for tests that assert NO call.** `mocks.NewClient(t)` plus
  `mock.AssertNotCalled(t, "MethodName", mock.Anything, ...)` is the
  right primitive for preserving invariants like "GetSecretValue is
  never invoked when the trust check refuses". The prior hand-rolled
  `secretReadCount` counter pattern doesn't transfer; embrace the
  framework's own assertion API.
- **`go test ./...` outside Moon fails envtest.** Always use `moon run
  root:test` for kube envtest cases — it sets `KUBEBUILDER_ASSETS`
  through `setup-envtest`. Not a new lesson (it's in CLAUDE.md) but
  worth re-internalising during a test-migration session that touches
  the envtest file.
