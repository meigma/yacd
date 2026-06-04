---
id: 059
title: CLI-native wallets + faucet removal — design & plan
started: 2026-06-03
---

## 2026-06-03 13:45 — Session split from 058 (overlap)
This work began under session 058 (via `session-new`) but collided with another
agent also operating in 058 (their `OPERATOR_INSTALL_PROPOSAL.md`). Per the user,
moved to **059**. The two design entries below (13:00 analysis, 13:36 pivot) and
the phased plan (`.journal/059/WALLET_REARCH_PLAN.md`) were carried over from 058
verbatim; 058 was restored to the other agent's kickoff. World-state at start:
`master` at `b611645`, clean; no impl worktree; dev stack not started.

## 2026-06-03 13:00 — Feature design: managed test wallets (analysis + decisions)
Request: add **managed test wallets** to YACD — generate / list / topup / export
named wallets, gated to faucet-enabled local networks. The user's original framing
was "extend the faucet to be a wallet container, keys held by the faucet on a
SQLite DB on the local PVC; CLI list/manage/export; wordlist names; gated off on
public networks." Asked for analysis + recommendation first (no code yet).

Ran an 11-agent analysis workflow (6-subsystem survey + 5 design-dimension
analysis; ~957k tokens). Load-bearing findings:
- The faucet is deliberately **stateless**: read-only root FS, CGO_ENABLED=0,
  distroless/static, nonroot, **NO writable volume**. The only PVC is
  cardano-node's `<net>-node-state`, mounted **read-only** into the faucet (for
  the genesis utxo keys). So "SQLite on the existing PVC" is the *largest* path
  (new dedicated faucet PVC + writable mount + relaxed hardening + pure-Go driver
  — `mattn/go-sqlite3` can't link; even `modernc.org/sqlite` is overkill vs
  bbolt/JSON for a handful), not the simple one. Colocating on the node PVC is
  wrong (inherits `PrimaryStateLost`; node-state loss strands all wallet funds).
- ~80% already exists: the single funded dev wallet (`spec.chainAPI.wallet`,
  session 053) generates an ed25519 key ONCE into a K8s **Secret**
  (`payment.skey`/`payment.vkey`/`address`), funds via faucet `/v1/topups`,
  confirms via Kupo, never regenerates. Keygen + cardano-cli-compatible address
  derivation = `internal/cardano/wallet` (golden-tested, **already imported by
  the faucet**). Multi-wallet is a generalization, not a new subsystem.

**Decisions (user-confirmed):**
- **Signing:** fund + export keys only — no server-side signing → the faucet
  never needs key custody.
