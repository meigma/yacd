---
id: 060
title: New session
started: 2026-06-03
---

## 2026-06-03 18:54 — Kickoff
Goal for the session: TBD — session started via `session-new`; awaiting the user's
actual request.

Current state of the world:
- Latest closed session is 058 (`yacd install` command shipped in PRs #94/#96).
  Session 059 is still `in-progress` (CLI-native wallets + faucet-removal design &
  plan paused for review at `.journal/059/WALLET_REARCH_PLAN.md`); 051 and 052 also
  remain `in-progress` in INDEX.
- `master` is at `5383f76` (`feat(cli): add yacd install command (#96)`); origin
  in sync.
- Active Worktrunk worktrees: `journal/jmgilman`, `feat/faucet-wallet-secret`
  (ahead 1: `f96f1b6` genesis-funded faucet wallet), `docs/mkdocs-site` (PR #91,
  diverged). No implementation worktree selected for this session yet.
- Carried open threads: PR3 for `yacd install` (uninstall + OCI version fetch);
  local-lifecycle P7 hardening; the session-059 faucet-removal plan; docs PR #91
  follow-ups; operator draft releases v0.1.0/v0.1.1 await a human Publish;
  release-please root PR #7; TEST_REPORT F2/F4; test-harness `yacd-env` Action.

Plan: wait for the user's request before selecting/creating an implementation
worktree, starting the dev stack, or doing substantive work.

## 2026-06-03 21:15 — External-access design converged; doc written
Goal landed: improve how the CLI connects to local clusters (the `yacd topup`
per-command port-forward is slow); make `yacd devnet` fast without a background
`connect`.

Design discussion (no code yet). Explored the relevant surfaces (see the doc's §2
for file:line grounding): status endpoints are single in-cluster URLs
(`status.go:setEndpointStatus`), Services are all ClusterIP, the CLI is hardwired
to forward (`forward.go` + `envcontract.go:loopbackURL`), devnet's k3d create has
no `--port` mappings, and the checked-in config is the Environment doc consumed
only by `up`/`devnet`.

Key turns in the discussion:
- Initially I objected to localhost-in-CRD on layering grounds and proposed a
  separate CLI config file. User pushed back (correctly): localhost is a *valid
  assertion* for a co-located devnet, and one CRD field unifies devnet + remote.
  Reversed — dropped the separate CLI config file entirely.
- Corrected my own error: the checked-in config IS the Environment doc
  (`devconfig`, consumed by `up -f`), not a (nonexistent) Viper config file.
- Verified k3d mechanics: create-time `--port` is STABLE (not experimental);
  only `k3d cluster edit --port-add` is experimental. serverlb proxies host port
  → same node port → needs NodePort (30000–32767) or Traefik. Pinning at create
  time avoids the experimental path.

Converged design (in `EXTERNAL_ACCESS_DESIGN.md`):
1. API/operator: `spec.chainAPI.{ogmios,kupo}.service.{type,nodePort}` +
   `externalURL`; mirror externalURL → `status.endpoints.*.externalURL` (additive).
2. devnet: pin host+node ports, k3d `--port "1337:30137@loadbalancer"` etc.,
   author default spec with NodePort + localhost externalURL.
3. CLI resolution (shared forward path): flag > YACD_* env > status.externalURL
   (probed) > port-forward fallback. Benefits `run` + topup `--await`.
Decided: spec→status mirror; keep the liveness probe. Faucet/topup-faucet leg
out of scope (token trust gate + session 059 faucet-removal plan).

Phased: P1 API+operator, P2 devnet plumbing, P3 CLI resolver (order matters).

Next: user reviews the design doc. No implementation worktree created yet; dev
stack not started.
