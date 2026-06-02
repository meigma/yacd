# YACD Test Plan (verified revision)

> **Revision note.** This is the reviewed/verified revision of the original
> `TEST_PLAN.md`. Every one of the 185 requirements was checked against the
> actual YACD source: each row was confirmed to target real code, its concrete
> claims (ports, condition type/reason strings, defaults, network magic, HTTP
> status codes, error-message intent, flag names) were verified line-by-line,
> overlaps were consolidated, and a **Level** column was added recording the
> right test mechanism. Consequential corrections were adversarially
> re-confirmed before being applied. See **Verification summary** below for what
> changed.

## Purpose

This document is a **declarative requirements list** for testing the YACD
runtime product. It states *what* must be verified and the *pass/fail criteria*
that decide each result. Requirements are written at integration / end-to-end
altitude — observable behavior and published contracts — rather than as
assertions about internal functions.

Unlike the first draft, this revision also records, per requirement, the
**test level** at which it should be implemented (E2E / Env / Unit — see
*Conventions*). That is an implementation-tier recommendation, not a mandate to
write the test a particular way; it exists so coverage can be planned against
the repository's deliberately layered test strategy (do not duplicate the
envtest behavior matrix in Chainsaw).

Each requirement is a row in a per-category table. Coverage is intended to be
audited against this document: every in-scope source area maps to at least one
category, and every category that can fail carries both positive and negative
requirements.

## Scope

**In scope** (the runtime product):

- Operator manager (`cmd/`)
- CRDs and controllers — `CardanoNetwork` and `CardanoDBSync`
  (`api/v1alpha1/`, `internal/controller/`)
- Developer CLI (`cli/`)
- Faucet service (`services/faucet/`)
- Artifact containers (`containers/cardano-tools/`, `containers/cardano-testnet/`)
- Network identity / public-pin integrity (`internal/cardano/`)
- Helm chart correctness (`charts/yacd/`)

**Out of scope**: CI/release scripting (`.github/`), image build/sign plumbing,
local dev-stack tooling (ctlptl/Tilt/Moon), and session/journal protocol
tooling. These may be tested separately; they are not part of this plan.

## Conventions

Each category below is a single table with these columns:

| Column | Meaning |
|--------|---------|
| `ID` | `PREFIX-NN`, stable and unique |
| `Scenario` | The condition under test |
| `Pass/Fail Criteria` | The observable outcome that decides the result |
| `Type` | `+` positive (expected success) or `−` negative (expected rejection/failure) |
| `Level` | The recommended test mechanism: `E2E`, `Env`, or `Unit` (a `(+X)` suffix marks a thin secondary smoke at level `X`) |

Pass criteria name concrete, observable outcomes: a specific status condition
type and value, a resource's presence or absence, an HTTP status, a process
exit code, or the intent of an error message. Negative requirements cover
realistic misconfigurations and contract violations, not contrived adversarial
inputs.

### Test-level taxonomy

The original draft framed the implementation choice as a binary (Chainsaw E2E
vs. "testenv"). Verification found that **most** requirements belong in neither
a real cluster nor envtest — they are pure unit/mocked tests (faucet HTTP
handlers, CLI flag/trust logic, the cardano-tools verbs, public-pin digests,
Helm rendering). Forcing those into "testenv" would be misleading, so the
`Level` column uses three honest tiers:

- **Unit** — *no* kube-apiserver and *no* cluster. Pure Go test (table /
  Testify+mockery / `net/http/httptest` / `go-internal/testscript` txtar) **or**
  `helm template` / `values.schema.json` validation / controller-gen RBAC-drift.
  Right for CLI flag & trust/transport logic (mocked `kube.Client`), devconfig
  load+render, faucet HTTP handlers (httptest + fake engine), the faucet topup
  engine (mock submitter/sources), the cardano-tools CLI verbs, public-pin
  digests & the profile registry, Helm rendering, and manager *flag* parsing.
- **Env** — real kube-apiserver via controller-runtime **envtest**, but *no*
  real pods/kubelet/networking. Right for CRD admission & CEL/OpenAPI
  validation, defaulting, reconcile *output* (owned children with correct
  ownerRefs / names / ports / volumes / mounts / init-containers), status
  *condition* transitions driven by fake child status, identity/immutability
  acceptance, and RBAC-marker↔reconcile alignment.
  *Caveat:* envtest does **not** run the garbage collector — a "children are
  GC'd after delete" assertion can verify ownerReferences in Env, but the
  *actual* cascade delete must be proven at E2E.
- **E2E** — the packaged operator in a real Kind cluster (**Chainsaw**) with
  real pods, kubelet, networking, workload controllers, or real
  cardano-node / Ogmios / Kupo / db-sync / Postgres processes. Right for node /
  Ogmios / Kupo / faucet actually reaching Ready, real endpoints answering
  (`queryNetwork/tip` over the Service, protected `/metrics` auth, faucet POST
  round-trip), real artifact serve-over-HTTP + sync between pods, Postgres
  accepting connections, db-sync writing rows, the actual GC cascade, and leader
  election. A handful are flagged **EXPENSIVE** (real public-network chain sync,
  mainnet Mithril restore, db-sync block population) — keep these smoke-only or
  manual; they are not cheap CI assertions.

The recommended split across the 185 requirements: **Unit 115, Env 53, E2E 17**
(primary level), with thin secondary smokes where a wiring test deserves one
real-cluster confirmation.

## Coverage map

| Source area | Categories |
|-------------|-----------|
| `cmd/` | MGR |
| `api/v1alpha1/` (CardanoNetwork) | CNV, CNI |
| `api/v1alpha1/` (CardanoDBSync) | DBV |
| `internal/controller/cardanonetwork/` | CNL, CNP, API, CNI |
| `internal/controller/cardanodbsync/` | DBF, DBS, DBD |
| `internal/controller/*` shared (ctrlkit, children, storage) | CNL, DBF |
| `cli/internal/devconfig/`, `cli/internal/render/` | CFG |
| `cli/internal/cli/` (lifecycle) | CLI |
| `cli/internal/cli/` (host access + env contract) | HST |
| `cli/internal/cli/` (topup) | TOP |
| `cli/internal/kube/` | CLI, HST, TOP |
| `services/faucet/internal/server/` | FCT |
| `services/faucet/internal/topup/`, `.../sources/` | FTX |
| `containers/cardano-tools/` | TLS |
| `internal/cardano/publicnet/`, `.../publicpins/`, `.../toolsimage/` | PIN |
| `containers/cardano-testnet/` | CTN |
| `charts/yacd/` | HLM |

