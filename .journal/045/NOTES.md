---
id: 045
title: New working session
started: 2026-05-31
---

## 2026-05-31 10:18 — Kickoff
Goal for the session: Start a new YACD journal session; the implementation or review goal has not been stated yet.
Current state of the world: `master` is clean at `2f28360 fix(cli): harden review findings (#73)`. The personal journal branch `journal/jmgilman` is clean and up to date after session 044. Recent context leaves the F0 transport redesign banked on `feat/f0-public-profile-pvc`; no active implementation worktree has been selected for this session.
Plan: Wait for the user's actual request, then choose or create the implementation Worktrunk worktree and start the dev stack if the work is implementation-level.

## 2026-05-31 10:31 — Status surface inventory
Goal for this checkpoint: summarize what `CardanoNetwork` and `CardanoDBSync` currently publish in status so we can evaluate operator visibility.
Current state of the world: No implementation worktree is selected yet. The inventory is grounded in `api/v1alpha1/*_types.go` plus the controller status and readiness writers under `internal/controller/cardanonetwork` and `internal/controller/cardanodbsync`.
Learned: `CardanoNetwork` publishes identity, cluster-local endpoints, faucet auth Secret reference, artifact bundle metadata, and component conditions. `CardanoDBSync` publishes endpoints, accepted database identity and placement, sync progress, primary-sidecar attachment material, and component/runtime conditions.

## 2026-05-31 10:39 — Node sync semantics
Goal for this checkpoint: clarify whether CardanoNetwork can report public-network "caught up" without a separate remote oracle.
Learned: upstream Cardano surfaces a local node sync percentage by comparing the node's current tip time/slot to wall-clock network time with tolerance; Ogmios also exposes `networkSynchronization`, `lastKnownTip`, and `lastTipUpdate` from its health endpoint. This is not an omniscient global-tip proof: it means the node appears synchronized from its own local view and connected node/Ogmios state.
Next: If we add this to `CardanoNetwork`, name it carefully as node/network synchronization rather than finality or externally verified public-tip equality.
## 2026-05-31 10:43 - implementation worktree and dev stack

- Created implementation worktree `feat/cardanonetwork-sync-status` at
  `.wt/feat-cardanonetwork-sync-status` from `master`.
- Ran `moon run root:dev-up` from the implementation worktree; Kind/Tilt dev
  stack is ready and Tilt UI is at `http://localhost:10350/`.

## 2026-05-31 10:59 - non-external node sync status implemented

- Implemented `CardanoNetwork.status.sync` from verified network artifacts and
  the owned Ogmios `/health` endpoint on branch
  `feat/cardanonetwork-sync-status`.
- Added `NodeSynchronized` and `NodeProgressing` conditions without feeding them
  into aggregate `Ready`.
- Committed implementation as `4065d4d feat(cardanonetwork): publish node sync
  status`.
- Validation passed: `moon run root:test`, `moon run root:check`, and
  `git diff --check`.

## 2026-05-31 11:22 - sync probe godoc cleanup

- Added private helper/type/constant godoc in `sync_probe.go` to match the
  repository's local controller style.
- Committed follow-up as `c5da81d docs(cardanonetwork): document sync probe
  helpers`.
- Revalidated with `moon run root:check` and `git diff --check`.

## 2026-05-31 13:12 - Ogmios disconnected review fix

- Addressed review findings for Ogmios `/health`: HTTP 500 health bodies are
  now parsed so disconnected Ogmios publishes `connectionStatus=disconnected`,
  and nullable `lastKnownTip`, `lastTipUpdate`, and `networkSynchronization`
  fields are treated as reachable unknown-state health data.
- Committed fix as `4f6892c fix(cardanonetwork): handle disconnected ogmios
  health`.
- Validation passed: `moon run root:test`, `moon run root:check`, and
  `git diff --check`.

## 2026-05-31 13:45 — Close

- PR #74 (`feat(cardanonetwork): publish node sync status`) was approved,
  squash-merged to `master` as `bfadcf6`, and the remote feature branch was
  deleted.
- Local `master` was fast-forwarded to `bfadcf6`; the session worktree
  `.wt/feat-cardanonetwork-sync-status` and local branch were removed.
- The YACD dev stack was shut down successfully with `moon run root:dev-down`.
- GitHub CI checks passed for PR #74, including `ci`, `e2e`,
  `cardano-tools-image`, and `Kusari Inspector`; release dry-run jobs were
  skipped as expected.
- Session 046 / `feat/f0-public-profile-pvc` remains active and untouched.
