---
title: CLI-native wallets + faucet removal — phased plan
session: 058
date: 2026-06-03
status: proposed (awaiting review)
related_sessions: [009, 053]
analysis_workflows: [wf_1c108c99-0f2 (subsystem survey + design analysis), wf_bb7e8066-c23 (removal verification)]
---

# CLI-native wallets + faucet removal

## Goal

Add managed test wallets to YACD (generate / list / topup / export named
wallets) **and**, as the vehicle for it, remove the in-cluster faucet service
entirely. Funding becomes a host/CLI concern: the CLI builds, signs, and submits
transactions directly against Ogmios/Kupo, spending from wallet keys held in
Kubernetes Secrets. The genesis funding key is surfaced as a well-known `faucet`
wallet so a fresh local network ships with one funded, interactable wallet.

This **supersedes** the standalone managed-wallet design discussed earlier in
session 058 (CLI-side Secrets, fund+export only, CLI-imperative, no faucet
change). The faucet-removal reframing keeps every wallet decision from that
design and additionally deletes the faucet service.

## Target architecture (decisions locked)

- **No in-cluster faucet service.** No faucet sidecar, HTTP API, auth Secret,
  Service, image, or `spec.chainAPI.faucet` API block.
- **All wallet + funding logic is CLI-side.** Keys live in per-wallet Kubernetes
  Secrets; tx build/sign/submit runs on the host via Apollo + forwarded
  Ogmios/Kupo. The CLI does Secret CRUD under the user's own kubeconfig.
- **The controller's only wallet job:** on local networks, surface the genesis
  `utxo1` signing key as a well-known `faucet` wallet Secret (extract-once).
- **`faucet` is wallet #0.** It holds the initial supply; `wallet list` shows it;
  `topup` / `wallet topup` defaults source to `--from faucet`, overridable to any
  wallet (wallet-to-wallet transfer).
- **Custody = per-wallet Secrets** named `<net>-wallet-<name>` (data keys
  `payment.skey` / `payment.vkey` / `address`, same shape as today's dev wallet),
  owned by the CardanoNetwork (cascade-delete on `yacd down`). `faucet` is a
  reserved wallet name.
- **Gate:** pure key management (`add` / `list` / `export` / `remove`) works on
  any network; **funding** (`topup`) requires a funded source wallet (exists on
  local) plus Ogmios + Kupo ready.
- **Signing model:** fund + export keys only. No server-side signing. The CLI
  never sends keys over an HTTP service; it reads them from Secrets and signs
  locally.
- **Sane ceiling:** ~50 wallets/network (soft cap, message points to direct
  node/Ogmios/Kupo for bulk). No `--count` / batch funding in v1.
- **Names:** embedded adjective-noun wordlist; selector accepts name OR
  pubkey/address.
- **`spec.chainAPI.wallet` (the controller-funded dev wallet) is removed.**
  `devnet`/`up` create+fund a default wallet CLI-side instead.
- **Manager stays free of the chain-tx stack.** ogmigo / kugo / Apollo's tx
  builder never enter `./cmd`'s import graph; the controller performs no chain
  side-effects.

## Why (the simplification)

Funding a wallet is just building+signing+submitting a tx — which the faucet
already does with Apollo/Ogmios/Kupo. The faucet service exists only because
something in-cluster held the keys and exposed them behind a token-gated HTTP
API. Moving tx-building to the host and reading the source key from a Secret is
the same code with different inputs, and it **deletes** an entire service +
released image + container + init container + Service + auth Secret + token
rotation + two API blocks (`faucet`, `wallet`) + their conditions/readiness + the
CLI bearer-token trust apparatus. It also moves funding (a side-effecting,
chain-dependent action) out of the reconcile loop, where it never belonged. Net:
a large reduction in moving parts that also delivers the wallet UX.

## Verified facts (grounding; from the two analysis workflows)

1. **Single Go module** `github.com/meigma/yacd` (only `containers/cardano-testnet`
   is separate). The CLI may import root `internal/cardano/wallet` directly (the
   Go `internal/` rule permits it module-wide; the faucet already does). Keygen
   needs **no relocation**. Only the faucet-scoped `services/faucet/internal/topup`
   must move out to be CLI-importable.
2. **No new third-party deps.** The CLI already imports `ogmigo` + `kugo` (for
   `topup --await`). Only Apollo's tx-builder is added — already in-module (faucet
   uses it). The Gorilla-WebSocket (Kusari) concern is unchanged (already present
   via ogmigo in the CLI).