## Verification summary

**Method.** 15 category-group agents read the plan and the real source in
parallel; each row was given a verdict and a `Level`. Every consequential flag
(hallucination / major-inaccuracy / infeasible) was handed to a second,
adversarial agent instructed to *refute* it; only flags that survived refutation
were applied. A final synthesis pass clustered cross-category overlaps.

**Result — the draft was strong:** of 185 rows, **175 were legit as written**,
**9 had a minor inaccuracy** (a named constant/string/scope was slightly off),
and **1 was hallucinated** (CNL-07). The adversarial pass **overturned 2 false
positives** (CNP-07, DBF-06) that an initial reviewer had wrongly flagged,
keeping them as legitimate tests. No row targeted F0-removed surface (no
`network-artifacts` ConfigMap, no `custom-public`, no cardano-testnet publisher,
no cardano-tools `report` verb) — the draft is current with `master`.

**The 11 rows changed in this revision** (all grounded in cited source):

| ID | Change |
|----|--------|
| MGR-03 | Re-scoped: an invalid `--log-format` is rejected at Kong **parse** (`enum:"json,text"`); the `unsupported log format` string in `newControllerLogger` is unreachable from the parsed entrypoint (unit-level guard only). |
| CNL-07 | **Hallucination fixed → negative row.** Local genesis-economics tuning is *not* implemented: the builder rejects any non-nil `spec.local.genesis` with `UnsupportedSpec` (`"local genesis tuning is not supported"`). The `GenesisProfile` enum exists in the API but the reconciler refuses it. |
| CNL-09 | Tightened: **any** local pool count `!= 1` is rejected at *reconcile* (`Degraded`, reason `UnsupportedSpec`, `"local pool count N is not supported"`); there is no API-level maximum, so it is not an admission rejection. |
| DBV-09 | Corrected defaults: db-sync `ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0`, postgres `postgres:17.2-alpine`, db `cexplorer`, user `postgres`, external port 5432, `passwordSecretRef.key=password`, `sslMode=disable`. `ledgerBackend=lsm` / insert `preset=full` only materialize when the optional `config` / `config.insert` parents are present. |
| DBF-08 | Corrected condition: a missing/not-ready `networkRef` yields `Degraded` (`NetworkUnavailable`) or `Progressing` (`NetworkStatusStale` / `NetworkArtifactsPending` / `NodeToNodeEndpointMissing`), **not** `NodeSocketReady` (which is primary-sidecar-only). |
| CFG-09 | Scoped: only `examples/**/yacd.yaml` are devconfig `Environment` documents; the `cardanodbsync-*.yaml` / Secret examples are CRD manifests that `devconfig.Load` rejects by design — validate them separately. |
| TOP-03 | Exact message: `"--lovelace must be greater than 0"`. |
| TLS-10 | Precise: the manifest file must be exactly `<envDir>/yacd-localnet-plan.json` (absolute); empty/non-absolute/wrong-path is rejected, as is a manifest missing `inputs.networkMagic` or `fingerprint.value`. |
| PIN-02 | Corrected mechanism: there is no separate upstream known-good table — each pin recomputes from the bytes embedded under `internal/cardano/publicnet/profiles`; `CompatibleNodeRelease=11.0.1` is the baseline. |
| PIN-05 | Split: only the **cardano-tools** image resolves via `toolsimage.Reference` (digest-pinned `…/cardano-tools:<ver>-yacd.5@sha256:d3283c…`); the **cardano-testnet** image is a tag (`…/cardano-testnet:<ver>-yacd.5`, not digest-pinned). Each honors its own `--default-*-image` override. |
| CNP-07 | Re-framed + cross-referenced to CNV-08: an unknown profile is rejected at **CRD enum admission** (the closed enum makes the reconcile-Degraded path largely unreachable); primary coverage is CNV-08. |

---

## MGR — Manager startup

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| MGR-01 | Manager starts with default flags | Process runs; Deployment reports `Available=True`; both controllers registered and watching their CRDs | + | E2E (+Env) |
| MGR-02 | `--log-format=json` | Emitted controller logs are JSON; `--log-format=text` emits text lines | + | Unit |
| MGR-03 | --log-format set to an unsupported value | Startup fails at flag parse via Kong enum validation (enum json,text); manager does not run. The "unsupported log format" string in newControllerLogger is unreachable from the parsed entrypoint — Kong rejects first — so it is a unit-level guard only (see MGR-04). | − | Unit |
| MGR-04 | `--log-level` set to an unsupported value | Startup fails at parse (enum violation); manager does not run | − | Unit |
| MGR-05 | Health/readiness probes | `/healthz` and `/readyz` on the health bind address return 200 when the manager is healthy | + | E2E |
| MGR-06 | Metrics served with `--metrics-secure` (default) to an authorized ServiceAccount | HTTPS `/metrics` returns 200 and exposes `go_goroutines` when called with a valid bearer token via the metrics ClusterRole | + | E2E |
| MGR-07 | Metrics requested without authorization | Request is rejected (not 200); metrics are not exposed to unauthenticated callers | − | E2E (+Unit) |
| MGR-08 | HTTP/2 default | HTTP/2 is disabled on the metrics/webhook servers unless `--enable-http2` is set | + | Unit |
| MGR-09 | `--metrics-bind-address=0` | Metrics server is disabled; no metrics endpoint is exposed | + | Unit |
| MGR-10 | Leader election enabled (`--leader-elect`) with multiple replicas | Exactly one replica becomes leader and performs reconciles; others stand by | + | E2E (+Unit) |
| MGR-11 | Default image flags resolve | `--default-faucet-image`, `--default-cardano-testnet-image`, `--default-cardano-tools-image` are honored by the controllers when a spec omits an override | + | Unit (+Env) |

