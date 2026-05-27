---
id: 025
title: Session 025
started: 2026-05-26
---

## 2026-05-26 21:00 — Kickoff
Goal for the session: start a new journal-backed work session; wait for the user's actual implementation or exploration request before making product changes.
Current state of the world: `master` is clean at `f5bbfbb` from PR #42, which fixed the dev stack by adding the `--default-cardano-testnet-image` override, chart value, and Tilt-local cardano-testnet rebuild path. The journal branch `journal/jmgilman` is clean and up to date, with sessions 022-024 read for context. Session 024 completed the post-refactor manual functional pass and left the dev stack stopped. The immediately preceding discussion explored the public-network db-sync topology problem: the current dedicated follower-node shape is clean for ownership but wasteful for preview/preprod/mainnet, and the leading architectural option was to keep `CardanoDBSync` as the service/config/status owner while letting `CardanoNetwork` be the sole primary Pod composition authority for any db-sync sidecar placement.
Plan: wait for the user's next request, then choose or create an implementation worktree, start `moon run root:dev-up` from that worktree before substantive implementation unless waived, and keep session notes updated at meaningful checkpoints.

## 2026-05-26 21:17 — Current master db-sync placement assessment
Reviewed current `master` (`f5bbfbb`) after the recent refactor stack for the public-network db-sync placement problem. Current code still has two distinct ownership domains: `CardanoNetwork` owns the primary Deployment and optional socket sidecars (Ogmios/Kupo/Faucet), while `CardanoDBSync` owns a separate two-container Deployment with a dedicated follower node plus db-sync, its state PVCs, pgpass/config, metrics Service, managed Postgres, and status. `CardanoDBSync` has no placement mode today; `spec.followerNode` only customizes the dedicated follower path.

Assessment: keep `CardanoDBSync` as the service/config/database/status owner and make `CardanoNetwork` the sole primary-Pod composition authority for any db-sync sidecar attachment. Do not try to make both controllers own the same Deployment. Add an explicit `spec.placement.mode` to `CardanoDBSync`, defaulting to the existing dedicated-follower behavior. In primary-sidecar mode, `CardanoDBSync` should reconcile DB/config/pgpass/state/metrics objects, while `CardanoNetwork` lists eligible claims and appends the db-sync init/container/volumes to the primary Deployment only when there is exactly one valid claim.

Key risks: current `CardanoNetwork` still rejects public mode, so public-network value needs a separate public-node/artifact builder slice; db-sync metrics defaults to port `8080`, which collides with the faucet in a shared Pod; primary-sidecar failure couples to the primary Pod's readiness and normal Service endpoints; config/Secret/PVC hash changes must roll the primary Deployment; sidecar dependency failures should detach or withhold attachment rather than wedge the primary node; multiple primary-sidecar claimants should produce `PlacementConflict` and attach none.
