---
id: 007
title: Pending session goal
started: 2026-05-21
---

## 2026-05-21 17:30 — Kickoff
Goal for the session: start a new YACD journal session; the concrete
implementation or research goal is still pending from the user.
Current state of the world: journal branch `journal/jmgilman` is present,
clean, and current with origin. Recent closed sessions completed the
cardano-testnet tools image/init fragment, the primary `CardanoNetwork`
workload, and primary node Service/status/readiness. The remaining known
phase-2 follow-up from session 006 is current-state documentation drift,
especially README text that still describes the reconciler as future work.
Plan: wait for the user's actual request. Once implementation work starts,
select or create an implementation Worktrunk worktree, run the required
`moon run root:dev-up` startup step there unless explicitly waived, and keep
the session notes updated at meaningful checkpoints.

## 2026-05-21 17:35 — Phase 3 shape
Goal for the checkpoint: read `.journal/PLAN.md` phase 3 and propose the
initial internal type shape for generating the Ogmios sidecar, Service, and
status endpoint.
What was reviewed: phase 3 targets shared ephemeral node IPC, an Ogmios
sidecar in the primary node Pod, a ClusterIP Ogmios Service, and status that
makes the Ogmios endpoint discoverable. Current `CardanoNetwork` API already
has `spec.chainAPI.ogmios` and `status.endpoints.ogmios`, so the first slice
should activate that surface instead of adding another CRD.
Proposal direction: keep Ogmios under the existing package-local
`primaryWorkloadBuilder`, add a small resolved Ogmios settings type to handle
controller-side defaults and validation, extend `primaryWorkloadResources`
with an explicit Ogmios Service, and keep sidecar rendering/status endpoint
helpers unexported in `internal/controller/cardanonetwork`.