## CNV — CardanoNetwork API validation

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| CNV-01 | `mode: local` with `spec.local` set and `spec.public` absent | Resource is admitted | + | Env |
| CNV-02 | `mode: local` with `spec.public` also set | Admission rejected citing the mode/local/public XOR rule | − | Env |
| CNV-03 | `mode: public` with `spec.public` absent | Admission rejected citing the mode/local/public XOR rule | − | Env |
| CNV-04 | `mode: public`, `profile: mainnet`, no `bootstrap.mithril` | Admission rejected requiring `bootstrap.mithril` for mainnet | − | Env |
| CNV-05 | `mode: public`, `profile: preview`/`preprod`, `bootstrap` set | Admission rejected: bootstrap is valid only for mainnet | − | Env |
| CNV-06 | `node.port` outside 1–65535 | Admission rejected by range validation | − | Env |
| CNV-07 | Ogmios/Kupo/faucet port outside 1–65535 | Admission rejected by range validation | − | Env |
| CNV-08 | Enum fields given unknown values (`mode`, `era`, `profile`, genesis `profile`) | Admission rejected by enum validation | − | Env |
| CNV-09 | Pool `margin` outside the `^(0(\.[0-9]+)?\|1(\.0+)?)$` pattern | Admission rejected by pattern validation | − | Env |
| CNV-10 | Minimal valid local spec omitting defaulted fields | Defaults applied: `era=conway`, node version default, Ogmios/Kupo `enabled=true`, faucet `enabled=false`, node port 3001 | + | Env |
| CNV-11 | Faucet `minTopUpLovelace` and `maxTopUpLovelace` defaulting | Unset values default to 1,000,000 and 10,000,000,000 respectively | + | Env |

## CNL — CardanoNetwork local reconciliation

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| CNL-01 | Apply a valid `mode: local` network | Primary node workload, node-state PVC, and node-to-node `ClusterIP` Service (port 3001) are created and owned by the CardanoNetwork | + | Env (+E2E) |
| CNL-02 | Local genesis is generated and staged | Network artifacts are staged on the node-state PVC and served over HTTP through the always-on serve sidecar/Service (port 8090); `ArtifactsReady=True` | + | E2E (+Env) |
| CNL-03 | No artifacts ConfigMap is produced | No `<net>-network-artifacts` ConfigMap exists; the primary Pod mounts no public-profile ConfigMap volume | − | Env |
| CNL-04 | Node becomes ready | `NodeReady=True` once the primary node container is running; `Ready=True` once endpoints are usable | + | E2E (+Env) |
| CNL-05 | Status endpoints published | `status.endpoints.nodeToNode` reports service name, port 3001, and `tcp://…:3001` URL | + | Env (+E2E) |
| CNL-06 | Network identity published | `status.network` reports resolved `mode=local`, `era`, network magic, and a non-empty localnet fingerprint | + | Env |
| CNL-07 | mode: local with spec.local.genesis set (e.g. profile: zero-fee) | Reconcile REJECTS the spec: Degraded=True, reason UnsupportedSpec, message "local genesis tuning is not supported"; no primary node workload, PVC, or Services are created. Genesis-economics tuning is NOT implemented — the GenesisProfile enum exists in the API but the builder refuses any non-nil spec.local.genesis. | − | Unit |
| CNL-08 | Steady state is not perpetually Progressing | After readiness, `Progressing=False` and `Degraded=False` | + | E2E (+Env) |
| CNL-09 | Local pool count not equal to 1 | Reconcile reports Degraded=True, reason UnsupportedSpec ("local pool count N is not supported"); no primary children are created (multi-node topology is not silently reconciled). There is no API-level maximum (Minimum=1 only), so rejection is at reconcile, not admission. | − | Unit (+Env) |
| CNL-10 | Deleting the CardanoNetwork | All owned children (workload, PVC, Services, Secrets, ConfigMaps) are garbage-collected | + | E2E (+Env) |

## CNP — CardanoNetwork public reconciliation

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| CNP-01 | Apply `mode: public`, `profile: preview` (or `preprod`) | Primary node joins the public network; profile artifacts are fetched and staged; node, Ogmios, and node-to-node endpoints are published | + | Env (+E2E) |
| CNP-02 | Resolved public identity | `status.network` reports `mode=public`, the resolved profile, and the profile's network magic (preview=2, preprod=1, mainnet=764824073) | + | Env (+Unit) |
| CNP-03 | Mainnet with Mithril bootstrap | A Mithril bootstrap init container seeds the node database from a verified snapshot before the node starts | + | Env (+E2E) |
| CNP-04 | Ogmios-backed sync probe | `status.sync` reports `source=ogmios`, connection status, last tip, and a `networkSynchronization` value in [0,1] | + | Unit (+Env) |
| CNP-05 | Sync lag computation | `status.sync.lagSlots`/`lagSeconds` are derived from inferred tip slot and slot length, never negative | + | Unit |
| CNP-06 | `NodeSynchronized` reflects catch-up | `NodeSynchronized=True` only once the node is caught up to the inferred tip; `NodeProgressing` reflects an advancing tip | + | Unit (+Env) |
| CNP-07 | Unknown public profile (enum boundary) | An unknown profile is rejected at CRD enum admission (Enum=preview;preprod;mainnet) — see CNV-08, the primary coverage. A value that passes admission but cannot be resolved drives reconcile to Degraded rather than fetching from an unverified source; the closed enum makes that reconcile path largely unreachable. | − | Env (+Unit) |

