---
id: 061
title: Faucet removal P4–P5 (cut over funding to CLI, delete faucet service, release + docs)
date: 2026-06-04
status: complete
repos_touched: [yacd]
related_sessions: [059, 060]
---

## Goal

Continue the session-059 faucet-removal re-architecture (P1–P3 already shipped):
implement **P4** — the breaking change that removes the in-cluster faucet HTTP
service and cuts funding to the host/CLI — and **P5** — release the faucet-free,
genesis-wallet operator and make the released CLI install it, plus docs.

## Outcome

**Goal met for P4 + the P5 release; P5 docs deferred (user's call).**

- **P4 done** (two PRs, split deliberately): the in-cluster faucet **service** is
  gone, and the genesis-funded `faucet` **wallet** is re-gated to local-mode
  alone.
  - PR-4a #107: removed the controller dev wallet (`spec.chainAPI.wallet`,
    `status.wallet`, `WalletReady`, the faucet-HTTP funding path); devnet/info now
    surface the genesis `faucet` wallet via `cli/internal/wallet` `Store.Faucet`.
  - PR-4b #108: deleted `services/faucet/`, the faucet image (release.yml jobs,
    Tiltfile, chart `faucet.image` + kyverno + `--default-faucet-image`), all
    controller faucet-service wiring (`faucet_auth*.go`, faucet container/Service/
    auth-Secret, `FaucetReady`, readiness/status/conditions/defaults), the API
    `FaucetSpec`/`FaucetStatus`/endpoint, and the CLI faucet trust-gate residue.
    **Re-gate:** `faucetWalletEnabled`/`resolveFaucetWalletSettings` now gate the
    genesis wallet on `Spec.Mode == Local` alone (dropped `faucet.enabled`).
- **P5 release done:** released **v0.2.0** (operator image `ghcr.io/meigma/yacd:v0.2.0`
  multi-arch + OCI `chart:0.2.0` + CLI binaries) via PR #109 (the pin change) and
  the release-please PR #87.
- **Validated end-to-end** with the *released* CLI v0.2.0 binary on k3d: `yacd
  devnet` installed **operator v0.2.0** (Deployment image confirmed `:v0.2.0`),
  the `<net>-wallet-faucet` Secret came up funded (devnet printed the address),
  and `yacd wallet add devnet --name alice --topup 5000000 --from faucet --await`
  **confirmed on-chain** (tx `8eb5e791…`). Cleaned up the cluster.
- **P5 docs deferred** to a later session (README.md, DESIGN.md, and the user's
  MkDocs PR #91 still carry the old faucet/`yacd topup` model).

## Key Decisions

- **Tag/appVersion pinning over digest pinning** (P5). Operator + CLI are ONE
  coupled release-please root component, so the v0.2.0 CLI can't digest-pin its
  own v0.2.0 operator (the digest doesn't exist until that release builds). The
  default install now resolves the manager image to the chart's appVersion
  (`repository:appVersion`); operator+CLI release together so the tag always
  resolves to the matching published image. (`--set image.digest` still pins;
  `--set image.tag` now repoints instead of being shadowed.)
- **Split P4 into 4a (cutover) then 4b (deletion)** so CI stayed green at each
  step and the highest-blast-radius edit (the wallet re-gate) co-located with the
  faucet-spec deletion in 4b.
- **The faucet wallet IS the funded wallet** — no separate auto-created dev
  wallet; devnet/info display the genesis `faucet` wallet, excluded from `wallet
  list`. P4 added no new funding code (topup primitive already existed from P3).
- **Merged the v0.2.0 release PR #87 as-is** even though release-please didn't
  refresh its changelog after #109 (the tag-pin refactor line is omitted from the
  v0.2.0 notes); #109's code is in v0.2.0 regardless since it's on master.
- **Each implementation PR adversarially reviewed** (multi-agent workflows) and
  live/CI-gated before merge; the user reviewed and approved each merge.

## Changes

- `internal/controller/cardanonetwork/*` + `api/v1alpha1/cardanonetwork_types.go`:
  removed dev wallet (4a) and the entire faucet service + `spec.chainAPI.faucet`
  (4b); re-gated the genesis faucet wallet to local mode.
- `services/faucet/` (whole tree), `.dev/ko-build-faucet.sh`, faucet jobs in
  `.github/workflows/release.yml`, chart faucet plumbing: deleted.
- `cli/internal/operator/values.go`: dropped `defaultManagerDigest`; `Default()`
  sets only the repository → chart renders `repository:appVersion` (#109).
- `cli/internal/{cli,operator,wallet}/*`: faucet trust-gate residue removed;
  devnet/info surface the faucet wallet; render/values/install tests rewritten
  for tag pinning.
- Released **v0.2.0** (root component: operator + CLI + chart).

## Open Threads

- **P5 docs (deferred):** rewrite `README.md` + `DESIGN.md` faucet/`yacd topup
  --address`/`--faucet-url`/trust-gate/dev-wallet prose to the genesis-faucet-wallet
  + `yacd wallet` model; rebase the user's MkDocs **PR #91** on v0.2.0 and rewrite
  its stale pages (`developer/funding.md`, `developer/connecting-tools.md`,
  `reference/cli.md`, `reference/cardanonetwork.md`, `host-access.md`). Bundle the
  `proto-*-install.log` → `.gitignore` chore there.
- **ogmigo ws-1006** non-fatal genesis-config warning during wallet funding —
  filed as **issue #110** (move off ogmigo / suppress at source).
- `cardano-tools` / `cardano-testnet` GitHub releases are still **drafts** (images
  already in GHCR) — publish if desired.

## References

- PRs: #107 (4a), #108 (4b), #109 (tag-pin), #87 (release v0.2.0); deferred docs
  PR #91; follow-up issue #110.
- Release: `v0.2.0` (`aecf7ef`), `ghcr.io/meigma/yacd:v0.2.0`, `chart:0.2.0`.
- Prior: `.journal/059/SUMMARY.md` (P1–P3). Concurrent: session 060 (#111/#112,
  also synced operator-install version tripwires to v0.2.0).
- Plan: `~/.claude/plans/please-propose-a-plan-optimized-dusk.md` (P4 then P5).
