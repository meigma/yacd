---
id: 053
title: Local lifecycle Phase 1 — cut operator v0.1.0
started: 2026-06-01
---

## 2026-06-01 18:07 — Kickoff

Goal for the session: begin executing the `LOCAL_LIFECYCLE_PLAN.md` (session 049),
starting with **Phase 1 — cut `v0.1.0` of the operator + Helm chart**. Per the
plan, Phase 1 is pure release plumbing (no operator behavior changes): drive the
existing release-please + `release.yml` flow to a real published `v0.1.0` — images
on `ghcr.io/meigma/yacd` and the Helm chart at `0.1.0` (appVersion `v0.1.0`) — so
the CLI lifecycle work has a reliable, versioned install target. User will give
the next concrete instructions after priming.

Current state of the world:
- Primed against the session protocol. Journal root = the `journal/jmgilman`
  Worktrunk worktree at `.wt/journal-jmgilman`; journal clean and up to date.
- Read session 049's `LOCAL_LIFECYCLE_DESIGN.md` + `LOCAL_LIFECYCLE_PLAN.md` (the
  approved, design-only plan) and the 049/050 summaries. Plan dependency graph:
  P1 (release v0.1.0) → P4 (funded wallet, v0.2.0) → P5 (operator install, embeds
  v0.2.0); P2 (toolbin) → P3 (cluster); P3,P5 → P6 (devnet) → P7 (hardening).
- The F0 redesign series is **COMPLETE** as of session 050 (PR-A/B/C/D merged);
  master is a coherent local + curated-public operator — the Phase-1 "don't cut
  mid-redesign" precondition appears satisfied.
- **Tension to resolve before doing Phase 1 work:** the plan says cut `v0.1.0`,
  but session 050 left **root release PR #7 open targeting `yacd 1.0.0`** (the
  release-please-computed version, deliberately unmerged awaiting a release
  decision). Phase 1's target version (`v0.1.0`) conflicts with the in-flight
  `1.0.0` release PR. Need a user decision on which version the operator's first
  published release should carry before driving release-please.
- Sessions 051 and 052 are stale `in-progress` rows in INDEX (started, never
  used; "awaiting the user's request"). Left untouched.

Plan: confirm priming complete, surface the v0.1.0-vs-1.0.0 version tension to the
user, and wait for concrete Phase 1 instructions before any release-please/CI work.
No implementation worktree created yet (Phase 1 is release plumbing; will create
one from fetched master once the approach is confirmed).

## 2026-06-01 19:23 — Redirect landed; PR #7 at 0.1.0; at the publish gate

Plan approved (pause-before-merge; redirect+execute only). Executed the redirect:

- Worktree `chore/release-v0.1.0` off fetched master under `.wt/`. Dev stack
  waived (pure release plumbing; verification uses a throwaway cluster + published
  chart).
- Redirect change: one-line clarification in the `.github/workflows/release.yml`
  header comment, carrying a component-scoped `Release-As: yacd@0.1.0` footer.
  `root:check` green, `git diff --check` clean.
- PR #83 → squash-merged to master as `925caec` with the scoped footer in the
  squash commit. (Non-publishing — only makes release-please recompute.)
- release-please run on `925caec` succeeded and **rewrote PR #7 to 0.1.0**:
  title `chore(master): release 0.1.0`, manifest `.`=`0.1.0`, Chart.yaml
  `version: 0.1.0` / `appVersion: "v0.1.0"`; container components untouched
  (`11.0.1-yacd.5`) — scoped footer worked exactly as designed.
- All 15 dry-run checks re-ran green on the rewritten PR #7 (binary, container
  x2 arch, faucet, helm, cardano-testnet/tools x2 arch, ci, e2e, kusari).

Pre-flight cleared:
- **Tag protection**: I have repo admin. Repo rulesets = `[]`, master has no
  branch protection, classic tag protection = 404 (none), and the org is below
  the tier that enables org rulesets (`orgs/meigma/rulesets` → 403 "Upgrade to
  Team"). So there is NO tag protection; the release-please.yml header comment
  about a "protected-tag bypass" is intent-only, nothing is configured. The app
  already creates `cardano-*/v*` tags, so creating `v0.1.0` will not be blocked.
  No admin action needed.
- Draft-release sequencing: low risk — component releases already produce draft
  GitHub releases via the same `draft:true`+`force-tag-creation:true` config.

**At the publish gate.** Refs that WILL publish when PR #7 merges (tag `v0.1.0`):
manager `ghcr.io/meigma/yacd:v0.1.0`, faucet `ghcr.io/meigma/yacd/faucet:v0.1.0`,
chart `oci://ghcr.io/meigma/yacd/chart` @ `0.1.0`, binaries
`yacd_0.1.0_{darwin,linux}_{amd64,arm64}` on a DRAFT GitHub release. GHCR has no
draft state, so the merge publishes images+chart immediately + attestations; only
the GitHub release stays draft for manual Publish. Awaiting explicit user go to
merge PR #7. This will be the first-ever real run of `release.yml` (dry-runs skip
publish) — will babysit and fix-forward if the publish/attest tail breaks.

## 2026-06-01 19:39 — v0.1.0 PUBLISHED; published-chart smoke green

User gave go. Merged release PR #7 (squash `a2cbdf3`) → release-please tagged
**`v0.1.0`** + created a draft GitHub release → `release.yml` fired (the
first-ever real root release run) and **succeeded across all 10 jobs**: binary
assets, manager image (amd64+arm64), faucet image (amd64+arm64), the two image
releases, helm chart release, inspection summary. No first-run breakage — the
publish/OCI-push/attest tail (untested by dry-runs) worked first try.