## API — Chain-API sidecars

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| API-01 | Default Ogmios + Kupo | Both sidecars are deployed; Ogmios Service exposes port 1337 (`ws://`), Kupo Service exposes port 1442 (`http://`); `OgmiosReady=True`, `KupoReady=True` | + | Env (+E2E) |
| API-02 | Faucet opt-in (`faucet.enabled=true`) | Faucet sidecar + `ClusterIP` Service (port 8080) deployed; `FaucetReady=True`; faucet endpoint published | + | Env (+E2E) |
| API-03 | Faucet auth Secret lifecycle | A `<net>-faucet-auth` Opaque Secret with a bearer token is created; `status.faucet.authSecretName` points to it | + | Env (+E2E) |
| API-04 | Faucet disabled by default | With no faucet config, no faucet Service or auth Secret exists; `FaucetReady=False` with reason `FaucetDisabled` | − | Env |
| API-05 | Disable Kupo at runtime (`kupo.enabled=false`) | Kupo Service is removed; `KupoReady=False` with reason `KupoDisabled`; no Kupo endpoint published | + | Env |
| API-06 | Disable faucet at runtime | Faucet Service and auth Secret are removed; `FaucetReady=False` reason `FaucetDisabled`; faucet endpoint and `status.faucet` cleared | + | Env |
| API-07 | Faucet image override using a different repository than the controller default | Rejected: faucet image overrides must share the controller's configured default repository | − | Unit (+Env) |
| API-08 | Endpoint URLs match Service identity | Published Ogmios/Kupo/faucet URLs use the cluster-DNS form `<scheme>://<svc>.<ns>.svc.cluster.local:<port>` with the correct scheme per service | + | Env (+E2E) |

## CNI — CardanoNetwork identity & immutability

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| CNI-01 | First reconcile accepts inputs | `status.network.localnetFingerprint` and `networkFingerprint` are recorded | + | Env |
| CNI-02 | Mutate an identity-affecting local input (e.g. network magic, era, genesis economics) after acceptance | Controller refuses the change and surfaces it (e.g. `Degraded`) rather than regenerating genesis under the live network | − | Env |
| CNI-03 | Recreate after deletion with changed inputs | A freshly created CardanoNetwork accepts the new inputs and records a new fingerprint | + | Env |
| CNI-04 | Non-identity field change (e.g. resources) | Change is reconciled without a fingerprint conflict | + | Env |

## DBV — CardanoDBSync API validation

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| DBV-01 | `database.managed` set, `database.external` absent | Resource is admitted | + | Env |
| DBV-02 | Both `database.external` and `database.managed` set | Admission rejected: exactly one database mode required | − | Env |
| DBV-03 | Neither database mode set | Admission rejected: exactly one database mode required | − | Env |
| DBV-04 | `placement.mode=primarySidecar` with `followerNode` set | Admission rejected: `followerNode` is invalid with `primarySidecar` | − | Env |
| DBV-05 | `networkRef.name` empty | Admission rejected by MinLength validation | − | Env |
| DBV-06 | External DB `passwordSecretRef` with empty name/key | Admission rejected by MinLength validation | − | Env |
| DBV-07 | Enum fields given unknown values (ledger backend, insert preset, tx_out mode, ledger mode, json type, ssl mode, placement mode) | Admission rejected by enum validation | − | Env |
| DBV-08 | External DB port outside 1–65535 | Admission rejected by range validation | − | Env |
| DBV-09 | Minimal valid managed spec omitting defaults | Defaults applied: db-sync image ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0, postgres image postgres:17.2-alpine, database cexplorer, user postgres; external-DB path defaults port 5432, passwordSecretRef.key password, sslMode disable. NOTE: ledgerBackend=lsm and insert preset=full only materialize when the optional config / config.insert objects are present (the apiserver does not synthesize omitted parents). | + | Env |

## DBF — CardanoDBSync dedicated follower

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| DBF-01 | Apply a managed-Postgres CardanoDBSync referencing a Ready network | Follower-node + db-sync workload and a managed Postgres workload (Deployment, Service, PVC, auth Secret) are created and owned by the CardanoDBSync | + | Env (+E2E) |
| DBF-02 | Artifacts transport | The follower fetches network artifacts over HTTP via a `network-artifacts-sync` init container into an emptyDir; it does **not** mount a `network-artifacts` ConfigMap | − | Unit (+E2E) |
| DBF-03 | Postgres readiness | `PostgresReady=True` (reason `PostgresReady`) once Postgres accepts local connections | + | E2E (+Env) |
| DBF-04 | Follower and db-sync readiness | `FollowerNodeReady=True` and `DBSyncReady=True` once both processes run | + | Env (+E2E) |
| DBF-05 | Sync progress reported | `status.sync` reports `nodeBlockHeight`, `dbBlockHeight`, and `lagBlocks`; `Synced` is `True`/`Synced` or `False`/`SyncLagging`, never a stale other value | + | E2E (+Env) |
| DBF-06 | Data lands in Postgres | After sync, the `block` table contains rows with non-null `block_no` | + | E2E |
| DBF-07 | Postgres endpoint published | `status.endpoints.postgres` reports service name, port 5432, and `postgres://…:5432/cexplorer`; `status.database.authSecretName` is set | + | Env (+E2E) |
| DBF-08 | networkRef points to a missing/not-ready network | Controller reports Degraded=True (reason NetworkUnavailable) for a missing/deleting network, or Progressing=True (reason NetworkStatusStale / NetworkArtifactsPending / NodeToNodeEndpointMissing) for a not-ready network — rather than crash-looping; no workload is applied. NodeSocketReady is a primary-sidecar-only signal, not this path. | − | Env |
| DBF-09 | Delete the CardanoDBSync | All owned children are garbage-collected | + | E2E (+Env) |

## DBS — CardanoDBSync primary sidecar

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| DBS-01 | `placement.mode=primarySidecar` against a non-mainnet network | `SidecarMaterialReady=True`; `status.placement.primarySidecar` publishes a revision plus the ConfigMap/pgpass-Secret/state-PVC/metrics-Service names | + | Env |
| DBS-02 | CardanoNetwork consumes the sidecar material | The primary Pod mounts the published material and runs the db-sync sidecar; CardanoNetwork reports `DBSyncAttachmentReady=True` | + | E2E (+Env) |
| DBS-03 | Attaching/changing the sidecar rolls the primary | Enabling or changing the attachment rolls the primary Deployment (revision changes) | + | Env (+E2E) |
| DBS-04 | Primary-sidecar db-sync on public mainnet | Rejected/blocked: mainnet db-sync via sidecar is not permitted until a bootstrap/sizing path is proven | − | Env |
| DBS-05 | Switch placement mode after a placement was accepted | Controller blocks switching between `dedicatedFollower` and `primarySidecar` on existing state; requires recreate with fresh/compatible DB | − | Env |
| DBS-06 | Accepted placement recorded | `status.database.acceptedPlacementMode` reflects the accepted mode | + | Env |

