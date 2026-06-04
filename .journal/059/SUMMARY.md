---
id: 059
title: CLI-native wallets + faucet removal (P1–P3 shipped)
date: 2026-06-03
status: complete
repos_touched: [yacd]
related_sessions: [009, 053, 058]
---

## Goal
Add managed test wallets to YACD (generate / list / topup / export named wallets).
After analysis the scope deliberately **pivoted** (user-directed) into a larger
re-architecture: **remove the in-cluster faucet service entirely** and make the CLI
own all wallet + funding, with the controller's only wallet job being to surface the
localnet genesis funding key as a well-known `faucet` wallet. Planned as a 5-phase
strangler in `.journal/059/WALLET_REARCH_PLAN.md`; this session executed **P1–P3**.

## Outcome
**Met for P1–P3** — the wallet feature and the faucet-removal foundation shipped,
merged, and were live-proven. **P4 (cut over devnet/dev-wallet funding to the CLI,
then delete the faucet service + `spec.chainAPI.{faucet,wallet}`) and P5 (release +
docs) remain** — the in-cluster faucet service still exists and is still used by the
controller's dev wallet. The CLI now manages wallets and funds them by building,
signing, and submitting transactions directly against Ogmios/Kupo (no faucet HTTP),
spending from the genesis-funded `faucet` wallet.

Five PRs squash-merged, each adversarially reviewed via background workflows and
live-validated on the Kind dev stack; plus a full manual functional test of the
`yacd wallet` surface that found and fixed 4 UX issues before close.

