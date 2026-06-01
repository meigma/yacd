---
id: 049
title: yacd CLI all-in-one local cluster lifecycle (k3d) design
started: 2026-06-01
---

## 2026-06-01 07:58 — Kickoff

Goal for the session: capture, persistently, the design for giving the `yacd`
CLI an "all-in-one" local lifecycle — one command to stand up a local
Kubernetes cluster, install the yacd operator (Helm), and create Cardano
network custom resources — and record the runtime decision and supporting
research. This session is design/recording first; no implementation is
committed to the product yet.

Current state of the world:
- The CLI lives under `cli/` (Cobra/Viper, binary built from `./cli/cmd/yacd`),
  with a `kube.Client` port (`cli/internal/kube`) already carrying
  `ApplyCardanoNetwork`/`Get`/`Delete`/`List`, `EnsureNamespace`, and the
  `up/down/list/connect/run/exec/info/topup` verb set (test-harness Phases 0–2
  done). The CLI today assumes a cluster already exists — it does NOT manage
  cluster lifecycle.
- The repo's operator dev loop uses **KinD** via `ctlptl` + Tilt
  (`kind-yacd-dev`), and Chainsaw e2e runs on KinD. That stays as-is.
- Prior turns in this conversation: ran the `deep-research` workflow on
  `tilt-dev/ctlptl` (how it works internally, importability). Findings (23/25
  claims verified): ctlptl is Apache-2.0 and importable, but pre-v1.0 (no API
  stability), heavy deps, and internally mostly **shells out to the provider
  binaries** (kind/k3d/minikube) + uses the Docker SDK for its own registry. Its
  genuinely useful idea is the **declarative apply/reconcile (desired-vs-current,
  idempotent)** model. Conclusion: do NOT import ctlptl; emulate the pattern.

Key decision already made (recorded to auto-memory `cli-local-runtime-k3d`):
- The CLI standardizes on a **single runtime: k3d** for end users. KinD stays for
  controller testing (native-kubeadm fidelity matters for catching controller
  bugs; that requirement does not extend to end users). k3d wins on first-class
  built-in registry (`k3d registry create` / `--registry-create` — no custom
  containerd/moby wiring, unlike KinD which has no native registry command),
  fast boot, light footprint, default local-path storage class.
- Auto-fetch a **pinned** k3d binary (and pinned k3s/k8s version) into an XDG
  path (`$XDG_DATA_HOME/yacd/bin/k3d-<version>`), SHA256-verify against the
  release `checksums.txt`, atomic install, sanity-check; preflight `docker info`.
  The only end-user requirement becomes Docker.
- Use k3d's built-in managed registry rather than rolling our own.

Proposed CLI lifecycle shape (design-level, not built):
| Stage | Mechanism |
| Preflight | probe `docker info`, fail fast with one actionable message |
| Binary | auto-fetch pinned k3d → XDG path, checksum-verify, sanity-check |
| Cluster up | `k3d cluster create` (pinned k3s version) + `--registry-create`; idempotent reconcile |
| Registry | k3d's managed registry (no custom wiring) |
| Operator | Helm Go SDK against `charts/yacd/` |
| Cardano nets | existing controller-runtime `kube.Client` port |

Plan for this session:
- Decide WHERE this design lives persistently (candidates: a new
  `.journal/049/` design doc like the test-harness `*_DESIGN.md`/`*_PROPOSAL.md`
  set; and/or `TECH_NOTES.md`; and/or root `DESIGN.md`). Confirm with the user.
- Write the design up: research summary (ctlptl internals + why not import),
  the k3d decision + rationale, the binary auto-fetch contract (XDG path,
  pinning, checksum, docker preflight), the CLI verb/lifecycle shape, and
  rejected alternatives (import ctlptl; KinD for the CLI; kind/k3d Go libs).
- Note the natural first implementation slice (binary-fetch + docker preflight +
  `cluster up`/`down`) for a future implementation session, but do NOT implement
  here.

Open question to resolve with the user before writing: target location/format
of the persisted design (journal design doc vs TECH_NOTES vs root DESIGN.md).

## 2026-06-01 08:24 — First-pass design proposal written

User chose: design lives under `.journal/049/`. Effort set to ultracode.
Ran an iterative, agent-orchestrated process per the user's requested method.

