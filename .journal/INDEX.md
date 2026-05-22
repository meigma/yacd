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
