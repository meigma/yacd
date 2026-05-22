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

## 2026-05-21 17:58 — Implementation start
Started implementation on Worktrunk branch `feat/ogmios-chain-api` at
`.wt/feat-ogmios-chain-api`. Ran the required session dev stack startup from
that worktree with `direnv allow && moon run root:dev-up`; it created the
`yacd-dev` Kind cluster, started Tilt, and reported `YACD dev stack is ready`.
Tilt UI is available on port `10350`, with logs under `.run/yacd-dev/tilt.log`.

## 2026-05-21 18:14 — Ogmios implementation
Implemented phase 3 on `feat/ogmios-chain-api` and pushed commit `281ef1e`
(`feat(cardanonetwork): expose ogmios chain api`). The slice adds default
Ogmios settings resolution, the sidecar mounted to the existing node IPC and
state volumes, a dedicated `<network>-ogmios` Service, Ogmios endpoint status,
`OgmiosReady`, and aggregate `Ready`. Explicit `enabled: false` removes the
owned Ogmios Service and clears the endpoint.
Validation passed with `moon run root:generate`, `moon run root:test`,
`moon run root:check`, `moon run root:test-e2e`, and `git diff --check`.
The Chainsaw smoke created `phase3-smoke`, reached `Ready=True`, verified both
node-to-node and Ogmios status endpoints, and confirmed an in-cluster curl pod
received HTTP 200 from `http://phase3-smoke-ogmios.yacd-smoke.svc.cluster.local:1337/health`.
After e2e cleanup, the kubectl context was restored to `kind-yacd-dev`; the
session dev stack remains running.

## 2026-05-21 19:09 — Review fixes
Addressed the Ogmios review feedback on `feat/ogmios-chain-api` and pushed
commit `e8104e6` (`fix(cardanonetwork): harden ogmios readiness`). The fix
adds Service delete RBAC, live pod/container-based `NodeReady` and
`OgmiosReady` status, `ogmios health-check` startup/readiness/liveness probes,
a package-local Ogmios/cardano-node compatibility table enforced during
workload build, and a Chainsaw smoke that performs a real
`queryNetwork/tip` call before disabling Ogmios and proving the owned Service
is deleted.
Validation passed with `moon run root:test`, `moon run root:test-e2e`,
`moon run root:check`, and `git diff --check HEAD`. The initial standalone
`go test ./internal/controller/cardanonetwork` failed because plain Go test
does not set envtest assets; the repo-supported `moon run root:test` path
passed.

## 2026-05-21 19:18 — Manual Ogmios function test
Ran a manual function test against the active `kind-yacd-dev` development
environment from `.wt/feat-ogmios-chain-api`. Confirmed the controller manager
was rolled out from the Tilt-built image and that in-cluster RBAC allows the
manager ServiceAccount to delete Services and list Pods.
Applied a throwaway `yacd-manual/manual-ogmios` `CardanoNetwork` with omitted
`spec.chainAPI`. The operator created the node PVC, node Service, Ogmios
Service, and two-container Deployment. The Pod reached ready with both
`cardano-node` and `ogmios` ready and zero restarts; the Ogmios startup,
readiness, and liveness probes all used `/bin/ogmios health-check --port
1337`. Status reached `Ready=True`, `NodeReady=True`, and
`OgmiosReady=True`, with the expected `ws://manual-ogmios-ogmios.yacd-manual.svc.cluster.local:1337`
endpoint. A separate curl pod queried the Service and confirmed `/health`
returned `connectionStatus:"connected"` and `queryNetwork/tip` returned a
JSON-RPC result.
Patched the same CR to `spec.chainAPI.ogmios.enabled=false`; the controller
deleted the Ogmios Service, removed the sidecar from the Deployment template,
cleared `status.endpoints.ogmios`, kept `NodeReady=True`, and set
`OgmiosReady=False`/`Ready=False` with reason `OgmiosDisabled`.
Also applied `manual-ogmios-invalid` with `cardanosolutions/ogmios:latest`;
the controller rejected it with `Degraded=True`, reason `UnsupportedSpec`,
message `ogmios image tag "latest" is not a supported release tag`, and
created no children. The throwaway namespace was deleted and kubectl context
is `kind-yacd-dev`.

## 2026-05-21 19:38 — Close
Closed the implementation work through PR #12:
https://github.com/meigma/yacd/pull/12. The PR was squash-merged into
`master` as `fe8b4fd` (`feat(cardanonetwork): expose ogmios chain api (#12)`)
after user approval. Local `master` in `/Users/josh/code/meigma/yacd` was
fast-forwarded to the merge commit, the `feat/ogmios-chain-api` worktree and
branch were removed, and the remote feature branch was deleted. The active
YACD development stack was stopped with `moon run root:dev-down`, which removed
the Tilt process, Kind cluster, and local registry.
Closeout artifacts written in the journal branch: `.journal/007/SUMMARY.md`,
the session row in `.journal/INDEX.md`, updated durable context in
`.journal/TECH_NOTES.md`, and this final `NOTES.md` entry.
