# YACD Test Coverage Analysis

Companion to `TEST_PLAN.md`. For every one of the **185** requirements it records
whether the **current** codebase tests satisfy it:

- **✅ Satisfied** — an existing test asserts the full Pass/Fail Criteria at an
  acceptable level (including the negative/edge intent for `−` rows).
- **🟡 Partial** — the behavior is touched, but not the full criteria: a named
  signal (condition reason, exact error string, port, default) is unasserted, the
  negative/edge case is missing, coverage sits at a weaker level than the row
  needs, or it was removed/disabled. The **Gap** column says exactly what to add
  for 100% alignment.
- **🔴 Not satisfied** — no existing test meaningfully asserts the requirement.

**Method.** 15 group auditors read each requirement and every relevant test file
(unit `*_test.go`, envtest, the single Chainsaw suite, cardano-tools `*.txtar`,
the Helm `rbac_test.go`) and graded coverage. Every `satisfied` and
`not-satisfied` verdict was then handed to an adversarial agent — told to find an
unasserted slice of each "satisfied" and to hunt for existing coverage behind
each "not-satisfied" — and downgraded/upgraded accordingly. A final pass
synthesized the roadmap below. This is a static coverage audit, not a test run.

## Headline

| | Count | Share |
|---|---|---|
| ✅ Satisfied | 74 | 40% |
| 🟡 Partial | 86 | 46% |
| 🔴 Not satisfied | 25 | 14% |
| **Total** | **185** | |

Coverage is **strong at the unit tiers** (CLI flag/trust logic, faucet HTTP
handlers and the transaction engine, the cardano-tools verbs, public-pin
integrity, Kong manager-option parsing) and **thin at the two tiers that go
through the apiserver**: CRD admission and manager-backed reconcile. The single
Chainsaw suite is a narrow manager-smoke and is the only E2E we have.

## Coverage posture

Strong, at exactly the recommended level:

- **CLI / host-access / topup happy paths** (`CLI-*`, `HST-*`, `TOP-*`) — table +
  mockery + envtest-backed `kube` adapter tests.
- **Faucet HTTP + engine** (`FCT-*`, `FTX-*`) — httptest handlers and a mock
  submitter/sources engine.
- **cardano-tools verbs + public pins** (`TLS-*`, `PIN-*`) — testscript `.txtar`
  and digest cross-check tests.
- **CardanoNetwork reconcile output** (`CNL-*`, `CNP-*`, `API-*`) — the best-covered
  controller area: 14 satisfied, 11 partial, **0** not-satisfied.

Three structural weaknesses drive almost every gap:

1. **CardanoNetwork CRD admission is entirely unguarded.** There is no
   `api/v1alpha1/cardanonetwork_validation_test.go` at all — even though the
   sibling `CardanoDBSync` has one. So the whole `CNV` cluster (mode XOR, enum
   closure, port ranges, the `margin` pattern, OpenAPI defaulting) is unverified
   at the apiserver. A bad spec could be admitted and only fail (or silently
   misbehave) at reconcile, and a CEL/enum/range regression would ship undetected.
2. **Reconcile-output contracts are proven by fake-client unit tests where the
   plan wants envtest.** A family of `Level=Env` rows is covered only by direct
   `Reconcile` / builder `Build()` tests, never through controller-runtime
   watches/`.Owns`. A prior session **removed the primary-sidecar happy-path
   manager envtest and it was never replaced**, so the `DBS` publish path has no
   Env proof.
3. **E2E is a single manager-smoke suite.** It never drives the CLI
   `down`/`run`/`exec`/`connect` verbs, never asserts the deletion-driven GC
   cascade, never sends an unauthenticated metrics request, and gives no
   render-level guard that the chart actually packages its CRDs/RBAC.

## Prioritized gap roadmap

Highest leverage first. Effort is a rough size for closing the whole cluster.

1. **[Env · medium] Build `cardanonetwork_validation_test.go`** (mirror
   `cardanodbsync_validation_test.go`). Single highest-leverage gap — an entire
   admission category is unguarded.
   `CNV-01/02/03/04/05/06/07/08/09/10/11`, `CNP-07` (folds in here).
2. **[Env · high] Restore manager-backed envtest for reconcile-output contracts**
   now proven only by fake-client unit tests — including the removed
   primary-sidecar happy path. `DBS-01/02/03/05/06`, `DBD-02`, `CNI-02/03/04`,
   `CNL-06`, `DBF-01/03`.
3. **[E2E · medium] Prove deletion-driven GC cascade.** No test anywhere deletes
   a parent and waits for owned children to reach `NotFound` (envtest has no GC;
   the CLI `down` test is mock-only; Chainsaw deletes the whole namespace, which
   cascades regardless of ownerRefs). `CNL-10`, `DBF-09`, `CLI-09`, `DBD-01`.
