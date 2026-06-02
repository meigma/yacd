# YACD Test Coverage Remediation Plan

A phased, **one-PR-per-phase** plan that takes the codebase from its current
coverage to **100% parity with `TEST_PLAN.md`** — every one of the 185
requirements `satisfied`. It is the action plan derived from
`TEST_COVERAGE_ANALYSIS.md`; read that first for the per-requirement audit.

## Starting point

| Status | Count |
|---|---|
| ✅ Satisfied (no work) | 74 |
| 🟡 Partial | 86 |
| 🔴 Not satisfied | 25 |
| **To close** | **111** |

## How to read this

Each phase below is **exactly one pull request** and carries:

- **Rows** — the `TEST_PLAN.md` requirement IDs it drives to `satisfied`.
- **Objective / Description / Approach** — what it proves and concretely how,
  grounded in the real files (the mechanism: unit / envtest / chainsaw / Go
  e2e / helm-template, and the existing pattern it mirrors).
- **Files** — repo paths to add or modify.
- **Success criteria** — observable "this PR achieved X" checks.
- **Exit criteria** — the merge gate (tests green, owned rows flipped, no flake).
- **Deps / Size / Risks**.

### Parity guarantee

The 12 phases **partition the 111 non-satisfied rows exactly** — every row is
owned by one phase, with no duplicates and no omissions (verified by set
arithmetic against the audit). The 74 already-satisfied rows need no work.
Completing all 12 phases therefore reaches 100%. Each phase's **Exit criteria**
include "owned rows flip to satisfied at their recommended Level," so parity is
checkable PR-by-PR.

### Sequencing & dependencies

Most phases are **independent** and can land in any order or in parallel across
contributors. The only hard edges:

- **P11 depends on P10** (the Go CLI e2e harness must exist before CLI verbs are
  driven against a live cluster).
- **P12 partially depends on P10** (its CLI-driven and real-binary bits reuse the
  harness; its leader-election bit does not).

Two **separate** E2E harnesses are involved — do not conflate them:

- **P9** extends the **existing Chainsaw** suite (`test/chainsaw/manager-smoke`,
  brought up by `.dev/scripts/test-e2e.sh`). It needs nothing new.
- **P10** adds a **new Go `test/e2e`** harness that drives the compiled `yacd`
  binary against a live cluster (port-forward / in-pod exec need a real kubelet).

Recommended order is by leverage and cost — cheap unit first, then admission
envtest, then reconcile envtest, then the E2E tier:

```
 cheap/independent            apiserver (envtest)         real cluster (E2E)
 P1  P2  P3  P4   ───────▶   P5  P6  P7  P8   ───────▶   P9 ── P10 ─▶ P11
                                                              └──────▶ P12
```

### Test-level reminder

`Unit` = no apiserver/cluster (table / mockery / httptest / testscript /
helm-template). `Env` = controller-runtime **envtest** (real apiserver, no
pods). `E2E` = packaged operator in **Kind/Chainsaw** or the **Go `test/e2e`**
harness (real pods, kubelet, networking). See `TEST_PLAN.md` → *Conventions* for
the full taxonomy. A few `E2E` rows are genuinely expensive (real public-network
sync, mainnet Mithril, real cardano-testnet binary output) and are explicitly
**gated/manual** in P12 rather than run on the per-PR CI lane.

## Phase summary

| Phase | PR title | Rows | Level | Size | Deps |
|---|---|---|---|---|---|
| P1 | `test(manager,chart): foundation + helm render/schema coverage` | 8 | Unit + Env | M | none |
| P2 | `test(faucet): cover HTTP negative paths and engine error/concurrency branches` | 12 | Unit | M | none |
| P3 | `test(cli): cover lifecycle/host-access/topup negative and JSON-output paths` | 16 | Unit | M | none |
| P4 | `test(config,tools,pins): cover devconfig negatives, cardano-tools verbs, pin integrity` | 16 | Unit | M | none |
| P5 | `test(api): add CardanoNetwork admission/defaulting envtest suite` | 11 | Env | M | none |
| P6 | `test(api): complete CardanoDBSync validation negatives, enums, and defaults` | 8 | Env | S | none |
| P7 | `test(cardanonetwork): manager-backed envtest for reconcile output, status and identity` | 12 | Env | M | none |
| P8 | `test(cardanodbsync): restore primary-sidecar manager envtest + db/placement reconcile coverage` | 12 | Env | L | none |
| P9 | `test(e2e): chainsaw GC-cascade, real Postgres/sidecar readiness, and metrics/health negatives` | 7 | E2E · Chainsaw | M | none |
| P10 | `test(e2e): add Go CLI e2e harness and root:test-e2e-cli task` | 0 (boilerplate) | E2E · Go harness (boilerplate) | M | none |
| P11 | `test(e2e): exercise yacd down/run/exec/connect against a live network` | 3 | E2E · Go harness | M | P10 |
| P12 | `test(e2e): gated real-binary and public-network coverage` | 6 | E2E · gated/manual | L | P10 (partial) |

## P1 — Manager foundation & Helm render/schema hardening  `M`

**PR:** `test(manager,chart): foundation + helm render/schema coverage`  
**Rows (8):** HLM-02, HLM-03, HLM-07, HLM-08, MGR-01, MGR-02, MGR-09, MGR-11  
**Level:** Unit + Env  ·  **Depends on:** none

**Objective.** Prove, with cheap non-cluster + envtest-foundation tests, that the packaged Helm chart actually ships the two CRDs and the metrics/leader-election RBAC and that values.schema.json gates valid vs invalid values, and that the manager's logging, metrics-disable, default-image wiring, and dual-controller registration behave as contracted. This closes the HLM render-guard hole (a CRD/RBAC drop would otherwise ship undetected) and flips four under-asserted MGR rows to their recommended Unit/Env levels.

**Description.** HLM-02/03/07/08 today rely only on the single Chainsaw manager-smoke and incidental Kyverno renders; there is no test whose purpose is to assert the chart packages its CRDs and RBAC or that the schema accepts/rejects values. MGR-02 only checks a substring common to both log formats; MGR-09 never feeds BindAddress="0"; MGR-11 never proves the manager flag reaches either reconciler field; MGR-01 only checks registerControllers returns nil and never confirms both controllers are registered/watching. This PR adds a new chart render/schema test file mirroring test/chart/rbac_test.go, extends the three cmd unit tests, refactors registerControllers into a testable buildControllers helper, and extends cmd/foundation_test.go to assert both controllers are wired through the manager.

**Approach.** HELM (Go tests in package chart_test, mirror test/chart/rbac_test.go helpers — run(), findObject(), findOptionalObject(), repoRoot() are already exported within the package). Add test/chart/render_test.go:
- HLM-02: run(t, repoRoot, "helm","template","yacd","charts/yacd","--namespace","yacd-system","--include-crds") — note --include-crds is REQUIRED because the chart keeps CRDs under charts/yacd/crds/ which `helm template` excludes by default (verified) — then findObject(rendered,"CustomResourceDefinition","cardanonetworks.yacd.meigma.io") and "cardanodbsyncs.yacd.meigma.io".
- HLM-03: default render (no --set), assert findObject for Deployment "yacd-controller-manager", Service "yacd-controller-manager-metrics-service", ClusterRole "yacd-metrics-auth-role" + ClusterRoleBinding "yacd-metrics-auth-rolebinding", ClusterRole "yacd-metrics-reader", Role "yacd-leader-election-role" + RoleBinding "yacd-leader-election-rolebinding" (exact names verified from the rendered output; RBAC names use yacd.fullname="yacd", not the controller-manager prefix).
- HLM-07: write a representative full valid values file to t.TempDir() (image.repository/tag/digest/pullPolicy, metrics.port in range, manager.logFormat=text, valid dnsLabel nameOverride, valid serviceAccount/rbac/leaderElection blocks) and run helm template ... -f values.yaml asserting success (run() fails the test on non-zero).
- HLM-08: table of invalid values files, each rendered via a NON-fatal exec helper (add a runExpectError() that returns (output, err) instead of t.Fatal, since run() fails on error) and assert err != nil plus the schema-violation substring. Cases verified against the live schema: manager.logFormat=yaml -> "value must be one of 'json', 'text'"; metrics.port=70000 -> "maximum"; unknown top-level key bogusKey -> "additional properties 'bogusKey' not allowed".

MANAGER (cmd unit + envtest foundation):
- MGR-02: add a sibling to TestNewControllerLogger that constructs newControllerLogger(managerOptions{LogFormat:"json"}, &buf) and json.Unmarshal(buf.Bytes()) into a map (asserting a "msg" key), then "text" and assert json.Unmarshal FAILS (non-JSON line). Mirrors the existing buffer-capture pattern in cmd/options_test.go.
- MGR-09: add a subtest to TestNewMetricsServerOptions feeding MetricsBindAddress:"0" and assert got.BindAddress=="0" (controller-runtime treats "0" as a disabled metrics server) and that no FilterProvider/SecureServing combination would serve — assert the disabled-address contract; document inline that the runtime "no endpoint" effect is the E2E half (MGR-07/E2E cluster), this Unit row asserts the passthrough.
- MGR-11: refactor cmd/setup.go registerControllers to delegate to a new buildControllers(mgr, options) (*cardanonetwork.CardanoNetworkReconciler, *cardanodbsync.CardanoDBSyncReconciler) helper that populates Default*Image fields, then registerControllers calls SetupWithManager on each. Add cmd/setup_test.go asserting buildControllers(mgr, managerOptions{DefaultFaucetImage:..., DefaultCardanoTestnetImage:..., DefaultCardanoToolsImage:...}) wires the faucet/testnet/tools images onto the cardanonetwork reconciler and testnet/tools onto the cardanodbsync reconciler (the exported fields verified in controller.go).
- MGR-01: extend cmd/foundation_test.go — after registerControllers, start the manager (mgr.Start in a goroutine with a cancelable ctx, WaitForCacheSync), then create a CardanoNetwork and a CardanoDBSync via the manager client and assert both are observed (informers exist for both kinds — getting them through mgr.GetCache().GetInformer for each GVK is the honest "registered and watching" foundation signal). Mirror the envtest bootstrap already in foundation_test.go (CRDDirectoryPaths to charts/yacd/crds).

**Files.**
- `test/chart/render_test.go`
- `cmd/options_test.go`
- `cmd/manager_test.go`
- `cmd/setup.go`
- `cmd/setup_test.go`
- `cmd/foundation_test.go`

**Success criteria.**
- HLM-02: a Go render test renders `helm template charts/yacd --include-crds` and asserts both CustomResourceDefinitions cardanonetworks.yacd.meigma.io and cardanodbsyncs.yacd.meigma.io are present.
- HLM-03: a default-values render test asserts Deployment yacd-controller-manager, Service yacd-controller-manager-metrics-service, and the metrics-auth (ClusterRole+ClusterRoleBinding), metrics-reader (ClusterRole), and leader-election (Role+RoleBinding) objects are all packaged.
- HLM-07: a representative full valid values document renders without a schema error (helm template exits 0).
- HLM-08: each schema-violating values file (manager.logFormat=yaml, metrics.port=70000, unknown top-level key) makes helm template exit non-zero with the matching schema-validation message asserted.
- MGR-02: the json log buffer json.Unmarshals to an object with a msg key and the text buffer fails json.Unmarshal (is non-JSON).
- MGR-09: newMetricsServerOptions with --metrics-bind-address=0 yields BindAddress=="0" (the disabled-metrics passthrough), asserted as a distinct case from :8443/:8080.
- MGR-11: buildControllers wires DefaultFaucetImage/DefaultCardanoTestnetImage/DefaultCardanoToolsImage from managerOptions onto the cardanonetwork reconciler and DefaultCardanoTestnetImage/DefaultCardanoToolsImage onto the cardanodbsync reconciler, asserted by a cmd unit test.
- MGR-01: cmd/foundation_test.go starts a manager-backed envtest and asserts informers/watches exist for both CardanoNetwork and CardanoDBSync (both controllers registered and watching), not merely that registerControllers returns nil.

**Exit criteria.**
- moon run root:test is green (Go tests run with KUBEBUILDER_ASSETS; the new chart and cmd tests included).
- moon run root:check is green (gofmt/vet/lint clean; refactored cmd/setup.go has no behavior change for the production path).
- All eight owned rows (HLM-02/03/07/08, MGR-01/02/09/11) flip from partial/not-satisfied to satisfied at their recommended Level (Unit for HLM-*, MGR-02/09/11; Env-foundation for MGR-01).
- No change to chart templates, values.schema.json, or production manager startup behavior beyond the buildControllers extraction; git diff --check clean.
- No new flaky test (helm/controller-gen are proto-pinned and already used by test/chart/rbac_test.go; the foundation envtest reuses the existing bootstrap).

**Risks.** Low overall. (1) The HLM render tests shell out to helm and controller-gen via exec — both are proto-pinned and already exercised by test/chart/rbac_test.go, so PATH availability in CI is proven; the schema-error message assertions are coupled to Helm v4's wording ("value must be one of", "maximum", "additional properties ... not allowed") which is stable for v4 but should be matched on a loose substring to tolerate minor phrasing changes. (2) The MGR-01 foundation test starts a real manager against envtest and must use a cancelable context + WaitForCacheSync and t.Cleanup to avoid a leaked goroutine/flake; this adds a few seconds of envtest startup but reuses the existing pattern in foundation_test.go. (3) MGR-09 asserts only the BindAddress="0" passthrough at Unit level — the actual "no endpoint exposed" runtime effect is genuinely an E2E/runtime property of controller-runtime's metrics server and is intentionally left to the E2E layer rather than faked here. (4) The cmd/setup.go buildControllers extraction is a pure refactor with no production behavior change; registerControllers must keep identical registration order and error handling.

---

## P2 — Faucet unit hardening: HTTP negative paths and engine error/concurrency branches  `M`

**PR:** `test(faucet): cover HTTP negative paths and engine error/concurrency branches`  
**Rows (12):** FCT-04, FCT-10, FCT-11, FCT-12, FCT-13, FCT-14, FTX-02, FTX-03, FTX-05, FTX-07, FTX-09, FTX-10  
**Level:** Unit  ·  **Depends on:** none

**Objective.** Prove the faucet's HTTP-handler negative/edge paths and the top-up engine's error-mapping and per-source-lock branches at the Unit level, closing every named FCT/FTX gap. The headline fix is unmasking FTX-07: the existing fakeSubmitter silently backfills SpentInputKeys, so the chain_unavailable branch on a blank TxID / zero spent inputs is currently unreachable by any test.