## DBD — CardanoDBSync database

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| DBD-01 | Managed Postgres without `authSecretRef` | Controller creates an owned auth Secret and reports its name in `status.database.authSecretName` | + | Env (+E2E) |
| DBD-02 | Managed Postgres with a user-supplied `authSecretRef` | Controller uses the referenced Secret; no owned credential Secret is created | + | Env |
| DBD-03 | External Postgres | No managed Postgres workload is created; db-sync connects using `host`/`port`/`database`/`user`/`passwordSecretRef`/`sslMode` | + | Env |
| DBD-04 | Insert preset translation | `config.insert.preset` (`full`/`only_utxo`/`only_governance`/`disable_all`) and explicit overrides are translated into the db-sync config consumed by the container | + | Unit |
| DBD-05 | Runtime/ledger-backend translation | `config.runtime` flags and `ledgerBackend` (`lsm`/`inmemory`) are reflected in the rendered db-sync configuration | + | Unit |
| DBD-06 | Managed Postgres parameters | `parameters.maintenanceWorkMem` / `maxParallelMaintenanceWorkers` are applied to the Postgres workload | + | Env |
| DBD-07 | External Postgres referencing a missing password Secret | Controller reports not-ready rather than starting db-sync with no credentials | − | Env |

## CFG — Developer config & render

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| CFG-01 | Load a valid local environment document | Parses; `apiVersion=yacd.meigma.io/devconfig/v1alpha1`, `kind=Environment` accepted | + | Unit |
| CFG-02 | Wrong `apiVersion` or `kind` | Load fails naming the required value | − | Unit |
| CFG-03 | Unknown field in the document | Strict decode fails on the unknown field | − | Unit |
| CFG-04 | Defaulted-but-required field omitted from YAML (e.g. `node.port`, `mode`, `local.networkMagic`) | Load fails: the field must be set explicitly in the developer config | − | Unit |
| CFG-05 | `mode: local` with a `public` block (or vice versa) | Load fails: the wrong block is not supported for the mode | − | Unit |
| CFG-06 | `mode: public`, `profile: mainnet` without `bootstrap.mithril` | Load fails requiring `bootstrap.mithril` | − | Unit |
| CFG-07 | Render a loaded environment to a CardanoNetwork | A CardanoNetwork manifest is produced whose spec matches the document's network spec | + | Unit |
| CFG-08 | Identity comes from the CLI, not the file | Rendered name/namespace derive from CLI args, not from any field in the document | + | Unit |
| CFG-09 | Each shipped examples/**/yacd.yaml developer document | Each examples/**/yacd.yaml (local, public-preview, public-preprod, public-mainnet) loads via devconfig and renders to a CardanoNetwork without error. The cardanodbsync-*.yaml / Secret examples are CRD manifests, not Environment documents — devconfig.Load rejects them by design; validate those separately (CRD apply). | + | Unit |

## CLI — Lifecycle verbs

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| CLI-01 | `up NAME -f file --dry-run` | Renders the CardanoNetwork manifest to stdout; no cluster resources are created | + | Unit |
| CLI-02 | `up NAME -f file` (default `--wait`) | Namespace ensured, CardanoNetwork applied, command blocks until `Ready` then exits 0 | + | Unit (+E2E) |
| CLI-03 | `up` without `--file` | Exits non-zero: `--file is required` | − | Unit |
| CLI-04 | `up` with `--wait` and `--timeout 0` | Exits non-zero: timeout must be greater than 0 | − | Unit |
| CLI-05 | `up` of a mainnet network without `--allow-mainnet` | Refused before apply, explaining mainnet requires `--allow-mainnet` | − | Unit |
| CLI-06 | `up --dry-run` of a mainnet network without `--allow-mainnet` | Renders manifest and prints a mainnet warning; applies nothing | + | Unit |
| CLI-07 | `NAME` that is not a DNS-1123 label | Exits non-zero with an invalid-name error | − | Unit |
| CLI-08 | Namespace defaults to NAME | With no `--namespace`, the environment lands in a namespace equal to NAME | + | Unit |
| CLI-09 | `down NAME` (default `--wait`) | Deletes the CardanoNetwork and blocks until it and its children are gone, then exits 0 | + | Unit (+E2E) |
| CLI-10 | `down NAME` for an absent network | Reported as success (idempotent) | + | Unit |
| CLI-11 | `list` / `list -A` | Lists CardanoNetworks in the namespace (or all namespaces) with name/namespace/mode/ready/endpoints | + | Unit |
| CLI-12 | `list` with no matches | Prints an explicit "none found" message naming the scope | + | Unit |
| CLI-13 | `info NAME --json` | Emits stable JSON with name, namespace, network identity, endpoints, faucet, and conditions | + | Unit |
| CLI-14 | `info`/`list` readiness from fresh status | `Ready` reflects a fresh Ready condition; a stale status reports not-ready | + | Unit |
| CLI-15 | `info`/`down`/`topup` for a nonexistent network | Exits non-zero with a not-found error | − | Unit |

