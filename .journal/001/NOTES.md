---
id: 001
title: Minimal design bootstrap
started: 2026-05-20
---

## 2026-05-20 12:50 — Kickoff
Goal for the session: draft a minimally viable root `DESIGN.md` for YACD that captures the initial vision and architecture strongly enough to bootstrap the first real prototype without trying to resolve every unknown.
Current state of the world: the repo is a fresh downstream copy of `../template-k8s`, still carrying the template `NginxDeployment` API and chart names. The product direction is a Kubernetes operator for Cardano developer environments: local-first, Kind/Tilt-friendly, namespace-scoped where practical, and aimed at builders rather than validators or stake pool operators. The operator should eventually stand up Cardano networks plus useful developer services such as Ogmios, Blockfrost-like APIs, or related tooling.
Plan: start with research before writing design text. First investigate `bloxbean/yaci` and adjacent Yaci tooling to determine whether it can be reused, partially reused, or treated mainly as influence for YACD's first prototype. Preserve findings in these notes as the investigation progresses.