**Description.** Adds pure-Go unit tests (no cluster, no apiserver) in the two existing faucet test files. On the HTTP side (services/faucet/internal/server/server_test.go) it adds httptest cases for: the funding Address in /v1/sources and /v1/sources/{name} bodies (FCT-04); omitted-lovelace -> 400 invalid_request "lovelace is required" (FCT-11); an unmapped route -> 404 not_found (FCT-12); an empty/unloadable auth token -> 500 internal_error and a rotated token honored per request -> 200 (FCT-13); an over-4KB body and a multiple-JSON-value body -> 400 invalid_request, alongside the existing unknown-fields case (FCT-10); and the engine->HTTP status mapping driven through the real topup.Service path: source_not_found->404, source_unavailable->503, chain_unavailable->503, invalid_request->400 (FCT-14). On the engine side (services/faucet/internal/topup/service_test.go) it: changes the fake submitter so the SpentInputKeys backfill is opt-out, making the FTX-07 chain_unavailable branch reachable, and asserts it for both blank TxID and zero spent inputs; adds a per-source barrier submitter proving two distinct sources enter SubmitTopUp concurrently while one source still serializes (FTX-09, complementing the existing FTX-08 serialization test); and asserts the exact bound/destination/source-not-found message strings (FTX-02 "lovelace must be positive"/"lovelace must be at least N"/"lovelace must be at most N"; FTX-03 "invalid destination address"; FTX-05 incomplete-source -> source_not_found). FTX-10's primary is satisfied by the mock build/sign/submit round-trip already proven at the engine seam (topup.Service over a TransactionSubmitter) plus the existing apollo helper tests (validateTransaction, submitSignedTransaction via fakeOgmiosSubmitter, filterExcludedUTxOs, sourceKeyAddress); the real Apollo/Ogmios/Kupo build-sign-submit half needs a live chain context (newChainContext().Init() + Ogmios UTxO query) and is explicitly deferred to P11 / the existing faucet Chainsaw smoke.

**Approach.** Mechanism: net/http/httptest handlers plus a mock topup engine/submitter, mirroring the existing server_test.go (performTopUpRequest/performRawRequestBody/decodeResponse helpers, testHandler/testHandlerWithSubmitter) and service_test.go (fakeSourceReader/fakeSubmitter, NewService, assertTopUpCode) patterns. No new framework.

FCT-04: in server_test.go extend TestHandlerListsSources and TestHandlerReturnsOneSource (or add focused cases) to assert body.Sources[i].Address and the single sources.Source.Address equal testSourceAddress (the writeSource fixture funding address).
FCT-11: POST a valid-auth body {"address":testDestinationAddress} (no lovelace) -> assert 400, body.Error.Code==topup.CodeInvalidRequest, message "lovelace is required" (server.go:276-278).
FCT-12: GET an unmapped path (e.g. /v1/unknown) -> assert 404, body.Error.Code==codeNotFound (server.go:200-201).
FCT-13: use NewHandlerWithAuthTokenFile (like TestHandlerTopUpReloadsAuthTokenFile) with a loader returning "" and separately returning an error -> assert 500, body.Error.Code==codeInternalError (server.go:296-305 requireAuth); keep the rotated-token 200 assertion.
FCT-10: add (1) a >maxTopUpBodyBytes (4KB) body via a large padded JSON string -> 400 invalid_request (http.MaxBytesReader in decodeRequestBody, server.go:406); (2) two concatenated JSON objects -> 400 invalid_request (the second decode != io.EOF, server.go:412-414).
FCT-14: drive the engine via topup.NewService with a fake submitter, asserting each mapping through writeTopUpError (server.go:344-363): source_not_found via an unknown source (404), source_unavailable via a malformed-key store source (503, as TestHandlerTopUpReportsSourceUnavailable already does), chain_unavailable via a submitter returning topup.Errorf(CodeChainUnavailable,...) (503), and invalid_request via a submitter returning topup.Errorf(CodeInvalidRequest,...) so the 400 comes from the engine path, NOT the request-decode short-circuit.

CRITICAL FTX-07 fix: the engine fakeSubmitter (service_test.go:438-440) backfills SpentInputKeys to testSpentInputKey whenever empty, masking service.go:181-186. Add an opt-out field (e.g. noSpentInputBackfill bool, or a distinct fake) so a result with TxID:"" or SpentInputKeys:nil reaches Submit unmodified; assert both blank-TxID and zero-spent-inputs cases return CodeChainUnavailable with messages "...returned an empty transaction id" and "...returned no spent source inputs".
FTX-09: add a barrier-style submitter keyed per source name (extend/replace blockingSubmitter, which today uses a single channel pair and only proves serialization) so two concurrent Submits for distinct sources (utxo1 and utxo2) both block inside SubmitTopUp at the same time (assert both started before either is released, e.g. via a sync.WaitGroup/2-slot barrier or two started channels with a timed select); contrast with the existing FTX-08 same-source serialization test.
FTX-02/03/05: in TestServiceSubmitRejectsInvalidRequests / TestServiceSubmitMapsSourceErrors, additionally assert topupErr.Message (not just Code): the three lovelace-bound strings, "invalid destination address", and add a CodeSourceIncomplete input case asserting it collapses to CodeSourceNotFound (mapSourceError, service.go:220).

**Files.**
- `services/faucet/internal/server/server_test.go`
- `services/faucet/internal/topup/service_test.go`

**Success criteria.**
- FCT-04: a server_test asserts the funding Address is present and equals the fixture address in both the /v1/sources list and the /v1/sources/{name} single-source response
- FCT-11: a POST with lovelace omitted returns 400 with body.Error.Code==invalid_request and message "lovelace is required"
- FCT-12: a request to an unmapped path returns 404 with body.Error.Code==not_found
- FCT-13: an empty/unloadable auth token returns 500 internal_error, and a rotated token file value is honored per request (200 with the new token)
- FCT-10: an over-4KB body and a multiple-JSON-value body each return 400 invalid_request (in addition to the existing unknown-fields case)
- FCT-14: source_not_found->404, source_unavailable->503, chain_unavailable->503, and invalid_request->400 are each asserted through the real topup.Service engine path (the 400 is produced by the engine, not the decode short-circuit)
- FTX-07: the engine fakeSubmitter no longer unconditionally backfills SpentInputKeys; both a blank TxID and zero spent inputs return CodeChainUnavailable with the exact engine messages
- FTX-09: two concurrent Submits for distinct sources are observed running inside SubmitTopUp simultaneously, proving the per-source lock does not serialize across different sources
- FTX-02/03/05: the rejection tests assert the exact bound message strings, the "invalid destination address" message, and that an incomplete source collapses to source_not_found
- FTX-10 (primary): the mock build/sign/submit round-trip is asserted at the engine seam; a code comment notes the real Apollo/Ogmios/Kupo E2E half is deferred to P11 / the existing faucet smoke

**Exit criteria.**
- moon run root:test is green (the faucet unit packages compile and pass under go test ./...)
- All twelve owned rows (FCT-04/10/11/12/13/14, FTX-02/03/05/07/09/10) flip from partial/not-satisfied to satisfied at Unit level per the analysis Gap column
- The FTX-07 masking is removed: the modified fake no longer hides the chain_unavailable branch for any other existing engine test (no regression in TestServiceSubmit* / TestResultJSON*)
- moon run root:check is green (lint/format), and git diff --check is clean
- No new test flake under -race (the FTX-09 concurrency test uses a deterministic barrier, not a sleep-based race)

**Risks.** FTX-09 concurrency is the only flake risk: it must use a deterministic 2-source barrier (e.g. a WaitGroup or paired started/release channels with a bounded timeout) rather than a sleep, and must be -race clean; the existing blockingSubmitter is built to prove serialization (single channel pair) and either needs a second instance per source or a small refactor to a source-keyed barrier. Modifying the engine fakeSubmitter's backfill is shared across many service_test cases, so the opt-out must default to the current backfill behavior to avoid silently breaking TestServiceSubmitUsesDefaultSource/UsesSelectedSource/PassesPendingInputExclusions. The over-size-body case must pad past maxTopUpBodyBytes (4KB) without tripping DisallowUnknownFields first (use a large valid-shaped or oversized-string field). All tests are pure unit (httptest + mocks), so CI cost is negligible and they run in the standard moon run root:test job; no nightly/manual gating needed. FTX-10's real-chain half is intentionally out of scope (no live Ogmios/Kupo in unit tests) and deferred.

---

## P3 — CLI / host-access / topup unit hardening  `M`

**PR:** `test(cli): cover lifecycle/host-access/topup negative and JSON-output paths`  
**Rows (16):** CLI-03, CLI-12, CLI-13, CLI-14, CLI-15, HST-03, HST-04, HST-06, HST-11, HST-15, TOP-01, TOP-02, TOP-03, TOP-05, TOP-08, TOP-10  
**Level:** Unit  ·  **Depends on:** none

**Objective.** Prove the YACD CLI's flag/precondition guards, output shapes, and host-env branches with mockery-backed verb tests against a mock kube.Client (no cluster), closing the exact unit-level gaps the audit names for 16 CLI/HST/TOP rows.