## HST — Host access & env contract

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| HST-01 | `run NAME -- cmd` against a Ready network | Forwards chain endpoints, runs `cmd` with the `YACD_*` environment set, tears forwards down on exit | + | Unit (+E2E) |
| HST-02 | `run` child exit code propagation | The child's non-zero exit code is propagated as the CLI's exit code | + | Unit |
| HST-03 | `run` with no command | Drops into `$SHELL` (or `/bin/sh`) with the environment set | + | Unit |
| HST-04 | Forward drops mid-run | Command is cancelled and the lost connection is reported (exit non-zero), not a bare success | − | Unit |
| HST-05 | `exec NAME -- cmd` | Runs `cmd` inside the primary node Pod with `CARDANO_NODE_SOCKET_PATH` and `YACD_*` set; propagates the remote exit code | + | Unit (+E2E) |
| HST-06 | `exec` against a not-ready network | Refused with a not-ready error before exec | − | Unit |
| HST-07 | `exec` with no command after `NAME` | Exits non-zero explaining a command is required | − | Unit |
| HST-08 | `connect NAME` | Writes `.yacd/<…>/endpoints.json` (dir 0700, file 0600), prints loopback URLs, holds forwards open until interrupted | + | Unit |
| HST-09 | `connect` endpoints file is token-free | The endpoints document never contains the faucet token | − | Unit |
| HST-10 | `connect` reconnect | A dropped forward is re-established with backoff; deletion of the network (NotFound) ends the loop | + | Unit |
| HST-11 | `connect` cleanup on interrupt | On Ctrl-C the endpoints file is removed and a disconnect message is printed | + | Unit |
| HST-12 | Host env loopback rewrite | `YACD_OGMIOS_URL`/`YACD_KUPO_URL`/`YACD_FAUCET_URL` point at `127.0.0.1:<localport>` preserving each published scheme (Ogmios stays `ws://`) | + | Unit |
| HST-13 | Pod env (`exec`) omits the faucet token | `podEnv` sets identity, ClusterIP chain URLs, and socket path, but never `YACD_FAUCET_TOKEN` | − | Unit |
| HST-14 | Host env includes the faucet token only when present | `YACD_FAUCET_TOKEN` is set for host processes only when a non-empty token is available | + | Unit |
| HST-15 | Identity env always present | `YACD_NETWORK` and `YACD_NAMESPACE` are always set; `YACD_NETWORK_MAGIC` is set when published | + | Unit |

## TOP — Faucet topup CLI

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| TOP-01 | `topup` against a faucet-ready network | Token fetched from the published auth Secret; request POSTed to the published faucet URL; CLI prints the returned txId/source/lovelace/destination | + | Unit |
| TOP-02 | `--address` empty | Exits non-zero: `--address is required`; no request sent | − | Unit |
| TOP-03 | --lovelace ≤ 0 | Exits non-zero with error "--lovelace must be greater than 0"; no request sent. | − | Unit |
| TOP-04 | Network not faucet-ready or stale status (`observedGeneration` behind, missing/stale `Ready`/`FaucetReady`, or `Degraded`) | Refused before sending; token is not read | − | Unit |
| TOP-05 | Network does not publish a faucet endpoint/auth Secret | Refused with a clear error; no empty-target fallback | − | Unit |
| TOP-06 | Default target is the published URL | With no `--faucet-url`, the request goes to the cluster-published faucet URL (no trust gate triggered) | + | Unit |
| TOP-07 | `--faucet-url` to a non-loopback host without `--trust-faucet-url` | Refused; error names the destination host and the auth Secret | − | Unit |
| TOP-08 | `--faucet-url` to a loopback host | Allowed without `--trust-faucet-url` (loopback is exempt) | + | Unit |
| TOP-09 | Trusted custom `http://` URL without `--allow-insecure-faucet-url` | Refused: plaintext token transmission requires the insecure ack | − | Unit |
| TOP-10 | `--faucet-url` that is not a valid absolute http/https URL | Refused with an invalid-URL error | − | Unit |
| TOP-11 | `--await` with confirmation visible via Kupo | Waits, then reports on-chain confirmation; exits 0 | + | Unit |
| TOP-12 | `--await` without a Kupo URL (no flag, no `YACD_KUPO_URL`) | Exits non-zero explaining Kupo is required | − | Unit |
| TOP-13 | `--json` output | Emits machine-readable JSON of the top-up result | + | Unit |

## FCT — Faucet HTTP API

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| FCT-01 | `GET /healthz` | Returns 200 `{"status":"ok"}` | + | Unit |
| FCT-02 | `GET /readyz` when sources are ready | Returns 200 | + | Unit |
| FCT-03 | `GET /readyz` when sources are not ready | Returns 503 with `not_ready` code | − | Unit |
| FCT-04 | `GET /v1/sources` and `GET /v1/sources/{name}` | Return the source list / a single source's public details (including funding address) | + | Unit |
| FCT-05 | `GET /v1/sources/{unknown}` | Returns 404 `source_not_found` | − | Unit |
| FCT-06 | `POST /v1/topups` with a valid bearer token and body | Returns 200 with the submitted transaction result | + | Unit |
| FCT-07 | `POST /v1/topups` without/with a wrong bearer token | Returns 401 `unauthorized` with `WWW-Authenticate: Bearer` | − | Unit |
| FCT-08 | `POST /v1/topups` with a non-POST method | Returns 405 with `Allow` header | − | Unit |
| FCT-09 | `POST /v1/topups` without `Content-Type: application/json` | Returns 415 `unsupported_media_type` | − | Unit |
| FCT-10 | `POST /v1/topups` with a body over the size limit, unknown fields, or multiple JSON values | Returns 400 `invalid_request` | − | Unit |
| FCT-11 | `POST /v1/topups` with `lovelace` omitted | Returns 400 `invalid_request` ("lovelace is required") | − | Unit |
| FCT-12 | Unknown route | Returns 404 `not_found` | − | Unit |
| FCT-13 | Auth token loaded from file (rotated) | The current token file value is honored per request; a missing/empty token yields 500 `internal_error` | + | Unit |
| FCT-14 | Topup engine error mapping | `source_not_found`→404, `source_unavailable`/`chain_unavailable`→503, `invalid_request`→400 | + | Unit |