4. **[mixed · medium] Metrics/health negative + HTTP contract.** The authorized
   `/metrics` 200 is covered, but the *rejection* of an unauthenticated request
   (`MGR-07`) is never exercised, `/healthz`+`/readyz` 200 is unasserted
   (`MGR-05`), and the `go_goroutines` body grep is effectively dead (its exit is
   swallowed before the chainsaw step's `exit 0`). `MGR-05/06/07/09/10`.
5. **[Unit · low] Helm render-level guards.** Nothing renders the chart and
   asserts the CRDs (`HLM-02`) and metrics-auth/metrics-reader/leader-election
   RBAC (`HLM-03`) are packaged; no valid/invalid `values.schema.json` render
   test. `HLM-02/03/07/08`.
6. **[Unit · low] Negative CLI/faucet verb guards.** Pure flag/precondition
   refusals with zero coverage: `up` without `-f` (`CLI-03`), `topup` without
   `--address` (`TOP-02`) / `--lovelace ≤ 0` (`TOP-03`) / missing faucet endpoint
   or auth Secret (`TOP-05`) / invalid `--faucet-url` (`TOP-10`), faucet
   omitted-lovelace 400 (`FCT-11`), unknown-route 404 (`FCT-12`), empty-token 500
   (`FCT-13`), `exec` against a not-ready network (`HST-06`), `info`/`topup`
   not-found (`CLI-15`).
7. **[Unit · medium] Faucet engine error-mapping + concurrency.** The
   `fakeSubmitter` masks the `chain_unavailable` branch on a blank TxID / zero
   spent inputs (`FTX-07`); the per-source lock's *non*-serialization across
   distinct sources is unasserted (`FTX-09`); the integrated build/sign/submit
   round-trip (`FTX-10`) and the engine→HTTP error mapping (`FCT-14`) are untested
   through the real path.

## Quick wins

Small, pure-Go additions that flip a partial/not-satisfied with little effort:

- **MGR-02** — assert the json buffer actually `json.Unmarshal`s (or has a `"msg"`
  key) and the text buffer is non-JSON, instead of only the shared substring.
- **DBV-02/03/04** — assert `err.Error()` names the XOR / `followerNode` rule, not
  just `IsInvalid`; **DBV-05/06** empty `networkRef.name` / `passwordSecretRef`
  name+key; **DBV-07** the 5 uncovered enum negatives; **DBV-08** above-range port
  (70000); **DBV-09** the db-sync image default.
- **TOP-02/03/05/10**, **CLI-03/15**, **HST-06** — mocked verb tests asserting a
  non-zero error and no downstream call.
- **FCT-11/12/13** — httptest cases: omitted-lovelace 400, unknown-route 404,
  empty-token 500; **FCT-10** body-size-limit and multi-JSON-value 400s; **FCT-04**
  assert the funding `Address` in the body.
- **CFG-02/05** — assert the named value and the mode/block XOR mismatches.
- **FTX-02/03** — assert the bound / destination message strings, not just the code.
- **HST-04/11/15** — exit-code on dropped forward, disconnect message on cancel,
  the network-magic-absent branch.
- **TLS-09** — assert the `(known: …)` profile enumeration; **CNP-05** the
  tip-ahead-of-inferred clamp-to-0 case.

## Biggest holes (entirely untested)

- **CardanoNetwork admission** — `CNV-02` (local+public both set), `CNV-03`
  (public absent in public mode), `CNV-08` (unknown enums; also `CNP-07`'s primary
  coverage), `CNV-09` (`margin` pattern). The apiserver rejects none of these in a
  test.
- **GC / finalizer cascade** — `DBF-09` (managed-Postgres + db-sync child set on
  delete) has no coverage at any level; `CNL-10` / `CLI-09` child-GC likewise.
- **Faucet money-path** — `FTX-07` (`chain_unavailable` on blank TxID / zero spent
  inputs, *actively masked* by the fake), `FTX-09` (cross-source lock).
- **DB rendering** — `DBD-06` (`maintenance_work_mem` / `max_parallel_maintenance_workers`
  never rendered or asserted).
- **Config** — `CFG-05` (mode/block XOR mismatch); `CFG-09` partially (the three
  public `yacd.yaml` examples, incl. the mainnet 300Gi + bootstrap regression
  guard, are never loaded/rendered).
- **Real-binary artifacts** — `TLS-01` / `CTN-01` / `CTN-02` (the actual
  cardano-testnet output layout + `utxo-keys`) are only touched transitively via
  the faucet smoke, never inspected.
- **Helm negative** — `HLM-08` (schema-violating values must fail the render).

## Systemic level-mismatches

- **(A) Reconcile-output proven only at Unit where the plan wants Env** —
  `DBS-01/02/03/05/06`, `DBD-02`, `CNI-02/03/04`, `CNL-06`, partially `DBF-01/08`:
  covered by `newTestReconciler` / `fake.NewClientBuilder` direct-`Reconcile` and
  `Build()` tests, with no manager-backed envtest through watches/`.Owns`. The
  removed primary-sidecar manager envtest is the sharpest instance.
- **(B) Admission proven only at the Go-logic layer where the plan wants Env** —
  `CNV-04/05/06/07/08`, `CNP-07` are validated as builder/`unsupportedSpec` error
  strings on a fake client, but the closed CEL/OpenAPI rules make that path
  unreachable from a real apiserver, so the actual admission contract is untested.
- **(C) Missing the `(+E2E)` secondary the plan calls for** — `HST-01/05` (real
  port-forward + in-cluster exec), `CLI-09` (down child-GC), `TLS-01` /
  `CTN-01/02` (real cardano-testnet output), `MGR-07/10` (unauth-reject,
  multi-replica leader), `FTX-10` (real Apollo/Ogmios/Kupo build-sign-submit) have
  solid unit/mock coverage but no E2E half.

---

## Appendix — per-requirement status

Full audit, grouped by category, in plan order. **Gap** is what to add for 100%
alignment (blank for ✅). Covering tests are abbreviated; see the workflow
transcript for full evidence and `levelMatchNote`s.

### MGR — Manager startup

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| MGR-01 | Manager starts with default flags | 🟡 partial | test/chainsaw/manager-smoke/chainsaw-test.yaml:step deploy-controller (wait Deployment Available=true); cmd/foundation… | No test asserts the explicit criterion that BOTH controllers are 'registered and watching their CRDs' as a distinct signal: foundation_test only asserts registerControllers returns nil (it never inspects that .For/.Owns/watches were set up), and chainsaw infers controller registration only indirect… |
| MGR-02 | `--log-format=json` | 🟡 partial | cmd/options_test.go:TestNewControllerLogger; cmd/options_test.go:TestParseManagerOptions (accepts slog logging options… | The criterion is that json output is actually JSON and text output is actually text lines. The test only asserts the message substring appears in BOTH formats; it never asserts the json case produces valid JSON (e.g. a parseable object / a `"msg":` key) nor that the text case is non-JSON line forma… |
| MGR-03 | --log-format set to an unsupported value | ✅ satisfied | cmd/options_test.go:TestParseManagerOptionsRejectsInvalidLogOptions (format); cmd/options_test.go:TestNewControllerLog… |  |
| MGR-04 | `--log-level` set to an unsupported value | ✅ satisfied | cmd/options_test.go:TestParseManagerOptionsRejectsInvalidLogOptions (level) |  |
| MGR-05 | Health/readiness probes | 🔴 not-satisfied | — | Add a direct assertion that the manager's health-probe bind address serves /healthz and /readyz with HTTP 200 when healthy. The plan itself flags this as a known gap. This could be an E2E curl against the health-probe port (per the row's E2E level) or an envtest/manager test asserting AddHealthzChe… |
| MGR-06 | Metrics served with `--metrics-secure` (default) to an authorized Ser… | 🟡 partial | test/chainsaw/manager-smoke/chainsaw-test.yaml: step deploy-controller curl-metrics Pod (HTTPS :8443/metrics 200 + SA … | The "exposes go_goroutines" conjunct of the criteria is effectively unasserted. The `grep -q "go_goroutines" /tmp/metrics.txt` on chainsaw-test.yaml:151 runs in a script with no `set -e`, immediately followed by an unconditional `exit 0` on line 152, so its non-zero exit is discarded. The Pod reach… |
| MGR-07 | Metrics requested without authorization | 🟡 partial | test/chainsaw/manager-smoke/chainsaw-test.yaml:step deploy-controller (authorized curl-metrics only); cmd/manager_test… | The negative criterion — a request WITHOUT authorization is rejected (not 200) — is never exercised. Chainsaw only curls with a valid bearer token; there is no unauthenticated/invalid-token curl asserting a 401/403 against the manager metrics endpoint. The (+Unit) half is only proven by FilterProvi… |
| MGR-08 | HTTP/2 default | ✅ satisfied | cmd/manager_test.go:TestNewTLSOptions |  |
| MGR-09 | `--metrics-bind-address=0` | 🟡 partial | cmd/options_test.go:TestParseManagerOptions (uses operator defaults) | No test asserts the actual outcome of the row: that BindAddress="0" yields a DISABLED metrics server / no exposed endpoint. TestNewMetricsServerOptions only exercises ":8443" and ":8080" — it never passes "0" nor asserts the disabled state, and there is no E2E/Env check that a 0-bound manager expos… |
| MGR-10 | Leader election enabled (`--leader-elect`) with multiple replicas | 🟡 partial | cmd/manager_test.go:TestNewManagerOptions; cmd/options_test.go:TestParseManagerOptions (accepts current manifest args) | The row's actual criterion — with multiple replicas, exactly ONE replica becomes leader and performs reconciles while others stand by — is never exercised. There is no multi-replica E2E (chainsaw deploys a single manager) and no test of the runtime election outcome (lease acquisition, single-active… |
| MGR-11 | Default image flags resolve | 🟡 partial | cmd/options_test.go:TestParseManagerOptions (accepts default faucet/cardano-testnet/cardano-tools image overrides); in… | The wiring that connects the manager FLAG to the reconciler FIELD is unproven. cmd/setup.go:registerControllers copies options.DefaultFaucetImage/DefaultCardanoTestnetImage/DefaultCardanoToolsImage onto the reconciler structs, but foundation_test calls registerControllers(mgr, managerOptions{}) wit… |

### CNV — CardanoNetwork API validation

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| CNV-01 | `mode: local` with `spec.local` set and `spec.public` absent | 🟡 partial | internal/controller/cardanonetwork/controller_envtest_test.go:TestCardanoNetworkControllerManagerCreatesAndRecreatesPr… | There is no test whose PURPOSE is to assert the admission contract: the success is incidental to setting up reconcile tests. The created object's spec.public-absent / local-present XOR is never the asserted subject, and the object is hand-built with every field set. Add a focused CardanoNetwork adm… |
| CNV-02 | `mode: local` with `spec.public` also set | 🔴 not-satisfied | — | Add an envtest (like cardanodbsync_validation_test.go testCases) that creates mode:local with both spec.local and spec.public set and asserts apierrors.IsInvalid citing the mode/local/public XOR rule. |
| CNV-03 | `mode: public` with `spec.public` absent | 🔴 not-satisfied | — | Add an envtest creating mode:public with spec.public absent and assert apierrors.IsInvalid citing the XOR rule. |
| CNV-04 | `mode: public`, `profile: mainnet`, no `bootstrap.mithril` | 🟡 partial | internal/controller/cardanonetwork/controller_test.go:TestCardanoNetworkReconcilerReconcileMarksUnsupportedInput | The row's Pass/Fail is ADMISSION rejection requiring bootstrap.mithril. The CRD CEL rule (PublicNetworkSpec XValidation, types.go:260; CRD line 626 'bootstrap.mithril is required only when public.profile is mainnet') rejects this at apiserver, which means the fake-client reconcile path the test exe… |
| CNV-05 | `mode: public`, `profile: preview`/`preprod`, `bootstrap` set | 🟡 partial | internal/cardano/publicnet/plan_test.go:TestBuildPlanRejectsUnsupportedProfiles (subtests "preview with mithril bootst… | No envtest/CEL-admission test exercises the CRD's XValidation rule (api/v1alpha1/cardanonetwork_types.go:260) for this case. There is no api/v1alpha1/cardanonetwork_validation_test.go, and controller_envtest_test.go does not submit a public preview/preprod network with bootstrap set to assert apier… |
| CNV-06 | `node.port` outside 1–65535 | 🟡 partial | internal/controller/cardanonetwork/builder_test.go:TestPrimaryWorkloadBuilderRejectsUnsupportedInput (subtest "invalid… | An envtest admission case that creates a CardanoNetwork with spec.node.port out of range (e.g. 0 and/or 70000) via apiClient.Create against the started API server and asserts apierrors.IsInvalid, exercising the CRD's Minimum=1/Maximum=65535 markers at admission. The existing "invalid node port" tes… |
| CNV-07 | Ogmios/Kupo/faucet port outside 1–65535 | 🟡 partial | internal/controller/cardanonetwork/builder_test.go:TestPrimaryWorkloadBuilderRejectsUnsupportedInput | No envtest/admission test submits an out-of-range chain-API port (ogmios/kupo/faucet port=0 or >65535) through the API server and asserts apierrors.IsInvalid, which is the exact contract the row specifies ("Admission rejected by range validation", layer Env). Existing range coverage is only at the … |
| CNV-08 | Enum fields given unknown values (`mode`, `era`, `profile`, genesis `… | 🔴 not-satisfied | — | Add Env admission cases setting each enum (mode, era, public profile, genesis profile) to an unknown value and assert apierrors.IsInvalid. This is also the cross-referenced primary coverage for CNP-07 (unknown public profile), which is likewise uncovered. |
| CNV-09 | Pool `margin` outside the `^(0(\.[0-9]+)?\|1(\.0+)?)$` pattern | 🔴 not-satisfied | — | Add an Env admission case setting spec.local.topology.pools.defaults.margin to a value violating the pattern -> assert apierrors.IsInvalid by pattern validation. |
| CNV-10 | Minimal valid local spec omitting defaulted fields | 🔴 not-satisfied | — | Add an Env test that creates a minimal mode:local CardanoNetwork omitting defaulted fields, Get()s it back, and asserts era=conway, node.version default, node.port=3001, chainAPI ogmios/kupo enabled=true, faucet enabled=false (mirroring the 'accepts ... and defaults fields' subtests in TestCardanoD… |
| CNV-11 | Faucet `minTopUpLovelace` and `maxTopUpLovelace` defaulting | 🔴 not-satisfied | — | Add an Env test that enables faucet without setting min/max top-up, Get()s back, and asserts the apiserver defaulted minTopUpLovelace=1000000 and maxTopUpLovelace=10000000000. |

### CNL — CardanoNetwork local reconciliation

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| CNL-01 | Apply a valid `mode: local` network | ✅ satisfied | internal/controller/cardanonetwork/controller_envtest_test.go:TestCardanoNetworkControllerManagerCreatesAndRecreatesPr… |  |
| CNL-02 | Local genesis is generated and staged | ✅ satisfied | test/chainsaw/manager-smoke/chainsaw-test.yaml; internal/controller/cardanonetwork/builder_test.go:TestPrimaryWorkload… |  |
| CNL-03 | No artifacts ConfigMap is produced | 🟡 partial | test/chainsaw/manager-smoke/chainsaw-test.yaml (lines 365-383: asserts network-artifacts ConfigMap object absent AND n… | The "No <net>-network-artifacts ConfigMap exists" half (ConfigMap OBJECT non-existence) is asserted ONLY at E2E (Chainsaw lines 369-372); it is NOT asserted at the recommended Env level by any reconcile/builder/envtest test — there is no assertNoPrimaryConfigMap / IsNotFound check for the network-a… |
| CNL-04 | Node becomes ready | ✅ satisfied | test/chainsaw/manager-smoke/chainsaw-test.yaml; internal/controller/cardanonetwork/controller_envtest_test.go:TestCard… |  |
| CNL-05 | Status endpoints published | ✅ satisfied | internal/controller/cardanonetwork/controller_envtest_test.go:TestCardanoNetworkControllerManagerCreatesAndRecreatesPr… |  |
| CNL-06 | Network identity published | 🟡 partial | internal/controller/cardanonetwork/controller_test.go:TestCardanoNetworkReconcilerReconcileCreatesPrimaryWorkload; int… | No test asserts status.network.mode==local, status.network.era, or status.network.networkMagic for a LOCAL network. Add an Env/Unit assertion on a reconciled local network that current.Status.Network.Mode==CardanoNetworkModeLocal, *Era==conway, and *NetworkMagic==42 (mirroring controller_test.go:20… |
| CNL-07 | mode: local with spec.local.genesis set (e.g. profile: zero-fee) | 🟡 partial | internal/controller/cardanonetwork/builder_test.go:TestPrimaryWorkloadBuilderRejectsUnsupportedInput | The reconcile-OUTPUT half of the criteria is unproven for THIS input: no test runs a genesis-set local network through Reconcile() to confirm Degraded=True/reason UnsupportedSpec, Ready/NodeReady=False, and that NO primary workload/PVC/Services are created. TestCardanoNetworkReconcilerReconcileMark… |
| CNL-08 | Steady state is not perpetually Progressing | ✅ satisfied | test/chainsaw/manager-smoke/chainsaw-test.yaml; internal/controller/cardanonetwork/controller_envtest_test.go:TestCard… |  |
| CNL-09 | Local pool count not equal to 1 | 🟡 partial | internal/controller/cardanonetwork/builder_test.go:TestPrimaryWorkloadBuilderRejectsUnsupportedInput | Like CNL-07, the reconcile-output clause is unproven for this input: no test runs a pool-count!=1 local network through Reconcile() to assert Degraded=True/reason UnsupportedSpec and that no primary children are created. Also only Count=2 is exercised; the 'any !=1' claim (e.g. 0 or 3) is not. Add … |
| CNL-10 | Deleting the CardanoNetwork | 🟡 partial | internal/controller/cardanonetwork/builder_test.go:TestPrimaryWorkloadBuilderBuildsPrimaryWorkload | No test exercises the actual GC cascade: nothing deletes the CardanoNetwork and asserts its owned children (Deployment, node-state PVC, node/Ogmios/Kupo/faucet/artifacts Services, faucet-auth Secret) become NotFound. envtest does not run the GC cascade, the CLI down test is mock-only, and the Chain… |

### CNP — CardanoNetwork public reconciliation

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| CNP-01 | Apply `mode: public`, `profile: preview` (or `preprod`) | 🟡 partial | internal/controller/cardanonetwork/controller_test.go:TestCardanoNetworkReconcilerReconcileCreatesPublicPreviewWorkloa… | No reconcile/envtest case for profile=preprod (only preview gets a reconcile test; preprod is builder-only). More importantly, the criteria 'primary node JOINS the public network' (real chain sync) is never proven — there is no E2E for a public network (correctly EXPENSIVE/manual). For full alignme… |
| CNP-02 | Resolved public identity | 🟡 partial | internal/controller/cardanonetwork/controller_test.go:TestCardanoNetworkReconcilerReconcileCreatesPublicPreviewWorkloa… | On the PUBLISHED status.network, only preview is fully asserted (mode=public, profile=preview, networkMagic=2). For mainnet, the reconcile test asserts only status.network.Profile==mainnet — it never asserts status.network.Mode==public nor status.network.NetworkMagic==764824073. For preprod there i… |
| CNP-03 | Mainnet with Mithril bootstrap | 🟡 partial | internal/controller/cardanonetwork/controller_test.go:TestCardanoNetworkReconcilerReconcileCreatesPublicMainnetWorkloa… | The criteria 'seeds the node database from a VERIFIED snapshot before the node starts' is proven only structurally (container ordering + verification-key env present) — no test exercises the actual Mithril restore/verification (correctly EXPENSIVE). That is acceptable for the Env recommendation; th… |
| CNP-04 | Ogmios-backed sync probe | 🟡 partial | internal/controller/cardanonetwork/sync_probe_test.go:TestCardanoNetworkReconcilerReconcilePublishesNodeSyncStatusWhen… | No test asserts that connection status and last tip are surfaced onto the published status.sync payload, which the criteria explicitly requires ("status.sync reports ... connection status, last tip ..."). Specifically: (1) status.sync.ConnectionStatus is never asserted -- the cited tests only asser… |
| CNP-05 | Sync lag computation | 🟡 partial | internal/controller/cardanonetwork/sync_probe_test.go:TestCardanoNetworkSyncStatusComputesInferredSlotAndLag; internal… | The 'never negative' clause is not directly exercised: the clamp is max(inferredTipSlot-tipSlot, 0) (sync_probe.go:284), but no test feeds a tip slot AHEAD of the inferred tip to assert lagSlots/lagSeconds clamp to 0 rather than going negative. Add a table case where tipSlot > inferred and assert *… |
| CNP-06 | `NodeSynchronized` reflects catch-up | ✅ satisfied | internal/controller/cardanonetwork/sync_probe_test.go:TestPrimaryNodeSyncStatusConditions; internal/controller/cardano… |  |
| CNP-07 | Unknown public profile (enum boundary) | 🟡 partial | internal/controller/cardanonetwork/builder_test.go:TestPrimaryWorkloadBuilderRejectsUnsupportedInput | The enum-admission rejection of an unknown profile (Enum=preview;preprod;mainnet) is a CRD-validation contract belonging to CNV-08 (Env) and is NOT exercised in this package or any cardanonetwork test here — there is no CRD-apply test for an unknown profile value. Within scope, no test drives a pro… |

### API — Chain-API sidecars

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| API-01 | Default Ogmios + Kupo | ✅ satisfied | internal/controller/cardanonetwork/controller_envtest_test.go:TestCardanoNetworkControllerManagerCreatesAndRecreatesPr… |  |
| API-02 | Faucet opt-in (`faucet.enabled=true`) | ✅ satisfied | internal/controller/cardanonetwork/controller_envtest_test.go:TestCardanoNetworkControllerManagerCreatesAndRecreatesPr… |  |
| API-03 | Faucet auth Secret lifecycle | ✅ satisfied | internal/controller/cardanonetwork/controller_envtest_test.go:TestCardanoNetworkControllerManagerCreatesAndRecreatesPr… |  |
| API-04 | Faucet disabled by default | ✅ satisfied | internal/controller/cardanonetwork/controller_test.go:TestCardanoNetworkReconcilerReconcileLeavesFaucetDisabledByDefau… |  |
| API-05 | Disable Kupo at runtime (`kupo.enabled=false`) | ✅ satisfied | test/chainsaw/manager-smoke/chainsaw-test.yaml; internal/controller/cardanonetwork/controller_test.go:TestCardanoNetwo… |  |
| API-06 | Disable faucet at runtime | ✅ satisfied | internal/controller/cardanonetwork/controller_envtest_test.go:TestCardanoNetworkControllerManagerCreatesAndRecreatesPr… |  |
| API-07 | Faucet image override using a different repository than the controlle… | ✅ satisfied | internal/controller/cardanonetwork/builder_test.go:TestPrimaryWorkloadBuilderRejectsUnsupportedInput |  |
| API-08 | Endpoint URLs match Service identity | ✅ satisfied | internal/controller/cardanonetwork/controller_envtest_test.go:TestCardanoNetworkControllerManagerCreatesAndRecreatesPr… |  |

### CNI — CardanoNetwork identity & immutability

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| CNI-01 | First reconcile accepts inputs | 🟡 partial | internal/controller/cardanonetwork/controller_test.go:TestCardanoNetworkReconcilerReconcileCreatesPrimaryWorkload; int… | For the local case the row wants BOTH localnetFingerprint AND networkFingerprint recorded; CreatesPrimaryWorkload only asserts the localnet one (the networkFingerprint-non-empty check for local rides on RepairsForgedNetworkIdentityStatus). This is essentially covered. The only gap vs the recommende… |
| CNI-02 | Mutate an identity-affecting local input (e.g. network magic, era, ge… | 🟡 partial | internal/controller/cardanonetwork/controller_test.go:TestCardanoNetworkReconcilerReconcileRejectsLocalnetInputChanges… | Two of the three identity-affecting inputs the scenario names as examples are not exercised as post-acceptance mutations by any cited test. (1) Era-after-acceptance: MarksUnsupportedInput sets era=babbage at creation (never accepted) and yields UnsupportedSpec, not a mutate-after-acceptance Unsuppo… |
| CNI-03 | Recreate after deletion with changed inputs | 🔴 not-satisfied | — | Add an Env test: create a local network (record fingerprint), delete it (let children GC / clear state), create a new same-named network with a different networkMagic, and assert it reconciles cleanly and records a new, different fingerprint with no Degraded/UnsupportedLocalnetChange. |
| CNI-04 | Non-identity field change (e.g. resources) | 🟡 partial | internal/controller/cardanonetwork/controller_test.go:TestCardanoNetworkReconcilerReconcileExpandsStorage; internal/co… | CNI-04 requires Level Env (real kube-apiserver via envtest). Both cited tests use a fake client builder with no apiserver, which the plan calls Unit. The envtest file has only two tests, neither mutating a non-identity field on an accepted localnet. So behavior is proven only at Unit level, not Env… |

### DBV — CardanoDBSync API validation

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| DBV-01 | `database.managed` set, `database.external` absent | ✅ satisfied | api/v1alpha1/cardanodbsync_validation_test.go:TestCardanoDBSyncDatabaseValidation/accepts_managed_database_and_default… |  |
| DBV-02 | Both `database.external` and `database.managed` set | 🟡 partial | api/v1alpha1/cardanodbsync_validation_test.go:TestCardanoDBSyncDatabaseValidation/rejects_both_database_modes | The pass criterion says the rejection must cite 'exactly one database mode required'. The test only checks IsInvalid(err); it never asserts the returned error message/CEL message ('exactly one of database.external or database.managed must be set'). Add an assertion on err.Error() (or the StatusErro… |
| DBV-03 | Neither database mode set | 🟡 partial | api/v1alpha1/cardanodbsync_validation_test.go:TestCardanoDBSyncDatabaseValidation/rejects_neither_database_mode | Same gap as DBV-02: the criterion 'exactly one database mode required' is not message-asserted. The test only checks IsInvalid; it does not confirm the failure is the database-mode XOR message rather than, e.g., a missing-required-field error. Add an err.Error()/cause assertion naming the XOR rule. |
| DBV-04 | `placement.mode=primarySidecar` with `followerNode` set | 🟡 partial | api/v1alpha1/cardanodbsync_validation_test.go:TestCardanoDBSyncDatabaseValidation/rejects_primary_sidecar_placement_wi… | The criterion 'followerNode is invalid with primarySidecar' is not message-asserted; only IsInvalid is checked. Add an assertion that the error message matches the rule message 'followerNode cannot be set when placement.mode is primarySidecar' so the test cannot pass on an unrelated invalidation. |
| DBV-05 | `networkRef.name` empty | 🔴 not-satisfied | — | Add a negative table case that sets spec.networkRef.name='' (empty) and asserts Create fails with apierrors.IsInvalid(err), ideally with a message confirming the MinLength=1 rejection on networkRef.name. The plan's 'Known coverage gaps' already flags DBV-05 as having no coverage. |
| DBV-06 | External DB `passwordSecretRef` with empty name/key | 🔴 not-satisfied | — | Add negative table cases setting spec.database.external.passwordSecretRef.name='' and (separately) passwordSecretRef.key='' and assert Create fails IsInvalid against the MinLength=1 markers on both fields. Per the plan's gap list (DBV-06), this is currently uncovered. |
| DBV-07 | Enum fields given unknown values (ledger backend, insert preset, tx_o… | 🟡 partial | api/v1alpha1/cardanodbsync_validation_test.go:TestCardanoDBSyncDatabaseValidation/rejects_invalid_external_database_ss… | 5 of 7 enums have NO negative case: ledgerBackend (config.ledgerBackend Enum=inmemory;lsm), insert preset (config.insert.preset Enum=full;only_utxo;only_governance;disable_all), tx_out mode (config.insert.txOut.mode Enum=enable;disable;consumed;prune;bootstrap), ledger mode (config.insert.ledger En… |
| DBV-08 | External DB port outside 1–65535 | 🟡 partial | api/v1alpha1/cardanodbsync_validation_test.go:TestCardanoDBSyncDatabaseValidation/rejects_invalid_external_database_po… | Only the lower-bound underflow (0) is tested; the upper bound (e.g. 65536, or >65535) is never exercised, and the rejection is asserted only as IsInvalid with no message confirming the range/Maximum rule. Add an above-range case (e.g. port=70000) and assert IsInvalid, and ideally message-assert the… |
| DBV-09 | Minimal valid managed spec omitting defaults | 🟡 partial | api/v1alpha1/cardanodbsync_validation_test.go:TestCardanoDBSyncDatabaseValidation/accepts_managed_database_and_default… | The db-sync image default 'ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0' (spec.image, types.go:102) is never asserted in either subtest. Add an assertion that spec.image defaults to that value on the minimal managed object. The ledgerBackend=lsm and insert preset=full defaults are correctly NOT as… |

### DBF — CardanoDBSync dedicated follower

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| DBF-01 | Apply a managed-Postgres CardanoDBSync referencing a Ready network | 🟡 partial | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileAppliesManagedPostgresAndGate… | Ownership ('owned by the CardanoDBSync') is only asserted for the managed auth Secret via controlledBy; the managed Postgres Deployment/Service/PVC and the follower db-sync Deployment/follower-PVC are NOT asserted to carry controller ownerReferences. The row recommends Env (+E2E); add explicit owne… |
| DBF-02 | Artifacts transport | ✅ satisfied | internal/controller/cardanodbsync/artifacts_transport_test.go:TestDedicatedFollowerServePathFetchesOverHTTP; internal/… |  |
| DBF-03 | Postgres readiness | 🟡 partial | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileAppliesManagedPostgresAndGate… | The unit path drives readiness from a faked Available Deployment / ready Pod, not a real Postgres accepting connections; that real 'accepts local connections' semantics is only exercised E2E. This is acceptable for the recommended E2E(+Env) split, so the gap is minor: the Env layer (envtest) does N… |
| DBF-04 | Follower and db-sync readiness | ✅ satisfied | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileReportsRuntimeReadyContainers… |  |
| DBF-05 | Sync progress reported | ✅ satisfied | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileReportsRuntimeReadyContainers… |  |
| DBF-06 | Data lands in Postgres | ✅ satisfied | test/chainsaw/manager-smoke/chainsaw-test.yaml (dbsync-psql block-table query) |  |
| DBF-07 | Postgres endpoint published | ✅ satisfied | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileAppliesManagedPostgresAndGate… |  |
| DBF-08 | networkRef points to a missing/not-ready network | 🟡 partial | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileReportsMissingNetwork; intern… | The "no workload is applied" clause of the DBF-08 Pass/Fail Criteria is unasserted by every cited test. The two missing/deleting tests (controller_test.go:800-826) contain only assertCondition calls for Degraded and Ready; neither asserts the absence of the dbsync Deployment/ConfigMap/PVC. The thre… |
| DBF-09 | Delete the CardanoDBSync | 🔴 not-satisfied | — | Add an E2E (chainsaw) step that deletes the CardanoDBSync and waits until the managed Postgres Deployment/Service/PVC/auth Secret and db-sync Deployment/ConfigMap/pgpass/state+follower PVCs are gone (real GC cascade). Optionally add an Env assertion that all children carry a controller ownerReferen… |

### DBS — CardanoDBSync primary sidecar

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| DBS-01 | `placement.mode=primarySidecar` against a non-mainnet network | 🟡 partial | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileAppliesPrimarySidecarResource… | No manager-backed envtest asserts the DBS-01 happy path. The row's declared Level is Env, but SidecarMaterialReady=True, the sha256: revision, and the four published resource names (ConfigMap/pgpass-Secret/state-PVC/metrics-Service) are asserted only by fake-client direct-Reconcile unit tests (newT… |
| DBS-02 | CardanoNetwork consumes the sidecar material | 🟡 partial | internal/controller/cardanonetwork/controller_test.go:TestCardanoNetworkReconcilerReconcileAttachesPrimarySidecarDBSyn… | No Env (envtest) or E2E (chainsaw) coverage for the primarySidecar consumption path, which is DBS-02's recommended level. To close the gap: (a) add a manager-backed envtest in internal/controller/cardanonetwork that creates a CardanoNetwork + a primarySidecar CardanoDBSync publishing SidecarMateria… |
| DBS-03 | Attaching/changing the sidecar rolls the primary | 🟡 partial | internal/controller/cardanodbsync/controller_test.go:TestPrimarySidecarMaterialRevisionChangesWithMaterialInputs; inte… | The row's actual contract — 'attaching/changing the attachment ROLLS THE PRIMARY Deployment (its revision changes)' — is about the CardanoNetwork primary Deployment getting a new pod-template revision when the attachment changes. No test asserts the primary Deployment's revision/pod-template actual… |
| DBS-04 | Primary-sidecar db-sync on public mainnet | ✅ satisfied | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileRejectsPublicMainnetPrimarySi… |  |
| DBS-05 | Switch placement mode after a placement was accepted | 🟡 partial | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileRejectsPrimarySidecarToDedica… | DBS-05 declares Level=Env, but the two cited tests are unit-level (fake client via newTestReconciler at controller_test.go:2059-2087 + direct Reconcile calls), not manager-backed envtest. controller_envtest_test.go has no test for the accepted-placement switch rejection: no assertion of conditionRe… |
| DBS-06 | Accepted placement recorded | 🟡 partial | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileAppliesPrimarySidecarResource… | Level mismatch: DBS-06 is specified as Level=Env, but every assertion of status.database.acceptedPlacementMode (both ==primarySidecar and ==dedicatedFollower, plus the backfill repair) is made only in controller_test.go, which runs against a fake client via direct reconciler.Reconcile() calls (newT… |

### DBD — CardanoDBSync database

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| DBD-01 | Managed Postgres without `authSecretRef` | ✅ satisfied | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileAppliesManagedPostgresAndGate… |  |
| DBD-02 | Managed Postgres with a user-supplied `authSecretRef` | 🟡 partial | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileUsesProvidedManagedPostgresAu… | (1) LEVEL MISMATCH. DBD-02 is planned Level=Env, but the test that actually asserts the DBD-02 contract (controller_test.go:138) is a fake-client direct-Reconcile UNIT test (newTestReconciler -> sigs.k8s.io/controller-runtime/pkg/client/fake), not envtest. The provided-authSecretRef path IS exercis… |
| DBD-03 | External Postgres | 🟡 partial | internal/controller/cardanodbsync/controller_test.go:TestCardanoDBSyncReconcilerReconcileAppliesExternalDatabaseWorklo… | No test asserts the ABSENCE of a managed Postgres workload on the external-database path, which is the first clause of the DBD-03 criteria ("No managed Postgres workload is created"). On the external fixture, no assertMissingObject (or equivalent NotFound check) exists for managedPostgresDeployment… |
| DBD-04 | Insert preset translation | 🟡 partial | internal/controller/cardanodbsync/builder_test.go:TestDBSyncWorkloadBuilderInsertPresetsDoNotUseDefaultedOverrides; in… | The only_governance preset — one of the four presets explicitly named in the DBD-04 Pass/Fail Criteria — is never exercised by any test. No test sets Spec.Config.Insert.Preset = CardanoDBSyncInsertPresetOnlyGovernance and asserts its rendered db-sync config. Its distinctive baseline from settings.g… |
| DBD-05 | Runtime/ledger-backend translation | 🟡 partial | internal/controller/cardanodbsync/builder_test.go:TestDBSyncWorkloadBuilderFingerprintChangesWithRuntimeConfig; intern… | ledgerBackend=inmemory is set in TestDBSyncWorkloadBuilderPreservesNestedPresetValuesUnlessOverridden but the rendered `ledger_backend: inmemory` line is NOT asserted there (only the downstream tx_out bootstrap/force_tx_in are). The runtime cache/epochTable/metricsPort flags are exercised for finge… |
| DBD-06 | Managed Postgres parameters | 🔴 not-satisfied | — | Add an Env (or builder Unit) assertion that a managed CardanoDBSync with parameters.maintenanceWorkMem and parameters.maxParallelMaintenanceWorkers renders the corresponding `-c maintenance_work_mem=<q>` and `-c max_parallel_maintenance_workers=<n>` args on the Postgres container. |
| DBD-07 | External Postgres referencing a missing password Secret | 🟡 partial | controller_test.go:TestCardanoDBSyncReconcilerReconcileReportsMissingExternalDatabaseSecret; controller_test.go:TestCa… | No test asserts the "rather than starting db-sync with no credentials" clause for the missing/invalid-Secret-from-start path. The secret-validation tests (controller_test.go:757, 770, 785) assert only conditions and never assert the db-sync Deployment (dbSyncWorkloadName) is absent, despite the rep… |

### CFG — Developer config & render

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| CFG-01 | Load a valid local environment document | ✅ satisfied | cli/internal/devconfig/config_test.go:TestLoadReadsEnvironmentConfig |  |
| CFG-02 | Wrong `apiVersion` or `kind` | 🟡 partial | cli/internal/devconfig/config_test.go:TestValidateRequiresEnvelope | Criteria says Load fails "naming the required value". Subcases assert only the field token: assert.Contains(err, "apiVersion") and assert.Contains(err, "kind") (config_test.go:449,454). Neither asserts the error names the required VALUE ("yacd.meigma.io/devconfig/v1alpha1" or "Environment"); a mess… |
| CFG-03 | Unknown field in the document | ✅ satisfied | cli/internal/devconfig/config_test.go:TestLoadRejectsUnknownTopLevelFields |  |
| CFG-04 | Defaulted-but-required field omitted from YAML (e.g. `node.port`, `mo… | 🟡 partial | cli/internal/devconfig/config_test.go:TestLoadRejectsOmittedConcreteCRDDefaults | The row explicitly names `mode` as an example, but omitting spec.network.mode is never tested as a standalone case (it would route through Validate's default 'mode must be local or public' rather than the explicit-fields pass, and is unasserted either way). local.era and the local.timing.slotLength… |
| CFG-05 | `mode: local` with a `public` block (or vice versa) | 🔴 not-satisfied | — | Add two negative Load cases: (1) mode:local with a `public:` block present -> err contains 'public is not supported with local mode'; (2) mode:public with a `local:` block present -> err contains 'local is not supported with public mode'. These exercise the mode/block XOR mismatch the row describes. |
| CFG-06 | `mode: public`, `profile: mainnet` without `bootstrap.mithril` | ✅ satisfied | cli/internal/devconfig/config_test.go:TestLoadRejectsUnsupportedPublicConfigs |  |
| CFG-07 | Render a loaded environment to a CardanoNetwork | 🟡 partial | cli/internal/render/render_test.go:TestCardanoNetworkRendersDeveloperConfig; cli/internal/render/render_test.go:TestMa… | No test asserts that the rendered CardanoNetwork's spec equals the document's network spec beyond the single scalar Spec.Local.NetworkMagic==42. The document (validConfig) also sets Spec.Mode=local, Spec.Node.Version="11.0.1", Spec.Node.Port=3001, Spec.Node.Storage.Size=2Gi, Spec.Local.Era=conway, … |
| CFG-08 | Identity comes from the CLI, not the file | 🟡 partial | cli/internal/render/render_test.go:TestCardanoNetworkRendersDeveloperConfig; cli/internal/render/render_test.go:TestCa… | The row's distinguishing claim 'derive from CLI args, NOT from any field in the document' is only proven implicitly (the document never carries identity). There is no test that demonstrates a name/namespace-like field in the YAML being ignored/rejected in favor of the args. A case feeding such a fi… |
| CFG-09 | Each shipped examples/**/yacd.yaml developer document | 🟡 partial | test/chainsaw/manager-smoke/chainsaw-test.yaml (manager-smoke step: `go run ./cli/cmd/yacd up phase4-smoke -n yacd-smo… | No test loads or renders the three public shipped documents (examples/public-preview/yacd.yaml, examples/public-preprod/yacd.yaml, examples/public-mainnet/yacd.yaml) through devconfig.LoadFile + render.CardanoNetwork. The intended Unit test that globs all four examples/**/yacd.yaml Environment docu… |

### CLI — Lifecycle verbs

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| CLI-01 | `up NAME -f file --dry-run` | ✅ satisfied | cli/internal/cli/up_test.go:TestUpDryRunPrintsManifestWithoutKubeClient; cli/internal/cli/up_test.go:TestUpDryRunDefau… |  |
| CLI-02 | `up NAME -f file` (default `--wait`) | ✅ satisfied | cli/internal/cli/up_test.go:TestUpEnsuresNamespaceAppliesAndWaits; cli/internal/cli/up_test.go:TestUpUsesNamespaceFlag… |  |
| CLI-03 | `up` without `--file` | 🔴 not-satisfied | — | Add a unit test that runs `up devnet` (no -f) and asserts the command errors with '--file is required' and exits non-zero without constructing the kube client. The plan's own Known coverage gaps section explicitly flags CLI-03 as untested. |
| CLI-04 | `up` with `--wait` and `--timeout 0` | ✅ satisfied | cli/internal/cli/up_test.go:TestUpRejectsInvalidWaitTimeoutBeforeApply |  |
| CLI-05 | `up` of a mainnet network without `--allow-mainnet` | ✅ satisfied | cli/internal/cli/up_test.go:TestUpRejectsMainnetApplyWithoutAllowFlag |  |
| CLI-06 | `up --dry-run` of a mainnet network without `--allow-mainnet` | ✅ satisfied | cli/internal/cli/up_test.go:TestUpDryRunAllowsMainnetWithWarning; cli/internal/cli/up_test.go:TestUpDryRunDoesNotWarnF… |  |
| CLI-07 | `NAME` that is not a DNS-1123 label | ✅ satisfied | cli/internal/cli/up_test.go:TestUpRejectsInvalidName; cli/internal/cli/down_test.go:TestDownRejectsInvalidName; cli/in… |  |
| CLI-08 | Namespace defaults to NAME | ✅ satisfied | cli/internal/cli/up_test.go:TestUpDryRunDefaultsNamespaceToName; cli/internal/cli/up_test.go:TestUpEnsuresNamespaceApp… |  |
| CLI-09 | `down NAME` (default `--wait`) | 🟡 partial | cli/internal/cli/down_test.go:TestDownDeletesAndWaitsUntilGone; cli/internal/cli/down_test.go:TestDownUsesNamespaceFla… | The criteria 'blocks until it AND its children are gone' is only proven at the parent level: the mock/staticClient model 'gone' purely as the parent CardanoNetwork returning NotFound; no test asserts the owned children (workload/PVC/Services/Secrets/ConfigMaps) are actually garbage-collected. There… |
| CLI-10 | `down NAME` for an absent network | ✅ satisfied | cli/internal/cli/down_test.go:TestDownIsIdempotentWhenAlreadyAbsent; cli/internal/kube/client_envtest_test.go:TestWait… |  |
| CLI-11 | `list` / `list -A` | ✅ satisfied | cli/internal/cli/list_test.go:TestListRendersTable; cli/internal/cli/list_test.go:TestListAllNamespacesPassesEmptyName… |  |
| CLI-12 | `list` with no matches | 🟡 partial | cli/internal/cli/list_test.go:TestListEmptyResultReportsNone; cli/internal/cli/list_test.go:TestListUsesDefaultNamespa… | No test asserts the `list -A` (all-namespaces) empty-result case. That branch (cli/internal/cli/list.go:176) prints `"No CardanoNetworks found."` with no scope named, so the "naming the scope" criterion is neither tested nor satisfied for the all-namespaces mode of the `list` with no matches scenar… |
| CLI-13 | `info NAME --json` | 🟡 partial | cli/internal/cli/info_test.go:TestInfoReadsGlobalKubeEnvironment; cli/internal/cli/info_test.go:TestInfoDefaultsNamesp… | No test asserts the "network identity" block of the info --json output. The criteria names network identity as a required JSON element, and the projection emits a `network` object (mode, networkMagic, era, profile, localnetFingerprint) at cli/internal/cli/info.go:130-140. The readyNetwork fixture s… |
| CLI-14 | `info`/`list` readiness from fresh status | 🟡 partial | cli/internal/cli/list_test.go:TestListRendersTable; cli/internal/cli/list_test.go:TestListJSONOutputShape | The row's core 'a STALE status reports not-ready' branch is untested at the verb level. No info/list test feeds a network whose Ready condition is present but with ObservedGeneration < Generation (the FreshCondition staleness path in conditions.go). The not-ready cases tested use a complete absence… |
| CLI-15 | `info`/`down`/`topup` for a nonexistent network | 🟡 partial | cli/internal/cli/down_test.go:TestDownIsIdempotentWhenAlreadyAbsent | CLI-15 requires `info` and `topup` (and the row text says down, though CLI-10 re-scopes down to idempotent success) to exit NON-zero with a not-found error for a nonexistent network. There is no test where info's GetCardanoNetwork returns kube.ErrNotFound and the command surfaces a not-found error … |

### HST — Host access & env contract

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| HST-01 | `run NAME -- cmd` against a Ready network | 🟡 partial | cli/internal/cli/run_test.go:TestRunInjectsYacdEnvironment; cli/internal/cli/run_test.go:runMock; cli/internal/cli/for… | Level mismatch: row recommends Unit (+E2E) but there is zero E2E. No Chainsaw/Kind test runs `yacd run` against a real Ready network to prove the SPDY port-forward + real child env actually work end-to-end (grep of test/chainsaw shows no run/exec/connect/forward references). The unit test only spot… |
| HST-02 | `run` child exit code propagation | ✅ satisfied | cli/internal/cli/run_test.go:TestRunPropagatesChildExitCode; cli/internal/cli/run_test.go:TestProcessExitCodeForExited… |  |
| HST-03 | `run` with no command | 🟡 partial | cli/internal/cli/run_test.go:TestRunCommandLine | Only the runCommandLine helper is tested, in isolation. No test exercises the full `run NAME` (no command) verb path proving the command actually drops into $SHELL with the YACD_* environment set on it. The criterion 'Drops into $SHELL ... with the environment set' (the env wiring on the shell proc… |
| HST-04 | Forward drops mid-run | 🟡 partial | cli/internal/cli/run_test.go:TestRunReportsDroppedForward | The "(exit non-zero)" sub-criterion is not asserted. TestRunReportsDroppedForward asserts only require.Error and that the message contains "lost connection to devnet/devnet"; it never resolves the exit code (e.g. via ResolveExit(err) -> assert non-zero, as TestRunPropagatesChildExitCode does at run… |
| HST-05 | `exec NAME -- cmd` | 🟡 partial | cli/internal/cli/exec_test.go:TestExecRunsInPodWithArgvOnlyEnv; cli/internal/cli/exec_test.go:TestExecPropagatesRemote… | Level mismatch: row recommends Unit (+E2E) and there is no E2E. No Chainsaw test runs `yacd exec` inside a real primary node Pod to prove the kubectl-exec path, socket reachability, and real remote exit-code propagation actually work in-cluster (test/chainsaw has no exec references). Coverage is mo… |
| HST-06 | `exec` against a not-ready network | 🔴 not-satisfied | — | Add a verb-level test that runs `exec NAME -- cmd` against a not-ready network (e.g. stale observedGeneration, missing/False Ready, or Degraded=True) and asserts a non-zero error referencing not-ready/stale/degraded, with NO Exec call attempted (no kubeClient.Exec expectation). The plan's own 'Know… |
| HST-07 | `exec` with no command after `NAME` | ✅ satisfied | cli/internal/cli/exec_test.go:TestExecRequiresCommand |  |
| HST-08 | `connect NAME` | ✅ satisfied | cli/internal/cli/connect_test.go:TestWriteEndpointsFile; cli/internal/cli/connect_test.go:TestRunConnectWritesFileAndE… |  |
| HST-09 | `connect` endpoints file is token-free | ✅ satisfied | cli/internal/cli/connect_test.go:TestWriteEndpointsFile; cli/internal/cli/connect_test.go:TestRunConnectWritesFileAndE… |  |
| HST-10 | `connect` reconnect | ✅ satisfied | cli/internal/cli/connect_test.go:TestRunConnectReEstablishesAfterDrop; cli/internal/cli/connect_test.go:TestRunConnect… |  |
| HST-11 | `connect` cleanup on interrupt | 🟡 partial | cli/internal/cli/connect_test.go:TestRunConnectWritesFileAndExitsOnCancel; cli/internal/cli/connect_test.go:TestRunCon… | The 'a disconnect message is printed' half is NOT asserted. On cancel, connect.go prints 'Disconnecting from <ns>/<name>.' to stderr (connect.go:127), but TestRunConnectWritesFileAndExitsOnCancel directs stderr to io.Discard and never checks for the message. Add an assertion that the disconnect mes… |
| HST-12 | Host env loopback rewrite | ✅ satisfied | cli/internal/cli/envcontract_test.go:TestHostEnvBuildsLoopbackContract; cli/internal/cli/envcontract_test.go:TestLoopb… |  |
| HST-13 | Pod env (`exec`) omits the faucet token | ✅ satisfied | cli/internal/cli/envcontract_test.go:TestPodEnvUsesClusterURLsAndOmitsToken; cli/internal/cli/exec_test.go:TestExecRun… |  |
| HST-14 | Host env includes the faucet token only when present | ✅ satisfied | cli/internal/cli/envcontract_test.go:TestHostEnvBuildsLoopbackContract; cli/internal/cli/envcontract_test.go:TestHostE… |  |
| HST-15 | Identity env always present | 🟡 partial | cli/internal/cli/envcontract_test.go:TestHostEnvBuildsLoopbackContract; cli/internal/cli/envcontract_test.go:TestPodEn… | The negative half of the criterion — 'YACD_NETWORK_MAGIC is set ONLY when published' — is untested. identityEnv (envcontract.go:180) gates the magic var on Status.Network != nil && NetworkMagic != nil, but no test builds a network with NetworkMagic (or Status.Network) nil and asserts YACD_NETWORK/Y… |

### TOP — Faucet topup CLI

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| TOP-01 | `topup` against a faucet-ready network | 🟡 partial | cli/internal/cli/topup_test.go:TestTopUpReadsSecretAndPostsToFaucet; cli/internal/cli/topup_test.go:TestTopUpUsesStatu… | No cited test (nor any test in cli/internal/cli/*_test.go) asserts that the returned destination is printed in the CLI output. The TOP-01 criteria explicitly enumerates four output fields (txId/source/lovelace/destination); destination is unverified in both output modes: the --json assertion in Tes… |
| TOP-02 | `--address` empty | 🔴 not-satisfied | — | Add a unit test that runs `topup NAME --lovelace 2000000` (or `--address ""`), asserts a non-zero error containing `--address is required`, and asserts no faucet POST and no GetSecretValue/GetCardanoNetwork-derived request was sent (mock with no Do/GetSecretValue expectation). |
| TOP-03 | --lovelace ≤ 0 | 🔴 not-satisfied | — | Add a unit test (table over 0 and a negative value) running topup with `--lovelace 0` / `--lovelace -1`, asserting a non-zero error whose message is exactly `--lovelace must be greater than 0` (TOP-03 revision pins the exact string) and that no request is sent. |
| TOP-04 | Network not faucet-ready or stale status (`observedGeneration` behind… | ✅ satisfied | cli/internal/cli/topup_test.go:TestTopUpRejectsStaleOrNotReadyStatus |  |
| TOP-05 | Network does not publish a faucet endpoint/auth Secret | 🔴 not-satisfied | — | Add unit tests that mutate readyNetwork to (a) drop the published faucet endpoint (Endpoints.Faucet=nil or URL="") and (b) drop the auth Secret name (Faucet=nil or AuthSecretName=""), asserting refusal with the exact errors `does not publish a faucet endpoint` (topup.go:200) and `does not publish a… |
| TOP-06 | Default target is the published URL | ✅ satisfied | cli/internal/cli/topup_test.go:TestTopUpUsesStatusEndpointByDefault; cli/internal/cli/topup_test.go:TestTopUpAllowsPub… |  |
| TOP-07 | `--faucet-url` to a non-loopback host without `--trust-faucet-url` | ✅ satisfied | cli/internal/cli/topup_test.go:TestTopUpRequiresTrustForRemoteCustomFaucetURLBeforeReadingSecret |  |
| TOP-08 | `--faucet-url` to a loopback host | 🟡 partial | cli/internal/cli/topup_test.go:TestTopUpReadsSecretAndPostsToFaucet (passes --faucet-url=http://127.0.0.1:<port> via h… | No test drives isLoopbackHost's `host == "localhost"` string branch (topup_trust.go:96) or the IPv6 `::1` loopback case; only 127.0.0.1 (the net.IP.IsLoopback path) is exercised, and there is no direct isLoopbackHost table test. To reach "satisfied", add either (a) a topup test passing `--faucet-ur… |
| TOP-09 | Trusted custom `http://` URL without `--allow-insecure-faucet-url` | ✅ satisfied | cli/internal/cli/topup_test.go:TestTopUpRequiresAllowInsecureForTrustedRemoteHTTPCustomFaucetURL; cli/internal/cli/top… |  |
| TOP-10 | `--faucet-url` that is not a valid absolute http/https URL | 🔴 not-satisfied | — | Add a unit test (table) passing invalid `--faucet-url` values: a relative/scheme-less value, an unsupported scheme (e.g. ftp:// or ws://), and a missing-host value (http://), asserting a non-zero error containing `invalid faucet URL` and (per branch) `scheme must be http or https` / `host is requir… |
| TOP-11 | `--await` with confirmation visible via Kupo | ✅ satisfied | cli/internal/cli/topup_await_test.go:TestTopUpAwaitConfirmsOnChain; cli/internal/cli/topup_await_test.go:TestAwaitConf… |  |
| TOP-12 | `--await` without a Kupo URL (no flag, no `YACD_KUPO_URL`) | ✅ satisfied | cli/internal/cli/topup_await_test.go:TestTopUpAwaitRequiresKupoURL |  |
| TOP-13 | `--json` output | ✅ satisfied | cli/internal/cli/topup_test.go:TestTopUpReadsSecretAndPostsToFaucet |  |

### FCT — Faucet HTTP API

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| FCT-01 | `GET /healthz` | ✅ satisfied | services/faucet/internal/server/server_test.go:TestHandlerHealth |  |
| FCT-02 | `GET /readyz` when sources are ready | ✅ satisfied | services/faucet/internal/server/server_test.go:TestHandlerReady |  |
| FCT-03 | `GET /readyz` when sources are not ready | ✅ satisfied | services/faucet/internal/server/server_test.go:TestHandlerReadyReportsMissingDefault |  |
| FCT-04 | `GET /v1/sources` and `GET /v1/sources/{name}` | 🟡 partial | services/faucet/internal/server/server_test.go:TestHandlerListsSources; services/faucet/internal/server/server_test.go… | No unit test asserts the funding address (sources.Source.Address) is present/correct in either the list or single-source response, which the row names explicitly ("including funding address"). TestHandlerListsSources asserts Name/Default only; TestHandlerReturnsOneSource asserts only Name. Add an a… |
| FCT-05 | `GET /v1/sources/{unknown}` | ✅ satisfied | services/faucet/internal/server/server_test.go:TestHandlerReturnsSourceNotFound |  |
| FCT-06 | `POST /v1/topups` with a valid bearer token and body | ✅ satisfied | services/faucet/internal/server/server_test.go:TestHandlerSubmitsTopUp |  |
| FCT-07 | `POST /v1/topups` without/with a wrong bearer token | ✅ satisfied | services/faucet/internal/server/server_test.go:TestHandlerTopUpRequiresBearerAuth |  |
| FCT-08 | `POST /v1/topups` with a non-POST method | ✅ satisfied | services/faucet/internal/server/server_test.go:TestHandlerRejectsTopUpUnsupportedMethod; services/faucet/internal/serv… |  |
| FCT-09 | `POST /v1/topups` without `Content-Type: application/json` | ✅ satisfied | services/faucet/internal/server/server_test.go:TestHandlerTopUpRequiresJSONContentType; services/faucet/internal/serve… |  |
| FCT-10 | `POST /v1/topups` with a body over the size limit, unknown fields, or… | 🟡 partial | services/faucet/internal/server/server_test.go:TestHandlerRejectsMalformedTopUpJSON; services/faucet/internal/server/s… | The row names three triggers; only unknown-fields and (incidentally) malformed JSON are tested. Add: (1) a body exceeding maxTopUpBodyBytes (4KB) via http.MaxBytesReader -> 400 invalid_request, and (2) a body containing multiple JSON values (decodeRequestBody's second decode != io.EOF) -> 400 inval… |
| FCT-11 | `POST /v1/topups` with `lovelace` omitted | 🔴 not-satisfied | — | Add a unit test posting a valid-auth JSON body with lovelace omitted (e.g. {"address":"<addr>"}) and assert status 400, body.Error.Code==topup.CodeInvalidRequest, and message "lovelace is required" (server.go:276-278). No existing test exercises this branch. |
| FCT-12 | Unknown route | 🔴 not-satisfied | — | Add a unit test performing a request to an unmapped path (e.g. GET /v1/unknown) and assert status 404 and body.Error.Code==codeNotFound ("not_found"). |
| FCT-13 | Auth token loaded from file (rotated) | 🟡 partial | services/faucet/internal/server/server_test.go:TestHandlerTopUpReloadsAuthTokenFile | The missing/empty-token -> 500 codeInternalError path is not tested. Add a unit test where the auth loader returns an empty string (or an error) and assert POST /v1/topups returns 500 with body.Error.Code==codeInternalError (server.go:296-305 requireAuth). The plan's Known coverage gaps explicitly … |
| FCT-14 | Topup engine error mapping | 🟡 partial | services/faucet/internal/server/server_test.go:TestHandlerTopUpReportsSourceNotFound; services/faucet/internal/server/… | The invalid_request->400 slice of the engine error mapping is NOT asserted through the engine path. The cited TestHandlerRejectsMalformedTopUpJSON (server_test.go:173) produces its 400 + CodeInvalidRequest at the request-decode short-circuit (server.go:272-275: decodeRequestBody failure -> writeErr… |

### FTX — Faucet topup engine

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| FTX-01 | Submit with empty source | ✅ satisfied | services/faucet/internal/topup/service_test.go:TestServiceSubmitUsesDefaultSource |  |
| FTX-02 | `lovelace` below min / above max / non-positive | 🟡 partial | services/faucet/internal/topup/service_test.go:TestServiceSubmitRejectsInvalidRequests | assertTopUpCode only checks the error Code, never the Message. The pass criteria requires the rejection to NAME the bound; service.go emits distinct messages ("lovelace must be positive", "lovelace must be at least %d", "lovelace must be at most %d") but no test asserts those strings or that the of… |
| FTX-03 | Invalid destination testnet address | 🟡 partial | services/faucet/internal/topup/service_test.go:TestServiceSubmitRejectsInvalidRequests | Only the Code is asserted; the criteria's "invalid destination address" intent (the WrapError message "invalid destination address") is not checked, and only one malformed-address shape (wrong HRP prefix) is exercised — no bad-checksum / non-bech32 / whitespace destination at the service layer. Add… |
| FTX-04 | Destination equals source address | ✅ satisfied | services/faucet/internal/topup/service_test.go:TestServiceSubmitRejectsSourceEqualsDestination |  |
| FTX-05 | Unknown / incomplete source | 🟡 partial | services/faucet/internal/topup/service_test.go:TestServiceSubmitMapsSourceErrors | The row says "Unknown / INCOMPLETE source". mapSourceError (service.go:220) collapses both CodeSourceNotFound AND CodeSourceIncomplete to CodeSourceNotFound, but no test drives the CodeSourceIncomplete input through the service. Add a table case with sourceErr=&sources.Error{Code: sources.CodeSourc… |
| FTX-06 | Successful submission | ✅ satisfied | services/faucet/internal/topup/service_test.go:TestServiceSubmitUsesDefaultSource; services/faucet/internal/topup/serv… |  |
| FTX-07 | Submitter returns empty txId or no spent inputs | 🔴 not-satisfied | — | service.go:181-186 returns CodeChainUnavailable when chainResult.TxID is blank OR len(SpentInputKeys)==0 on an otherwise successful submit. The fakeSubmitter actually MASKS this: SubmitTopUp backfills SpentInputKeys to testSpentInputKey whenever the result has none (service_test.go:438-440), so the… |
| FTX-08 | Concurrent submissions for one source | ✅ satisfied | services/faucet/internal/topup/service_test.go:TestServiceSubmitSerializesSameSource; services/faucet/internal/topup/s… |  |
| FTX-09 | Concurrent submissions across different sources | 🔴 not-satisfied | — | sourceLocks.lock keys the mutex by source name (service.go:311-322), so different sources should never block each other. Add a test that launches concurrent Submits for two distinct sources (e.g. utxo1 and utxo2) against a blocking submitter and asserts BOTH enter SubmitTopUp concurrently (both sta… |
| FTX-10 | Chain submit via Ogmios builds/signs/submits | 🟡 partial | services/faucet/internal/topup/apollo/client_test.go:TestValidateTransaction; services/faucet/internal/topup/apollo/cl… | The end-to-end 'builds/signs/submits' path through Client.SubmitTopUp — newChainContext().Init(), sourceUTxOs() Ogmios query, the apollo builder Complete()/Sign(), and the wiring of those into a single round-trip — is NEVER exercised together; only the isolated helpers are unit-tested. The empty-so… |
| FTX-11 | Source store load/validate | ✅ satisfied | services/faucet/internal/sources/sources_test.go:TestStoreRejectsTraversalNames; services/faucet/internal/sources/sour… |  |

### TLS — cardano-tools container

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| TLS-01 | `generate` a localnet artifact environment | 🟡 partial | containers/cardano-tools/cmd/yacd-cardano-tools/testdata/generate-dry-run.txtar; containers/cardano-tools/internal/gen… | No test exercises an ACTUAL generated localnet state directory: real generation shells out to the cardano-testnet binary, which is absent in unit tests, so no test asserts that a populated state directory plus a real yacd-localnet-plan.json manifest (with a real network magic and fingerprint value)… |
| TLS-02 | `stage` a generated state directory | ✅ satisfied | containers/cardano-tools/internal/stage/stage_test.go:TestRunStagesFlatServedDirectory |  |
| TLS-03 | `serve` an artifact directory | 🟡 partial | containers/cardano-tools/internal/serve/serve_test.go:TestServeReturnsAllowlistedArtifact; containers/cardano-tools/in… | Two parts of the row's exact criteria are unasserted. (1) 'non-GET/mutating requests are not served': the handler returns 405 'method not allowed' for any method other than GET/HEAD (serve.go:110-112), but no test issues a POST/PUT/DELETE and asserts the 405 / non-serving behavior. (2) 'manifest.js… |
| TLS-04 | `sync` from a serve endpoint | 🟡 partial | containers/cardano-tools/internal/artifactsync/sync_test.go:TestRunWritesAndVerifiesEveryFile; containers/cardano-tool… | The auditor's three cited tests do not assert the core "verifying each against the served manifest" half of the criteria, i.e. that per-file sha256 digest verification against the served manifest actually gates the write (fail-closed on a digest mismatch). All three cited tests either serve bytes t… |
| TLS-05 | `sync`/`fetch` encountering an HTTP redirect | 🟡 partial | containers/cardano-tools/internal/artifactsync/sync_test.go:TestRunRefusesRedirect | The row names both 'sync/fetch'. Only the sync half is tested. The fetch CLI builds an identical redirect-refusing client (containers/cardano-tools/internal/cli/fetch.go:36-40), and fetch's download() rejects any non-200 status (fetch.go:144 'unexpected status'), but no fetch_test case serves a 3xx… |
| TLS-06 | `fetch --profile preview/preprod/mainnet` | 🟡 partial | containers/cardano-tools/internal/fetch/fetch_test.go:TestRunWritesVerifiedArtifacts; containers/cardano-tools/interna… | Only the 'preview' profile is fetched-and-verified at unit level. The row names 'preview/preprod/mainnet'. preprod and mainnet are exercised only in fetch-dry-run.txtar (which prints URLs/pin status but downloads and verifies NOTHING) and not in any non-dry-run fetch test, so the claim that fetch '… |
| TLS-07 | A pinned file whose bytes do not match its digest | ✅ satisfied | containers/cardano-tools/internal/fetch/fetch_test.go:TestRunFailsOnPinnedDigestMismatch; containers/cardano-tools/int… |  |
| TLS-08 | `fetch`/`sync --dry-run` | ✅ satisfied | containers/cardano-tools/internal/fetch/fetch_test.go:TestRunDryRunWritesNothing; containers/cardano-tools/internal/ar… |  |
| TLS-09 | `fetch --profile` unknown | 🟡 partial | containers/cardano-tools/internal/fetch/fetch_test.go:TestRunRejectsUnknownProfile; containers/cardano-tools/cmd/yacd-… | No test asserts the "naming the supported profiles" half of the criteria. Both cited tests only match the literal prefix "unknown profile" (assert.Contains in the unit test; `stderr 'unknown profile'` in the txtar) and never assert that the error enumerates the supported profiles (preview/preprod/m… |
| TLS-10 | Manifest path / shape validation | 🔴 not-satisfied | — | Add a unit test (artifactset package) for ReadManifest covering each rejection branch with its exact message: empty path, non-absolute path, a path that is not <envDir>/yacd-localnet-plan.json, a manifest JSON missing inputs.networkMagic ('...inputs.networkMagic is required'), and a manifest missin… |
| TLS-11 | `version` / `--version` | ✅ satisfied | containers/cardano-tools/cmd/yacd-cardano-tools/testdata/version.txtar |  |

### PIN — Network identity & public pins

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| PIN-01 | Curated profile registry integrity | 🟡 partial | internal/cardano/publicnet/plan_test.go:TestBuildPlanCuratedProfiles; internal/cardano/publicnet/publicpins_identity_c… | The 'files in deterministic order' half of the criterion is never asserted directly: no test checks that publicpins.Profile.Files (or curatedProfiles file list) equals an exact expected ordered slice (config, byron, shelley, alonzo, conway, topology, then optional). Order is only exercised implicit… |
| PIN-02 | Pinned digests match embedded ground-truth | 🟡 partial | internal/cardano/publicnet/publicpins_crosscheck_test.go:TestPublicPinsMatchEmbeddedProfiles; internal/cardano/publicn… | No test pins the exact baseline value "CompatibleNodeRelease=11.0.1". The cited TestBuildPlanCuratedProfiles (plan_test.go:91) only asserts plan.Manifest.CompatibleNodeRelease == operationsBookNodeRelease (constant-vs-constant), so a drift of the operationsBookNodeRelease/publicpins.CompatibleNodeR… |
| PIN-03 | Operator manifest vs fetch agreement | 🟡 partial | internal/cardano/publicnet/publicpins_crosscheck_test.go:TestPublicPinsCoverCuratedProfiles; containers/cardano-tools/… | There is no test that directly asserts AGREEMENT between the two outputs — i.e. nothing computes the operator's BuildPlan manifest/fingerprint for a profile and compares it to what cardano-tools fetch verifies/produces for the same profile. Agreement is structural (shared package) rather than asser… |
| PIN-04 | Unpinned files are intentionally unpinned | 🟡 partial | internal/cardano/publicnet/publicpins_crosscheck_test.go:TestPublicPinsMatchEmbeddedProfiles; containers/cardano-tools… | No test directly asserts that the four genesis files (byron/shelley/alonzo/conway) and checkpoints carry no fetch-time pin (file.Pinned false / expectedSHA256 empty). The cited crosscheck test asserts only the one-directional invariant unpinned-implies-empty-digest (publicpins_crosscheck_test.go:47… |
| PIN-05 | Tools-image resolution | ✅ satisfied | internal/cardano/toolsimage/toolsimage_test.go:TestReference; internal/cardano/toolsimage/toolsimage_test.go:TestDiges… |  |

### CTN — cardano-testnet container

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| CTN-01 | Init produces the expected localnet layout | 🟡 partial | test/chainsaw/manager-smoke/chainsaw-test.yaml:deploy-controller (create-env init container + faucet round-trip); inte… | No test asserts CTN-01's literal Pass/Fail criteria — that the init OUTPUT DIRECTORY contains the expected named artifacts and funded UTxO source entries that stage/the faucet expect. The chainsaw proof is purely transitive (faucet endpoints answering), never inspecting the /state/env layout (e.g. … |
| CTN-02 | Funded source addresses are derivable | 🟡 partial | test/chainsaw/manager-smoke/chainsaw-test.yaml:deploy-controller (faucet GET /v1/sources/utxo1 + top-up source utxo2);… | No test asserts CTN-02 directly against REAL create-env output — i.e. that the faucet source-address init / sources.Store can read the generated source material as produced by the actual cardano-testnet binary. The sources_test.go fixtures are synthetic (hand-written utxo.vkey/utxo.skey/utxo.addr i… |

### HLM — Helm chart correctness

| ID | Scenario | Status | Covering tests | Gap for 100% |
|----|----------|--------|----------------|--------------|
| HLM-01 | Manager RBAC vs controller-gen | ✅ satisfied | test/chart/rbac_test.go:TestManagerRBACMatchesControllerGen |  |
| HLM-02 | CRDs install | 🟡 partial | test/chainsaw/manager-smoke/chainsaw-test.yaml:deploy-controller | Add a chart-render/unit assertion that both CRDs ship with the chart and install: e.g. render the chart (Helm v3 installs charts/*/crds/ automatically, or move CRDs into templates) and assert a CustomResourceDefinition named `cardanonetworks.yacd.meigma.io` and `cardanodbsyncs.yacd.meigma.io` are p… |
| HLM-03 | Manager Deployment + metrics Service | 🟡 partial | test/chainsaw/manager-smoke/chainsaw-test.yaml:deploy-controller | Add a unit/render test on a default `helm template charts/yacd` that asserts presence of: the Deployment (`yacd-controller-manager`), the metrics Service (`…-metrics-service`), the metrics-auth ClusterRole+ClusterRoleBinding, the metrics-reader ClusterRole, and the leader-election Role+RoleBinding.… |
| HLM-04 | Kyverno image policy is opt-in | ✅ satisfied | test/chart/rbac_test.go:TestKyvernoImageVerificationPolicyIsOptional |  |
| HLM-05 | Kyverno policy when enabled | ✅ satisfied | test/chart/rbac_test.go:TestKyvernoImageVerificationPolicyRendersGitHubAttestationPolicy |  |
| HLM-06 | Kyverno image-reference override | ✅ satisfied | test/chart/rbac_test.go:TestKyvernoImageVerificationPolicyAllowsExplicitImageReferenceOverride |  |
| HLM-07 | values.schema.json accepts a valid values file | 🟡 partial | /Users/josh/code/meigma/yacd/test/chart/rbac_test.go:TestKyvernoImageVerificationPolicyRendersGitHubAttestationPolicy … | No test deliberately renders a representative full valid custom values document (image, metrics, manager, tls, serviceAccount, valid enums, in-range ports, valid dnsLabel/dnsSubdomain names) and asserts the render succeeds with schema-acceptance as its purpose. Existing coverage is incidental: Helm… |
| HLM-08 | values.schema.json rejects invalid values | 🔴 not-satisfied | — | Add a negative test: render with a values file that violates the schema (e.g. metrics.port=0 / 70000, manager.logFormat=yaml, an unknown top-level key, image.repository='', or a non-dnsLabel nameOverride) and assert `helm template` exits non-zero with a schema validation error. This is the Type- re… |