Two workflows run (background, structured outputs archived in the session
transcript dir):
1. `yacd-cli-lifecycle-ground` — 6 code-map agents (Explore) over cli/, kube
   port, devconfig, charts/yacd, dev stack, build/release + 5 k3d/Helm
   feasibility researchers. Key facts: kube.NewClient needs NO change to target
   a new cluster (clientcmd from --kubeconfig/--context/KUBECONFIG/default);
   `up NAME -f file` is used by CI e2e + Chainsaw + yacd-env against an EXISTING
   cluster (the hard constraint); k3d create is non-idempotent + context always
   `k3d-<name>`; registry should be opt-in (yacd pulls published ghcr.io images);
   k3d-as-library pulls Docker SDK + client-go skew (favor shell-out); repo pins
   tools via proto (host-only, so the shipped CLI needs its own in-Go XDG fetcher);
   Helm crds/ are install-once.
2. `yacd-cli-lifecycle-refine` — 3 alternative command surfaces + 5 adversarial
   critics (feasibility / technical / user-need / contract-CI / completeness).

Decisive refinements from the critics (all four forks resolved):
- A: SEPARATE VERB, not overload `up`. The killer: CI selects its cluster via
  AMBIENT `KUBECONFIG` (no --context flag); the CLI can't distinguish ambient
  from default at the flag layer, so "provision unless explicit flag" breaks CI;
  relaxing `up`'s ExactArgs(1)+required -f breaks pinned tests.
- B: pre-render + SSA (not Helm SDK) — repo already does SSA; Helm crds/ are
  install-once.
- C: isolated managed kubeconfig; mechanism = set KUBECONFIG=<managed file> in
  the child k3d process env (k3d clobbers ~/.kube/config by default otherwise).
- D: shell-out to a pinned, checksum-verified binary (library = ~115 modules +
  Docker SDK + client-go skew + Go 1.24.4 + logrus).
New requirements surfaced: ClusterProvisioner + BinaryResolver ports (hexagonal
seam like kube.Client); EnsureCluster state machine (partial-create recovery);
cluster file lock (shared cluster raced by worktrees/invocations); version-skew/
upgrade contract; embedded build-time checksum (not runtime checksums.txt);
honest first-run progress (no 2-min promise); uninstall/cleanup + binary GC.
BIGGEST product gap (HIGH): "fund an address" is NOT zero-config today — no
wallet/keygen; topup needs a user-supplied addr_test… Needs operator-side
funded-wallet bootstrap (DESIGN.md anticipates it). Flagged as top dependency.

Wrote `.journal/049/CLI_LIFECYCLE_DESIGN.md` (first pass) with: user story,
experience walkthrough, mental model, command surface, the 4 resolved forks,
architecture+code overlay, adversarial findings→resolutions, rejected
alternatives, the funded-wallet dependency, a phased plan, and 5 open decisions
for the user (verb name; wallet sequencing; up targeting precedence; defer
`cluster` nouns?; ephemeral-by-default?).

NEXT: paused for user review of the proposal. Open decisions in §13 await the
user's calls before any implementation.

## 2026-06-01 — Review round 1 (decisions)

User reviewing the proposal incrementally:
- DROPPED the proposed `yacd query tip` verb. Querying stays `yacd exec NAME --
  cardano-cli …`; `devnet`/`up`/`info` print a copy-pasteable exec hint with the
  network magic interpolated from published status (no hard-coded 42). `info`
  remains the structured-status inspector. (Committed bd0ef55.)
- DECIDED the all-in-one verb's network model: `yacd devnet` takes **NO name** —
  zero-config, ensures the SINGLETON cluster (k3d-yacd) + operator + one default
  network named "devnet". Additional networks via existing `yacd up NAME -f FILE`;
  `yacd down NAME` removes one network; `yacd devnet down` tears down the whole
  cluster. There is exactly ONE managed cluster; cluster-per-network is out of
  scope (would need a future `--cluster` opt-in). `devnet` is flag-light/zero-
  config; custom-spec networks use `up -f`. Doc §4/§5 updated.

Still open (§13): verb name (devnet vs dev/quickstart/start); funded-wallet
sequencing; up targeting precedence; defer `cluster` nouns (now effectively
yes — folded into devnet/devnet down/status); ephemeral-by-default.