## FTX — Faucet topup engine

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| FTX-01 | Submit with empty source | The configured default source is used | + | Unit |
| FTX-02 | `lovelace` below min / above max / non-positive | Rejected `invalid_request` naming the bound | − | Unit |
| FTX-03 | Invalid destination testnet address | Rejected `invalid_request` (invalid destination address) | − | Unit |
| FTX-04 | Destination equals source address | Rejected `invalid_request` | − | Unit |
| FTX-05 | Unknown / incomplete source | Rejected `source_not_found` | − | Unit |
| FTX-06 | Successful submission | Returns lowercase-hex txId, source name, source/destination address, lovelace; records spent inputs | + | Unit |
| FTX-07 | Submitter returns empty txId or no spent inputs | Rejected `chain_unavailable` | − | Unit |
| FTX-08 | Concurrent submissions for one source | Serialized per source; earlier-spent inputs are excluded from later submissions (no double-spend of the same UTxO) | + | Unit |
| FTX-09 | Concurrent submissions across different sources | Proceed in parallel (per-source lock only) | + | Unit |
| FTX-10 | Chain submit via Ogmios builds/signs/submits | The transaction is built from the source UTxOs and submitted through the chain client; failures surface as `chain_unavailable` | + | Unit (+E2E) |
| FTX-11 | Source store load/validate | Sources with invalid names or missing key material are rejected with the matching source error code | − | Unit |

## TLS — cardano-tools container

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| TLS-01 | `generate` a localnet artifact environment | Produces a localnet state directory plus a `yacd-localnet-plan.json` manifest with network magic and a fingerprint value | + | E2E (+Unit) |
| TLS-02 | `stage` a generated state directory | Flattens it into a served artifact directory consumable by `serve`/`sync` | + | Unit |
| TLS-03 | `serve` an artifact directory | Serves the files and `manifest.json` read-only over HTTP; non-GET/mutating requests are not served | + | Unit |
| TLS-04 | `sync` from a serve endpoint | Mirrors all manifest files into the output dir, verifying each against the served manifest | + | Unit |
| TLS-05 | `sync`/`fetch` encountering an HTTP redirect | Refused (redirects disabled) so a download cannot be silently moved to another host | − | Unit |
| TLS-06 | `fetch --profile preview/preprod/mainnet` | Downloads the curated profile files and verifies every pinned file against its sha256 digest | + | Unit |
| TLS-07 | A pinned file whose bytes do not match its digest | Fetch fails loudly; the artifact is not staged | − | Unit |
| TLS-08 | `fetch`/`sync --dry-run` | Prints the resolved manifest/file list and downloads nothing | + | Unit |
| TLS-09 | `fetch --profile` unknown | Fails naming the supported profiles | − | Unit |
| TLS-10 | Manifest path / shape validation | A manifest path that is empty, not absolute, or not exactly <envDir>/yacd-localnet-plan.json is rejected; a manifest missing inputs.networkMagic or fingerprint.value is rejected with the corresponding "is required" error. | − | Unit |
| TLS-11 | `version` / `--version` | Prints injected version/commit/date (dev defaults when unset) | + | Unit |

## PIN — Network identity & public pins

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| PIN-01 | Curated profile registry integrity | Each profile (preview, preprod, mainnet) exposes its files in deterministic order with the correct network magic and `RequiresNetworkMagic` | + | Unit |
| PIN-02 | Pinned digests match embedded ground-truth | Each pinned profile digest (config.json + topology.json for all profiles; mainnet adds Mithril genesis.vkey + ancillary.vkey) recomputes to the same sha256 as the bytes embedded under internal/cardano/publicnet/profiles; there is no separate upstream known-good table. Unpinned files (genesis, checkpoints, peer-snapshot) carry no digest. CompatibleNodeRelease=11.0.1 is the recorded baseline. | + | Unit |
| PIN-03 | Operator manifest vs fetch agreement | The fingerprint/manifest the operator builds for a public profile matches what `cardano-tools fetch` verifies for the same profile | + | Unit |
| PIN-04 | Unpinned files are intentionally unpinned | Genesis/checkpoints/peer-snapshot carry no fetch-time pin (authenticated downstream or optional) | + | Unit |
| PIN-05 | Tools-image resolution | cardano-tools image resolves via toolsimage.Reference to ghcr.io/meigma/yacd/cardano-tools:<toolVersion>-yacd.5@sha256:d3283c… unless --default-cardano-tools-image overrides; cardano-testnet image resolves to ghcr.io/meigma/yacd/cardano-testnet:<toolVersion>-yacd.5 (tag, not digest-pinned) unless --default-cardano-testnet-image overrides. | + | Unit |

## CTN — cardano-testnet container

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| CTN-01 | Init produces the expected localnet layout | The init output directory contains the artifacts and funded UTxO sources `stage`/the faucet expect | + | E2E (+Unit) |
| CTN-02 | Funded source addresses are derivable | The faucet source-address init can read the generated source material (e.g. `utxo1`, `utxo2`) | + | E2E (+Unit) |

## HLM — Helm chart correctness

| ID | Scenario | Pass/Fail Criteria | Type | Level |
|----|----------|--------------------|------|------|
| HLM-01 | Manager RBAC vs controller-gen | The chart's `yacd-manager-role` ClusterRole rules equal a freshly generated controller-gen role (no drift) | + | Unit |
| HLM-02 | CRDs install | The chart installs the `CardanoNetwork` and `CardanoDBSync` CRDs | + | Unit |
| HLM-03 | Manager Deployment + metrics Service | A default render produces the controller Deployment and the `…-metrics-service`; metrics-auth/metrics-reader/leader-election RBAC is present | + | Unit |
| HLM-04 | Kyverno image policy is opt-in | A default render does **not** include the `yacd-verify-image` ClusterPolicy | − | Unit |
| HLM-05 | Kyverno policy when enabled | With `kyverno.imageVerification.enabled=true`, the policy enforces GitHub-native attestation for the `ghcr.io/meigma/yacd` and `…/faucet` image references (keyless issuer, subject regex, SLSA provenance build type) | + | Unit |
| HLM-06 | Kyverno image-reference override | `kyverno.imageVerification.imageReferences[*]` overrides the default image references | + | Unit |
| HLM-07 | values.schema.json accepts a valid values file | A valid values document renders without a schema error | + | Unit |
| HLM-08 | values.schema.json rejects invalid values | A values file violating the schema (wrong type / out-of-range / unknown required) fails the render | − | Unit |

## Consolidation & cross-references