**Description.** The CLI lifecycle/host-access/topup verbs have strong happy-path coverage but a cluster of untested negative guards, output-shape conjuncts, and env branches. This PR adds focused mockery-based tests (no apiserver, no Kind) to the existing cli/internal/cli/*_test.go suites: the `up` empty-`--file` rejection (CLI-03); `topup` precondition guards — missing `--address` (TOP-02), `--lovelace<=0` with the exact `--lovelace must be greater than 0` string (TOP-03), missing published faucet endpoint / auth Secret (TOP-05), invalid `--faucet-url` (TOP-10), and `localhost`/`::1` loopback exemption (TOP-08); `exec` against a not-ready network refusing before Exec (HST-06); `info`/`topup` not-found exit (CLI-15); the `list -A` none-found message and CLI-12 scope text; the `info --json` network-identity block (CLI-13); stale-status not-ready readiness in info/list (CLI-14); the `topup` happy-path printing txId/source/lovelace/destination (TOP-01); `run` with no command dropping to `$SHELL` with the env set (HST-03); the dropped-forward non-zero exit code (HST-04); the connect cancel disconnect message (HST-11); and the network-magic-absent identity branch (HST-15). Every test asserts a concrete signal (exact error substring, exit code via ResolveExit, JSON field, or absence of a downstream mock call) and reuses the established fixtures (readyNetwork, listTestNetwork, runMock, successfulTopUpHTTPResponse, newKubeMock/newHTTPMock).

**Approach.** All tests are pure Go + testify + mockery via the existing harness in cli/internal/cli/testhelpers_test.go — no envtest, no Kind. Mirror the patterns already in each verb's _test.go.

CLI-03 (up_test.go): run `up devnet` with no `-f`; the guard at up.go:38-40 fires before devconfig.LoadFile and before the kube factory, so use a newKubeMock with NO expectations and assert err contains `--file is required` (mockery fails if any client method is touched).

TOP-02/03/05/10/08/01/15 (topup_test.go): reuse readyNetwork("devnet") + kubeClientFactory(newKubeMock). TOP-02: `topup devnet --lovelace 2000000` (no --address) -> err `--address is required`, no GetCardanoNetwork expectation. TOP-03: table over `--lovelace 0` and `--lovelace -1` -> err exactly `--lovelace must be greater than 0` (topup.go:53), no client call. TOP-05: two subtests mutating readyNetwork — drop Endpoints.Faucet (or URL="") -> `does not publish a faucet endpoint` (topup.go:200); set Status.Faucet=nil (or AuthSecretName="") -> `does not publish a faucet auth Secret` (topup.go:87); each asserts AssertNotCalled GetSecretValue. TOP-10: table of bad `--faucet-url` values (relative `/x`, `ftp://h`, `ws://h`, `http://` missing host) -> err contains `invalid faucet URL` plus the branch tail (`scheme must be http or https` / `host is required`) from parseHTTPURL (topup_trust.go:68-80); assert no GetSecretValue. TOP-08: drive the `localhost` and IPv6 `::1` loopback branches of isLoopbackHost (topup_trust.go:94-100) by passing `--faucet-url http://localhost:<port>` / `http://[::1]:<port>` against an httptest.NewServer (or a direct isLoopbackHost table test) and asserting success WITHOUT --trust-faucet-url. TOP-01: extend the existing TestTopUpReadsSecretAndPostsToFaucet stdout assertions (non-JSON mode) to also assert `Destination: addr_test1dest` is printed (topup.go:147), closing the destination conjunct. CLI-15 topup half: GetCardanoNetwork returns `fmt.Errorf("cardanonetwork devnet/devnet %w", kube.ErrNotFound)` -> command exits non-zero, err contains `not found`.

CLI-15/CLI-13/CLI-14 (info_test.go): not-found — GetCardanoNetwork returns wrapped kube.ErrNotFound -> non-zero, err `not found`. CLI-13 — assert the `info --json` output contains the network block (`"mode": "local"`, `"networkMagic": 42`, and `"era": "conway"`) from newInfo (info.go:130-140), which existing tests skip. CLI-14 — feed a readyNetwork whose Ready condition has ObservedGeneration=0 (< Generation=1) so FreshCondition (conditions.go:36) returns nil; assert info --json reports the condition still listed but a list/info readiness of not-ready (pair with a list_test.go case asserting items[0].Ready==false on the same stale shape).

CLI-12 (list_test.go): add a `list -A` empty case — ListCardanoNetworks(ctx,"") returns empty -> stdout `No CardanoNetworks found.` (list.go:176, the all-namespaces branch that currently names no scope), complementing the existing namespaced TestListEmptyResultReportsNone.

HST-06 (exec_test.go): readyNetwork mutated to not-ready (e.g. Ready condition False, or ObservedGeneration stale) -> `exec devnet -- cardano-cli ...` returns non-zero err referencing not-ready/stale (requireReady, forward.go:124-134); mock has GetCardanoNetwork only, NO PrimaryPodName/Exec expectation.

HST-03/HST-04 (run_test.go): HST-03 — reuse runMock(t, never) with `t.Setenv("SHELL", "/bin/sh")` (no t.Parallel, matching TestRunCommandLine), run `run devnet` with no command, and assert the child shell saw the env, e.g. `run devnet` then a no-cmd path; since dropping into an interactive $SHELL needs a real shell, assert via a `SHELL` pointing at `sh -c 'printf %s "$YACD_NETWORK"'`-style wrapper is not possible (SHELL is argv[0]); instead set SHELL to `/bin/sh` and feed stdin `printf %s "$YACD_NETWORK"
` via In, asserting stdout==`devnet` — proving runCommandLine(nil) drops to $SHELL with the YACD_* env wired (run.go:80-90, runChild env at run.go:113). HST-04 — extend TestRunReportsDroppedForward to also call ResolveExit(err) and assert a non-zero code (the existing test asserts only the message), closing the exit-code conjunct.

HST-11 (connect_test.go): clone TestRunConnectWritesFileAndExitsOnCancel but route commandContext.err to a bytes.Buffer; after cancel and clean return, assert the buffer contains `Disconnecting from devnet/devnet.` (connect.go:127).

HST-15 (envcontract_test.go): add an identityEnv/podEnv table case building a network with Status.Network=nil (and a second with NetworkMagic=nil) and assert YACD_NETWORK and YACD_NAMESPACE are present while YACD_NETWORK_MAGIC is absent (envcontract.go:175-184), the negative half of the always-present-identity contract.

**Files.**
- `cli/internal/cli/up_test.go`
- `cli/internal/cli/topup_test.go`
- `cli/internal/cli/info_test.go`
- `cli/internal/cli/list_test.go`
- `cli/internal/cli/exec_test.go`
- `cli/internal/cli/run_test.go`
- `cli/internal/cli/connect_test.go`
- `cli/internal/cli/envcontract_test.go`

**Success criteria.**
- CLI-03: `up devnet` (no -f) errors with `--file is required` and no kube.Client method is invoked (newKubeMock with zero expectations passes).
- TOP-02: missing `--address` errors `--address is required` with no GetCardanoNetwork/GetSecretValue/HTTP call.
- TOP-03: `--lovelace 0` and `--lovelace -1` both error with the exact string `--lovelace must be greater than 0` and send no request.
- TOP-05: a network missing the published faucet endpoint errors `does not publish a faucet endpoint`, and one missing the auth Secret errors `does not publish a faucet auth Secret`; GetSecretValue is never called.
- TOP-10: relative, non-http/https-scheme, and host-less `--faucet-url` values each error containing `invalid faucet URL` and never read the Secret.
- TOP-08: `--faucet-url` to `http://localhost:<port>` and `http://[::1]:<port>` succeed WITHOUT `--trust-faucet-url`, exercising the localhost and ::1 branches of isLoopbackHost.
- TOP-01: the non-JSON topup happy path prints `Destination: <addr>` alongside txId/source/lovelace.
- CLI-15: `info` and `topup` against a GetCardanoNetwork returning wrapped kube.ErrNotFound exit non-zero with a `not found` error.
- CLI-12: `list -A` with no matches prints `No CardanoNetworks found.` (all-namespaces branch).
- CLI-13: `info NAME --json` output contains the network-identity block (mode, networkMagic, era).
- CLI-14: a network with a stale Ready condition (ObservedGeneration < Generation) is reported not-ready by info/list.
- HST-06: `exec` against a not-ready network exits non-zero with a not-ready/stale error and never calls PrimaryPodName or Exec.
- HST-03: `run NAME` with no command drops into $SHELL with the YACD_* environment set (observable via the shell reading YACD_NETWORK).
- HST-04: the dropped-forward run case resolves to a non-zero exit code via ResolveExit, not a bare success.
- HST-11: on connect cancel, stderr contains `Disconnecting from <ns>/<name>.`.
- HST-15: identity env always sets YACD_NETWORK and YACD_NAMESPACE but omits YACD_NETWORK_MAGIC when Status.Network / NetworkMagic is nil.

**Exit criteria.**
- `moon run root:test` is green (KUBEBUILDER_ASSETS set via setup-envtest); the new cli/internal/cli tests pass deterministically with -race.
- `moon run root:check` passes (gofmt/vet/lint clean; git diff --check clean).
- All 16 owned rows (CLI-03/12/13/14/15, HST-03/04/06/11/15, TOP-01/02/03/05/08/10) flip from partial/not-satisfied to satisfied at the Unit level the plan recommends, each via an assertion on the exact named signal.
- No new flaky tests: the run/connect goroutine cases reuse the existing Eventually/done-channel patterns and bounded timeouts already in the suite.
- No production code changes (test-only PR); no envtest or Chainsaw additions (these rows are Unit-level by the plan).

**Risks.** Low CI cost: all tests are pure-Go mockery/httptest with no apiserver or cluster. Two mild flakiness hazards, both already mitigated by existing patterns: (1) the HST-03 drop-to-$SHELL test exec's a real /bin/sh, so it must set SHELL deterministically and avoid t.Parallel (t.Setenv constraint, as TestRunCommandLine already does), and reading YACD_NETWORK through the shell depends on the runChild env wiring rather than an interactive PTY; if asserting shell-side env proves brittle, fall back to asserting runCommandLine(nil)==[$SHELL] plus a hostEnv-level assertion that the env is attached. (2) HST-11/connect goroutine timing reuses the suite's require.Eventually + done-channel idiom, so no new timing model is introduced. The CLI-14 stale-status case must use the FreshCondition staleness semantics (ObservedGeneration < Generation) precisely, not a missing condition, to actually exercise the intended branch.

---

## P4 — Config, cardano-tools & public-pin unit hardening  `M`

**PR:** `test(config,tools,pins): cover devconfig negatives, cardano-tools verbs, pin integrity`  
**Rows (16):** CFG-02, CFG-04, CFG-05, CFG-07, CFG-08, CFG-09, PIN-01, PIN-02, PIN-03, PIN-04, TLS-03, TLS-04, TLS-05, TLS-06, TLS-09, TLS-10  
**Level:** Unit  ·  **Depends on:** none

**Objective.** Close the pure-Go (no-cluster) coverage gaps in the developer config loader/renderer, the cardano-tools serve/sync/fetch verbs, and the public-pin registry, flipping 16 partial/not-satisfied rows to satisfied at their recommended Unit level. This proves the F0 artifact-integrity contract and the devconfig negative surface entirely in `go test`, with no envtest or Chainsaw cost.

**Description.** CFG: assert the devconfig loader names the required value on a wrong apiVersion/kind (CFG-02), rejects an omitted-but-required `mode` (CFG-04), rejects mode/block XOR mismatches (local spec carrying a `public:` block and vice versa, CFG-05), renders a CardanoNetwork whose full spec equals the document's network spec rather than a single scalar (CFG-07), derives name/namespace from CLI args while ignoring any identity-shaped field in the file (CFG-08), and loads+renders every shipped `examples/**/yacd.yaml` Environment document including a mainnet 300Gi-storage/bootstrap regression guard (CFG-09).

PINS: assert the curated `publicpins` registry exposes each profile's files in a fixed deterministic order with the correct NetworkMagic and RequiresNetworkMagic (PIN-01), pin the exact `CompatibleNodeRelease=11.0.1` baseline alongside the existing embedded-byte digest recompute (PIN-02), assert the operator's `publicnet.BuildPlan` manifest agrees with what `cardano-tools fetch` (`fetch.pinsFor`) verifies for the same profile (PIN-03), and assert the genesis/checkpoints/peer-snapshot files carry no fetch-time pin in both directions (PIN-04).

cardano-tools: assert `serve` returns 405 to non-GET/mutating methods and serves `manifest.json` (TLS-03), that `sync` fail-closes on a per-file digest mismatch against the served manifest (TLS-04), that the `fetch` half also refuses HTTP redirects (TLS-05, only the `sync` half exists today), that `fetch --profile preprod`/`mainnet` download-and-verify against their real pins, not just `preview` (TLS-06), that an unknown `fetch --profile` error enumerates the supported profiles (TLS-09), and that `artifactset.ReadManifest` rejects each bad path/shape with its exact message (TLS-10).

**Approach.** All work lives in existing test packages with established patterns; no new harness. Mechanisms: table tests + Testify, `net/http/httptest`, and the existing `fakeDoer`/`bundleServer` HTTP fakes.

CFG-02: extend `cli/internal/devconfig/config_test.go:TestValidateRequiresEnvelope` rows to `assert.Contains(err, APIVersion)` / `assert.Contains(err, Kind)` (the loader already formats `"apiVersion must be %q"` / `"kind must be %q"`, config.go:99-104).
CFG-04: add a standalone `mode` omission row to `TestLoadRejectsOmittedConcreteCRDDefaults` (strip `mode: local`) and assert `spec.network.mode` is named.
CFG-05: add two new rows to a negative Load test — `validConfig` with a `public:` block appended (expect `public is not supported with local mode`, config.go:118) and `validPublicPreviewConfig` with a `local:` block appended (expect `local is not supported with public mode`, config.go:125).
CFG-07: in `cli/internal/render/render_test.go`, extend `TestCardanoNetworkRendersDeveloperConfig` to deep-assert the rendered `network.Spec` against the loaded `environment.Spec.Network` (Mode, Node.Version/Port/Storage.Size, Local.Era/NetworkMagic/Timing/Topology) via `assert.Equal` on the whole struct.
CFG-08: add a render case where the document YAML carries a stray `metadata.name`-like field — since the strict decoder rejects unknown top-level keys, prove identity-from-CLI by asserting the rendered name/namespace equal the CLI args and differ from any in-spec string, with `metadata` absent from `CardanoNetworkSpec`.
CFG-09: add `TestExampleDocumentsLoadAndRender` in `devconfig` (or `render`) globbing `../../../examples/**/yacd.yaml`, calling `LoadFile` + `render.CardanoNetwork(env, "example", "example")` and requiring no error for all four; add a regression sub-test that the mainnet example with `node.storage.size: 20Gi` is rejected with `at least 300Gi for public mainnet` (runtime_support.go:84) and that mainnet without `bootstrap.mithril` is rejected.

PIN-01: in `internal/cardano/publicnet/publicpins_crosscheck_test.go` (or a new `publicpins_registry_test.go` in package `publicpins`), assert each `Lookup(name).Files` `ArtifactKey` slice equals an exact expected ordered list and that NetworkMagic/RequiresNetworkMagic match (2/true, 1/true, 764824073/false).
PIN-02: add `assert.Equal(t, "11.0.1", publicpins.CompatibleNodeRelease)` and `operationsBookNodeRelease` to lock the baseline.
PIN-03: new `TestOperatorManifestAgreesWithFetchPins` comparing `publicnet.BuildPlan(...).Manifest` (NetworkMagic, RequiresNetworkMagic, fingerprint inputs) against the URL/pin set produced from `publicpins.Lookup` (the same source `fetch.pinsFor` consumes), proving the operator and fetch share one definition.
PIN-04: assert `file.Pinned==false && file.SHA256==""` for the four genesis keys + checkpoints + peer-snapshot, and `Pinned==true` only for config/topology (+mainnet mithril vkeys).

TLS-03: in `serve_test.go`, add a POST/PUT/DELETE case asserting 405 and a `manifest.json` served case (ManifestKey is in `OptionalKeys`, contract.go:46).
TLS-04: in `artifactsync/sync_test.go`, keep `TestRunFailsOnDigestMismatch` and add an assertion that the per-file write is gated by manifest verification (no file on mismatch already asserted; add an explicit served-manifest-vs-bytes check).
TLS-05: add `TestFetchRefusesRedirect` in `fetch/fetch_test.go` — a `fakeDoer` returning a 302 status surfaces as `unexpected status` (download() rejects non-200, fetch.go:144), mirroring `artifactsync` `TestRunRefusesRedirect`.
TLS-06: parametrize `TestRunWritesVerifiedArtifacts` over preprod and mainnet, sourcing bodies/pins from `publicpins.Lookup` and the embedded profile bytes (mainnet needs the two pinned mithril vkeys).
TLS-09: extend `TestRunRejectsUnknownProfile` to `assert.Contains(err, "preview")`/`"preprod"`/`"mainnet"` (fetch.go:53 already prints `known: ...`).
TLS-10: new `containers/cardano-tools/internal/artifactset/read_test.go` table-testing `ReadManifest(envDir, path)` for empty path, non-absolute path, wrong filename, missing `inputs.networkMagic`, and missing `fingerprint.value`, asserting each exact message (read.go:37-62).

**Files.**
- `cli/internal/devconfig/config_test.go`
- `cli/internal/devconfig/examples_test.go`
- `cli/internal/render/render_test.go`
- `internal/cardano/publicpins/publicpins_registry_test.go`
- `internal/cardano/publicnet/publicpins_crosscheck_test.go`
- `internal/cardano/publicnet/operator_fetch_agreement_test.go`
- `containers/cardano-tools/internal/serve/serve_test.go`
- `containers/cardano-tools/internal/artifactsync/sync_test.go`
- `containers/cardano-tools/internal/fetch/fetch_test.go`
- `containers/cardano-tools/internal/artifactset/read_test.go`

**Success criteria.**
- CFG-02: a wrong apiVersion/kind Load failure asserts the error names the required value (the APIVersion/Kind constant), not just the field token.
- CFG-04: omitting spec.network.mode is a standalone Load failure naming spec.network.mode; CFG-05: a local document with a public: block and a public document with a local: block each fail with the matching 'not supported with X mode' message.
- CFG-07: the render test deep-asserts the rendered CardanoNetwork.Spec equals the loaded environment.Spec.Network across Mode/Node/Local fields (not only NetworkMagic).
- CFG-08: a render case proves name/namespace come from CLI args and are independent of document contents.
- CFG-09: all four examples/**/yacd.yaml load via devconfig and render to a CardanoNetwork with no error; a regression sub-test asserts a mainnet example under 300Gi is rejected ('at least 300Gi for public mainnet') and mainnet without bootstrap.mithril is rejected.
- PIN-01: each profile's File ArtifactKey order is asserted against an exact expected slice with correct NetworkMagic (2/1/764824073) and RequiresNetworkMagic (true/true/false).
- PIN-02: the existing embedded-byte digest recompute passes AND CompatibleNodeRelease is pinned to the literal '11.0.1'.
- PIN-03: a test computes the operator BuildPlan manifest for each profile and asserts agreement (magic, requiresMagic, pinned URLs/digests) with fetch.pinsFor's view of the same publicpins profile.
- PIN-04: genesis (byron/shelley/alonzo/conway), checkpoints, and peer-snapshot are asserted unpinned (Pinned=false, SHA256 empty) and config/topology (+mainnet mithril vkeys) pinned.
- TLS-03: serve returns 405 to a POST/PUT/DELETE and serves manifest.json with 200.
- TLS-04: sync writes nothing and errors on a per-file digest mismatch against the served manifest (fail-closed).
- TLS-05: a fetch download of a 3xx-redirecting source is refused (unexpected status), matching the existing sync redirect test.
- TLS-06: fetch downloads and verifies preprod and mainnet profiles against their real pins, not only preview.
- TLS-09: the unknown fetch --profile error enumerates preview, preprod, and mainnet.
- TLS-10: ReadManifest rejects empty/non-absolute/wrong-path manifests and manifests missing inputs.networkMagic or fingerprint.value, each with its exact 'is required'/'must be' message.

**Exit criteria.**
- moon run root:test is green (these are Unit tests; KUBEBUILDER_ASSETS not required for the changed packages, but the standard runner is used).
- moon run root:check passes (gofmt/vet/lint clean; git diff --check clean).
- All 16 owned rows (CFG-02/04/05/07/08/09, PIN-01/02/03/04, TLS-03/04/05/06/09/10) have a test asserting their exact named gap at Unit level.
- No new flake and no reliance on network access: all HTTP is httptest/fakeDoer, all profile bytes come from the embedded internal/cardano/publicnet/profiles assets or publicpins.

**Risks.** Low risk: pure Go unit tests in established packages, no cluster, no envtest. Main hazards: (1) PIN-02/TLS-06 hardcode the literal CompatibleNodeRelease and the mainnet pinned digests/fingerprint; an intentional upstream profile bump will require updating these constants in lockstep (that coupling is the point, but it makes this PR sensitive to a concurrent profile-rotation PR — sequence after any pin bump). (2) CFG-09's examples/**/yacd.yaml glob uses a relative path from the test's package dir; must use the correct ../ depth (cli/internal/devconfig -> ../../../examples) and skip non-Environment example files (cardanodbsync-*.yaml/Secret manifests) by globbing only yacd.yaml. (3) The CFG-09 mainnet 300Gi regression guard tests a constructed under-storage document, not the shipped example (the shipped mainnet example sets no storage block, so the guard does not fire on it) — the test must build the negative input explicitly. No CI cost beyond a few ms of added unit tests.

---

## P5 — CardanoNetwork CRD admission & defaulting envtest (new suite)  `M`

**PR:** `test(api): add CardanoNetwork admission/defaulting envtest suite`  
**Rows (11):** CNV-01, CNV-02, CNV-03, CNV-04, CNV-05, CNV-06, CNV-07, CNV-08, CNV-09, CNV-10, CNV-11  
**Level:** Env  ·  **Depends on:** none

**Objective.** Prove the entire CardanoNetwork CRD admission and defaulting contract against a real apiserver (envtest). Today there is no `cardanonetwork_validation_test.go` at all, so the CEL XOR rules, enum closure, port ranges, the pool `margin` pattern, and OpenAPI defaults are unverified — a bad spec is only caught (or silently mishandled) at reconcile. This is the single highest-leverage gap in the audit.

**Description.** Creates `api/v1alpha1/cardanonetwork_validation_test.go`, mirroring `cardanodbsync_validation_test.go` exactly: a started `envtest.Environment` loading `charts/yacd/crds`, a `client.Create()` table for negatives asserting `apierrors.IsInvalid`, and `Get`-back subtests asserting apiserver-applied defaults. Covers the mode/local/public XOR (CNV-02/03), the mainnet `bootstrap.mithril` CEL rule both directions (CNV-04/05), node and chain-API port ranges (CNV-06/07), enum closure for mode/era/profile/genesis-profile (CNV-08, which is also the primary coverage for CNP-07), the pool `margin` pattern (CNV-09), and OpenAPI defaulting (CNV-01/10/11: era=conway, node version, node port 3001, ogmios/kupo enabled, faucet disabled, faucet min/max top-up 1000000/10000000000).

**Approach.** Mechanism: controller-runtime envtest against a started apiserver (`KUBEBUILDER_ASSETS` via `moon run root:test`); no pods. Mirror `cardanodbsync_validation_test.go` verbatim — one shared `testEnv` per Test function (start/stop once to bound cost), a `runtime.Scheme` with clientgo + yacd v1alpha1, a minimal-valid `mode: local` builder returning an `*unstructured.Unstructured` (built unstructured so apiserver OpenAPI defaults are observable on `Get`-back — a typed struct would carry client-side zeros), and the `testCases` mutate-loop ending in `require.Error` + `assert.True(apierrors.IsInvalid(err))`. Defaulting positives `Create` the minimal object then `Get` it back and assert via `unstructured.Nested*`. Negatives map to the markers in `cardanonetwork_types.go` — the Enum markers on mode/era/profile/genesis-profile, the Pattern on `margin`, Min/Max on the ports, and the CEL `XValidation` for the XOR and mainnet-bootstrap rules.

**Files.**
- `api/v1alpha1/cardanonetwork_validation_test.go (new)`

**Success criteria.**
- CNV-02/03 each Create() a mode/local/public XOR violation and assert apierrors.IsInvalid; both also assert err.Error() names the 'mode must match exactly one of spec.local or spec.public' rule
- CNV-04 (mainnet, no bootstrap.mithril) and CNV-05 (preview/preprod with bootstrap set) each assert apierrors.IsInvalid against the bootstrap.mithril CEL rule
- CNV-06 (node.port 0 and 70000) and CNV-07 (ogmios/kupo/faucet port out of 1-65535) each assert apierrors.IsInvalid by range validation
- CNV-08 asserts apierrors.IsInvalid for unknown spec.mode, spec.local.era, spec.public.profile, and spec.local.genesis.profile (also serving as CNP-07 primary coverage)
- CNV-09 asserts apierrors.IsInvalid for a margin violating ^(0(\.[0-9]+)?|1(\.0+)?)$
- CNV-01/10 Create a minimal valid mode:local spec, Get it back, and assert apiserver defaults era=conway, node.version=11.0.1, node.port=3001, ogmios/kupo enabled=true, faucet absent/disabled
- CNV-11 Get-back asserts faucet minTopUpLovelace=1000000 and maxTopUpLovelace=10000000000 defaulted by the apiserver

**Exit criteria.**
- `moon run root:generate` produces no diff (asserts the shipped CRD contract; no marker changes intended).
- `moon run root:test` green, including the new `api/v1alpha1` CardanoNetwork suite.
- `moon run root:check` passes (gofmt/vet/lint; `git diff --check` clean).
- CNV-01..11 flip from partial/not-satisfied to satisfied at Level=Env.
- No new flake across two consecutive `root:test` runs.

**Risks.** Adds one more started envtest apiserver (~1–3s); acceptable — `root:test` already starts envtest for both controllers; share a single `testEnv` per Test. Create objects MUST be built as `unstructured` so apiserver OpenAPI defaulting (not client-side zeros) is what `Get`-back observes.

---

## P6 — CardanoDBSync CRD admission hardening (existing suite)  `S`

**PR:** `test(api): complete CardanoDBSync validation negatives, enums, and defaults`  
**Rows (8):** DBV-02, DBV-03, DBV-04, DBV-05, DBV-06, DBV-07, DBV-08, DBV-09  
**Level:** Env  ·  **Depends on:** none

**Objective.** Close the named negative / message / default gaps in the EXISTING `cardanodbsync_validation_test.go`: assert the CEL rule messages (not just `IsInvalid`), the empty-MinLength rejections, the five uncovered enum negatives, the above-range port, and the db-sync image default.

**Description.** The XOR / `followerNode` negatives already exist but assert only `IsInvalid`; switch them to also assert the error names the documented CEL rule (DBV-02/03/04). Add empty-`networkRef.name` and empty `passwordSecretRef` name/key MinLength rejections (DBV-05/06), the five missing enum negatives — ledgerBackend, insert preset, txOut mode, ledger mode, jsonType (DBV-07), an above-range external port (DBV-08), and a `Get`-back assertion that `spec.image` defaults to `ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0` (DBV-09).

**Approach.** Mechanism: the same envtest validation pattern already in the file — extend its `testCases` table and the `accepts managed database` defaulting subtest. For the CEL message assertions, match a stable documented substring of the message (not the whole error) to limit churn if `cardanodbsync_types.go` wording changes. Marker references: the `database.external`/`managed` XOR and `followerNode`/`primarySidecar` CEL, MinLength on `networkRef.name` and `passwordSecretRef.{name,key}`, the Enum markers on the `config` fields, Min/Max on the external port, and the `image` default.

**Files.**
- `api/v1alpha1/cardanodbsync_validation_test.go`

**Success criteria.**
- DBV-02/03/04 now assert err.Error() names the XOR and followerNode CEL messages (not only IsInvalid)
- DBV-05 (empty networkRef.name) and DBV-06 (empty passwordSecretRef name and key) assert apierrors.IsInvalid against MinLength=1
- DBV-07 adds the 5 missing enum negatives (ledgerBackend, insert preset, txOut mode, ledger mode, jsonType) asserting IsInvalid
- DBV-08 adds an above-range external port (70000) asserting IsInvalid; DBV-09 asserts spec.image defaults to ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0

**Exit criteria.**
- `moon run root:test` green (the extended `cardanodbsync_validation_test.go`).
- `moon run root:check` passes.
- DBV-02..09 flip to satisfied at Level=Env.
- No new flake.

**Risks.** CEL message-string assertions are brittle if `cardanodbsync_types.go` messages change — assert a stable documented substring, not the whole error string.

---

## P7 — CardanoNetwork reconcile/identity envtest  `M`

**PR:** `test(cardanonetwork): manager-backed envtest for reconcile output, status and identity`  
**Rows (12):** CNI-01, CNI-02, CNI-03, CNI-04, CNL-03, CNL-06, CNL-07, CNL-09, CNP-02, CNP-04, CNP-05, CNP-07  
**Level:** Env  ·  **Depends on:** none

**Objective.** Prove the CardanoNetwork reconcile-output, identity/immutability, network-identity status, no-ConfigMap invariant, and Ogmios sync-probe contracts through real controller-runtime watches/.Owns (envtest) and the existing unit harness, lifting eleven partial rows to SATISFIED at the Level the plan recommends.

**Description.** Eleven CardanoNetwork rows are currently graded partial because their declared contract is proven only by fake-client direct-Reconcile/Build() unit tests where the plan wants Env, or because a named slice (a status field, the no-ConfigMap object absence, the tip-ahead clamp) is unasserted. This PR closes each named gap: it adds manager-backed envtest cases (mirroring the existing TestCardanoNetworkControllerManagerCreatesAndRecreatesPrimaryWorkload pattern in controller_envtest_test.go) for the Env-level rows — fingerprints recorded on first reconcile (CNI-01), identity-input mutation refused with Degraded/UnsupportedLocalnetChange (CNI-02), a non-identity field change reconciling cleanly with no fingerprint conflict (CNI-04), the no <net>-network-artifacts ConfigMap object + no public-profile ConfigMap volume invariant (CNL-03), ArtifactsReady sourced from serve-sidecar readiness (CNL-06 identity + the ArtifactsReady slice), and resolved public identity mode/profile/magic (CNP-02). It strengthens the unit-level rows in place: CNL-07 (local genesis set -> Degraded/UnsupportedSpec, no children) and CNL-09 (pool count != 1, including 0 and 3, -> Degraded/UnsupportedSpec, no children) by extending the TestCardanoNetworkReconcilerReconcileMarksUnsupportedInput table; CNP-04 (status.sync.Source=ogmios + ConnectionStatus + LastTip + NetworkSynchronization surfaced onto published status) and CNP-05 (lagSlots/lagSeconds never negative, incl. a tip-ahead-of-inferred clamp-to-0 case) by extending sync_probe_test.go. CNP-07 is covered as the in-package reconcile-boundary note (an unresolvable-but-admitted profile drives Degraded, no fetch from an unverified source); its primary admission coverage is CNV-08, owned by the validation-test phase, which this row cross-references. Also folds in CNI-03 (recreate-after-deletion with changed inputs): a manager-backed envtest that creates a local network, records its fingerprint, deletes the network AND its fingerprint-carrying state PVC (envtest runs no GC), recreates a same-named network with a different networkMagic, and asserts a new distinct fingerprint with Degraded=False and no UnsupportedLocalnetChange.

**Approach.** Mechanism: controller-runtime envtest (real kube-apiserver, no kubelet) for the Env rows, plus the existing fake-client unit harness for the Unit rows. Mirror the proven setup in internal/controller/cardanonetwork/controller_envtest_test.go: envtest.Environment with CRDDirectoryPaths ../../../charts/yacd/crds, ctrl.NewManager with Metrics/HealthProbe BindAddress "0" and SkipNameValidation, a CardanoNetworkReconciler wired with syncProberOverride: syncedNodeSyncProber() and timingProberOverride: syncedNodeTimingProber() and a fixed Now, SetupWithManager, mgr.Start in a goroutine, WaitForCacheSync, then drive via a separate apiClient. Reuse helpers localCardanoNetwork/enableFaucet/publicCardanoNetwork/conditionHas/findCondition and the names.go accessors. Per row:
- CNI-01 (Env): create a local network, mark the primary Deployment/Pod ready as the existing test does, then assert status.network.LocalnetFingerprint AND NetworkFingerprint are both non-empty (the existing test only reads them as a baseline for the forge-repair step; add an explicit first-reconcile assertion).
- CNI-02 (Env): after acceptance, apiClient.Update spec.local.NetworkMagic (and a subtest for era), reconcile, assert conditionHas(Degraded,True,UnsupportedLocalnetChange) and Progressing,False,UnsupportedLocalnetChange, and that the PVC/Deployment localnetFingerprintAnno is unchanged — promoting the fake-client TestCardanoNetworkReconcilerReconcileRejectsLocalnetInputChanges contract to manager-backed Env.
- CNI-04 (Env): after acceptance, mutate a non-identity field (e.g. node resources / storage size) and assert it reconciles with Degraded=False/ReconcileSucceeded and the fingerprint annotations unchanged (no UnsupportedLocalnetChange).
- CNL-03 (Env): on a created local network assert apierrors.IsNotFound for ObjectKey{<net>-network-artifacts} ConfigMap and assertNoVolumeNamed(... "network-artifacts") on the primary Deployment pod template (lift the object-absence assertion from chainsaw-only to Env).
- CNL-06 (Env): assert status.network.Mode==local, *Era==conway, *NetworkMagic==42, LocalnetFingerprint non-empty; and (ArtifactsReady slice) assert ArtifactsReady flips True/ArtifactsReady only once the serve sidecar container is marked ready (the existing test already marks serveContainerName ready — add the targeted ArtifactsReady-from-serve assertion).
- CNP-02 (Env): create a public preview network, mark ready, assert status.network.Mode==public, *Profile==preview, *NetworkMagic==2; add preprod (magic 1) as a subtest to close the preprod hole.
- CNP-07 (Env boundary note): document + assert in the public reconcile path that an admitted profile which cannot resolve drives Degraded rather than a fetch; reference CNV-08 as primary admission coverage in a code comment.
- CNL-07 / CNL-09 (Unit): extend the TestCardanoNetworkReconcilerReconcileMarksUnsupportedInput table with rows {local genesis set}, {pool count 0}, {pool count 3} reusing localCardanoNetwork + assertNoPrimaryChildren + assertCondition(Degraded,True,UnsupportedSpec). builder.go:321/324 already emit the exact messages.
- CNP-04 (Unit + Env smoke): extend sync_probe_test.go to assert status.sync.Source==ogmios, ConnectionStatus=="connected", a non-nil last tip slot, and NetworkSynchronization on the published status (TestCardanoNetworkReconcilerReconcilePublishesNodeSyncStatusWhenCaughtUp already asserts Source+NetworkSynchronization+LagSlots; add ConnectionStatus + Tip.Slot). The envtest already runs through syncedNodeSyncProber giving the Env smoke.
- CNP-05 (Unit): add a table case to TestCardanoNetworkSyncStatusComputesInferredSlotAndLag where tipSlot > inferredTipSlot and assert *LagSlots==0 and *LagSeconds==0 (exercises the max(...,0) clamp at sync_probe.go:284). CNI-03: added to `controller_envtest_test.go` following the existing manager bootstrap (started mgr, prober overrides, WaitForCacheSync); the old fingerprint PVC MUST be deleted by hand after the network delete, or the recreate is correctly rejected as UnsupportedLocalnetChange against the stale annotation.

**Files.**
- `internal/controller/cardanonetwork/controller_envtest_test.go`
- `internal/controller/cardanonetwork/controller_test.go`
- `internal/controller/cardanonetwork/sync_probe_test.go`

**Success criteria.**
- CNI-01: a manager-backed envtest asserts both status.network.LocalnetFingerprint and NetworkFingerprint are recorded non-empty on first reconcile of a local network.
- CNI-02: an envtest mutates network magic (and era in a subtest) after acceptance and asserts Degraded=True/UnsupportedLocalnetChange and Progressing=False/UnsupportedLocalnetChange with the PVC+Deployment localnetFingerprintAnno unchanged.
- CNI-04: an envtest mutates a non-identity field after acceptance and asserts it reconciles with Degraded=False/ReconcileSucceeded and no fingerprint conflict.
- CNL-03: an envtest asserts apierrors.IsNotFound for the <net>-network-artifacts ConfigMap and assertNoVolumeNamed(...,"network-artifacts") on the primary Deployment, at Env level.
- CNL-06: an envtest asserts status.network.Mode==local, *Era==conway, *NetworkMagic==42, and ArtifactsReady=True/ArtifactsReady is sourced from the serve sidecar becoming ready.
- CNP-02: an envtest asserts status.network.Mode==public with *Profile/*NetworkMagic equal to preview=2 and (subtest) preprod=1.
- CNL-07: the unsupported-input reconcile table includes a local-genesis-set row asserting Degraded=True/UnsupportedSpec, all readiness conditions False, and no primary children created.
- CNL-09: the table includes pool-count 0 and 3 rows asserting Degraded=True/UnsupportedSpec (message "local pool count N is not supported") and no primary children.
- CNP-04: the published-status sync test asserts status.sync.Source==ogmios, ConnectionStatus=="connected", a non-nil last tip slot, and NetworkSynchronization.
- CNP-05: a sync-status table case with tipSlot ahead of the inferred tip asserts *LagSlots==0 and *LagSeconds==0 (clamp-to-0, never negative).
- CNP-07: the public reconcile path asserts an admitted-but-unresolvable profile drives Degraded (no unverified fetch) with a code comment cross-referencing CNV-08 as primary admission coverage.
- CNI-03 manager-backed envtest creates, deletes (network + fingerprint PVC), recreates a same-named local network with a different networkMagic, and asserts a new distinct fingerprint with Degraded=False and no UnsupportedLocalnetChange

**Exit criteria.**
- moon run root:test is green (KUBEBUILDER_ASSETS set via setup-envtest), including the new envtest cases under internal/controller/cardanonetwork.
- moon run root:check passes (gofmt/vet/lint clean; git diff --check clean).
- All eleven owned rows (CNI-01/02/04, CNL-03/06/07/09, CNP-02/04/05/07) flip to SATISFIED at the plan's recommended Level, closing the exact Gap named in TEST_COVERAGE_ANALYSIS.md.
- No new flake: each Eventually uses the existing 10s/100ms bounds and the deterministic syncedNode*Prober overrides; no reliance on real pods/GC.
- No change to non-test source files (controller/builder/sync_probe remain untouched); this PR is test-only.

**Risks.** Adding two more full envtest functions (each Start()s its own apiserver) raises the package's envtest wall-clock; mitigate by folding the CNI/CNL-03/CNL-06/CNP-02 assertions into one or two shared managers rather than one apiserver per row, following the existing single-suite pattern. Manager-backed Eventually polls can flake under a loaded CI runner — keep the deterministic prober overrides and fixed Now so readiness is data-driven, not timing-driven. CNP-07 has no real unresolvable-profile reachable through the closed enum, so its Env assertion is a boundary/comment cross-ref to CNV-08 rather than a live admission rejection; honest scope, not a full negative test. No CI-cost beyond envtest (no Kind/Chainsaw needed for any owned row).

---

## P8 — CardanoDBSync reconcile/placement envtest + db rendering coverage  `L`

**PR:** `test(cardanodbsync): restore primary-sidecar manager envtest + db/placement reconcile coverage`  
**Rows (12):** DBD-02, DBD-03, DBD-04, DBD-05, DBD-06, DBD-07, DBF-01, DBF-08, DBS-01, DBS-03, DBS-05, DBS-06  
**Level:** Env  ·  **Depends on:** none

**Objective.** Prove the CardanoDBSync reconcile-output, placement-acceptance, and database-rendering contracts at the Level the plan demands: a manager-backed envtest for the primary-sidecar publish path (restoring the test session 048 removed) plus the follower/database/placement rows, and Unit-level builder coverage for the two untested Postgres-tuning/translation rows. This closes the single sharpest level-mismatch the audit names (DBS publish path has no Env proof).

**Description.** The audit's systemic weakness (A) is that CardanoDBSync reconcile output and placement acceptance are proven only by fake-client direct-Reconcile unit tests, and the one manager-backed envtest that exercised the primary-sidecar happy path through controller-runtime watches/.Owns was removed in commit 22a5e8f (F0 PR-B2) and never replaced. This PR restores that manager envtest and extends the cardanodbsync envtest suite so each owned Env row asserts its named signal through a started apiserver+manager, and adds the two missing Unit-level db-rendering assertions. Concretely: (DBS-01) SidecarMaterialReady=True with status.placement.primarySidecar publishing a sha256: revision plus the ConfigMap/pgpass-Secret/state-PVC/metrics-Service names; (DBS-03) changing the published sidecar revision re-stamps the CardanoNetwork primary Deployment's pod-template annotation (rolls the primary); (DBS-05) switching placement.mode after acceptance is blocked with Degraded/UnsupportedDatabaseIdentityChange and the recreate message, acceptedPlacementMode unchanged; (DBS-06) status.database.acceptedPlacementMode records the accepted mode; (DBF-01) a managed-Postgres follower against a Ready network creates and owns (controllerRef) the follower db-sync Deployment+PVC and the managed Postgres Deployment/Service/PVC/auth Secret; (DBF-08) missing network -> Degraded/NetworkUnavailable and not-ready network -> Progressing/NetworkStatusStale|NetworkArtifactsPending|NodeToNodeEndpointMissing, with no db-sync workload applied; (DBD-02) user-supplied managed authSecretRef is used and no owned credential Secret is created; (DBD-03) external Postgres creates no managed workload and wires host/port/db/user/passwordSecretRef/sslMode; (DBD-07) external missing-password Secret reports not-ready and applies no db-sync Deployment. The two Unit rows: (DBD-06) parameters.maintenanceWorkMem/maxParallelMaintenanceWorkers render as -c maintenance_work_mem=<kB> and -c max_parallel_maintenance_workers=<n> args (managedPostgresArgs is currently entirely untested); (DBD-04) the only_governance insert preset and (DBD-05) ledgerBackend=inmemory + config.runtime flags appear in the rendered db-sync config.

**Approach.** Mechanism: controller-runtime envtest (real apiserver, no kubelet) for the Env rows + table/builder Unit tests for DBD-04/05/06. Mirror the existing patterns exactly.

ENV (extend internal/controller/cardanodbsync/controller_envtest_test.go, reusing startCardanoDBSyncTestManager, createReadyNetworkWithArtifacts, requireDBSyncDegradedReasonEventually, managedCardanoDBSync, localCardanoDBSync, primarySidecarCardanoDBSync, externalDatabaseSecretFor, providedManagedPostgresAuthSecretFor, readyCardanoNetwork):
- DBS-01/DBS-06: new TestCardanoDBSyncControllerManagerPublishesPrimarySidecarMaterial — create a primarySidecar dbSync against a Ready network, Eventually assert SidecarMaterialReady=True (apimeta.FindStatusCondition), status.placement.primarySidecar.Revision has prefix "sha256:", Resources.{ConfigMapName,PGPassSecretName,StatePVCName,MetricsServiceName} equal the dbSyncConfigMapName/dbSyncPGPassSecretName/dbSyncStatePVCName/dbSyncMetricsServiceName helpers, and status.database.acceptedPlacementMode==primarySidecar (mirrors the unit assertions at controller_test.go:304-334).
- DBS-03 (rolls the primary): RESTORE TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync recovered from git commit 22a5e8f (in package internal/controller/cardanonetwork/controller_envtest_test.go — that is where its helpers readyPrimarySidecarDBSync, conditionTypeArtifactsReady, primaryWorkloadSelectorLabels, serveContainerName live, and where the dual-reconciler wiring belongs). It wires both CardanoNetworkReconciler (with syncProberOverride/timingProberOverride) and ctrldbsync.CardanoDBSyncReconciler on one manager, marks the primary Deployment Available + serve sidecar ready, awaits ArtifactsReady, then bumps the attached db-sync's published PrimarySidecar.Revision and asserts the primary Deployment's spec.template.annotations[dbSyncSidecarRevisionAnno] re-stamps to the new value via requireDeploymentDBSyncSidecarRevisionEventually; re-add helpers primarySidecarExternalSecret/requireDeploymentContainerEventually/requireDeploymentDBSyncSidecarRevisionEventually only if not already present. Verify readyPrimarySidecarDBSync still exists (it does, controller_test.go:2363) before re-adding.
- DBS-05: new TestCardanoDBSyncControllerManagerBlocksPlacementSwitchAfterAcceptance — primarySidecar dbSync reaches acceptedPlacementMode==primarySidecar, then Update spec.placement.mode=dedicatedFollower (Generation bump), Eventually assert Degraded=True/UnsupportedDatabaseIdentityChange and status.database.acceptedPlacementMode still primarySidecar (mirrors controller_test.go:391-419).
- DBF-01: new TestCardanoDBSyncControllerManagerCreatesAndOwnsManagedFollower — managedCardanoDBSync against a Ready network; Eventually Get the follower Deployment (dbSyncWorkloadName), follower PVC (dbSyncFollowerPVCName), managed Postgres Deployment/Service/PVC (managedPostgresDeploymentName/managedPostgresServiceName/managedPostgresPVCName) and auth Secret (managedPostgresAuthSecretName) and assert each controlledBy(obj, dbSync) — closes the ownerReference gap the audit names for DBF-01.
- DBF-08: new TestCardanoDBSyncControllerManagerWaitsOnUnavailableAndStaleNetwork — (a) dbSync referencing a non-existent network -> requireDBSyncDegradedReasonEventually(...conditionReasonNetworkUnavailable) + assertMissingObject-equivalent Eventually IsNotFound on dbSyncWorkloadName; (b) reuse the existing envtest's stale-network arm (it already drives conditionReasonNetworkStatusStale) and add a no-workload-applied check. Asserts the "no workload is applied" clause the audit flags as unasserted.
- DBD-02: new TestCardanoDBSyncControllerManagerUsesProvidedManagedAuthSecret — managed dbSync with AuthSecretRef + a pre-created provided Secret; Eventually assert the provided Secret has empty OwnerReferences (no owned credential Secret), the managed Postgres POSTGRES_PASSWORD env points at the provided Secret name, and status.database.authSecretName is empty (mirrors controller_test.go:138-159 at Env level).
- DBD-03: new TestCardanoDBSyncControllerManagerWiresExternalDatabase — localCardanoDBSync (external) against a Ready network + its external secret; Eventually assert the follower db-sync Deployment exists and assertMissingObject-equivalent IsNotFound for managedPostgresDeploymentName/Service/PVC, and the rendered ConfigMap/pgpass wire host/port/database/user/sslMode.
- DBD-07: new TestCardanoDBSyncControllerManagerRefusesExternalMissingPasswordSecret — external dbSync with NO password Secret created; Eventually assert Degraded=True/conditionReasonExternalDatabaseSecretMissing and IsNotFound for dbSyncWorkloadName (the "rather than starting db-sync with no credentials" clause).

UNIT (extend internal/controller/cardanodbsync/builder_test.go / a containers_test.go, mirroring TestDBSyncWorkloadBuilderInsertPresetsDoNotUseDefaultedOverrides and asserting resources.ConfigMap.Data[dbSyncConfigFileName] substrings):
- DBD-06: new TestManagedPostgresArgsRenderTuningParameters — call managedPostgresArgs with a managed spec setting MaintenanceWorkMem (resource.Quantity) and MaxParallelMaintenanceWorkers, assert the returned args contain "-c","maintenance_work_mem=<kB>" (via postgresMemoryQuantity) and "-c","max_parallel_maintenance_workers=<n>"; plus a nil-parameters case returning nil. This is the one fully-untested rendering path.
- DBD-04: add an only_governance subtest (the one preset of four never exercised) asserting its distinctive rendered db-sync config lines.
- DBD-05: add an assertion in/near TestDBSyncWorkloadBuilderPreservesNestedPresetValuesUnlessOverridden that the rendered config contains "ledger_backend: inmemory" when ledgerBackend=inmemory (currently only downstream tx_out lines are asserted), and that config.runtime flags surface in the rendered config.

Avoid duplicating: keep DBD-04/05/06 at Unit (no apiserver) per plan; do not move them into envtest. Do not add a new Moon task — run via moon run root:test (sets KUBEBUILDER_ASSETS).

**Files.**
- `internal/controller/cardanodbsync/controller_envtest_test.go`
- `internal/controller/cardanonetwork/controller_envtest_test.go`
- `internal/controller/cardanodbsync/builder_test.go`
- `internal/controller/cardanodbsync/containers_test.go`

**Success criteria.**
- A manager-backed envtest asserts SidecarMaterialReady=True and status.placement.primarySidecar publishes a sha256: revision plus the ConfigMap/pgpass-Secret/state-PVC/metrics-Service names (DBS-01) and status.database.acceptedPlacementMode==primarySidecar (DBS-06), both through a started apiserver+manager rather than a fake client
- The restored TestCardanoNetworkControllerManagerAttachesPrimarySidecarDBSync wires both reconcilers and asserts the CardanoNetwork primary Deployment's pod-template dbSyncSidecarRevisionAnno re-stamps when the attached db-sync's published revision changes (DBS-03 rolls the primary)
- An envtest asserts switching placement.mode after acceptance yields Degraded=True/UnsupportedDatabaseIdentityChange with acceptedPlacementMode unchanged (DBS-05)
- An envtest asserts a managed-Postgres follower against a Ready network creates and controllerRef-owns the follower db-sync Deployment+PVC and managed Postgres Deployment/Service/PVC/auth Secret (DBF-01)
- An envtest asserts a missing networkRef -> Degraded/NetworkUnavailable and a not-ready network -> Progressing/NetworkStatusStale|NetworkArtifactsPending|NodeToNodeEndpointMissing, with the db-sync Deployment IsNotFound in both (DBF-08, no-workload clause)
- An envtest asserts a user-supplied managed authSecretRef is used with no owned credential Secret created (DBD-02), and that external Postgres creates no managed workload and wires host/port/db/user/sslMode (DBD-03)
- An envtest asserts external Postgres with a missing password Secret reports not-ready (ExternalDatabaseSecretMissing) and leaves the db-sync Deployment IsNotFound (DBD-07)
- A Unit builder/containers test asserts managedPostgresArgs renders -c maintenance_work_mem=<kB> and -c max_parallel_maintenance_workers=<n> (DBD-06, previously zero coverage), the only_governance preset (DBD-04), and ledger_backend: inmemory + runtime flags (DBD-05) in the rendered config

**Exit criteria.**
- moon run root:check passes (gofmt/vet/lint clean, no git diff --check whitespace errors)
- moon run root:test passes green with KUBEBUILDER_ASSETS set via setup-envtest, including the new cardanodbsync and cardanonetwork envtest functions
- All 12 owned rows (DBD-02..07 portion, DBF-01, DBF-08, DBS-01, DBS-03, DBS-05, DBS-06) flip from partial/not-satisfied to satisfied at their planned Level (Env for DBD-02/03/06-env-equivalents/DBF/DBS, Unit for DBD-04/05/06)
- No new flake: each envtest Eventually uses the suite's existing timeouts (10s/100ms or 1-2min for revision re-stamp) and the dual-reconciler test reuses the recovered markAvailable/awaitArtifactsReady re-stamping loop to avoid stale-cache races

**Risks.** The restored dual-reconciler primary-sidecar envtest is the historically flakiest case: the primary Deployment generation bumps on each sidecar attach, flipping serve-sidecar readiness, so the recovered markAvailable/awaitArtifactsReady re-stamp loop and the 2-minute revision-re-stamp Eventually MUST be carried over verbatim to avoid stale-cache races under CI load; this is the main flake risk and the reason the test is L not M. CI cost: this PR adds ~7 new envtest functions, each starting its own apiserver via startCardanoDBSyncTestManager (the suite pattern), which lengthens root:test wall-clock; acceptable since they run in the existing PR test job, but consider whether two of the lightweight Env rows (DBD-02/DBD-03) can share one manager start to bound cost. envtest does not run the GC cascade, so DBF-01 asserts ownerReferences only (the actual cascade is an E2E row owned elsewhere). Scope risk: DBS-02 and DBF-03/DBF-06 are deliberately NOT in this set (E2E/consume-side) — keep the restored network-package test focused on the DBS-03 roll assertion and do not let it drift into asserting DBSyncAttachmentReady end-to-end readiness which belongs to E2E.

---

## P9 — Chainsaw E2E hardening: GC-cascade, real Postgres/sidecar readiness, and metrics/health negatives  `M`

**PR:** `test(e2e): chainsaw GC-cascade, real Postgres/sidecar readiness, and metrics/health negatives`  
**Rows (7):** CNL-10, DBF-03, DBF-09, DBS-02, MGR-05, MGR-06, MGR-07  
**Level:** E2E · Chainsaw  ·  **Depends on:** none — extends the existing Chainsaw suite (`test/chainsaw`); independent of the P10 Go harness

**Objective.** Prove the seven E2E-only contracts that only the packaged operator in a real Kind cluster can demonstrate: deletion-driven garbage collection of owned children via ownerReferences (not namespace teardown), real managed-Postgres connection readiness, the primary-sidecar db-sync attachment in the live primary Pod, and the unauthenticated/unhealthy negative paths on the manager's metrics and health endpoints.

**Description.** Extends the single Chainsaw suite test/chainsaw/manager-smoke/chainsaw-test.yaml (no new harness; it is the only E2E we have) to close seven gaps the analysis flags as E2E-only. (1) MGR-05: add a curl Pod step asserting the manager's health-probe bind address serves /healthz and /readyz with HTTP 200. (2) MGR-06: fix the dead go_goroutines assertion — the curl-metrics inner script has no set -e, so the grep -q "go_goroutines" on line 151 has its non-zero exit swallowed before exit 0 on line 152; rewrite so the body assertion gates success. (3) MGR-07: clone the curl-metrics Pod with NO token and with a bogus token and assert http_code 401/403 (not 200). (4) DBF-03: tighten the existing phase6-managed wait to assert PostgresReady=True with reason PostgresReady is reached via a real Postgres accepting connections (the dbsync-psql probe already proves a live connection — bind the PostgresReady reason assertion to it). (5) DBS-02: apply a NEW primarySidecar CardanoDBSync (phase5-sidecar) referencing phase4-smoke and assert CardanoNetwork phase4-smoke reports DBSyncAttachmentReady=True and the primary Deployment phase4-smoke-node runs the db-sync sidecar container in its real Pod. (6+7) CNL-10 + DBF-09: the load-bearing GC-cascade — delete the parent CR specifically (kubectl delete cardanonetwork phase4-smoke / kubectl delete cardanodbsync phase6-managed) and LEAVE the yacd-smoke namespace standing, then poll until every owned child reaches NotFound by name. Today cleanup (line 861) deletes the whole namespace, which cascades regardless of ownerRefs and proves nothing about garbage collection. All steps stay on the local network and obey dash-portability (set -eu, POSIX case/[ ], $(seq) loops) like the existing steps.

**Approach.** Mirror the existing manager-smoke conventions exactly: workDir ../../.., export KUBECTL_KUBERC, set -eu, $(seq 1 N) poll loops with kubectl get -o jsonpath, POSIX case for status matching, and curl Pods cloned from the existing curl-metrics / curl-faucet pattern (restricted securityContext, curlimages/curl:8.11.1, tmp emptyDir). Step ordering within step deploy-controller, all before cleanup, all on the local phase4-smoke network. MGR-07 (negative metrics): right after the existing authorized curl-metrics success assertion, add a curl-metrics-unauth Pod (no metrics-reader binding, or explicit bogus Authorization header) looping curl -sk -o /dev/null -w '%{http_code}' against the :8443/metrics URL with (a) no Authorization header and (b) -H 'Authorization: Bearer bogus', asserting each http_code is 401 or 403 via POSIX case '401|403) ;; *) exit 1'; wait for Pod phase Succeeded. MGR-06 fix: add 'set -eu' to the curl-metrics inner /bin/sh -c script (or replace 'grep -q ...; exit 0' with 'if grep -q "go_goroutines" /tmp/metrics.txt; then exit 0; fi; echo missing; exit 1') so the body grep gates. MGR-05 (health 200): add a curl/exec step targeting the manager health-probe bind address — read cmd/options.go and the charts/yacd manager Deployment for the exact --health-probe-bind-address and Pod port before wiring (do NOT assume :8081); if no Service fronts it, kubectl exec curl from inside the manager Pod, asserting 200 for /healthz and /readyz. DBS-02 (sidecar attach): after phase4-smoke reaches Ready, apply a primarySidecar CardanoDBSync phase5-sidecar (spec.networkRef.name=phase4-smoke, spec.placement.mode=primarySidecar, database.managed, local non-mainnet which DBS-04 permits); poll phase5-sidecar for SidecarMaterialReady=True, then poll cardanonetwork phase4-smoke for DBSyncAttachmentReady=True, and assert the db-sync sidecar container is present in the phase4-smoke-node Deployment template and live primary Pod via kubectl get deployment/pod -o jsonpath (confirm the real sidecar container name in internal/controller/cardanonetwork/dbsync_sidecar.go before hardcoding). DBF-03 (real Postgres readiness): the phase6-managed wait already asserts PostgresReady=True && reason==PostgresReady and dbsync-psql proves a live psql connection (DBF-06); add a comment + an assertion binding PostgresReady=True/PostgresReady to the successful dbsync-psql connection so 'accepts local connections' is the asserted subject. CNL-10 + DBF-09 (GC cascade — the subtle one): add a NEW step as the final positive step. FIRST kubectl delete cardanodbsync phase6-managed (and phase5-sidecar) --wait=true, then poll until every owned CardanoDBSync child is NotFound by name: phase6-managed-dbsync, phase6-managed-postgres (Deployment+Service), phase6-managed-postgres-auth, phase6-managed-dbsync-pgpass (Secrets), phase6-managed-dbsync-config (ConfigMap), phase6-managed-dbsync-state/-follower-state/-postgres-state (PVCs), phase6-managed-dbsync-metrics (Service). THEN kubectl delete cardanonetwork phase4-smoke --wait=true and poll until phase4-smoke-node (Deployment), phase4-smoke-node-state (PVC), -node/-ogmios/-kupo/-faucet/-artifacts (Services), phase4-smoke-faucet-auth (Secret) are NotFound. CRITICALLY do NOT delete the yacd-smoke namespace in this step — NotFound must prove ownerReference GC, not namespace cascade. DBSync delete runs before network delete because the follower references the network. Child names/labels confirmed from internal/controller/cardanodbsync/names.go + labels.go (yacd.meigma.io/cardanodbsync) and cardanonetwork/labels.go (yacd.meigma.io/cardanonetwork). The existing cleanup kubectl delete namespace yacd-smoke is kept only as the safety-net teardown.

**Files.**
- `test/chainsaw/manager-smoke/chainsaw-test.yaml`

**Success criteria.**
- MGR-07: a curl Pod issues an unauthenticated and a bogus-token request to the manager metrics endpoint and the step asserts each http_code is 401 or 403 (never 200); Pod reaches phase Succeeded.
- MGR-06: the go_goroutines body assertion is load-bearing - removing go_goroutines from the metrics output would now fail the curl-metrics Pod (the swallowed-exit-before-exit-0 bug is gone).
- MGR-05: a step asserts the manager health-probe address returns HTTP 200 for both /healthz and /readyz.
- DBF-03: the suite asserts PostgresReady=True with reason PostgresReady on phase6-managed bound to a real psql connection succeeding against the managed Postgres Service.
- DBS-02: a primarySidecar CardanoDBSync (phase5-sidecar) referencing phase4-smoke drives CardanoNetwork phase4-smoke to DBSyncAttachmentReady=True, and the db-sync sidecar container is asserted present in the real primary Pod / phase4-smoke-node Deployment template.
- CNL-10: after kubectl delete cardanonetwork phase4-smoke (namespace left intact), all owned children (phase4-smoke-node Deployment, phase4-smoke-node-state PVC, -node/-ogmios/-kupo/-faucet/-artifacts Services, phase4-smoke-faucet-auth Secret) are polled to NotFound.
- DBF-09: after kubectl delete cardanodbsync phase6-managed (namespace left intact), all owned children (phase6-managed-dbsync & -postgres Deployments, -postgres Service, -dbsync-metrics Service, -postgres-auth & -dbsync-pgpass Secrets, -dbsync-config ConfigMap, -dbsync-state/-follower-state/-postgres-state PVCs) are polled to NotFound.
- All new scripts use set -eu and POSIX-portable constructs consistent with the existing dash-targeted steps; no bashisms introduced.

**Exit criteria.**
- moon run root:test-e2e is green locally (Kind-backed chainsaw run passes end to end, including the new GC-cascade, sidecar-attach, metrics-negative, and health steps).
- moon run root:check passes (no shell/yaml lint or git diff --check whitespace regressions in the edited chainsaw file).
- CNL-10, DBF-03, DBF-09, DBS-02, MGR-05, MGR-06, MGR-07 each flip from partial/not-satisfied to satisfied at the E2E level the plan recommends.
- The GC-cascade step deletes the parent CRs specifically and leaves the yacd-smoke namespace standing, so NotFound proves ownerReference GC and not namespace teardown.
- No new flake: poll loops have bounded $(seq) retries with diagnostic dumps on failure, mirroring existing steps; the catch block dumps the new resources on failure.

**Risks.** CI cost: this is the single most expensive job in the repo (real Kind cluster, real cardano-node/Ogmios/Kupo/db-sync/Postgres). The GC-cascade adds delete+poll time but is small relative to the existing 12m+ sync waits; the DBS-02 sidecar attach spins up an additional primarySidecar CardanoDBSync (extra Postgres+db-sync pods) which adds real pod-scheduling and image-pull time - keep its readiness wait scoped to SidecarMaterialReady/DBSyncAttachmentReady (material attachable) rather than full db-sync block population to avoid a second long sync. Ordering hazard: the CardanoDBSync children must be deleted (and confirmed gone) before the CardanoNetwork is deleted, because the follower references the network; getting this backwards risks a finalizer/ordering hang. Bind-address risk: MGR-05 requires confirming the actual --health-probe-bind-address and whether a Service or in-Pod exec is needed before wiring - verify against cmd/options.go and the chart Deployment, do not assume :8081. The GC-cascade is destructive to phase4-smoke/phase6-managed, so it must be the final positive step after every other assertion that depends on those resources (e.g. the existing kupo/faucet-disable patch must run before the delete). Flake guard: poll with bounded retries and dump kubectl get/describe on timeout; extend the existing catch block to dump curl-metrics-unauth and phase5-sidecar.

---

## P10 — CLI e2e harness (boilerplate) — Go test/e2e package + root:test-e2e-cli Moon task  `M`

**PR:** `test(e2e): add Go CLI e2e harness and root:test-e2e-cli task`  
**Rows (0):** _none (boilerplate — enables a later phase)_  
**Level:** E2E · Go harness (boilerplate)  ·  **Depends on:** none — boilerplate; unblocks P11

**Objective.** Stand up a reusable Go e2e harness that drives the compiled yacd CLI against a live Kind+operator cluster, so the live-kubelet-dependent verbs (run/connect/exec/topup, port-forward, in-pod exec) can be asserted in P11. This PR proves the harness end to end with a trivial smoke and wires it into CI as a distinct, gateable job.

**Description.** BOILERPLATE PHASE — owns 0 TEST_PLAN requirement rows; it is the enabling substrate for P11 (which will assert HST-01/HST-05/CLI-09/MGR-07 and similar E2E `(+E2E)` secondaries the analysis flags as missing in "Systemic level-mismatches (C)" — real port-forward, in-cluster exec, down child-GC, unauth metrics reject). YACD already ships every CLI verb (`up`/`down`/`list`/`info`/`version`/`run`/`connect`/`exec`/`topup`) and the kube adapter helpers (`Adapter.Forward`, `Adapter.Exec`, `Adapter.PrimaryPodName` in cli/internal/kube/access.go, and the YACD_* contract in cli/internal/cli/envcontract.go) — Test Harness Phases 1-2 are landed. What is missing is a Go test harness that exercises the *built binary* against a real cluster: the existing E2E layer is the single Chainsaw manager-smoke suite, which never drives the CLI host-access verbs. Phase 0 (TEST_HARNESS_PHASE0_RESULTS.md) already empirically proved the substrate: cold-start to Ready is 27s (full pipeline 112s) vs the 10-12m budget on ubuntu-latest, teardown GC is clean, and run/exec host-access cross-confirm the same tip — but the *documented* `.dev/scripts/test-e2e.sh` `docker build .` path is BROKEN (.dockerignore strips the `go:embed` publicnet profiles), so this harness must build manager/faucet via ko (`.dev/ko-build.sh` / `.dev/ko-build-faucet.sh`), exactly as Phase 0 did. This PR delivers that harness plus a smoke that proves it.

**Approach.** Mechanism: a new Go package `test/e2e` (build-tagged `//go:build e2e` so `moon run root:test` never compiles cluster-bound code) driven by a new `.dev/scripts/test-e2e-cli.sh` and `root:test-e2e-cli` Moon task. Mirror the substrate-provisioning shape of `.dev/scripts/test-e2e.sh` but fix its known defect: build the manager and faucet with ko via `.dev/ko-build.sh`/`.dev/ko-build-faucet.sh` (NOT `docker build`), `kind load` them, preload Ogmios/Kupo to dodge Docker Hub rate limits (Phase 0 caveat 2), `kubectl apply -f charts/yacd/crds`, then `helm upgrade --install yacd charts/yacd -n yacd-system` with the ko image refs and `IfNotPresent` — the exact sequence proven in TEST_HARNESS_PHASE0_RESULTS.md. The script exports `KUBECONFIG` and runs `go test -tags e2e ./test/e2e/...`.