3. **Manager is clean today** (`go list -deps ./cmd` shows no ogmigo/kugo/Apollo
   tx-builder; only Apollo address/key subpackages via `wallet`). Keep it that way:
   the relocated tx engine is a separate package the controller never imports.
4. **Genesis key extraction is clean.** `cardano-testnet create-env` generates the
   genesis utxo keys and the genesis pre-funds their addresses with the initial
   supply (you extract, you cannot inject). `GenesisUTxOSigningKey_ed25519` →
   `PaymentSigningKeyShelley_ed25519` is a JSON `type`-field rename (identical raw
   32-byte ed25519 + CBOR); the faucet already spends these keys directly.
   Address derivation reuses `wallet.DeriveTestnetAddress` (golden-tested =
   cardano-cli).
5. **Transport already exists.** `cli/internal/cli/forward.go:forwardEndpoints`
   already forwards Ogmios + Kupo (not just the faucet).
6. **Full deletion surface is enumerated** (see workflow `wf_bb7e8066-c23`,
   `verify:deletion-surface`).

---

## Phased plan

Strangler sequence — each phase is an independently mergeable PR that keeps CI
(build, unit, envtest, Chainsaw e2e) green and the system working. The faucet
keeps running until Phase 4.

### Phase 1 — Extract the transaction engine (pure refactor, no behavior change)

**Scope:** Relocate the faucet's chain-tx logic into a shared, CLI-importable
package.

- Move `services/faucet/internal/topup/service.go` + `topup/apollo/client.go`
  (+ tests) → `internal/cardano/tx` (engine) and, if useful,
  `internal/cardano/tx/apollo`.
- Restructure inputs so the engine takes a **signing key (bytes/envelope) + dest
  + lovelace + OgmiosURL + KupoURL** — not a disk-backed `sources.Store`. The
  faucet's `sources` package (disk reading) stays in the faucet as a thin adapter
  that reads the key and calls the relocated engine.
- Repoint the faucet to import `internal/cardano/tx`.

**Key files:** `services/faucet/internal/topup/**` → `internal/cardano/tx/**`;
faucet `cli/root.go` wiring.

**Tests:** the moved unit tests pass at the new location; faucet image still
builds; Chainsaw still green.

**Acceptance:** behavior identical; faucet still works end-to-end;
`go list -deps ./cmd` still shows no ogmigo/kugo/Apollo-tx-builder.