- **Custody:** CLI-side **Kubernetes Secrets**; faucet + controller **UNCHANGED**
  (faucet gets **NO ServiceAccount**). The CLI does keygen + Secret CRUD under the
  user's own kubeconfig (same creds it already uses for `GetSecretValue` / apply);
  funds via the existing `/v1/topups`. Avoid the faucet+SA middle ground (broad
  Secret RBAC + export-over-HTTP = blast-radius expansion; cuts against the
  deliberate "no faucet Secret list RBAC" posture + DESIGN.md "not a general
  wallet platform").
- **Seed wallets:** CLI-imperative only for v1; declarative
  `spec.chainAPI.faucet.seedWallets[]` (controller-owned, generate-once) deferred.
- **Scale:** NOT a driver. Secrets handle low-hundreds fine (on k3d, Secrets are
  *already* kine→SQLite); the real bottleneck at scale is funding throughput
  (N on-chain txs, serialized per funding UTxO) — identical in both designs. For
  >100s, users go direct to the node/Ogmios/Kupo. v1 gets a **sane soft ceiling**
  (~dozens) with a message pointing to direct access; **no `--count` / no batch
  funding in v1**.
- **Lifecycle (my default, unobjected):** wallet Secrets owned by the
  CardanoNetwork (ownerRef) → cascade-delete on `yacd down` (ephemeral devnet;
  funds vanish on teardown anyway).

**v1 surface (all CLI; no faucet/controller/CRD change; no new third-party deps):**
- `yacd wallet add NET [--name N] [--topup L] [--await]` → generate via
  `internal/cardano/wallet` → create labeled Secret (ownerRef→network) → optional
  fund via `/v1/topups`, self-heal on confirm (mirror dev-wallet FundedTxID).
- `yacd wallet list NET [--json]` → list Secrets by label.
- `yacd wallet topup NET WALLET L [--await]` → today's `topup` keyed by selector.
- `yacd wallet export NET WALLET [--out DIR] [--force]` → read Secret → write
  `0600` `.skey`/`.vkey`/`.addr` in the gitignored `.yacd/<ns>/<net>/wallets/...`.
- `yacd wallet remove NET WALLET` → delete the Secret.
- Reuse `requireFaucetReady` (public-network gate falls out free); self-forward
  transport like `topup` (no `yacd run` wrapper). Add a tiny pubkey-hex accessor
  to `internal/cardano/wallet.Material`. New `kube.Client` methods: create / list
  / delete labeled wallet Secrets. Embed an adjective-noun wordlist for names.

**Open design sub-decisions (recommended defaults):** wallet selector = second
positional (`yacd wallet topup NET WALLET L`); name stored as a (lowercased,
DNS-1123) label for free selector lookup + annotation for display, so no separate
index is needed; clearly distinguish the new managed wallets from the existing
controller `status.wallet` dev wallet; pick the exact soft-ceiling number.

**Next:** (pending user OK) write a short design doc + phased plan to
`.journal/059/`, then implement on a fresh impl worktree (start dev stack then).
Analysis artifacts: workflow run `wf_1c108c99-0f2`.

## 2026-06-03 13:36 — Pivot: remove the faucet service entirely (CLI-native wallets)
User proposed a larger refactor that SUPERSEDES the standalone wallet design:
**remove the in-cluster faucet service altogether**, make the CLI own all
wallet+funding (build/sign/submit txns directly via Apollo + forwarded
Ogmios/Kupo, keys read from Secrets), and have the controller's only wallet job
be exposing the genesis `utxo1` key as a well-known `faucet` wallet Secret.
`topup` defaults source `--from faucet`, overridable to any wallet.

Ran a 2nd verification workflow (`wf_bb7e8066-c23`, 4 Explore agents). Verified:
- **Single Go module**; the CLI can already import root `internal/cardano/wallet`
  (one agent wrongly said it couldn't — the Go `internal/` rule permits it
  module-wide; the faucet does it). Only the faucet-scoped
  `services/faucet/internal/topup` must relocate → `internal/cardano/tx` to be
  CLI-importable. Keygen needs no move.
- **No new third-party deps**: CLI already has ogmigo+kugo (topup --await); only
  Apollo's tx-builder is added, already in-module. Gorilla-WS (Kusari) concern
  unchanged. Manager stays clean (`go list -deps ./cmd` has no ogmigo/kugo/
  Apollo-tx) as long as the controller never funds + `tx` stays out of ./cmd.
- **Genesis key extraction is clean**: create-env generates+funds the genesis
  utxo keys (can't inject, must extract); `GenesisUTxOSigningKey_ed25519` →
  `PaymentSigningKeyShelley_ed25519` is a JSON type-field rename (same raw
  ed25519+CBOR; faucet already spends these). Use the existing narrow-SA in-pod
  publisher pattern (mirror `<net>-artifact-publisher`) to patch the key into a
  controller-owned `<net>-wallet-faucet` Secret.
- CLI already forwards Ogmios+Kupo (`forwardEndpoints`); deletion surface fully
  enumerated (faucet service+image+jobs, sidecar+init+Service+auth Secret+rotation,
  `spec.chainAPI.{faucet,wallet}`, FaucetReady/WalletReady, topup_trust token gate,
  Chainsaw, examples, docs).

**Verdict (mine, user-aligned):** worth doing — it's a net REDUCTION (delete a
service+image+2 API blocks) that also ships the wallet UX, and moves funding out
of the reconcile loop.

**Plan written:** `.journal/059/WALLET_REARCH_PLAN.md` — 5 phases (strangler,
each PR green): P1 extract tx engine → `internal/cardano/tx`; P2 controller
surfaces the `faucet` wallet Secret (additive, narrow-SA init publisher); P3 CLI
wallet store+verbs+direct submission; P4 cut over funding + delete the faucet
(breaking CRD change); P5 release + docs. Open decisions captured with defaults
(topup alias, utxo1-only, remove spec.chainAPI.wallet, ceiling=50, name label).

**STATUS: paused for user review of the plan.** No code yet; no impl worktree;
dev stack not started.

## 2026-06-03 15:05 — Phase 1 shipped: extract chain-tx engine (PR #95, awaiting review)
User approved executing the plan via per-phase workflows. Agreed cadence: fresh
worktree off origin/master → workflow (implement → 3-lens adversarial review →
fix → build/vet gate) → I re-verify + open PR → **PAUSE for human review before
any merge** → after merge, rebase next phase on new master.

**Phase 1 (pure refactor, no behavior change):** extracted the faucet's stateless
chain-tx engine → new domain-pure `internal/cardano/tx` (`Submitter` port + `Apollo`
adapter + `Request`/`Result`/`Error` + `doc.go`, mirrors localnet/dbsync). Faucet
keeps its orchestration (bounds/locks/pending/sources.Store), adapts
`sources.FundingSource` → `tx.Request`; `mapChainError` preserves `topup.Error`
codes (HTTP behavior unchanged). Deleted faucet `topup/apollo` (git rename).

Workflow `wf_b2a5ed7c-9c6` (6 agents): **0 must-fix** across regression /
hexagonal+go-style / tests+boundary lenses; 1 minor fixed (doc the test-injection
field), 1 deferred (`tx` coverage 53.9% — validation covered, rest is the live-
Ogmios adapter). I re-verified on-branch: gofmt/build/vet clean, `root:check` ✅,
`root:test` ✅ (incl. controller envtests), boundary holds (`go list -deps ./cmd`
free of ogmigo/kugo/tx; faucet imports tx).

Branch `refactor/cardano-tx-engine` (worktree `.wt/refactor-cardano-tx-engine`),
commit `0ab7611`, **PR #95 open — NOT merged, awaiting human review.** No dev stack
started (pure refactor; tests cover it).

Open-decision defaults carried (re-confirm before P3): keep `topup` alias,
`utxo1`-only as `faucet`, remove `spec.chainAPI.wallet`, ceiling 50, label
`yacd.meigma.io/wallet=<name>`.

**Next:** after PR #95 merges → P2 (controller surfaces the genesis key as the
`faucet` wallet Secret), rebased on the new master.

