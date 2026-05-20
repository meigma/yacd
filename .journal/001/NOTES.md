---
id: 001
title: Minimal design bootstrap
started: 2026-05-20
---

## 2026-05-20 12:50 — Kickoff
Goal for the session: draft a minimally viable root `DESIGN.md` for YACD that captures the initial vision and architecture strongly enough to bootstrap the first real prototype without trying to resolve every unknown.
Current state of the world: the repo is a fresh downstream copy of `../template-k8s`, still carrying the template `NginxDeployment` API and chart names. The product direction is a Kubernetes operator for Cardano developer environments: local-first, Kind/Tilt-friendly, namespace-scoped where practical, and aimed at builders rather than validators or stake pool operators. The operator should eventually stand up Cardano networks plus useful developer services such as Ogmios, Blockfrost-like APIs, or related tooling.
Plan: start with research before writing design text. First investigate `bloxbean/yaci` and adjacent Yaci tooling to determine whether it can be reused, partially reused, or treated mainly as influence for YACD's first prototype. Preserve findings in these notes as the investigation progresses.

## 2026-05-20 12:53 — Yaci repo split
Research checkpoint: `bloxbean/yaci` itself is not a Kubernetes-native environment manager; it is a Java Cardano mini-protocol library. The closest adjacent projects are `bloxbean/yaci-devkit` for local devnet orchestration and `bloxbean/yaci-store` for indexing/datastore/API behavior. Current investigation is using fresh GitHub metadata plus local shallow clones under `/tmp/yacd-yaci-research/`.
Current read: YACD probably should not treat `yaci` alone as the comparable product. The meaningful comparison is the Yaci stack: DevKit + Store + optional Ogmios/Kupo + core Yaci protocol library.

## 2026-05-20 12:56 — Yaci initial assessment
Research checkpoint: Yaci core is a real bespoke Java mini-protocol implementation, not just a wrapper over Ogmios or cardano-cli. Yaci DevKit is the closest product analogue to YACD, but it is process/home-directory/Docker-Compose oriented and delegates much of the runtime to real Cardano binaries plus optional Ogmios, Kupo, Yaci Store, and Yano. Yaci Store is a plausible optional indexer/API component, with useful Blockfrost-compatible ambitions, but its Java/Spring footprint and active compatibility/correctness issue surface make it a better optional profile than a required MVP dependency.
Current recommendation: let the Yaci stack strongly influence the first YACD prototype, but do not embed Yaci core in the Go operator or reuse DevKit as the operator control plane. Prototype a Kubernetes-native CRD that owns Cardano node resources and optional developer services; treat DevKit and Store as references or workload components to test, not as the architecture's center of gravity.
