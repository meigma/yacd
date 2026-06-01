---
id: 051
title: New session
started: 2026-06-01
---

## 2026-06-01 16:41 — Kickoff
Goal for the session: not yet stated — session started via `session-new`,
awaiting the user's actual request.

Current state of the world:
- `master` at `bd8e0bf` (clean). The **F0 redesign series is COMPLETE** through
  PR-D (sessions 046–050): the runtime artifact data path is fully HTTP/PVC-based,
  the `<net>-network-artifacts` ConfigMap and `custom-public` profile are gone,
  the cardano-testnet artifact publisher and the cardano-tools `report` verb are
  deleted, and the manager's `cardano-tools` image default is digest-pinned to
  `11.0.1-yacd.5@sha256:d3283ca5…`. YACD now supports only local + curated-public
  (preview/preprod/mainnet).
- Open release-please PR **#7** (`yacd 1.0.0`) is intentionally left open awaiting
  a deliberate operator-release decision.
- Session **049** (yacd CLI all-in-one local cluster lifecycle / k3d design) is a
  separate concurrent session still `in-progress`.

Carried/open threads (none blocking):
- cardano-testnet digest-pin parity (optional, mirrors PR-D's cardano-tools work).
- Deterministic primary-sidecar manager-envtest refactor (the flaky test removed
  in 048).
- TEST_REPORT F2/F4 remain.
- Test-harness Phases 3 (release), 4 (`yacd-env` Action), 5 (examples + how-to).

Plan: wait for the user's request before doing substantive work.
