---
id: 001
title: Minimal design bootstrap
date: 2026-05-20
status: complete
repos_touched: [yacd]
related_sessions: []
---

## Goal
Draft a minimally viable root `DESIGN.md` for YACD that captures the initial
vision and architecture strongly enough to bootstrap the first real prototype,
without trying to resolve every unknown or define final APIs.

## Outcome
The goal was met. PR #1 added `DESIGN.md` on `master`, and the journal now
contains a component-focused `.journal/PLAN.md` for the initial prototype path.

## Key Decisions
- Primary CRD should be environment/network-shaped, not narrowly node-shaped,
  so it can grow from one local node into future multi-node topology.
- Ogmios should be the default node-side API in the first prototype, because it
  provides practical chain access without distributing the node Unix socket.
- Supporting services should be separate CRDs/controllers, with heavy IPC
  services such as db-sync using their own follower node instead of mutating the
  primary node Pod.
- db-sync is the first supporting-service priority because it matches the
  team's existing internal workflow; Yaci Store is the next optional indexer/API
  candidate after that model is proven.
- A bespoke faucet/topup path should stay narrow and use Ogmios, while the CLI
  handles imperative developer actions such as ad hoc topups.
- The developer-facing config should be a single local pane of glass compiled
  by the CLI into decomposed Kubernetes CRDs.

## Changes
- `DESIGN.md` - added the high-level YACD architecture explanation, goals,
  non-goals, product shape, CRD direction, socket model, service priorities, and
  first prototype direction.
- `.journal/PLAN.md` - added a rough component-focused implementation sequence
  for the first prototype.
- `.journal/001/NOTES.md` - recorded research and decision checkpoints across
  Yaci/Yaci DevKit/Yaci Store, faucet options, Ogmios vs Blockfrost, socket
  access, follower nodes, design drafting, and plan drafting.
- `.journal/TECH_NOTES.md` - condensed durable architecture context for future
  sessions.
- `.journal/INDEX.md` - marked this session complete.

## Open Threads
- Choose the primary CRD name and initial API group/type shape.
- Decide how much local genesis generation the operator owns in the first
  prototype.
- Define the smallest db-sync configuration surface after inspecting the real
  runtime needs.
- Decide whether the first faucet implementation is in-cluster, CLI-side, or a
  hybrid.
- Replace the template `NginxDeployment` sample and chart naming as the first
  implementation slice.

## References
- PR #1: https://github.com/meigma/yacd/pull/1
- Merged commit: `35d7823` (`docs: add initial YACD design (#1)`)
- Design: `DESIGN.md`
- Prototype plan: `.journal/PLAN.md`