Harness helpers in `test/e2e` (the banked design from TEST_HARNESS_PROPOSAL.md §6, realized as test infra):
- `runYacd(t, args...) result` — builds `./cli/cmd/yacd` once via `go build` in `TestMain` to a temp binary, runs it with the test KUBECONFIG, captures exitCode/stdout/stderr; a `result.JSON(&v)` helper unmarshals `--json` stdout (used by `yacd list --json`/`yacd info --json`).
- Local-network fixture `newNetwork(t)` — runs `yacd up phase4-smoke -n <ns> -f examples/local/yacd.yaml --timeout 10m` (the identical command the Chainsaw apply step at manager-smoke/chainsaw-test.yaml:197 already uses) and registers a `t.Cleanup` that runs `yacd down`; this is the create/teardown via yacd up/down the brief asks for, reusing examples/local/yacd.yaml.
- client-go-free endpoint discovery — read the operator's published `status.endpoints.*` straight off the CardanoNetwork via `kubectl get cardanonetwork ... -o jsonpath` wrapped in a Go helper (no client-go import in the test package), matching the analysis's preference to consume the published contract rather than internal labels (the same contract `PrimaryPodName` consumes).
- signal helper `startInterruptible(t, args...)` — starts a long-lived child (`yacd connect`/`yacd run`) with `Setpgid`, returns a handle whose `Interrupt()` sends SIGINT and waits, so P11 can assert connect's hold-open-until-^C and cleanup-on-interrupt (HST-08/HST-11) without leaking processes.

