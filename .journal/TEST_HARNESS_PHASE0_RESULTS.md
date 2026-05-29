# YACD Test Harness — Phase 0 Feasibility Results

Status: **complete — GO** (session 036, 2026-05-29). Companion to
[`TEST_HARNESS_PLAN.md`](./TEST_HARNESS_PLAN.md) (Phase 0) and
[`TEST_HARNESS_PROPOSAL.md`](./TEST_HARNESS_PROPOSAL.md) (§10 risks).

This document is the evidence and go/no-go decision for Phase 0 — *Validate
feasibility*. It was produced by a throwaway measurement spike that stood up
KinD + the operator + a representative local `CardanoNetwork` on a GitHub-hosted
runner and measured the three load-bearing assumptions. The spike branch
(`spike/phase0-ci-feasibility`) and its workflow were discarded after the run;
the run logs remain at the URL below.

## Decision

**GO for the CI story on GitHub-hosted standard runners.** Cold-start to Ready
is ~6× under the budget, teardown is clean via Kubernetes GC, and the
`run`/`exec` host-access model is empirically confirmed. The remaining risks are
mitigations to fold into Phase 4's action (image preload, the 2-core private
tier), not blockers.

## How it was measured

- **Run:** `meigma/yacd` Actions run
  [`26660099746`](https://github.com/meigma/yacd/actions/runs/26660099746),
  `ubuntu-latest`, push-triggered. Total job wall-clock ~5m27s.
- **Environment:** `examples/local/yacd.yaml` — local mode, node 11.0.1, 2Gi
  PVC, Ogmios + Kupo + faucet enabled, network magic 42, Conway era, compressed
  timing (slot 100ms / epoch 500), 1 pool. (CardanoNetwork only — the harness
  `up` target. No CardanoDBSync.)
- **Build path:** images built with **ko** (`.dev/ko-build.sh`,
  `.dev/ko-build-faucet.sh`) — the operator's real build path — then
  `kind load`ed. `cardano-testnet:11.0.1-yacd.4` was pulled from the public
  ghcr package and preloaded (mirrors the proposal's preload-published model).
  Ogmios/Kupo were left to pull from Docker Hub during bring-up (the proposal
  does not preload them), so their pull time is part of the cold-start number.
- **Operator install:** CRDs via `kubectl apply -f charts/yacd/crds`, then
  `helm upgrade --install yacd charts/yacd -n yacd-system` with the ko images
  and `IfNotPresent`.

## Results

### ① Cold-start time-to-Ready — **GO** (huge margin)

| Stage | Seconds |
|---|---:|
| `kind create cluster` | 41 |
| cardano-testnet pull (published) | 8 |
| `kind load` (manager + faucet + cardano-testnet) | 22 |
| operator install (helm → manager Available) | 14 |
| **`yacd deploy … --wait` (localnet → `Ready=True`)** | **27** |
| **Cold-start total (excl. source image build)** | **112 (~1m52s)** |
| _(manager+faucet ko build, separable¹)_ | _101_ |

`yacd deploy` reported `CardanoNetwork yacd-smoke/phase4-smoke is ready` in 27s,
including the Docker Hub pulls of Ogmios/Kupo, the `cardano-testnet` create-env
init, and all four sidecars reaching readiness. **112s end-to-end vs the
10–12 minute (600–720s) budget — a ~6× margin.**

¹ The manager/faucet images are built from source because nothing is published
yet (chart is `0.0.0`). The proposal's real action installs a *published* chart
and pulls *published* images, so the 101s build disappears once Phase 3 lands.

### ② Teardown completeness — **GO** (clean GC, no finalizers)

`kubectl delete cardanonetwork phase4-smoke` returned rc=0; all **11**
owner-referenced children were garbage-collected in **3s** with **zero**
remaining and no finalizer stall. The 11 (all carrying a `CardanoNetwork`
controller ownerReference, confirmed in code and at runtime): primary
Deployment, node-state PVC, four Services (node / ogmios / kupo / faucet),
faucet-auth Secret, network-artifacts ConfigMap, and the artifact-publisher
ServiceAccount / Role / RoleBinding. No finalizers are registered and no
per-network cluster-scoped RBAC exists, so plain GC suffices. This closes the
proposal §10 "clean ownerRef teardown is UNVERIFIED" risk.

### ③ Host-access assumptions — **GO** (both paths, cross-confirmed)

All four chain-API containers co-locate in the single primary node Pod
(`phase4-smoke-node-…`), and all Services select it on container-named ports, so
one set of port-forwards serves every API:

- **`run`/`connect` (host port-forward):** Ogmios `200`
  (`queryNetwork/tip` → slot 130), Kupo `/matches` `200`, faucet `/readyz` `200`.
- **`exec` (in-pod node socket):**
  `cardano-cli query tip --socket-path /ipc/node.socket --testnet-magic 42`
  succeeded — block 6, epoch 0, Conway, **slot 130, same block hash** as the
  Ogmios query. The two independent paths agreeing on the tip is strong evidence
  both work and observe the same node.

This confirms the proposal §6 `run`/`exec` split and the §10 "exec reach"
assumption.

### Budget / schedulability

Runner reported `nproc=4`, `mem=15989MB`; node allocatable `4 / ~15.6Gi`. The
localnet sets **no CPU/memory requests** on cardano-node/ogmios/faucet, yet
scheduled and ran with **0 OOM events and 0 evictions/unschedulable events**.

## Caveats & residual risks

1. **Runner spec is larger than the plan assumed.** `ubuntu-latest` delivered
   **4 vCPU / 16 GB**, not the 2 vCPU / 7–8 GB in the plan — GitHub upgraded the
   standard hosted runner for public repos. The 27s/112s result is therefore on
   a more generous box. The ~6× margin makes the 2-core case very likely fine,
   but **the 2 vCPU / 7 GB tier (still the default for private repos) is
   untested.** Recommendation: Phase 4's gating job should run once on the
   consuming repo's actual tier before external sign-off.
2. **Single sample; Docker Hub pull jitter.** Ogmios/Kupo pulled from Docker Hub
   without hitting the anonymous rate limit *this run*. On busier runner IPs a
   `toomanyrequests` stall could add minutes or fail bring-up. Mitigation:
   extend the proposal's preload (currently manager/faucet/cardano-testnet only)
   to also `kind load` Ogmios/Kupo, or pull them via an authenticated mirror.
3. **No CardanoDBSync.** By design this measured the harness `up` target only.
   A db-sync environment is materially heavier (the existing Chainsaw smoke
   budgets ~15m for it) and needs its own measurement if the harness later
   targets db-sync environments.
4. **Discovered defect — the documented e2e path is broken.**
   `moon run root:test-e2e` → `.dev/scripts/test-e2e.sh` builds the manager with
   `docker build .`, which **fails**: the root `.dockerignore` ignores
   everything and re-includes only `**/*.go` + `go.{mod,sum}`, so the embedded
   `internal/cardano/publicnet/profiles/*/*` files are stripped from the build
   context and `//go:embed profiles/preview/* profiles/preprod/* profiles/mainnet/*`
   errors with `pattern profiles/mainnet/*: no matching files found`. This has
   been latent since the public profiles landed (2026-05-27) because `test-e2e`
   is `runInCI: false` and nothing exercises it. **The working build is ko.**
   Fix before Phase 4 wires a gating job around `test-e2e`: either build via ko
   in `test-e2e.sh`, or re-include the profile assets in `.dockerignore`.

## Implications for later phases

- **Phase 4 (CI action):** feasibility is proven. Build it on `ubuntu-latest`,
  preload all images (incl. Ogmios/Kupo), keep the 12m timeout as generous
  headroom, and validate once on the consumer's runner tier. Fix the
  `test-e2e.sh`/`.dockerignore` defect first (see caveat 4).
- **Phase 1/2:** the `run`/`exec`/`connect` reachability the CLI verbs depend on
  is confirmed against the rendered Pod, so those verbs can be built without
  re-litigating host access.