Canonical published refs (record for P4/P5 to pin against):
- Manager: `ghcr.io/meigma/yacd:v0.1.0`
  `@sha256:cb3d42ecc52283d55ddecf6b7fdee00c2eea2cc44daf28f7ccf8c54aaab7a7f5`
  (linux/amd64 + linux/arm64)
- Faucet:  `ghcr.io/meigma/yacd/faucet:v0.1.0`
  `@sha256:d4ae37c9322cb1b97ba2914edf48502f39af26e578686c9eccc4ef5efd06568b`
  (amd64 + arm64)
- Chart:   `oci://ghcr.io/meigma/yacd/chart` version `0.1.0`
  `@sha256:27647a75c13db57432ce650dd12d47274869dbe260f819ea7b9f2b920f7985f6`
  (appVersion `v0.1.0`)
- Binaries: `yacd_0.1.0_{darwin,linux}_{amd64,arm64}` on the draft GitHub release.
- All three artifact classes carry GitHub-native attestations.

Published-chart smoke (the key Phase-1 exit criterion) — PASS:
- Throwaway `kind` cluster `yacd-v010-smoke` (isolated kubeconfig; deleted after).
- `helm install yacd oci://ghcr.io/meigma/yacd/chart --version 0.1.0` with NO
  overrides → operator Deployment `Available=True`; both CRDs installed; the
  rendered + running manager image resolved by default to the published
  `ghcr.io/meigma/yacd:v0.1.0` (digest `cb3d42ec…`).
- `yacd up phase4-smoke -n yacd-smoke -f examples/local/yacd.yaml` (same path as
  the chainsaw e2e) → `CardanoNetwork` **Ready=True** (Degraded=False;
  Node/Ogmios/Kupo/Faucet/Artifacts Ready=True; endpoints published). Faucet
  sidecar ran the published `ghcr.io/meigma/yacd/faucet:v0.1.0`; cardano-tools
  digest-pinned `11.0.1-yacd.5`, cardano-testnet `11.0.1-yacd.5`, ogmios
  v6.14.0, kupo v2.11.0 — all expected defaults.
  (NodeSynchronized/NodeProgressing False = `NetworkTimingUnavailable`,
  expected visibility-only localnet state, not part of aggregate Ready.)

State: **Phase 1 essentially complete.** Open items: (1) the GitHub draft
release `v0.1.0` is intentionally left for the user to Publish (their decision);
(2) prune the merged `chore/release-v0.1.0` worktree at wrap-up. Next plan phases
unblocked: P4 (funded wallet → v0.2.0) and P5 (operator install embedding the
release). Per the plan, P2 (toolbin) / P3 (cluster) are independent and can start
in parallel.

## 2026-06-01 21:05 — Phase 4 funded-wallet: implementation landed on branch

New task: execute session-049 plan Phase 4 (funded wallet, operator-side, ships
in v0.2.0). Plan written + approved (`~/.claude/plans`). Key decisions: cardano-
testnet create-env has NO `--initial-funds` (verified against the published
binary), so the operator GENERATES the wallet key (user-owned, stored in an owned
Secret) and FUNDS it via the existing faucet `/v1/topups` path, confirming
on-chain via Kupo. User chose: WalletReady gates aggregate Ready; default funding
100,000 ADA.

Implemented on worktree `feat/funded-wallet` (off `a2cbdf3`), committed `370a1d7`:
- `internal/cardano/wallet` (new pure pkg): ed25519 keygen → cardano-cli text
  envelopes + addr_test derivation (reuses Apollo via the faucet's logic, which
  was lifted here; `sources.go` now calls it). Golden test pinned to a real
  `cardano-cli address build` vector (seed 0x01*32 →
  addr_test1vqxk54m7j3q6mrkevcunryrwf4p7e68c93cjk8gzxkhlkpsffv7s0).
- API: `spec.chainAPI.wallet{enabled,fundingLovelace}` + `status.wallet
  {address,keySecretName,funded,fundedTxID}` + `WalletReady` condition; CRD +
  deepcopy regenerated.
- Controller (cardanonetwork): owned `<net>-wallet` Secret create-once (mirrors
  faucet_auth.go; key material NEVER regenerated); funding orchestration in
  `wallet.go` gated on Faucet+Kupo ready — Kupo REST confirm (balance = source of
  truth, self-heal) + faucet POST via injectable seams in `wallet_funding.go`
  (plain net/http; verified the manager pulls NO ogmigo/gorilla/kugo, only
  Apollo address/key/bech32). WalletReady wired into AggregateReady (gates Ready
  when enabled); funding error → Degraded; pending → Progressing (15s requeue).
  Validation: wallet requires local + faucet + kupo + fundingLovelace ≤ faucet
  max. Teardown deletes the wallet Secret only on explicit disable (not on
  degrade — it's the user's key). No new RBAC.
- examples/local/yacd.yaml: wallet enabled (100k ADA) + faucet maxTopUpLovelace
  raised to 100000000000.
- Tests: wallet golden; controller unit tests (settings validation, create-once
  preserve, funding state machine via mocked seams: disabled/pending/confirmed/
  submit-once/no-resubmit/funding-error→degraded/confirm-error→degraded);
  manager-backed envtest extended (wallet Secret + status.wallet.address; confirmer
  override returns funded so Ready gating still reaches True); Chainsaw asserts the
  `<net>-wallet` Secret + WalletReady=True + status.wallet.funded on the real
  localnet.

Green: `root:test` + `root:check` pass; `git diff --check` clean. `root:test-e2e`
(real funding path) running now. Next: e2e green → open PR → cut v0.2.0 (auto
0.1.0→0.2.0, no Release-As) with the same pause-before-merge gate.