Smoke (this PR's proof, asserting no requirement row): `TestHarnessSmoke` runs `yacd version` (exit 0, stdout contains the injected build line) and, against the live fixture, `yacd list -n <ns> --json` and asserts the phase4-smoke network appears Ready — proving binary build, cluster wiring, fixture up/down, and JSON capture all work.

Docs: a top-of-file doc comment in `test/e2e/doc.go` records WHY these verbs need a live kubelet — port-forward (SPDY through the kubelet, `Adapter.Forward`), in-pod exec (`Adapter.Exec`), and real Service endpoints answering — and therefore cannot be envtest (envtest has no kubelet, no pods, no networking, no GC), citing the TEST_PLAN E2E taxonomy.

CI wiring: `root:test-e2e-cli` is a distinct Moon task (separate from `root:test-e2e`) with `runInCI: true` but added to CI as its OWN job so it is independently gateable/parallelizable; given the heavier bring-up it is labeled in the task description as the CLI-e2e gate. Inputs scoped to goSources + chartSources + the new script so caching is correct.

**Files.**
- `test/e2e/doc.go`
- `test/e2e/harness.go`
- `test/e2e/fixture.go`
- `test/e2e/endpoints.go`
- `test/e2e/signal.go`
- `test/e2e/smoke_test.go`
- `test/e2e/main_test.go`
- `.dev/scripts/test-e2e-cli.sh`
- `moon.yml`
- `.github/workflows/ci.yml`

**Success criteria.**
- `test/e2e` Go package exists, is gated by `//go:build e2e` so `moon run root:test` (plain envtest run) does not compile it, and builds the `./cli/cmd/yacd` binary once in TestMain
- Harness exposes the four helpers the brief names: runYacd(args) capturing exitCode/stdout/JSON, a local-network fixture (yacd up/down on examples/local/yacd.yaml), client-go-free endpoint discovery from status.endpoints, and a SIGINT signal helper for a long-lived child
- `.dev/scripts/test-e2e-cli.sh` provisions Kind + ko-built manager/faucet (NOT docker build) + helm-installed chart and reconciles phase4-smoke, reusing the Phase 0 / Chainsaw substrate sequence
- `moon run root:test-e2e-cli` runs the e2e package green: `yacd version` exits 0 with the build line, and `yacd list --json` against the live cluster shows phase4-smoke Ready
- test/e2e/doc.go documents why port-forward/exec/live-endpoint verbs require a kubelet and cannot be envtest
- A distinct CI job invokes `root:test-e2e-cli` separately from the existing Chainsaw `root:test-e2e` job, so it is independently gateable/parallelizable

**Exit criteria.**
- `moon run root:check` green (lint/build/vet, including the e2e-tagged package under `go vet -tags e2e`)
- `moon run root:test` green and unchanged (the `//go:build e2e` tag keeps the new package out of the envtest run; no new envtest compiled)
- `moon run root:test-e2e-cli` green locally on Kind: binary builds, fixture comes up to Ready and tears down clean, `TestHarnessSmoke` passes
- Existing `moon run root:test-e2e` (Chainsaw) still passes unchanged
- The new CI job runs `root:test-e2e-cli` as its own gateable step and is green; no background process or Kind cluster leaks (script trap-deletes the cluster)

**Risks.** CI cost/time is the main risk: this is a full Kind + ko-build + localnet-to-Ready bring-up (~112s on ubuntu-latest per Phase 0, but heavier than a unit job), so it MUST be a separate CI job, not folded into the PR unit lane — labeled here as the CLI-e2e gate so it can be parallelized or made required-but-isolated. Flakiness vectors: Docker Hub rate-limiting Ogmios/Kupo pulls (mitigate by preloading both, per Phase 0 caveat 2); the 2-vCPU private-runner tier is untested (Phase 0 caveat 1) — gate on ubuntu-latest for now. Must NOT reuse `.dev/scripts/test-e2e.sh`'s broken `docker build .` path (strips go:embed profiles) — build via ko. The signal helper must set Setpgid and have t.Cleanup kill the process group to avoid leaking `connect`/`run` children across tests. Scope risk: keep this PR strictly boilerplate — the smoke asserts no TEST_PLAN row; resist adding HST/CLI assertions here (those are P11).

---

## P11 — CLI host-access E2E (down/run/exec/connect against a live network)  `M`

**PR:** `test(e2e): exercise yacd down/run/exec/connect against a live network`  
**Rows (3):** CLI-09, HST-01, HST-05  
**Level:** E2E · Go harness  ·  **Depends on:** P10

**Objective.** Prove the YACD CLI host-access verbs actually work against a packaged, real-cluster network (not just mocks): run forwards Ogmios to loopback and a probe answers queryNetwork/tip, exec reaches the node socket in-pod, and down drives a real GC-cascade WAIT to exit 0. This closes the (+E2E) secondary the plan calls for on CLI-09, HST-01, and HST-05, which today have only unit/mock coverage.

**Description.** CLI-09, HST-01, and HST-05 are all "Unit (+E2E)" rows whose unit halves exist but whose real-cluster halves are entirely missing: the coverage audit notes the down test models "gone" purely as the parent CardanoNetwork returning NotFound (never an owned-child GC cascade), and that there is zero E2E driving run/exec/connect/forward — a grep of test/chainsaw shows no run/exec/connect/forward references. This PR adds the E2E half on the P10 harness, driving the real yacd binary against the live phase4-smoke local network (namespace yacd-smoke) the existing manager-smoke suite already stands up. It asserts the CLI-observable WAIT/exit behavior of down (complementing the chainsaw object-GC proof), the run port-forward + YACD_* env contract end to end (HST-01/HST-02), and the in-pod exec socket path + remote exit-code propagation (HST-05), plus a cheap connect lifecycle smoke (endpoints.json perms/token-free, loopback reachability, SIGINT cleanup). It stays on the local network with bounded per-verb timeouts and adds no expensive chain sync.

**Approach.** Mechanism: extend the P10 Go-based E2E harness under test/e2e (gated by moon run root:test-e2e / .dev/scripts/test-e2e.sh, runInCI:true), reusing the already-running phase4-smoke / yacd-smoke network that manager-smoke brings up via `go run ./cli/cmd/yacd up phase4-smoke -n yacd-smoke -f examples/local/yacd.yaml`. Drive the real CLI binary as a subprocess from the harness host (which has the Kind kubeconfig and localhost reachability that SPDY port-forward needs), mirroring how chainsaw already shells the CLI. Each verb gets one focused, bounded sub-test:
- HST-01/HST-02 (run): `yacd run phase4-smoke -n yacd-smoke -- <probe>` where the probe reads YACD_OGMIOS_URL (ws://127.0.0.1:<localport>) and issues the same JSON-RPC the chainsaw curl-ogmios step uses ({"jsonrpc":"2.0","method":"queryNetwork/tip"}), asserting a "result" payload with a tip — but over the forwarded loopback WebSocket rather than the in-cluster Service. Reuse the in-repo Ogmios/WebSocket client (the faucet topup apollo/chain client already dials Ogmios) for the probe, or a tiny gorilla/websocket round-trip, so we assert a real tip rather than a bare 200. Then assert exit-code propagation: `yacd run ... -- sh -c "exit 7"` yields CLI exit 7 (HST-02), mirroring run_test.go:TestRunPropagatesChildExitCode but through the real forward.
- HST-05 (exec): `yacd exec phase4-smoke -n yacd-smoke -- cardano-cli query tip` runs in the primary cardano-node Pod over CARDANO_NODE_SOCKET_PATH=/ipc/node.socket (exec.go pins these), asserting a JSON tip in stdout; then `yacd exec ... -- sh -c "exit 5"` (or a failing cardano-cli) propagates the remote exit code (mirrors exec_test.go:TestExecPropagatesRemoteExitCode but in-cluster).
- CLI-09 (down): run last so it tears down the network. `yacd down phase4-smoke -n yacd-smoke` (default --wait) blocks on kube.WaitGone until the CR and owned children are NotFound and exits 0; assert exit 0 and that a CR Get returns NotFound. Then a second `yacd down phase4-smoke -n yacd-smoke` is idempotent success (exit 0) per down.go's DeleteCardanoNetwork tolerating absence. This complements the chainsaw object-level GC assertions by proving the CLI WAIT/exit contract specifically.
- connect (cheap lifecycle smoke, supports HST-01 family): start `yacd connect phase4-smoke -n yacd-smoke` as a backgrounded subprocess in a temp CWD, wait for .yacd/<ns>/phase4-smoke/endpoints.json, assert dir 0700 / file 0600 and that the document carries no faucet token, do one loopback curl against the kupoUrl from the file, then send SIGINT and assert the file is removed and the "Disconnecting" message printed.
Because down destroys phase4-smoke, sequence the host-access verbs (run/exec/connect) before down within one ordered E2E test, or have P10 expose a fresh per-test network; prefer ordering on the existing network to avoid a second 10m bring-up. Keep each verb under a bounded context (e.g. 60-90s) so a hung forward fails fast rather than wedging CI.

**Files.**
- `test/e2e/hostaccess_test.go`
- `test/e2e/helpers_test.go`
- `docs/host-access.md`

**Success criteria.**
- yacd run phase4-smoke -n yacd-smoke -- <probe> succeeds: the probe reads YACD_OGMIOS_URL (ws://127.0.0.1:<localport>) and gets a queryNetwork/tip 'result' with a real tip over the forwarded loopback connection (HST-01)
- yacd run phase4-smoke -- sh -c "exit 7" makes the CLI exit 7, proving non-zero child exit-code propagation through the real port-forward wrapper (HST-02)
- yacd exec phase4-smoke -n yacd-smoke -- cardano-cli query tip returns a JSON tip from inside the primary node Pod over /ipc/node.socket, and a failing in-pod command propagates its remote exit code (HST-05)
- yacd down phase4-smoke -n yacd-smoke (default --wait) blocks until the CardanoNetwork and its owned children are NotFound and exits 0; a CR Get afterward returns NotFound (CLI-09 WAIT/exit half)
- A second yacd down phase4-smoke -n yacd-smoke exits 0 (idempotent success on an already-gone network)
- connect smoke: .yacd/yacd-smoke/phase4-smoke/endpoints.json is written 0600 under a 0700 dir, contains no faucet token, a loopback curl against its kupoUrl succeeds, and SIGINT removes the file and prints the disconnect message
- docs/host-access.md notes the verbs are exercised end-to-end by the e2e suite (no new YACD_* contract claims beyond what exists)

**Exit criteria.**
- moon run root:test-e2e is green with the new host-access E2E test driving real run/exec/connect/down against phase4-smoke
- All three owned rows (CLI-09, HST-01, HST-05) flip from partial to satisfied at the (+E2E) secondary level the plan recommends; unit halves remain green
- moon run root:check and moon run root:test stay green (the new test compiles, is e2e-tagged/segregated so it does not run in the unit job, and adds no flake)
- git diff --check clean; the host-access verbs run before down within the ordered E2E so phase4-smoke teardown does not race the run/exec/connect assertions

**Risks.** CI cost and flakiness are the main risks. (1) These verbs need a real Ready phase4-smoke network, which is a ~10m local-node bring-up — acceptable only because it shares the existing test-e2e job (runInCI:true) rather than the PR unit CI; do not move this to the unit job. (2) SPDY port-forward from the harness host depends on Kind localhost reachability and a freshly resolved primary Pod; a pod restart mid-forward could surface run's lost-connection path — keep per-verb timeouts bounded (60-90s) and retry the forward establish once rather than asserting on a single attempt. (3) Ordering hazard: down destroys phase4-smoke, so run/exec/connect MUST execute before down in the same ordered test (or P10 must hand out a fresh network) — a wrong order would tear the network down under the host-access probes. (4) The Ogmios loopback probe needs a WebSocket round-trip (Ogmios JSON-RPC), not the plain HTTP-POST shortcut the in-cluster chainsaw curl uses; reuse the repo's existing Ogmios client to avoid a brittle hand-rolled handshake. (5) connect's SIGINT cleanup assertion is timing-sensitive; poll for endpoints.json removal with a short bounded deadline instead of a fixed sleep.

---

## P12 — Real-binary and public-network E2E coverage (gated nightly + documented manual)  `L`

**PR:** `test(e2e): gated real-binary and public-network coverage`  
**Rows (6):** CNP-01, CNP-03, CTN-01, CTN-02, MGR-10, TLS-01  
**Level:** E2E · gated/manual  ·  **Depends on:** P10 (partial)

**Objective.** Close the six requirements that can only be proven by running real cardano-testnet/cardano-tools binaries or by joining a real public network, by moving them out of the ~8-min PR CI job into a gated nightly GitHub workflow plus one documented manual proof. This proves the actual create-env output layout, the funded utxo-keys the faucet depends on, the real generate plan+fingerprint, single-leader election, a bounded public-preview join, and (manually) the mainnet Mithril bootstrap.

**Description.** CTN-01/CTN-02 and TLS-01 today are only touched transitively through the faucet chainsaw smoke; nobody ever inspects the real cardano-testnet env-dir layout (configuration.yaml, yacd-localnet-plan.json, utxo-keys/utxo1..) or the funded source material the faucet source-address init reads. MGR-10's "exactly one leader" runtime outcome is never exercised (chainsaw deploys a single manager). CNP-01 (public preview join + partial sync) and CNP-03 (mainnet Mithril restore) are flagged EXPENSIVE in the plan and have no real-network proof. This PR makes each row SATISFIED at an honestly-gated level: CTN-01/02, TLS-01, MGR-10, and CNP-01 become a new nightly Chainsaw/real-binary job (cron, not per-PR); CNP-03 becomes a documented, periodically-executed manual runbook with a recorded proof artifact, because a mainnet Mithril restore is too heavy for any routine CI runner. The honest claim is: "satisfied" here means a green nightly run (or a recorded manual proof for CNP-03), NOT a per-PR assertion.

**Approach.** Gating mechanism mirrors the existing .github/workflows/security-scan.yml convention exactly (on: schedule: cron + workflow_dispatch:, permissions: {} then per-job contents: read). Add .github/workflows/e2e-nightly.yml with a nightly cron and manual dispatch, reusing the setup-toolchain + Go cache blocks from ci.yml. It runs a new Moon task root:test-e2e-nightly (backed by .dev/scripts/test-e2e-nightly.sh, cloned from .dev/scripts/test-e2e.sh) that points Chainsaw at a separate test/chainsaw-nightly directory using the same test/chainsaw/chainsaw-config.yaml shape, so PR CI's root:test-e2e (test/chainsaw/manager-smoke) is untouched and stays ~8 min.

CTN-01/CTN-02 + TLS-01 (real binaries): the real cardano-testnet and cardano-tools binaries do NOT exist on the host or in the Go module path — they live only inside the published container images (ghcr.io/meigma/yacd/cardano-testnet and .../cardano-tools, per internal/cardano/toolsimage). So inspection must happen inside a container, not as a plain envtest. Add a docker-based step in the nightly script that runs the cardano-tools image's `generate` verb (the real-binary path of containers/cardano-tools/internal/generate/generate.go, which execs cardano-testnet create-env and then enrichConfigFile) against a tmp output dir, then asserts the layout the code contracts in internal/cardano/localnet/types.go Layout: env-dir/configuration.yaml exists, env-dir/yacd-localnet-plan.json exists and (jq) carries inputs.networkMagic and fingerprint.value (TLS-01 + the TLS-10 manifest shape over a REAL plan), and env-dir/utxo-keys/utxo1/{utxo.addr,utxo.vkey,utxo.skey} exist with the GenesisUTxO key types that services/faucet/internal/sources/sources.go discovers (CTN-01 layout; CTN-02 derivability is then proven by execing the faucet image's source-address listing against that utxo-keys dir, asserting utxo1/utxo2 resolve to addr_test1 addresses). This mirrors the existing containers/cardano-tools generate-dry-run.txtar contract but exercises the non-dry-run real binary the txtar deliberately stubs out.

MGR-10 (leader election): add test/chainsaw-nightly/leader-election/chainsaw-test.yaml that installs the chart with replicaCount=2 and leaderElection.enabled=true (charts/yacd/values.yaml + controller-deployment.yaml --leader-elect arg + rbac-leader-election.yaml Role/RoleBinding already support this), waits for the Deployment to report 2 ready replicas, then asserts exactly one holder via the coordination.k8s.io Lease (assert the Lease named yacd... exists with a single .spec.holderIdentity) and a script step greps the two manager pods' logs for exactly one "successfully acquired lease" and the standby's "attempting to acquire leader lease" without reconcile activity.

CNP-01 (public preview, bounded): add test/chainsaw-nightly/public-preview/chainsaw-test.yaml applying mode: public, profile: preview, with a time-boxed assert (chainsaw timeout) requiring OgmiosReady=True and a status.sync probe showing source=ogmios with networkSynchronization advancing above a small floor (NOT full sync) within a bounded window; the test passes on bounded partial progress and is marked nightly-only because it needs real internet egress + a real cardano-node and can take many minutes.

CNP-03 (mainnet Mithril): do NOT automate. Add docs/runbooks/mainnet-mithril-bootstrap.md describing the manual/periodic proof: apply a mode: public, profile: mainnet, bootstrap.mithril network on a sized cluster, observe the Mithril init container verify-and-restore a snapshot before cardano-node starts (the structural ordering is already covered in controller_test.go), and record the run output as the periodic proof. The nightly workflow links this runbook in its job summary so the gap is tracked, not silently dropped.

**Files.**
- `.github/workflows/e2e-nightly.yml`
- `.dev/scripts/test-e2e-nightly.sh`
- `moon.yml`
- `test/chainsaw-nightly/leader-election/chainsaw-test.yaml`
- `test/chainsaw-nightly/public-preview/chainsaw-test.yaml`
- `test/chainsaw-nightly/real-binary/chainsaw-test.yaml`
- `docs/runbooks/mainnet-mithril-bootstrap.md`

**Success criteria.**
- A new gated workflow .github/workflows/e2e-nightly.yml runs on cron + workflow_dispatch only (mirrors security-scan.yml's on:/permissions: {} shape), never on pull_request, so PR CI cost is unchanged
- A new Moon task root:test-e2e-nightly drives test/chainsaw-nightly without touching root:test-e2e (test/chainsaw/manager-smoke stays the per-PR smoke)
- CTN-01: the nightly real-binary job runs the cardano-tools image generate verb and asserts the env dir contains configuration.yaml, yacd-localnet-plan.json, and utxo-keys/utxo1/{utxo.addr,utxo.vkey,utxo.skey}
- CTN-02: the same job execs the faucet image source listing against that utxo-keys dir and asserts utxo1 and utxo2 resolve to addr_test1 GenesisUTxO addresses (the funded sources the faucet expects)
- TLS-01: the generated yacd-localnet-plan.json from the REAL binary is asserted (jq) to carry inputs.networkMagic and a 64-hex fingerprint.value
- MGR-10: leader-election chainsaw test deploys 2 manager replicas and asserts exactly one Lease holderIdentity plus exactly one 'acquired lease' across the two pods
- CNP-01: public-preview chainsaw test joins preview and asserts OgmiosReady=True with bounded networkSynchronization progress within a time-boxed window (partial sync, not full)
- CNP-03: docs/runbooks/mainnet-mithril-bootstrap.md documents the manual/periodic proof and is linked from the nightly job summary; the row is satisfied-by-documented-manual-proof, explicitly NOT by routine CI

**Exit criteria.**
- moon run root:check is green (new Moon task wired, scripts shellcheck-clean, no whitespace via git diff --check)
- The existing per-PR moon run root:test-e2e (manager-smoke) is unchanged and still green; the nightly suite lives under test/chainsaw-nightly and does not run in PR CI
- A manual workflow_dispatch of e2e-nightly.yml completes green: real-binary (CTN-01/02, TLS-01) and leader-election (MGR-10) jobs pass; public-preview (CNP-01) passes within its time-boxed window
- docs/runbooks/mainnet-mithril-bootstrap.md exists, is linked from the nightly job summary, and records at least one executed proof so CNP-03 is demonstrably satisfied by documented manual proof
- CNP-01, CNP-03, CTN-01, CTN-02, MGR-10, TLS-01 each flip from partial/not-satisfied to satisfied at the gated level the plan recommends

**Risks.** Flakiness and CI cost are the central honest risks. CNP-01 joins the real preview network over the public internet: it is slow (many minutes), bandwidth-heavy, and can flake on upstream peer/egress issues — it MUST be time-boxed to bounded partial sync and kept nightly-only with generous chainsaw timeouts; never gate a PR on it. The real-binary jobs (CTN-01/02, TLS-01) pull and run the published cardano-tools/cardano-testnet images and exec create-env, which is minutes-long and image-pull-dependent; acceptable nightly, not per-PR. MGR-10 adds a second manager pod plus lease settling time and is the cheapest of the set but still slower than the single-replica smoke. CNP-03 is deliberately NOT automated — a mainnet Mithril restore needs hundreds of GiB and is infeasible on a standard GitHub runner; faking it would be dishonest, so it is a documented manual proof with the structural ordering already unit-covered. Scope risk: keep the nightly suite under a separate test/chainsaw-nightly tree and a separate Moon task so it cannot accidentally bloat the per-PR e2e job.

---

## Appendix — requirement → phase index

Every non-satisfied `TEST_PLAN.md` row and the phase that closes it (the 74 already-satisfied rows are omitted).

| Requirement | Phase |
|---|---|
| CFG-02 | P4 |
| CFG-04 | P4 |
| CFG-05 | P4 |
| CFG-07 | P4 |
| CFG-08 | P4 |
| CFG-09 | P4 |
| CLI-03 | P3 |
| CLI-09 | P11 |
| CLI-12 | P3 |
| CLI-13 | P3 |
| CLI-14 | P3 |
| CLI-15 | P3 |
| CNI-01 | P7 |
| CNI-02 | P7 |
| CNI-03 | P7 |
| CNI-04 | P7 |
| CNL-03 | P7 |
| CNL-06 | P7 |
| CNL-07 | P7 |
| CNL-09 | P7 |
| CNL-10 | P9 |
| CNP-01 | P12 |
| CNP-02 | P7 |
| CNP-03 | P12 |
| CNP-04 | P7 |
| CNP-05 | P7 |
| CNP-07 | P7 |
| CNV-01 | P5 |
| CNV-02 | P5 |
| CNV-03 | P5 |
| CNV-04 | P5 |
| CNV-05 | P5 |
| CNV-06 | P5 |
| CNV-07 | P5 |
| CNV-08 | P5 |
| CNV-09 | P5 |
| CNV-10 | P5 |
| CNV-11 | P5 |
| CTN-01 | P12 |
| CTN-02 | P12 |
| DBD-02 | P8 |
| DBD-03 | P8 |
| DBD-04 | P8 |
| DBD-05 | P8 |
| DBD-06 | P8 |
| DBD-07 | P8 |
| DBF-01 | P8 |
| DBF-03 | P9 |
| DBF-08 | P8 |
| DBF-09 | P9 |
| DBS-01 | P8 |
| DBS-02 | P9 |
| DBS-03 | P8 |
| DBS-05 | P8 |
| DBS-06 | P8 |
| DBV-02 | P6 |
| DBV-03 | P6 |
| DBV-04 | P6 |
| DBV-05 | P6 |
| DBV-06 | P6 |
| DBV-07 | P6 |
| DBV-08 | P6 |
| DBV-09 | P6 |
| FCT-04 | P2 |
| FCT-10 | P2 |
| FCT-11 | P2 |
| FCT-12 | P2 |
| FCT-13 | P2 |
| FCT-14 | P2 |
| FTX-02 | P2 |
| FTX-03 | P2 |
| FTX-05 | P2 |
| FTX-07 | P2 |
| FTX-09 | P2 |
| FTX-10 | P2 |
| HLM-02 | P1 |
| HLM-03 | P1 |
| HLM-07 | P1 |
| HLM-08 | P1 |
| HST-01 | P11 |
| HST-03 | P3 |
| HST-04 | P3 |
| HST-05 | P11 |
| HST-06 | P3 |
| HST-11 | P3 |
| HST-15 | P3 |
| MGR-01 | P1 |
| MGR-02 | P1 |
| MGR-05 | P9 |
| MGR-06 | P9 |
| MGR-07 | P9 |
| MGR-09 | P1 |
| MGR-10 | P12 |
| MGR-11 | P1 |
| PIN-01 | P4 |
| PIN-02 | P4 |
| PIN-03 | P4 |
| PIN-04 | P4 |
| TLS-01 | P12 |
| TLS-03 | P4 |
| TLS-04 | P4 |
| TLS-05 | P4 |
| TLS-06 | P4 |
| TLS-09 | P4 |
| TLS-10 | P4 |
| TOP-01 | P3 |
| TOP-02 | P3 |
| TOP-03 | P3 |
| TOP-05 | P3 |
| TOP-08 | P3 |
| TOP-10 | P3 |
