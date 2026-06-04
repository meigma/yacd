---
id: 061
title: Faucet removal P4–P5 (cut over funding, delete faucet service, release + docs)
started: 2026-06-04
---

## 2026-06-04 07:36 — Kickoff

Goal for the session: continue the CLI-native-wallets / faucet-removal
re-architecture from session 059. **P1–P3 are done and merged** (PRs #95, #98,
#99, #97, #106). This session takes on **P4 (the breaking PR — cut over
funding to the CLI, then delete the faucet service + `spec.chainAPI.{faucet,
wallet}`)** and **P5 (faucet-free operator release + re-render the CLI's
embedded chart + docs)**, per `.journal/059/WALLET_REARCH_PLAN.md`.

Current state of the world:
- After P1–P3 the CLI fully manages wallets and funds them by building/signing/
  submitting txns directly against Ogmios/Kupo, spending from a genesis-funded
  `faucet` wallet (`<net>-wallet-faucet` Secret, generated once by the
  controller; funded at genesis via the `cardano-tools fund-genesis` init step).
- **The in-cluster faucet service still exists and is still used** by the
  controller's dev wallet (`spec.chainAPI.wallet`), which is still faucet-HTTP-
  funded. Nothing in P1–P3 deleted the faucet.
- `master` HEAD is `0b3a629` (PR #106, the `yacd wallet` verbs).
- cardano-tools is at `11.0.1-yacd.6` (genesis-fund verb), digest-pinned in
  `internal/cardano/toolsimage`.
- Operator last released `v0.1.1` (manager + faucet image + chart); the CLI
  embeds a digest-pinned render of `charts/yacd` (session 058 moved this to an
  in-memory render of the in-place `//go:embed`'d chart — `charts/embed.go`).

P4 scope (from the plan, breaking CRD change — pre-1.0, ephemeral devnets):
- `devnet`/`up`: create+fund a default wallet CLI-side so the funded-wallet UX
  survives; remove controller-side `spec.chainAPI.wallet` funding entirely.
- Delete the faucet service (`services/faucet/`, Dockerfile, ko-build-faucet,
  Tiltfile resource, release.yml faucet jobs, chart `faucet.image.*` +
  `--default-faucet-image`).
- Delete controller faucet wiring (faucetContainer, source-address init, faucet
  Service, `<net>-faucet-auth` Secret + rotation/hash/repair +
  `revokePrimaryFaucetExposure`, faucet readiness/status, `FaucetReady`,
  `spec.chainAPI.faucet` + defaults/validation).
- CLI: drop faucet-auth-token reads + the bearer-token trust gate; replace
  `requireFaucetReady` with the new "Ogmios+Kupo ready + funded source" gate.
- Rewrite Chainsaw (drop the faucet HTTP smoke), update examples + docs.
- Fold in the durable ogmigo/Apollo Ogmios-client cleanup if convenient (the
  GenesisParams ws-1006 trap — open thread from 059).

P5 scope: faucet-free operator release (release-please → release.yml),
re-render/re-pin the CLI's embedded chart to the new digests, CLI release, docs
update (coordinate with in-flight docs PR #91).

Plan: read `.journal/059/WALLET_REARCH_PLAN.md` (done) + the session-059 SUMMARY
(done). Next: set up an implementation worktree off fresh `origin/master`,
`moon run root:dev-up`, then design/execute P4 (likely with an adversarial
deletion-surface check before the breaking PR — workflow `wf_bb7e8066-c23`'s
`verify:deletion-surface` is the enumerated checklist). Awaiting the user's
go-ahead before substantive work.

## 2026-06-04 12:16 — P4 design + P4a implemented (awaiting human review)

Plan approved (`/Users/josh/.claude/plans/please-propose-a-plan-optimized-dusk.md`).
Design workflow `wf_6286065c-09e` (survey + 2 designs + adversarial critique) drove
3 locked decisions with the user:
1. **Funded-wallet UX = option B: "the faucet wallet IS the funded wallet."** No
   auto-created default wallet; devnet/info display the genesis-funded `faucet`
   wallet. P4 adds NO new funding code (the P3 `wallet topup --from faucet`
   primitive already exists). Revises the plan's "create+fund a default wallet"
   wording.
2. **Split P4 into PR-4a (cutover) + PR-4b (deletion).**
3. **P4 validated on the dev stack; the faucet-free operator release + embedded
   re-pin that makes `yacd devnet` work end-to-end is P5.** devnet/info wallet
   display MUST degrade gracefully when the faucet wallet Secret is absent (older
   embedded v0.1.1 operator) — now covered by tests.

Implementation worktree: `.wt/refactor-faucet-removal-p4a` (branch
`refactor/faucet-removal-p4a`, off master 0b3a629). dev stack up (own kind cluster).

**PR-4a DONE — commit `6cdb250` (unsigned; see auth note), 28 files +186/−1099.**
- Removed the controller dev wallet entirely: deleted `wallet_funding.go` + the
  dev-wallet half of `wallet.go`; removed `walletSettings`/`resolveWalletSettings`,
  the dev-wallet apply/status/condition wiring across builder/controller/status/
  conditions/delete/names/resources; API dropped `WalletSpec`/`WalletStatus`/
  `spec.chainAPI.wallet`/`WalletReady` (regenerated CRD+deepcopy).
- KEPT the genesis faucet wallet + shared Secret apply core + the whole faucet
  SERVICE (faucet service deletion is PR-4b).
- CLI: `lifecycle.Up` + `devnet` + `info` display the genesis faucet wallet via
  `cli/internal/wallet` `Store.Faucet` (graceful when absent); dropped
  `chainAPI.wallet` from devnet.yaml/init.yaml/examples/local + rewrote prose.
- Chainsaw: asserts the `<net>-wallet-faucet` Secret instead of the dev wallet;
  faucet HTTP service smoke preserved.
- Gates GREEN: `root:check`, `root:test` (envtest+unit), `root:test-e2e` (real
  Kind/Chainsaw, 186s PASS), `go build`, manager dep-boundary (`./cmd` clean of
  ogmigo/kugo/Apollo-tx-builder). Adversarial review `wf_26b14b10-333` (16 agents)
  found NO defects; added 2 graceful-absence coverage tests it suggested.

**Auth note (BOTH agent caches expired mid-session; worked during session-new):**
gpg signing fails (committed `--no-gpg-sign`; GitHub squash-merge signs server-side)
and the GitHub SSH key is not loaded (push blocked). To restore:
`ssh-add --apple-use-keychain ~/.ssh/id_ed25519_macbook` and unlock gpg. Branch is
NOT pushed yet.

Next: PAUSE for human review of PR-4a (per user instruction: pause before each
merge). After approval + auth restore: push branch, open PR, pause again before
merge. Then PR-4b (faucet service deletion + the builder.go re-gate to local-only
+ Chainsaw/RBAC/dbsync-test), then P5 (release).
