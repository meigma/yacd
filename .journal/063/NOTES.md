---
id: 063
title: New session
started: 2026-06-06
---

## 2026-06-06 15:14 — Kickoff
Goal for the session: not yet stated — session started via `session-new`,
awaiting the user's actual request.

Current state of the world:
- master is at `f2501b7` (PR #116, external-access P3 — CLI resolver). The
  3-phase external-access design is COMPLETE and released as **v0.2.1**
  (`ghcr.io/meigma/yacd:v0.2.1`, `chart:0.2.1`); GitHub release left as a draft.
- Faucet removal is COMPLETE (sessions 059/061, v0.2.0): no in-cluster faucet
  service; the controller surfaces a genesis-funded `faucet` wallet (local mode
  only) and the CLI owns all wallet management + funding via direct Ogmios/Kupo
  txns (`yacd wallet {list,add,topup,export,remove}`). The CLI installs the
  operator by appVersion tag.
- Known open threads carried from recent sessions:
  - Docs are stale: `README.md` + `DESIGN.md` + the MkDocs **PR #91**
    (`docs/mkdocs-site` branch, +13 ahead) still describe the pre-faucet-removal
    model and pre-session-057 CLI; they need a rewrite to the genesis-faucet-wallet
    + `yacd wallet` + external-access (NodePort/externalURL) model.
  - ogmigo ws-1006 non-fatal genesis-config warning during wallet funding —
    issue #110 (move off ogmigo / suppress at source).
  - GitHub releases for v0.2.1 (root), `cardano-tools`, `cardano-testnet` are
    drafts (GHCR artifacts already live) — publish if desired.
  - TEST_REPORT F2/F4; the `yacd-env` GitHub Action (test-harness Phase 4).
  - Stale `in-progress` INDEX rows 051 + 052 (052 = the docs PR #91 stream).

Plan: await the user's request, then load task-relevant skills and set up an
implementation worktree + dev stack if the work calls for it.
