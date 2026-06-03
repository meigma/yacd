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