Verification surfaced 19 overlap clusters. **Most are *intentional layered
coverage*, not duplication** — the same contract is deliberately asserted at the
operator, CLI, and server layers, and each layer earns its own test. Those are
kept distinct and merely cross-referenced. Only the genuinely redundant cases
were merged/re-scoped (and are already reflected in the tables above).

**Merged / re-scoped (applied above):**

- **CNP-07 → CNV-08.** An unknown public profile is rejected at CRD enum
  admission. CNP-07 is re-framed as the reconcile-boundary note and points at
  CNV-08 as primary coverage.
- **MGR-03 ↔ MGR-04.** Both are "invalid `--log-*` flag fails at Kong parse";
  MGR-03 is re-scoped to the `newControllerLogger` unit guard and cross-refs
  MGR-04 for the parse-time rejection.

**Layered-coverage families (keep distinct, share one canonical contract):**

- **Endpoint URL-form** — canonical **API-08** (`<svc>.<ns>.svc.cluster.local:<port>`,
  correct scheme per service); re-asserted in slices by API-01/API-02 (Ogmios/Kupo
  ws/http), CNL-05 (`tcp://…:3001` nodeToNode), DBF-07 (`postgres://…:5432/cexplorer`),
  HST-13 (pod/exec cluster URLs). Host-side mirror is **HST-12** (loopback rewrite
  `127.0.0.1:<localport>`, scheme preserved).
- **Faucet bearer-token Secret** — produced by the operator (**API-03** create +
  status, API-06 teardown), consumed by the CLI (TOP-01 reads from status, TOP-05
  refuses when absent), validated by the server (FCT-07 auth, FCT-13 rotation), and
  guarded for leaks (HST-09 endpoints.json is token-free, HST-14 host-only token).
  Distinct credential cluster from the **managed-Postgres** auth Secret (DBD-01 owns,
  DBD-02 user-supplied).
- **Network identity / fingerprint** — **CNI-01** records it on first reconcile;
  CNL-06 / CNP-02 publish mode-specific identity; **CNI-02** enforces immutability,
  joined by the unsupported-spec family CNL-07 (genesis tuning rejected) and CNL-09
  (pool count ≠ 1 rejected).
- **Artifacts-over-HTTP, never a ConfigMap** (the F0 invariant) — **CNL-03**
  (network primary Pod) and **DBF-02** (follower `network-artifacts-sync` init).
- **Delete → children GC'd** — CNL-10 (network), DBF-09 (db-sync), CLI-09 (`down`
  waits until gone). All rely on Kubernetes GC, which **envtest does not run** →
  the cascade itself is E2E (ownerRefs can be checked in Env).
- **Pinned-digest integrity** — PIN-02/PIN-04 recompute pins from embedded bytes;
  TLS-06/TLS-07 exercise the same pins at fetch time. PIN-01 (registry shape) and
  PIN-03 (operator manifest == fetch agreement) share the `publicpins` registry.
- **Default-image flag resolution** — MGR-11 (parse + wire both reconcilers),
  PIN-05 (`toolsimage.Reference` / testnet formula), API-07 (faucet override must
  share the configured repository): three layers of one mechanism.
- **DBS-04 ↔ CNP-03** jointly define the mainnet boundary: CNP-03 is the Mithril
  bootstrap that *does* seed mainnet; DBS-04 rejects primary-sidecar db-sync on
  mainnet until a bootstrap/sizing path is proven.
- **CLI-15 ↔ CLI-10** were re-scoped so they don't contradict: CLI-15 covers
  `info`/`topup` not-found → non-zero; CLI-10 keeps `down`'s idempotent
  absent-network success.

## Known coverage gaps

These are sub-cases already implied by an in-scope row but with **no existing
test** today (flagged for the implementer — not new scope):

- **MGR-05** — no direct `/healthz`,`/readyz` 200 assertion at any layer (only
  implicit via Deployment `Available`).
- **CNL-08** — steady-state `Progressing=False` ∧ `Degraded=False` is never
  explicitly asserted (chainsaw only waits `Ready=True`).
- **CLI-03** — the `up` empty-`--file` (`--file is required`) path is untested.
- **DBV-07 / DBV-09** — 5 of 7 DBSync enums (ledger backend, insert preset,
  tx_out mode, ledger mode, json type) have no negative case; the db-sync image /
  `ledgerBackend=lsm` / `preset=full` defaults are unasserted (and only default
  when the parent `config` object is present).
- **HST-06** — `exec`'s require-ready gate is untested at the verb.
- **HST-15** — the network-magic-absent branch is uncovered.
- **FCT-10 / FCT-13** — body-size-limit and multiple-JSON-value `400`, and the
  missing/empty-token `→ 500` path, are uncovered.
- **FTX-07 / FTX-09** — empty-txId / zero-spent-inputs `→ chain_unavailable`, and
  parallel-across-different-sources, are uncovered.
- **TLS-03 / TLS-05 / TLS-10** — `serve` non-GET/405 + `manifest.json` served;
  the `fetch` redirect-refusal half (only `sync` has a redirect test); and the
  `ReadManifest` rejection cases.
- **CTN-01 / CTN-02** and several admission rows (CNV-02/03/04/05/08/09/10/11,
  DBV-05/06) have `none` existing coverage and rely on a real binary or CEL
  admission no current test exercises.

## Verification of this document

A reviewer can confirm the plan hangs together by checking:

1. The file is valid GitHub-flavored Markdown; every category table has the five
   columns (`ID`, `Scenario`, `Pass/Fail Criteria`, `Type`, `Level`).
2. The coverage map references every in-scope source area, each mapped to ≥1
   category.
3. Every category that can fail contains at least one `+` and one `−` row.
4. Each `Pass/Fail Criteria` cell names an observable outcome (condition
   type+status, resource presence/absence, HTTP status, exit code, or error
   intent) — no "works correctly" placeholders.
5. IDs are unique and sequential per prefix.
6. Each `Level` is one of `E2E` / `Env` / `Unit` (with an optional `(+X)`
   secondary), consistent with the taxonomy in *Conventions*.