## 2026-06-03 15:41 — P1 merged; P2 mechanism PIVOT (generate + genesis-fund)
PR #95 squash-merged (`88b8ba3`); master ff'd; P1 worktree/branch cleaned up.
Created P2 worktree `.wt/feat-faucet-wallet-secret` off master.

**P2 mechanism research (2 workflows):**
- `wf_81bd5cd7-940` (4 agents): the deleted artifact-publisher SA pattern (session
  048) is the only way to write a Secret from in-pod; re-adding it needs NEW manager
  RBAC on serviceaccounts/roles/rolebindings + a new cardano-tools API-writing verb
  + image release. Also confirmed: the distroless node container has no shell/cat/tar,
  so CLI-side host extraction of the genesis key is BLOCKED.
- User pushed back: "why can't the controller generate it and push to a secret before
  starting the node?" → ran `wf_e7c8259c-a3b` (2 agents, repo+web). VERDICT: **the
  controller CAN.** `create-env` has no fund-a-supplied-address flag, BUT Shelley
  genesis `initialFunds` funds ANY arbitrary enterprise address (no key needed by the
  node); editing it requires recomputing `ShelleyGenesisHash` (cardano-cli genesis
  hash — `cardano-tools` already does this enrichment via EnrichGenesisHashes).

**ADOPTED (cleaner) P2:** controller GENERATES the faucet key (`wallet.New`, once) +
writes `<net>-wallet-faucet` Secret directly (existing RBAC, mirrors dev wallet); a
PVC-only init container funds that address into the local genesis `initialFunds` +
rehashes. **No publisher SA, no new RBAC, no API-writing verb.** Plan doc P2 section
rewritten accordingly. Tradeoff: complexity moves from K8s RBAC → Cardano genesis
surgery (supply accounting, hash, read-path) which is **live-only provable → dev
stack required for P2.** Recipe details to nail first: repoint an existing
initialFunds utxo entry to our addr (no supply math) vs add; shell+jq vs a
cardano-tools genesis-fund verb (release only if jq absent); confirm node genesis
read path.

**Next:** launch P2 implement workflow (nail recipe → controller+init+genesis-edit →
adversarial review → fix), then I live-validate on the dev stack, then PR + pause.

## 2026-06-03 16:29 — P2 SHIPPED: genesis-funded faucet wallet (PR #97, live-proven, awaiting review)
Empirically nailed the genesis recipe by running the REAL `cardano-testnet:11.0.1-yacd.5`
image via docker create-env: `initialFunds` is a POPULATED map of hexAddr→lovelace
(6×15M ADA; maxLovelaceSupply 100M → ~10M headroom); the local node config carries
**NO ShelleyGenesisHash** (so NO hash recompute needed); a wallet's bech32 address
**bech32-decodes exactly to its initialFunds key**; the image has sh/bech32/sed but
**no jq**. So the mechanism is even simpler than the P2 plan said: **no RBAC, no jq, no
hash recompute, no cardano-tools release.**

