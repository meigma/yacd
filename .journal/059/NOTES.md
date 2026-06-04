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

## 2026-06-03 — P2a MERGED; release PR #99 green (paused before merge)
User approved #98; I squash-merged it (`e6f3c64`) with the `Release-As:
cardano-tools@11.0.1-yacd.6` footer (verified present in the merge commit) → master ff'd,
#98 worktree/branch removed. release-please opened **PR #99 `chore(master): release
cardano-tools 11.0.1-yacd.6`** (bumps `.release-please-manifest.json` + cardano-tools
CHANGELOG). **All 15 checks PASS** (ci, e2e, cardano-tools-image, Kusari, + Binary/
Container/cardano-testnet/cardano-tools/Helm dry-runs). Per the user, **PAUSED before
merging #99** — merging it publishes the yacd.6 image. (Note: `gh pr checks --watch`
died once on a transient api.github.com connection error; re-queried — all green.)

**Next:** on the user's go-ahead, merge #99 → yacd.6 publishes → P2b: re-pin the digest
in `internal/cardano/toolsimage`, swap #97's init container to `yacd-cardano-tools
fund-genesis`, re-live-validate, mark #97 ready, merge.

## 2026-06-03 18:45 — P2b DONE: #97 reworked to the verb, live-reproven (ready for review)
User approved merging #99. Squash-merged #99 → release-please tagged
`cardano-tools/v11.0.1-yacd.6`, the "Release cardano-tools Image" workflow published
it. **Published digest: `sha256:02fcb64d0d3e5d63dfa13484068eacbdc7ae34694fdb51cfa733763fbc433188`**
(got via `docker buildx imagetools inspect`).

P2b edits (in #97, `feat/faucet-wallet-secret`): bumped `internal/cardano/toolsimage`
Revision yacd.5→yacd.6 + the new Digest; rewrote `faucetWalletGenesisFundingInitContainer`
to run the cardano-tools image + `fund-genesis --env-dir --address --lovelace` (dropped
the /bin/sh script + the env-var/shell consts); updated 3 tests (init_container_test,
controller_envtest_test helper, builder_test) from env/script asserts → verb-arg asserts.
Build/vet/gofmt clean, root:check ✅, root:test ✅ (cardanonetwork envtest fresh).

**LIVE re-proven on the Kind dev stack**: applied a local faucet network → Ready=True,
Degraded=False; the init container ran the VERB (image `cardano-tools:tilt` = locally-
built same source; log "funded addr_test1vra77… (key 60fbef43…) with 1000000000000
lovelace"); `cardano-cli query utxo` → faucet addr holds **1,000,000 ADA on-chain**.
Published yacd.6 image confirmed to ship the verb. Test network deleted. #97 amended +
marked READY (was draft).

⚠️ **STALE-BASE HAZARD HIT TWICE** (see [[shared-master-rebase-before-push]]): while I
did P2b, the other agent merged **#94→#95... and #96 (`yacd install`)** to the SHARED
origin/master. After my first amend+force-push, `git diff origin/master` showed #96 as
"reverted" (my branch based on the older `9477801`; master had advanced to `5383f76`).
Caught it, `git fetch` + `git rebase origin/master` (disjoint, clean) + force-push →
#97 now 14 files only (cardanonetwork + toolsimage). LESSON: with a concurrent agent
merging to shared master, ALWAYS re-fetch + rebase + verify `git diff origin/master` is
clean immediately before (force-)pushing a PR.

**Status: #97 OPEN, ready, NOT merged — awaiting human review.** Dev stack still up.
**Next:** after #97 merges → P3 (CLI wallet store + verbs + direct submission via
internal/cardano/tx). Re-confirm the 5 open decisions before P3.

## 2026-06-03 19:12 — P2 MERGED (#97); P3 started (design running)
User approved #97; squash-merged (`911f663`). **P2 (genesis-funded faucet wallet) is
fully DONE+merged.** Brought the dev stack DOWN (`root:dev-down` from the #97 worktree,
Kind cluster deleted), removed the #97 worktree/branch, ff'd master, created P3 worktree
`feat/cli-wallet-verbs`.

**P3 decisions (user-confirmed):** wallet selector = **second positional**
(`yacd wallet topup NET WALLET L`), accepting name | pubkey | bech32 address; the
standalone **`yacd topup` is REMOVED and folded into `yacd wallet topup`** (faucet wallet
= default `--from` source). Locked the rest at documented defaults: `add` generates-only
unless `--topup`; ceiling ~50 wallets/network; label `yacd.meigma.io/wallet=<name>`;
`spec.chainAPI.wallet` (dev wallet) removal stays in **P4**.

**P3 scope:** new `yacd wallet {list,add,topup,export,remove}` subtree; wallet store over
labeled K8s Secrets (data payment.skey/vkey/address, ownerRef→network); CLI-side tx
submission via `internal/cardano/tx` (forward Ogmios+Kupo, read source wallet Secret,
**decode the key ENVELOPE → raw hex** for tx.Request, submit, confirm via the existing
kugo path); delete the faucet-HTTP `topup` transport + token trust gate. The CLI gains
Apollo's tx-builder (ogmigo+kugo already present); the MANAGER must stay tx-free.

Launched P3 **design** workflow `wf_91bc901b-be8` (3 agents: CLI/kube surface; tx-funding
path + envelope decode; store/verbs/selector) → file-by-file plan. **Next:** review the
design, then implement (workflow) + live-validate (dev-up from P3 worktree), PR + pause.

## 2026-06-03 20:11 — P3 SHIPPED: CLI wallet verbs + direct tx submission (PR #106, live-proven)
Design (`wf_91bc901b-be8`) nailed the recipe: envelope→raw-hex decode = `wallet.
DecodePaymentKeyEnvelope` (manager-safe, reuses the faucet CBOR recipe); funding =
resolve selector → read source Secret → decode → forwardEndpoints(Ogmios+Kupo) →
tx.Apollo.Submit → reuse awaitConfirmation; store = labeled Secrets matching P2's
wallet-name/wallet-source labels.

Implemented via `wf_d4c977bd-e2a` (7 agents, 2 sequential passes: foundation → verbs):
correctness lens **0 findings**; boundary lens confirmed manager tx-free + seam injected;
tests lens 4 must-fix gaps (--from, readiness gating, partial-failure, export edge cases)
all FIXED. Files: new `cli/internal/wallet` store pkg (store/selector/names + embedded
adjectives.txt/nouns.txt), `cli/internal/cli/wallet*.go` verbs + `wallet_fund.go` helper +
`wallet_await.go` (renamed from topup_await.go), `wallet.DecodePaymentKeyEnvelope` +
golden test, kube.Client Secret ops + regen'd mocks + a `tx.Submitter` mock/seam. DELETED
the standalone topup (topup.go/_trust.go/_transport.go/_test.go + mocks/http_doer.go).

Verified: gofmt/build/vet clean, root:check ✅, root:test ✅ (cli/internal/cli + cli/internal/
wallet + internal/cardano/wallet fresh-pass). **Manager boundary holds**: `go list -deps
./cmd` tx-free; CLI imports tx; wallet pkg manager-safe.

**LIVE-PROVEN on the Kind dev stack** (operator with P2 → faucet wallet): built the CLI,
applied a faucet network (Ready 40s), then `yacd wallet add fw --name alice --topup
1000000000 --await` → the CLI built+signed+submitted a real funding tx from the faucet
wallet (tx `1198bc05…`), **confirmed on-chain**; `cardano-cli query utxo` → alice holds
**1,000,000,000 lovelace**; `wallet list` shows alice (managed-by-cli); `wallet export`
wrote `0600` `.skey`/`.vkey`/`.addr`, the `.skey` a valid `PaymentSigningKeyShelley_ed25519`
envelope. Test network deleted.

**Known non-blocking follow-up:** Apollo chain-context init logs a transient Ogmios
websocket close-1006 during funding (the ogmigo/Gorilla-WS dep the analysis flagged); the
tx still succeeds. Quiet/handle later (e.g. ogmigo.NopLogger or a retry).

Branch `feat/cli-wallet-verbs` (worktree `.wt/feat-cli-wallet-verbs`), commit `27dc75e`,
base current (origin/master still 911f663 — no stale-base this time). **PR #106 OPEN — NOT
merged, awaiting review.** Dev stack LEFT UP (P4 needs it).

**Next:** after #106 merges → **P4** (cut over devnet/dev-wallet funding to the CLI, then
DELETE the in-cluster faucet service + `spec.chainAPI.{faucet,wallet}` + conditions +
Chainsaw/examples/release). The big breaking PR.

## 2026-06-03 21:36 — P3 full MANUAL functional test (live) + 4 UX fixes (pushed to #106)
User asked for a full manual functional test of the new `yacd wallet` commands to be
100% sure before shipping. Drove it directly on the Kind dev stack (live, stateful) over
the whole surface: add (auto/wordlist name, --name, generate-only, --topup --await,
--json, duplicate→reject, reserved-faucet→reject); list (empty/populated, faucet excluded,
--json); topup (by name | pubkey | bech32 address; --from another wallet — **exact balance
math** alice 1B→799999340 after 200M+fee to carol; --await + fire-and-forget; --from
unknown + unknown-dest errors); export (0600, --out, overwrite refusal + --force; **real
cardano-cli derives the same addr from the exported vkey + skey↔vkey round-trips**);
remove (delete, reserved-faucet reject); gating (network-not-found, **faucet-less network →
"not funding-ready"**). All happy paths + selectors + cross-wallet + end-to-end key
usability PASS.

**Found + fixed 4 UX issues (commit `c765310` on #106), each re-validated live:**
1. **🔴 --json broken on funding:** Apollo's `OgmiosChainContext.GenesisParams` does a
   hardcoded `fmt.Printf` to STDOUT on a non-fatal genesis-config fetch hiccup (websocket
   close-1006), corrupting `wallet topup/add --json`. Fix: `redirectStdoutToStderr()` around
   the submit in `wallet_fund.go` (the CLI prints results via the captured
   commandContext.out, so its stdout stays clean; Apollo's noise → stderr). Re-tested:
   `--json` is now valid JSON, warning on stderr.
2. **🟡 invalid wallet name:** `--name 'Bad_Name!'` passed a raw value as a K8s label →
   confusing raw label-validation error. Fix: `store.Create` validates `IsDNS1123Label` up
   front → "invalid wallet name … must be a lowercase DNS-1123 label".
3. **🟢 export missing wallet** leaked the Secret name → now `wallet "X" not found`.
4. **🟢 remove missing wallet** said "Removed" (idempotent but misleading) → now
   `wallet "X" not found` (existence check before delete).
Added tests (store invalid-name, remove not-found, updated remove mock). gofmt/build/vet
clean, root:check ✅, root:test ✅. Test networks deleted.

**Note for P3 follow-up / P4:** the Apollo genesis-config websocket-1006 hiccup is now
silenced off stdout but still a flaky transient (the ogmigo/Gorilla-WS dep). Funding always
succeeds (Apollo falls back to defaults). Lower priority now that --json is clean.

#106 = 2 commits (feature `27dc75e` + fixes `c765310`), **OPEN, NOT merged, awaiting review.**
Dev stack still up.

## 2026-06-04 06:53 — Close
User approved (`SGTM, merge`). Squash-merged **#106 → `0b3a629`**; master fast-forwarded;
the `feat/cli-wallet-verbs` worktree + local/remote branch removed. **All session-059
work (P1–P3) is landed** across #95/#98/#99/#97/#106 (see SUMMARY). The **dev stack was
brought down** (`root:dev-down`; Kind cluster deleted) — no Kind/Tilt/registry left running.

**Hand-off:** the CLI now owns wallet management + funding (direct Ogmios/Kupo txns); the
in-cluster faucet service + `spec.chainAPI.{faucet,wallet}` still exist and are **removed in
P4** (the big breaking PR), followed by **P5** (faucet-free release + embedded-chart re-render
+ docs). Resume from `.journal/059/WALLET_REARCH_PLAN.md` P4 and the SUMMARY "Open Threads".
Carried: the ogmigo/Apollo Ogmios-client replacement (websocket-1006/genesis follow-up).
SUMMARY.md + INDEX (059 → complete) + TECH_NOTES written. Session closed.