**Risks:** low. Pure move. Watch that `internal/cardano/tx` does NOT get pulled
into `./cmd` (it won't unless the controller imports it — it must not).

### Phase 2 — Surface the genesis key as the `faucet` wallet Secret (additive)

**Scope:** The controller exposes the funded genesis `utxo1` key as a well-known
`faucet` wallet Secret, using the existing narrow-SA in-pod publisher pattern.

- Controller creates an owned, empty `<net>-wallet-faucet` Secret shell (ownerRef
  → CardanoNetwork; labels: managed wallet, name=`faucet`, source=genesis), plus a
  `<net>-wallet-publisher` ServiceAccount + Role limited (by `resourceNames`) to
  `get`/`patch` that one Secret — mirroring the existing
  `<net>-artifact-publisher` SA pattern.
- A **wallet-publisher init container** (tools image, projected SA token) reads
  `/state/env/utxo-keys/utxo1/utxo.skey`, rewrites the envelope `type` to
  `PaymentSigningKeyShelley_ed25519`, derives the address (cardano-cli, matching
  the wallet golden test), and patches `payment.skey`/`payment.vkey`/`address`
  into the Secret. Idempotent: skip if already populated (extract-once; never
  overwrite — the address is funded, regeneration strands funds).
- Additive: the faucet service, its source-address init, and the dev wallet all
  keep working.

**Key files:** `internal/controller/cardanonetwork/{init_container,resources,
names,rbac}.go`; new publisher SA/Role manifests (Helm `charts/yacd/templates`).

**Tests:** envtest — the `<net>-wallet-faucet` Secret shell + SA/Role are created
and owned; a unit/golden test that the envelope conversion + derived address are
correct. Live (k3d/Kind): after a fresh local network, the Secret holds a valid
`payment.skey` whose address matches the on-chain funded genesis UTxO.

**Risks:** the extracted address must match the genesis-funded address — golden +
live verify. The narrow publisher SA is a one-shot Secret write (get/patch on one
named Secret), NOT the broad faucet-service RBAC we rejected earlier; note this
distinction in the PR.

### Phase 3 — CLI wallet store + verbs + CLI-side tx submission

**Scope:** The full CLI wallet surface, funding via the relocated tx engine.

- `internal/cardano/wallet`: add a raw-pubkey-hex accessor on `Material` (the
  canonical wallet ID for lookup).
- `cli/internal/kube`: extend the `Client` port with labeled wallet-Secret CRUD
  (create / list / get / delete) + regenerate mocks.
- New verbs (`cli/internal/cli/wallet*.go`, parent `yacd wallet`):
  - `wallet add NET [--name N] [--topup L] [--await]` — keygen (`wallet.New`) →
    create owned Secret → optional fund (persist key first, fund as a separate
    self-healing step keyed on confirmed balance, mirroring the dev-wallet
    `FundedTxID` idempotency). Echo the generated name.
  - `wallet list NET [--json]` — list labeled wallet Secrets.
  - `wallet topup NET WALLET L [--await] [--from WALLET]` — fund a wallet from a
    source wallet (default `faucet`) via the tx engine.
  - `wallet export NET WALLET [--out DIR] [--force]` — read Secret, write `0600`
    `.skey`/`.vkey`/`.addr` under the gitignored `.yacd/<ns>/<net>/wallets/<name>/`
    tree.
  - `wallet remove NET WALLET` — delete the Secret.
- Funding path: `forwardEndpoints` (Ogmios+Kupo) → read source skey from its
  Secret → `internal/cardano/tx` build+sign+submit → confirm via existing kugo
  path. Default source = `faucet`.
- Rework `topup` to use the tx engine too (the faucet HTTP topup path becomes
  unused by the CLI; still present for the controller dev-wallet funder until
  Phase 4).
- Gate, naming, selector: reuse the faucet-ready style gate but reframed as
  "Ogmios+Kupo ready + a funded source wallet exists"; embed an adjective-noun
  wordlist; name-or-pubkey selector disambiguated by shape; soft ~50 ceiling.

**Key files:** `cli/internal/cli/wallet*.go`, `cli/internal/kube/*`,
`cli/internal/cli/{topup,forward,options,root}.go`, `internal/cardano/wallet`,
`cli/internal/mocks/*`.

**Tests:** unit (verbs against mocked `Client` + tx engine seam); a gated
`YACD_WALLET_LIVE` k3d e2e: add → list → topup from `faucet` → export →
cardano-cli reads the exported key → remove. Manager still clean.

**Risks:** `add` is generate+fund and not atomic — report partial success
(created/funding-pending) clearly. Concurrency: two concurrent topups from one
source can double-spend a UTxO; rely on node rejection + retry (acceptable for a
dev tool).

### Phase 4 — Cut over funding + delete the faucet (the breaking PR)

**Scope:** Move all funding to the CLI, delete the faucet service and both API
blocks.

- `devnet`/`up`: create + fund a default wallet CLI-side (e.g.
  `wallet add <default> --topup …` from `faucet`) so the "funded 100k ADA wallet"
  UX survives; surface it in output. Remove the controller-side
  `spec.chainAPI.wallet` funding entirely (`wallet_funding.go`, `WalletStatus`,
  `WalletReady`, dev-wallet Secret apply). The controller now performs no chain
  side-effects.
- Delete the faucet service: `services/faucet/` (the tx engine already moved in
  P1), `Dockerfile`, `.dev/ko-build-faucet.sh`, Tiltfile faucet resource,
  `release.yml` faucet build/release jobs, chart `faucet.image.*` +
  `--default-faucet-image`.
- Delete controller faucet wiring: `faucetContainer`, the faucet source-address
  init container (the wallet-publisher init from P2 remains), faucet Service,
  `<net>-faucet-auth` Secret + rotation + hash annotation + repair requeue +
  `revokePrimaryFaucetExposure`, faucet readiness/status, `FaucetReady`,
  `spec.chainAPI.faucet` + its defaults/validation.
- CLI: remove faucet-auth-token reads + the bearer-token trust gate
  (`topup_trust.go` reduces to a lighter "untrusted custom Ogmios/Kupo URL" check
  or is removed); replace `requireFaucetReady` with the new gate.
- Update Chainsaw: replace the faucet HTTP smoke (curl /v1/topups, 401/400/200)
  with the new model — assert the `faucet` wallet Secret exists, the network is
  Ready, and (optionally) a CLI-driven topup lands on-chain. Remove faucet
  Service/auth assertions.
- Update `examples/*/yacd.yaml` (drop `chainAPI.faucet`/`wallet`), host-access
  docs, and the devnet flow.

**Key files:** broad — see workflow `verify:deletion-surface` for the exhaustive
checklist (api/v1alpha1, internal/controller/cardanonetwork/*, cli/internal/cli/*,
charts/yacd, release.yml, test/chainsaw, examples, Tiltfile).

**Tests:** envtest updated (no faucet/wallet conditions); Chainsaw rewritten;
`moon run root:generate` idempotent after CRD field removal; gated k3d e2e green.

**Risks:** **breaking CRD change** (removing `FaucetSpec`/`WalletSpec`/their
status). Pre-1.0 and devnets are ephemeral, so acceptable; call it out in the
changelog. Ensure no orphaned references (`grep -r faucet`).

### Phase 5 — Release + docs

**Scope:** Ship the faucet-free operator and the wallet CLI; update docs.

- Cut a new operator release (manager + chart, no faucet image) via
  release-please → `release.yml`.
- Re-render the CLI's **embedded operator chart** (`.dev/scripts/
  render-operator-chart.sh`, the `manifests/operator.yaml` SSA bundle) to the new
  digests with the faucet removed; release the CLI.
- Update the docs site / `docs/host-access.md` for the wallet model and the new
  `devnet`/quickstart funding flow. Coordinate with the in-flight docs PR (#91).

**Risks:** the `devnet` embedded-render digest pin must match the new operator
release; this is the same maintenance step noted in sessions 054/055.

---

## Cross-cutting concerns

- **Manager dependency boundary (load-bearing):** keep `internal/cardano/tx` out
  of `./cmd`'s import graph. The controller does no funding. Re-verify with
  `go list -deps ./cmd | grep -E 'apollo|ogmigo|kugo'` (only Apollo address/key
  is allowed, via `wallet`).
- **Extraction SA is narrow, not the rejected broad RBAC:** the `wallet-publisher`
  SA can only `get`/`patch` one named Secret, one-shot at bootstrap — distinct
  from giving a long-lived faucet HTTP service broad Secret RBAC (which we
  rejected). Mirrors the existing artifact-publisher SA.
- **Security posture:** keys in RBAC-gated Secrets (on k3d, kine→SQLite under the
  hood); no token, no HTTP key surface, no export-over-network. `export` writes
  `0600` files in the gitignored `.yacd` tree. Document that wallet keys are
  unencrypted devnet test material, not a secret-management solution.
- **CRD breaking change:** removing `spec.chainAPI.{faucet,wallet}` and their
  status. Pre-1.0; ephemeral devnets; note in changelog. No conversion webhook
  needed for a dev tool.
- **Release coordination:** operator image/chart → embedded CLI render → CLI
  release must land together in Phase 5.

## Open decisions (recommended defaults — confirm during review)

1. **`yacd topup` alias:** keep `topup` as a convenience that funds an
   address-or-wallet from `faucet`, with `wallet topup` as the wallet-keyed form?
   _Default: keep both; `topup` delegates to the same engine._
2. **Genesis sources:** expose only `utxo1` as `faucet`, or all `utxoN` as
   `faucet`, `faucet-2`, …? _Default: only `utxo1` in v1._
3. **`spec.chainAPI.wallet` removal:** confirmed removed (devnet funds CLI-side)?
   _Default: remove._
4. **Wallet-count ceiling number:** _Default: 50._
5. **Name label key:** e.g. `yacd.meigma.io/wallet=<name>` (lowercased,
   DNS-1123) for free selector lookup + display annotation. _Default: as stated._

## Rejected alternatives

- **Faucet-held SQLite/bbolt store on a new PVC** (the original framing): makes
  the faucet stateful (new PVC, writable mount, relaxed hardening, pure-Go driver,
  export-over-HTTP), concentrates key custody, and is the hardest thing to
  reverse. Rejected in favor of Secrets + CLI.
- **Inject a controller-generated key into the genesis:** `create-env` doesn't
  support funding a caller-supplied address. Extraction is the only path.
- **Funding inside the controller via the tx engine:** breaks the manager's clean
  dependency boundary (pulls ogmigo/kugo/Apollo into `./cmd`). Funding stays
  CLI-side.

## References

- Earlier session-058 wallet design + decisions: `.journal/058/NOTES.md`.
- Analysis workflow `wf_1c108c99-0f2` (6-subsystem survey + 5-dimension design
  analysis). Verification workflow `wf_bb7e8066-c23` (modules / genesis-key /
  deletion-surface / CLI-tx feasibility).
- Precedents: today's dev wallet (`internal/controller/cardanonetwork/wallet.go`,
  `wallet_funding.go`; `internal/cardano/wallet`), the artifact-publisher narrow
  SA pattern, the CLI `topup`/`forward`/`connect` host-access machinery.
