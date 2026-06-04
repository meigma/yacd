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