## Key Decisions
- **Generate+genesis-fund, not extract-via-publisher.** The original P2 plan
  (extract create-env's utxo key via an in-pod publisher + a narrow ServiceAccount)
  was rejected after the user asked "why can't the controller generate it?" Verified
  (workflow) that Shelley genesis `initialFunds` funds any address with no node-held
  key, so the controller generates the faucet key and a PVC-only init container funds
  that address — eliminating the publisher SA, re-introduced RBAC, and the original
  reason for a tools-image change.
- **CLI-side Kubernetes Secrets for custody; fund + export only (no server-side
  signing).** Keys live in labeled Secrets (RBAC/encryption/backup for free), the CLI
  signs locally. The faucet collapses into the wallet model as wallet `faucet`.
- **The genesis edit is a tested Go verb, not a shell script.** A live functional
  test would have shipped a `sed`/`grep` init container; the user pushed for a proper
  `cardano-tools fund-genesis` verb (robust JSON, big.Int supply accounting, golden
  decode), at the cost of one cardano-tools release. This forced the P2a/P2b split.
- **Wallet selector = second positional (name | pubkey | bech32 address); `yacd topup`
  folded into `yacd wallet topup`** (faucet = default `--from`).
- **Manager dependency boundary held throughout:** `./cmd` pulls no ogmigo/kugo/
  Apollo-tx-builder; only the CLI gained the chain-tx stack; the new
  `internal/cardano/wallet` helpers stay cbor/json/ed25519-only.

## Changes (merged PRs, in order)
- **#95 (`88b8ba3`)** refactor(faucet): extract the stateless chain-tx engine →
  domain-pure `internal/cardano/tx` (Submitter port + Apollo adapter); faucet keeps
  its orchestration. Pure refactor.
- **#98 (`e6f3c64`)** feat(cardano-tools): `fund-genesis` verb — adds an `initialFunds`
  allocation for a bech32 address (Apollo bech32 decode, big.Int headroom check,
  idempotent, atomic). Released as `cardano-tools 11.0.1-yacd.6` via **#99 (`9477801`)**.
- **#97 (`911f663`)** feat(cardanonetwork): genesis-funded `faucet` wallet — the
  controller generates a faucet payment key once into `<net>-wallet-faucet`, ensured
  before the Deployment build; an init container runs `fund-genesis` (cardano-tools
  yacd.6, digest-pinned in `internal/cardano/toolsimage`) to fund it at genesis.
- **#106 (`0b3a629`)** feat(cli): `yacd wallet {list,add,topup,export,remove}` +
  `cli/internal/wallet` store + name|pubkey|address selector + wordlist + the funding
  path (forward Ogmios/Kupo → decode source Secret envelopes → `tx.Apollo.Submit` →
  confirm via kugo); removed the standalone faucet-HTTP `topup`. Includes the manual
  functional-test fixes (commit `c765310`): route Apollo's stdout genesis-config
  warning to stderr (valid `--json`), DNS-1123 wallet-name validation, friendly
  not-found for export/remove.

## Open Threads
- **P4 (the big breaking PR):** cut `devnet`/dev-wallet funding over to the CLI, remove
  `spec.chainAPI.{faucet,wallet}` + their conditions, delete `services/faucet/` + the
  faucet image + sidecar/Service/auth-Secret, rewrite Chainsaw + examples + release.
  Needs the dev stack for live validation; likely a design pass + may split.
- **P5:** faucet-free operator release + re-render the CLI's embedded chart + docs.
- **ogmigo/Apollo Ogmios client (durable):** Apollo's `OgmiosChainContext.GenesisParams`
  fails its `ogmigo.GenesisConfig("shelley")` ws read (close 1006) every funding and
  `fmt.Printf`s to stdout. Harmless TODAY (the empty `Base.GenesisParameters{}` fallback
  is not read by the fee path — fees come from `LatestEpochParams`, which succeeds — and
  our TTL is slot-based), and now redirected to stderr. It is a latent trap if a future
  tx feature ever needs genesis constants. Root cause is the SundaeSwap ogmigo client on
  the discontinued Gorilla WebSocket toolkit (Kusari-flagged). Fix = move off ogmigo /
  use Ogmios HTTP queries; fold into P4/P5.
- **`spec.chainAPI.wallet` (controller dev wallet)** is untouched and still faucet-HTTP-
  funded; P4 removes/reworks it.
- Pre-existing carried threads (deterministic primary-sidecar manager-envtest refactor,
  TEST_REPORT F2/F4, the `yacd-env` Action + examples/how-to) are unaffected.

## References
- PRs: #95, #98, #99, #97, #106 (commits above). PR #106 carries a comment with the
  full manual functional-test results.
- Plan: `.journal/059/WALLET_REARCH_PLAN.md` (5-phase). Analysis workflows referenced
  in `NOTES.md`: `wf_1c108c99-0f2`, `wf_bb7e8066-c23`, `wf_e7c8259c-a3b`,
  `wf_b2a5ed7c-9c6` (P1), `wf_3c0062be-ad2` (P2a), `wf_ae95f05f-939` (P2b),
  `wf_91bc901b-be8`/`wf_d4c977bd-e2a` (P3 design/impl).
- Prior: `.journal/053/SUMMARY.md` (dev wallet + v0.1.x releases), `.journal/058/`
  (operator-install; this session split out of 058 due to an ID overlap).

## Lessons
- **Shared `origin/master` moves under you in multi-agent sessions.** Worktrees share
  one `.git`; a concurrent agent merging PRs (#94/#96 here) silently staled this branch
  twice — once force-pushed a PR that "reverted" #96 before I caught it on the diff.
  Always re-fetch + rebase + verify `git diff origin/master` lists only your files right
  before any (force-)push. Saved as auto-memory `shared-master-rebase-before-push`.
- **A manual functional test earns its keep.** The `--json`-corrupting Apollo stdout
  warning, the raw-K8s-error invalid-name path, and the leaky/misleading not-found
  messages all passed unit tests + envtest but only surfaced by running the real CLI
  against a live network.
- **gopls in a Worktrunk worktree reports false "BrokenImport"/"undefined"** (the
  worktree isn't in the gopls workspace). Always confirm with a real `go build`/`go test`
  rather than trusting the IDE diagnostics on worktree files.
