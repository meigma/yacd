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
