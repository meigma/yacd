---
id: 040
title: TEST_REPORT F0 assessment
date: 2026-05-29
status: abandoned
repos_touched: [this-repo]
related_sessions: [029]
---

## Goal
Continue fixing the remaining `.journal/TEST_REPORT.md` issues, starting with
F0: public mainnet network creation fails because the generated network
artifact ConfigMap exceeds Kubernetes' 1 MiB object-data cap.

## Outcome
No production implementation was landed. The session became an assessment of F0,
then was closed after deciding that the next attempt should take a different
route. F0 remains open.

## Key Decisions
- The current public-network path is the root problem: it copies the public
  profile files into one owned `<network>-network-artifacts` ConfigMap and
  mounts that ConfigMap directly into the primary Pod as `/profile`.
- Splitting the raw public profile across multiple ConfigMaps would probably
  unstick mainnet, but it would preserve the public/local architecture mismatch
  and keep Kubernetes object storage as the artifact transport.
- Gzipping the mainnet profile files is small enough for a single ConfigMap in
  byte-count terms. The checked-in mainnet profile files are 1,561,240 bytes raw
  and 708,172 bytes when gzipped per file, leaving 340,404 bytes below the
  1,048,576-byte Kubernetes ConfigMap data limit before the small generated
  `connection.json`.
- A naive compressed ConfigMap would push ungzip knowledge into every downstream
  consumer. The better direction is to make public networks use an init-time
  materialization/publisher model closer to local networks, with a compact
  profile source and runtime files materialized into the Pod filesystem.
- The curated public profile files are non-secret public network material. Custom
  public profiles can currently be sourced from a Secret, but the resulting
  artifact publication is still a non-secret contract and must not be used for
  private keys or credentials.

## Changes
- No implementation changes.
- Journal closeout only: this summary, `.journal/INDEX.md`,
  `.journal/TECH_NOTES.md`, and the final `.journal/040/NOTES.md` entry.

## Open Threads
- F0 still needs an implementation. The likely next slice is to redesign public
  profile materialization so the primary Pod gets files from an init container
  and a compact artifact source, rather than directly mounting a raw ConfigMap.
- If an HTTP-serving sidecar or node-local file server is added later, it should
  be treated as a downstream consumption convenience. It does not replace the
  primary Pod's need to materialize profile files before `cardano-node` starts.
- Re-run the mainnet probes from `.journal/TEST_REPORT.md` after F0 is fixed;
  the original F1/F3 probes were blocked by this ConfigMap-size failure.
- The singleton dev stack was already running for another recorded worktree
  (`feat-cli-host-access-ports`) and was not stopped by this session closeout.

## References
- `.journal/TEST_REPORT.md` - F0 reproduction and original suggested fixes.
- `.journal/029/SUMMARY.md` - adversarial test pass that discovered F0.
- `internal/controller/cardanonetwork/plan.go` - public profile artifacts are
  copied into `ArtifactData`.
- `internal/controller/cardanonetwork/artifacts.go` - artifact data is written
  into one ConfigMap.
- `internal/controller/cardanonetwork/resources.go` and
  `internal/controller/cardanonetwork/containers.go` - public profiles are
  mounted directly from that ConfigMap into the primary Pod.
