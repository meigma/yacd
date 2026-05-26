# Session Journal

| ID  | Date       | Title | Status | Summary |
|-----|------------|-------|--------|---------|
| 001 | 2026-05-20 | Minimal design bootstrap | complete | Researched Yaci-adjacent tooling, established the initial YACD architecture direction, merged `DESIGN.md`, and recorded the first prototype plan. |
| 002 | 2026-05-20 | YACD foundation branding pass | complete | Rebranded the operator foundation as YACD, removed the template Nginx API/controller surface, merged PR #2, and left `master` ready for the first real environment API slice. |
| 003 | 2026-05-20 | First YACD environment prototype | complete | Added the first `CardanoNetwork` CRD, localnet plan package, read-only controller adapter, and managed Kind/Tilt dev-stack lifecycle. |
| 004 | 2026-05-20 | cardano-testnet tools image and init fragment | complete | Added the YACD `cardano-testnet` tools image, released `11.0.1-yacd.1`, and generated the first localnet init-container fragment from `localnet.Plan`. |
| 005 | 2026-05-21 | Primary CardanoNetwork workload | complete | Added the singleton primary node Deployment/PVC reconciliation path, localnet identity protection, manual Kind/Tilt proof, and dev rebuild/churn fixes. |
| 006 | 2026-05-21 | Primary node service, status, and readiness | complete | Completed the primary node Service, endpoint status, runtime readiness, and installed-operator Kind smoke for the phase-2 runtime path. |
| 007 | 2026-05-21 | Ogmios chain API | complete | Added Ogmios as the default `CardanoNetwork` chain API with sidecar, Service, status endpoint, readiness, compatibility checks, and protocol-level smoke coverage. |
| 008 | 2026-05-22 | Developer CLI foundation | complete | Added the first `yacd` developer CLI with config-driven deploy, readiness waiting, status/connection info, release wiring, and installed-operator smoke coverage. |
| 009 | 2026-05-23 | Phase 5 Kupo and faucet | complete | Added Kupo as a first-class chain API and merged the opt-in authenticated faucet/top-up vertical with CLI, Secret, sidecar, Service, and smoke coverage. |
| 010 | 2026-05-23 | Faucet E2E assessment and dev image fix | complete | Verified the CLI-to-faucet funding path end to end, fixed ko-compatible local faucet image wiring, merged PR #16, and cleaned up the dev stack/worktree. |
| 011 | 2026-05-23 | Phase 6 db-sync supporting service | complete | Added the first `CardanoDBSync` API-only CRD slice with typed spec/status, generated artifacts, scheme registration, and PR #17 merged. |
| 012 | 2026-05-24 | CardanoNetwork artifact ConfigMap | complete | Added exact localnet artifact publishing through a controller-owned ConfigMap, released the publish-capable tools image, merged PR #20, and cleaned up the session worktrees. |
| 013 | 2026-05-24 | CardanoDBSync controller runtime | complete | Added the first `CardanoDBSync` controller/runtime path with external Postgres, follower/db-sync workloads, artifact validation, status, and PR #23 merged. |
| 014 | 2026-05-24 | Managed Postgres for CardanoDBSync | complete | Added the managed Postgres runtime path for `CardanoDBSync`, hardened auth/identity behavior, merged PR #24, and cleaned up the dev stack/worktree. |
| 015 | 2026-05-24 | CardanoDBSync progress probes | complete | Added efficient Postgres/Ogmios db-sync progress probes, released the hash-enriching tools image, merged PR #31, and cleaned up the session worktrees. |
| 017 | 2026-05-25 | ctrlkit foundation | complete | Added and integrated shared controller utility foundations while keeping Cardano/YACD domain contracts outside `ctrlkit`, then merged PR #33. |
| 018 | 2026-05-26 | dbsync planner package refactor | complete | Split `internal/cardano/dbsync` into a localnet-style layout with godocs, errors.Join validation, renamed Runtime disable booleans, exported `DefaultInsertOptions`, stable Spec JSON tags, and a frozen `DatabaseIdentityFingerprint` wire shape, then merged PR #35. |
| 019 | 2026-05-26 | localnet planner package refactor | complete | Split `internal/cardano/localnet` into a per-responsibility file layout mirroring `dbsync`, added a wire-tag stability note, renamed the private path helper, and merged PR #36 with the public API unchanged. |