Implemented via workflow `wf_ae95f05f-939` (implement → 3 adversarial lenses → fix →
gate): 1 blocker fixed (envtest now asserts faucet-wallet Secret deletion on disable),
rest minor nits. Design: controller GENERATES the faucet key (`wallet.New`, once,
shared `applyWalletSecret` core with the dev wallet) + writes `<net>-wallet-faucet`
Secret directly, **ensured BEFORE Build** so the address is injected into a new shell
init container (`faucet-wallet-genesis-funding`, existing cardano-testnet image) that
bech32-decodes the addr and **ADDS** an initialFunds entry (additive, default
1,000,000 ADA = `defaultFaucetWalletFundingLovelace`, fits headroom; existing utxo
sources untouched). Init order: create-env → faucet-wallet-genesis-funding →
served-artifacts → faucet-source-addresses. Gated local+faucet-enabled.

**Caught a stale-base issue:** PR #94 (other agent's operator-render) had merged to
master (`ded61fa`); my P2 worktree was branched off the pre-#94 `88b8ba3`, so `git diff
master` showed #94 as "reverted". My actual changes were disjoint (13 cardanonetwork
files only) → committed + `git rebase origin/master` (clean), re-verified green.

Gates (rebased, fresh): build/vet/gofmt/check clean, `root:test` PASS with
**cardanonetwork envtest fresh 26s**. Boundary intact.

**LIVE-PROVEN on the Kind dev stack** (real published tools image): applied a local
faucet `CardanoNetwork` → **Ready=True, Degraded=False, FaucetReady=True**; init log
"Funded faucet wallet 60dd1f87… with 1000000000000 lovelace"; `cardano-cli query utxo`
showed the faucet addr `addr_test1vrw3lpl…` holding **1,000,000 ADA on-chain at
genesis**. Test network deleted.

Branch `feat/faucet-wallet-secret` (worktree `.wt/feat-faucet-wallet-secret`), commit
`e6f6687`, **PR #97 open — NOT merged, awaiting human review.** Dev stack LEFT RUNNING
(review pause; useful for P3/P4).

**Next:** after PR #97 merges → P3 (CLI wallet store + verbs + direct submission via
internal/cardano/tx), rebased on new master. Re-confirm the 5 open decisions before P3.

## 2026-06-03 17:43 — P2 SPLIT into P2a/P2b (Go verb replaces the shell script)
User reviewed #97 and asked: put the genesis edit in a proper Go binary, not a fragile
sed/grep shell script blasted into init args. Agreed — it's more robust + unit-testable.
Consequence: the verb must be PUBLISHED before the controller can pin it (CI e2e pulls
the published image), so P2 splits:
- **P2a (PR #98, OPEN):** new `cardano-tools fund-genesis --env-dir --address --lovelace`
  verb. Decodes bech32 → initialFunds hex key via Apollo `DecodeAddress` (3 golden
  vectors locked, incl. the live `60dd1f87…` one); parses genesis via json.Number→
  `big.Int` (lovelace > 2^53 — no float64 loss, tested at 2^53+1); validates
  sum(initialFunds)+lovelace ≤ maxLovelaceSupply; idempotent; atomic write preserving
  all other fields. Workflow `wf_3c0062be-ad2` (6 agents): correctness lens 0 findings;
  5 must-fix applied (stale doc.go, 2^53 test, empty-addr, writeAtomic error paths,
  decodeInteger); I fixed a `copyloopvar` lint nit. Scoped to containers/cardano-tools/
  only. Verified: root:check ✅, fresh cardano-tools tests ✅, **and run against a REAL
  create-env genesis** (decode→correct key, 6→7 initialFunds, maxLovelaceSupply + other
  fields preserved, idempotent re-run). **#98 must squash-merge with footer
  `Release-As: cardano-tools@11.0.1-yacd.6`** so release-please cuts the image.
- **P2b (PR #97, now DRAFT):** after #98 + the yacd.6 release land, rework #97 — re-pin
  the cardano-tools digest in `internal/cardano/toolsimage`, swap the genesis-funding
  init container from `/bin/sh`+script to `yacd-cardano-tools fund-genesis` on the
  cardano-tools image, re-live-validate on the dev stack. #97's controller/Secret/
  ordering code is unchanged; only the init-container builder swaps. Marked #97 draft +
  commented so the shell version isn't merged.

Merge order for the human: (1) squash #98 with the Release-As footer → (2) merge the
release-please cardano-tools PR (publishes yacd.6) → (3) I rework + re-validate #97 →
(4) merge #97. Dev stack still up (Kind) for P2b.

**Next:** await #98 review/merge + the yacd.6 release, then do P2b (#97 rework).
