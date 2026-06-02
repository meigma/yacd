---
id: 053
title: Local lifecycle Phases 1 + 4 — operator v0.1.0 release + funded developer wallet (v0.1.1)
date: 2026-06-02
status: complete
repos_touched: [yacd]
related_sessions: [049, 050]
---

## Goal
Execute the session-049 `LOCAL_LIFECYCLE_PLAN.md`, starting with **Phase 1** (cut
the operator's first published release) and then **Phase 4** (a pre-funded
developer wallet bootstrapped by the operator, shipped in the next release). Both
are prerequisites for the `yacd devnet` CLI work (P5/P6).

## Outcome
**Met.** Two real, published, attested operator releases were cut through the
release-please → `release.yml` pipeline, each merged behind an explicit
user "go" gate (the merge is the irreversible GHCR publish):

- **Phase 1 → operator `v0.1.0`** (PRs #83 redirect + #7 release). The pipeline
  had never actually run a root `v*` release before; the first real run was green
  across all 10 jobs. Published-chart smoke (fresh cluster, no overrides) brought
  the operator Available and a local `CardanoNetwork` to Ready.
- **Phase 4 → operator `v0.1.1`** (PRs #84 feature + #85 release). Local
  `CardanoNetwork`s gained an opt-in funded developer wallet. Validated end to end:
  unit + manager-backed envtest, a green Linux Chainsaw e2e, and a published-chart
  smoke where the wallet address held exactly **100,000 ADA** (1 UTxO) on-chain
  from the real faucet-funded path.

Both GitHub **draft releases (`v0.1.0`, `v0.1.1`) are intentionally left
unpublished** for the user to Publish (the images + chart are already public in
GHCR — only the GitHub release page is draft).

## Key Decisions
- **First release is `v0.1.0`, not `1.0.0`.** PR #7 was computed at 1.0.0 by a
  merged `Release-As: yacd@1.0.0` footer; redirected via a newer scoped
  `Release-As: yacd@0.1.0` (the latest applicable footer wins). User wanted to
  stay pre-1.0.
- **Wallet release is `0.1.1`, not the plan's "0.2.0".** The repo's
  `release-please-config.json` sets `bump-patch-for-minor-pre-major: true`, so a
  `feat` bumps the PATCH digit pre-1.0. User chose to accept the natural `0.1.1`
  rather than forcing 0.2.0 with a `Release-As`. **`0.1.1` is the version P5 will
  embed** (supersedes the plan's "v0.2.0" naming everywhere).
- **Operator generates + faucet-funds the wallet.** `cardano-testnet create-env`
  has no `--initial-funds` (verified against the published binary), and exposing a
  genesis key would revive the in-pod→API key-writer PR-B2 removed. So the operator
  generates the key (user-owned, stored once in an owned `<net>-wallet` Secret),
  publishes the address in `status.wallet`, funds it via the existing faucet
  `/v1/topups`, and confirms on-chain via Kupo (balance = source of truth).
- **`WalletReady` gates aggregate `Ready`; default funding 100,000 ADA** (user
  choices). The example faucet `maxTopUpLovelace` was raised to fit.
- **Transient funding errors retry as pending; only a faucet 4xx rejection
  degrades.** Initial e2e bricked because the controller funded the instant
  `FaucetReady` flipped True (pod not yet a live Service endpoint → connection
  refused → Degraded → `up --wait` gave up). Connectivity errors are now retried.
- **Single source of address derivation.** New `internal/cardano/wallet` package
  reuses the faucet's Apollo derivation (lifted there; `sources.go` calls it),
  golden-tested against a real `cardano-cli address build` vector. Manager pulls
  **no** ogmigo/Gorilla-WebSocket/kugo — confirmed via `go list -deps ./cmd`.

## Changes
Phase 1 (release plumbing only):
- `.github/workflows/release.yml` — one-line header note carrying the scoped
  `Release-As: yacd@0.1.0` redirect (PR #83). release-please then set the manifest
  + `charts/yacd/Chart.yaml` to 0.1.0.

Phase 4 (PR #84, squash `0103d6c`):
- `internal/cardano/wallet/{doc,wallet,wallet_test}.go` — new pure pkg: ed25519
  keygen → cardano-cli text envelopes + `addr_test` derivation + golden test.
- `api/v1alpha1/cardanonetwork_types.go` — `spec.chainAPI.wallet{enabled,
  fundingLovelace}`, `status.wallet{address,keySecretName,funded,fundedTxID}`,
  `WalletReady` condition (+ regenerated CRD + deepcopy).
- `internal/controller/cardanonetwork/` — `wallet.go` (create-once Secret +
  funding orchestration), `wallet_funding.go` (HTTP faucet funder + Kupo confirmer
  seams + 4xx-rejection classification), plus wiring in `builder.go`,
  `conditions.go`, `controller.go`, `status.go`, `delete.go`, `names.go`,
  `resources.go`, and tests (`wallet_test.go`, extended `controller_envtest_test.go`).
- `services/faucet/internal/sources/sources.go` — derivation delegates to the
  shared wallet pkg.
- `examples/local/yacd.yaml` — wallet enabled (100k ADA) + faucet max raised.
- `test/chainsaw/manager-smoke/chainsaw-test.yaml` — wallet Secret + funded +
  WalletReady assertions; disable phase also disables the wallet.

## Open Threads — NEXT STEPS for the next agent
- **Publish the two draft GitHub releases** (`v0.1.0`, `v0.1.1`) when ready — user
  decision; artifacts are already live in GHCR.
- **Phase 5 (next): operator install via SSA**, embedding the chart at **`0.1.1`**
  (NOT 0.2.0). Build-time render of `charts/yacd` → `//go:embed` → server-side
  apply (CRDs-first, label prune, version reconcile). Ports `operator` +
  `operator/ssa`. Depends on P1+P4 (both done). See `.journal/049/
  LOCAL_LIFECYCLE_{PLAN,DESIGN}.md` §7 / Phase 5.
- **P2 (toolbin) and P3 (cluster+clusterstate) are independent** and can proceed in
  parallel (P3 needs P2). P3→P6, P5→P6.
- Published refs to pin against:
  - manager `ghcr.io/meigma/yacd:v0.1.1@sha256:5d53ca824dacad39c482dc93edfd2db4a65d5803f43dce5b18b1a7482b0f8e21`
  - faucet `ghcr.io/meigma/yacd/faucet:v0.1.1@sha256:826f8d52f0a4b0f607e2293cf72a8217de27700b5e5f1b35e1af86ef18fd3f66`
  - chart `oci://ghcr.io/meigma/yacd/chart:0.1.1@sha256:a8d24dfaa19a4af0279ed26654ff36a44e5cf50a05a5e0ffa02481688a5a049f`
- **Wallet residual risk (low, dev-only):** a status-patch failure immediately
  after a successful faucet submit could resubmit and double-fund (extra UTxO).
  Acceptable for local devnets; a submitted-at marker would close it if it matters.
- Carried from prior sessions: deterministic primary-sidecar manager-envtest
  refactor; TEST_REPORT F2/F4; test-harness Phases 3 (release) is now effectively
  served by the operator releases but the dedicated `yacd-env` Action (P-harness 4)
  + examples/how-to (5) remain.

## References
- PRs: #83 (v0.1.0 redirect), #7 (release 0.1.0), #84 (funded wallet feat,
  `0103d6c`), #85 (release 0.1.1, `8c388cd`).
- Plan/design: `.journal/049/LOCAL_LIFECYCLE_PLAN.md`,
  `.journal/049/LOCAL_LIFECYCLE_DESIGN.md`. Prior: `.journal/050/SUMMARY.md`
  (Release-As scoping lesson), `.journal/049/SUMMARY.md` (design).

## Lessons
- **release-please pre-1.0 bumps are PATCH-level** here
  (`bump-patch-for-minor-pre-major: true`): a `feat` goes 0.1.0→0.1.1, not 0.2.0.
  Don't assume minor bumps pre-1.0; check the config (or override with a scoped
  `Release-As`).
- **Gating `Ready` on a runtime side effect (faucet funding) needs transient-error
  tolerance.** Funding the instant a sidecar's container is ready races the Service
  endpoint becoming live (the pod isn't Ready until ALL containers are) →
  connection refused. Retry transient/connectivity errors as progressing; reserve
  Degraded for definitive rejections, or `up --wait` bricks on a startup race.
- **A wallet/example that hard-requires a sibling feature breaks that feature's
  disable test.** Enabling the wallet (requires faucet+kupo) made the e2e's
  disable-faucet phase trip validation; the disable path had to disable all three
  together.
