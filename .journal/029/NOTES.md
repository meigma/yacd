---
id: 029
title: Continue dbsync work
started: 2026-05-28
---

## 2026-05-28 07:03 — Kickoff
Goal for the session: Continue work from the last few sessions, specifically focusing on dbsync.
Current state of the world: Sessions 026-028 closed the primary-sidecar manual pass, public CardanoNetwork profiles and mainnet bootstrap, and public non-mainnet CardanoDBSync primary-sidecar support. Public mainnet db-sync remains unsupported; the likely next slice is an agile managed-Postgres snapshot restore prototype for public mainnet primary-sidecar db-sync.
Plan: Prime the session context first, then wait for the concrete dbsync request before selecting or creating an implementation worktree and starting the dev stack.

## 2026-05-28 07:12 — DB Sync snapshot investigation
Investigated upstream CardanoDBSync snapshot/restore behavior before implementing anything. Upstream state snapshots bundle PostgreSQL plus db-sync ledger state, are created after stopping db-sync with `cardano-db-tool prepare-snapshot` followed by `scripts/postgresql-setup.sh --create-snapshot`, and are restored with `scripts/postgresql-setup.sh --restore-snapshot <snapshot.tgz> <state-dir>`. Restore recreates the database by default, requires an empty ledger-state directory, and restores both ledger state and LSM data when applicable. Current official release `13.7.1.0` links only mainnet snapshot directories, with schema `13.7` and `13.6` compatibility. The public bucket is discoverable via the S3 list API under `cardano-db-sync/`; current `13.7` has two mainnet snapshots and sha256 sidecars. Takeaway for YACD: public mainnet db-sync support should treat snapshot restore as managed-Postgres-only bootstrap first, with snapshot URL/schema/block/arch/hash/backend as accepted identity inputs.

## 2026-05-28 07:22 — Node ledger snapshot terminology
Clarified that "ledger snapshot" is not the right top-level YACD product boundary. Cardano node ChainDB has immutable, volatile, and ledger snapshot subdirectories; the ledger snapshot is enough for restart/replay within a ChainDB, but not a complete portable environment restore by itself. Public node bootstrap should keep using Mithril's Cardano DB snapshot flow because the client discovers, downloads, verifies, and unpacks a full node DB plus ancillary ledger data. For local/YACD-created restore, the simple first slice should snapshot the full node DB/PVC plus generated network material, not only `db/ledger`. The CRD can still stay universal by pointing at a snapshot descriptor URL with component entries (`node`/`dbsync`) and metadata, while the operator dispatches each component to existing restore tools.

## 2026-05-28 07:37 — Snapshot manifest direction
Design leaning: make a small standardized YACD snapshot manifest the common contract, and have the CLI produce it. Support two packaging modes instead of forcing one: a self-contained YACD bundle for snapshots YACD creates, and an external-artifacts manifest for existing tooling outputs such as Mithril Cardano DB snapshots plus upstream db-sync `.tgz` snapshots. The CRD should reference the manifest URL/checksum and selected restore components, while the manifest records artifact URLs/checksums/formats/tool metadata. This keeps the operator universal without making users repackage large public artifacts.

## 2026-05-28 07:53 — Snapshot design draft
Created `.journal/SNAPSHOT_DESIGN.md` as the first proposal draft. The document uses the agreed outline: introduction, manifest specification, YACD-native snapshotting create/consume, public snapshot consumption, and open design questions. It recommends a CLI-produced manifest as the common contract, supports both self-contained YACD bundles and external public artifacts, and keeps the CRD restore surface small.

## 2026-05-28 07:38 — Session refocus: break-the-operator manual pass
Pivot for the rest of session 029. The user redirected from snapshot design to a focused adversarial manual test pass against the operator on the local Kind/Tilt stack. Scope:
- Goal: get the operator into "unexpected" states — infinite reconcile loops, changes that silently break the underlying node/db-sync, or unrecoverable error conditions. Explicitly out of scope: legitimate declared error states where the controller correctly says "I am failing because <X>" through events/status conditions.
- Bonus angle: evaluate whether legitimate error states are sufficiently observable — does status/events let a user reasonably infer what is wrong?
- Constraint: no CLI in this pass. All probing is via direct CRD edits or Kubernetes API manipulation (kubectl, raw resource mutations, child resource tampering).
- Approach the user asked for:
  1. Build a firm operator-architecture mental model.
  2. Spawn parallel subagents to theorize ways the operator could be broken.
  3. Synthesize, dedupe, and present a final candidate list for review before turning it into a test plan.
Cleanup: an erroneously created `.journal/030/` was removed; session 029 stays open and absorbs this work. Erroneous start commit `526aad3` remains in the journal history for accuracy; this NOTES entry records the pivot.

## 2026-05-28 07:38 — Erroneous session 030 cleanup
Removed `.journal/030/` from disk. The earlier `docs(journal): start session 030` push (`526aad3`) is preserved in history rather than rewritten — the next journal commit records the cleanup and refocus instead.

## 2026-05-28 10:24 — Dev stack mishap and recovery
The first `moon run root:dev-up` was run with Bash cwd silently shifted to `.wt/journal-jmgilman/` and brought up the OLD template-k8s stack (cluster `kind-template-k8s-dev`, registry `template-k8s-registry`, manager registering `NginxDeployment`). Caught it after seeing the wrong Tiltfile path in the output. Tore it all down (`docker rm -f template-k8s-registry`, `ctlptl delete cluster kind-template-k8s-dev`), cleared stale state under `.run/yacd-dev/`, killed a stray `tilt up --context kind-template-k8s-dev` process holding port 10350, then re-ran `moon run root:dev-up` from `/Users/josh/code/meigma/yacd` (master). Stack is now correctly `kind-yacd-dev` with `yacd-controller-manager-6bb4f5699-llg28` running and both CRDs installed. Lesson: every Bash command in this session must start with `cd /Users/josh/code/meigma/yacd` or use absolute paths — Bash cwd drifted at some point during the journal-write phase and stuck there.

## 2026-05-28 10:33 — A1 NOT-A-BUG: custom-profile CM watch is 1:1 but proportionate
Adversarial test A1 from the synthesized list: rapidly mutate a public-custom-profile ConfigMap and check whether the network controller's `Watches(&corev1.ConfigMap{}, customProfileConfigMapEventHandler)` (in `internal/controller/cardanonetwork/public_profile_source.go`) drives a reconcile storm with no real spec change.

Setup: namespace `break-a1`, ConfigMap `profile-cm` populated with all `SupportedCustomProfileKeys()` keys filled with placeholder values, CardanoNetwork `a1-net` referencing it. The bundle did NOT pass deeper validation — the network settled at `Degraded=True / UnsupportedSpec ("custom RequiresNetworkMagic value \"\" is not supported")`, observedGeneration=1, no owned Deployment ever rendered.

Measurement: 60s baseline window with no mutations = 0 reconciles. 30 mutations at 1Hz = 30 reconciles, exactly 1:1 with mutation events. Recovery tail (30s post-burst) = 0 reconciles. The workqueue stops dequeuing within ~1s of the last event; the `unsupportedSpec` path does not return an error to controller-runtime, so no rate-limited requeue treadmill ever spins up.

Verdict NOT-A-BUG because (1) 1:1 is proportionate not amplified (anything sublinear would require debouncing real content updates, which is the wrong tradeoff), (2) no Deployment exists so no pod-template annotation churn is possible in this configuration, (3) recovery is clean. A genuine storm would require >1:1 amplification or per-event Deployment rolls; neither occurred. The follow-up question of whether a *valid* custom-profile bundle with content updates would roll the primary Deployment per mutation is covered by A3 (artifact-CM drift), not A1.

Evidence under `.run/break-pass/a1/`. Namespace cleanly torn down, no orphans.

## 2026-05-28 10:39 — A2 NOT-A-BUG: NetworkStatusStale clears in ≤1s, no wedge
Adversarial test A2 from the synthesized list: patch `CardanoNetwork.spec.node.port` between 3001/3002 at ~5Hz for 60s and check whether the dependent CardanoDBSync gets wedged in `Progressing=True / NetworkStatusStale` because the network's `observedGeneration` falls behind `generation`.

Setup: namespace `break-a2`, same junk public-custom-profile CM trick as A1, network `a2-net`, fake external-Postgres password Secret `pg-pass`, CardanoDBSync `a2-dbsync` referencing the network. Both objects settled with their own legitimate `Degraded` (network: `UnsupportedSpec`; dbsync: `Progressing=True / NetworkArtifactsPending`).

Burst: 210 patches over 60s = effective 3.5Hz (kubectl latency caps the loop at sub-5Hz despite `sleep 0.2`). Across 53 1Hz status samples during the burst, the network's `obsGen` matched `gen` on every sample but one (a 1-generation lag at 72/71). The DBSync's `Progressing.reason` flipped to `NetworkStatusStale` on 6 of those 53 samples — each cleared on the following sample (≤1s). Recovery was immediate: the first post-burst sample at t+6s already showed the network at 210/210 and DBSync back on its baseline `NetworkArtifactsPending`. The DBSync watch on `CardanoNetwork` (no predicate, fires on every update including status-only) re-enqueues the DBSync as soon as the network's catch-up status is patched.

Verdict NOT-A-BUG because the theory's premise — observedGeneration falling persistently behind generation — does not hold in practice. The `UnsupportedSpec` early-exit path is fast enough that the network controller absorbs ~5Hz patches with at most 1 generation of lag. The DBSync enters `NetworkStatusStale` only momentarily and always recovers within 1-2 reconciles. The synthesis-list assumption that a single-threaded worker would fall arbitrarily behind under sustained editing was wrong for this controller's actual reconcile cost.

UX nit (not promoted): the `NetworkStatusStale` condition message reads "Referenced CardanoNetwork status has not observed the latest generation." No generation delta, no transient-hint. Worth filing as a minor message-polish later but not material here. Same comment applies to G2 in the synthesis list.

Evidence under `.run/break-pass/a2/`. Namespace cleanly torn down.

## 2026-05-28 10:56 — A3 BUG-B (caveated): artifact-CM corruption rolls pod 1:1 with no operator-side backoff
Adversarial test A3 from the synthesized list: corrupt the network artifact ConfigMap (`<network>-network-artifacts`) repeatedly and check whether the controller's delete-and-republish path causes sustained pod rolls.

Setup: namespace `break-a3`, local-mode CardanoNetwork `a3-net` (2Gi storage, 1 pool, conway, slot 100ms epoch 500). The cardano-testnet create-env init container took ~80s to publish the artifact CM with content hash `sha256:485591…`. Initial Deployment generation = 2. The first subagent run set up + completed phase 1 sampling cleanly but exited before driving the corruption loop and the recovery phase, so I picked up where it left off. (Note: the subagent appears to have called TaskUpdate against my parent task list and then declared the test "in progress, waiting for the monitor to fire" — this is an agent-side bug to remember next time we hand off long-running orchestration. Future test prompts should explicitly forbid subagent use of TaskCreate/TaskUpdate and require synchronous execution.)

Test execution: 16 total corruption iterations across two runs (initial subagent iter 0-3, my follow-up `run-finish.sh` iter 4-15), all of `data.topology.json` patched to a unique string at ~8s cadence. Phase 3 = 90s recovery window with no mutations.

Findings (from `.run/break-pass/a3/phase{1,2}-samples.tsv` and `finish-samples.tsv`):
- **Annotation stays in sync (BUG-A NOT confirmed):** `deploy_anno_uid` matched the live `cm_uid` on every stable sample, including transient sub-second windows where a new annotation was stamped before the CM `Get` returned the new UID (cache-vs-write race, not a divergence).
- **Sustained corruption = sustained rolls (BUG-B confirmed):** 16 corruption iterations produced exactly +16 Deployment generations (2 → 18), 16 owned-CM UID rotations, and 16 ReplicaSets (Kubernetes RS GC trimmed to 11 once `revisionHistoryLimit` defaulted in). Each iteration rolled the primary pod through Pending → Running. Operator pod itself stayed `1/1 Running` with no restarts; manager isn't crashing under pressure, it's just servicing every event.
- **Operator log evidence:** `configMapOperation` alternates between `"updated"` (controller patched data back in place) and `"created"` (controller recreated after kubectl-patch happened to delete-and-recreate the CM via merge semantics). `deploymentOperation:"updated"` fires on essentially every reconcile during the burst because the artifact-CM UID annotation on the pod template changes with each CM rotation. No rate limit or circuit between rotations.
- **Recovery is clean:** Last corruption at 10:54:32; from 10:54:33 onward `deploy_gen` froze at 18 for the full 90s recovery window, `cm_uid` stable, `net_ready=True`, `artifactsReady=True`. The controller does not enter a self-sustaining loop after pressure stops.

Why BUG-B is "caveated": the operator's behaviour is *correct* on each event in isolation — drift must trigger republish, and a republish via delete-then-create necessarily rotates the CM UID, which is the intentional rollout trigger for pod-template freshness. The gap is that there is no per-network rate limit on artifact-driven pod rolls. An adversary or buggy sidecar with permission to PATCH a single ConfigMap can keep the primary pod restarting indefinitely at the corruption cadence; with ~3s per roll on M4 Max + Kind, ~5s patch cadence holds availability under 50% indefinitely. Two reasonable mitigations to consider later: (a) only roll the Deployment when the artifact `data-hash` annotation actually changes (skip the roll when delete-and-recreate produced an identical content hash, which is the common case under adversarial corruption since the controller restores the same canonical content), or (b) a coarse per-network rolling-restart rate limit on artifact-recovery-driven rollouts. Either keeps the controller's drift-reaction correct while bounding pod-thrash damage.

Promoted from the synthesis list to "real finding worth filing." Filed as a Category-A finding but the root mitigation lives in `internal/controller/cardanonetwork/apply.go` (`artifactConfigMapNeedsRecovery` + `setDeploymentArtifactConfigMapUID`); design choice is whether the UID-change-as-rollout-trigger semantics should be replaced or rate-limited.

Evidence under `.run/break-pass/a3/`. Namespace cleanly torn down, no PVC leak.

## 2026-05-28 11:08 — A4 BUG-A (severe) + BUG-C + UX-GAP: placement peer toggle severs stable sidecar 1:1
Adversarial test A4 from the synthesized list: one CardanoNetwork, one stable CardanoDBSync at `primarySidecar` (winner), one toggler CardanoDBSync flipping `placement.mode` between `primarySidecar` and `dedicatedFollower` every 12s.

Setup: namespace `break-a4`, local-mode CardanoNetwork `a4-net` (reached Ready in ~20s), two CardanoDBSyncs against fake external Postgres. Baseline: stable's sidecar fully attached to the primary Pod (container set `cardano-node, ogmios, kupo, cardano-db-sync`), Deployment generation = 2, `DBSyncAttachmentReady=False/DBSyncAttachmentPending` (sidecar exists but the Pod isn't Ready because external pg-pass points at 127.0.0.1:5432 — that's fine for this test).

Burst: 10 toggles over 120s = 10 generation bumps on the primary Deployment (2 → 12), 84 `"Applied"` reconcile log entries with `deploymentOperation:"updated"`, and 10 actual container-set changes on the live primary Pod. On every `primarySidecar` toggle the network detached the `cardano-db-sync` sidecar from the Deployment template; on every `dedicatedFollower` toggle it re-attached it. Mid-cycle the stable DBSync's `SidecarMaterialReady` flipped True↔False 10 times with reason `PlacementConflict`/`applyBlocked`. Recovery (30s, no toggles): generation frozen at 12, attachment correctly matches last toggler mode.

Two root causes, both confirmed in code by the agent:
1. `internal/controller/cardanodbsync/placement.go:31-38` — `reconcilePlacement` treats `len(claims) > 1` as a **symmetric** block: both claimants get `applyBlocked / PlacementConflict`. No winner-by-CreationTimestamp or winner-by-UID tiebreak. The pre-existing stable claimant loses `SidecarMaterialReady=True` the instant a peer flips to `primarySidecar`.
2. `internal/controller/cardanonetwork/dbsync_sidecar.go:60-67` — `primaryDBSyncAttachment` returns no `Attachment` when `len(claims) != 1`, so `primaryWorkloadBuilder.Build(network)` renders the Deployment **without** the sidecar container. The resulting PodTemplateSpec diff is a real rollout, not just a status update.

Together they amplify each external toggle into a real Pod rollout that severs the cardano-db-sync sidecar's node-socket continuity and any in-progress db-sync work. The placementPeerEventHandler correctly uses `GenerationChangedPredicate`, so this is NOT a runaway watcher loop — but each external toggle is amplified into a full rolling-update and `applyBlocked` cascade. A hostile or merely indecisive second user with `cardanodbsync` create/edit permission can keep an unrelated user's stable primary-sidecar attachment in roll-detach-reattach perpetuity at the toggle cadence.

Severity reasoning (promoting this from synthesis 🟡 medium to high): YACD is currently single-tenant local-dev focused, but the multi-tenancy story is in scope as the operator matures (hosted cluster shared by a team is called out in DESIGN.md goals). Two reasonable mitigations to consider:
- **Stable-winner tiebreak:** in `primarySidecarClaims`, sort by `CreationTimestamp` (then UID) and pick claim[0] as winner; revoke its attachment only when *it itself* becomes non-attachable. Late competitors get `PlacementConflict` immediately on themselves but don't dethrone the incumbent.
- **Status-only conflict for non-winners:** even without a true tiebreak, the network can keep attaching to the current claimant until that specific claimant changes, treating other claims as informational PlacementConflict on themselves only.

UX-GAP (G4 in the synthesis list, now sharpened): the `PlacementConflict` message reads only "CardanoNetwork %q has multiple primarySidecar CardanoDBSync claims; exactly one primary-sidecar claim is allowed". It names neither (a) which DBSync is currently winning, (b) the names of the conflicting claimants, nor (c) which one the user should change. A user receiving this on `a4-dbs-stable` has no signal that `a4-dbs-toggler` is the cause. Mitigation: list the conflicting peers and identify the incumbent in the message.

Evidence under `.run/break-pass/a4/`. Operator log has 84 `Applied` reconcile entries during the 2-min burst, all with `deploymentOperation:"updated"`. Namespace cleanly torn down. Subagent followed the "no TaskCreate/TaskUpdate, synchronous execution" rules added after A3.

## 2026-05-28 11:22 — A5 NOT-A-BUG: Owns watch shields Ogmios Service faster than any external attacker
Adversarial test A5 from the synthesized list: delete the CardanoNetwork's Ogmios Service every 20s and check whether the dependent CardanoDBSync's `Synced` condition flaps on the 30s probe cadence.

Setup: namespace `break-a5`, local CardanoNetwork `a5-net` (Ready in ~165s), CardanoDBSync `a5-dbsync` with managed Postgres (PostgresReady+DBSyncReady briefly satisfied in ~225s, then dbsync container began crash-looping). DBSync never reached `Synced=True` in the time budget — baseline `Synced=False / RuntimeProbesPending` ("db-sync progress will be probed after workloads are ready"). Test ran the burst against this baseline.

Burst: 6 Ogmios Service deletions, 20s apart. Of 60 2-second samples during the burst, the Service was observed missing in 0 — the network's `Owns(&corev1.Service{})` watch fired faster than the 2-second sampler could catch a gap. Each cycle produced a fresh `resourceVersion` (8926 → 8981 → 9021 → 9059 → 9101 → 9140), proving the recreations happened, but the Service is restored in well under a second so there is never an observable hole the probe could fall into. Synced status transitions during the burst: 0. Ready status transitions: 0. Network OgmiosReady stayed True throughout 60 samples. Operator log shows no `NodeTipUnavailable` reason at any point.

Verdict NOT-A-BUG. Two reasons:
1. **`Owns` watch as defensive shield.** Unlike A3 (where the attack vector was DATA corruption that the controller has to detect→republish), here the attack is a resource DELETION the controller observes instantly via its watch and recreates in the same reconcile pass. The proposed flap is purely external-pressure-driven and self-extinguishing — the adversary's `kubectl delete` is racing the operator's `Owns` watch and losing every time.
2. **Probe path was never reached due to unrelated dbsync container crash-loop.** Even if the probe had been active, the test couldn't isolate the Ogmios-driven flap because the dbsync container was unstable (4 restarts at baseline). The DBSync stayed in `RuntimeProbesPending` (only Postgres was being probed; the Ogmios-tip path is gated on `FollowerNodeReady && DBSyncReady`, see `status.go:115`). Four brief samples in cycle 1 did reach the full probe and reported `SyncLagging` ("db-sync has not indexed any blocks yet"), but the lagging reason was DB-side, not Ogmios-side — even with a healthy Ogmios returning `nodeBlockHeight=103`.

Architectural observation worth keeping in TECH_NOTES later: `Owns(&corev1.Service{})` (and other Owns watches on instantly-recreatable resources) makes the operator robust against external resource-deletion attacks that A5-class theories typically target. The pattern from A3 — where the attacker mutates *content* the controller has to detect-then-fix — is the more productive surface for future probe-related tests; A4 confirmed that mutation-via-spec-toggle is similarly productive.

DBSync container crash-loop noted as a follow-up worth investigating: a fresh managed-Postgres CardanoDBSync against a local-mode network restarted the dbsync container 4 times within the test window. Likely a startup race with the follower-node socket; not investigated here.

Evidence under `.run/break-pass/a5/`. Namespace cleanly torn down.

## 2026-05-28 11:24 — Category A synthesis (pause for user review)
Five tests run; outcomes:

| Test | Theory | Verdict | Severity |
|------|--------|---------|----------|
| A1 | Custom-profile CM watch storm | NOT-A-BUG | — |
| A2 | NetworkStatusStale wedge via spec spam | NOT-A-BUG | — |
| A3 | Artifact CM corruption → Deployment roll loop | BUG-B (confirmed) | medium |
| A4 | Placement peer oscillation flap | BUG-A + BUG-C + UX-GAP | **high** |
| A5 | Runtime probe Synced oscillation via Ogmios deletion | NOT-A-BUG | — |

Two real findings:

**A3 — Artifact CM external corruption causes 1:1 pod rolls with no operator-side backoff.** An adversary or buggy sidecar with `patch configmaps` permission on `<network>-network-artifacts` can keep the primary Pod restarting at the corruption cadence indefinitely. Operator handles each event correctly; the gap is the absence of a per-network rate limit on artifact-recovery-driven rollouts. Two reasonable mitigations: (a) only roll the Deployment when the artifact `data-hash` annotation actually changes (skip the roll when delete-and-recreate produced identical canonical content), (b) coarse per-network rate limit on artifact-recovery rollouts. Code: `internal/controller/cardanonetwork/apply.go` — `artifactConfigMapNeedsRecovery` + `setDeploymentArtifactConfigMapUID`.

**A4 — Placement peer toggling severs an existing stable primary-sidecar attachment every cycle (severe).** `reconcilePlacement` treats `len(claims) > 1` as a *symmetric* PlacementConflict with no winner tiebreak, and the network's `primaryDBSyncAttachment` returns no attachment whenever conflict exists. A second user with `cardanodbsync` create/edit permission can keep an unrelated user's stable primary-sidecar attachment in a perpetual roll-detach-reattach cycle. Mitigation: stable-winner tiebreak by `CreationTimestamp` (then UID); only revoke an attachment when its specific holder is removed/changed. PlacementConflict message currently names neither the incumbent nor the conflicting peers (UX-GAP G4 sharpened). Code: `internal/controller/cardanodbsync/placement.go:31-38` + `internal/controller/cardanonetwork/dbsync_sidecar.go:60-67`.

Three non-findings:

**A1** — watch handler enqueues 1:1 with CM mutations as theorized, but at proportionate rates this is the correct behaviour (anything sublinear would risk debouncing real updates), and there is no Deployment churn because validation rejects pre-workload.

**A2** — single-threaded network worker comfortably keeps `observedGeneration` at parity under ~5Hz patches (max lag = 1 generation); DBSync enters `NetworkStatusStale` only momentarily and unsticks within 1-2 reconciles every time.

**A5** — the network's `Owns(&corev1.Service{})` watch restores deleted Services in <2s, well under the probe's 30s cadence, so external Service-deletion attacks cannot create a probe gap. Bonus: dbsync container crash-loop is a separate follow-up worth a closer look.

UX nits accumulated during Category A (G-list candidates): A2's `NetworkStatusStale` message has no generation delta; A4's `PlacementConflict` names neither the incumbent nor the conflicting peers. Both worth filing as message-polish later.

Pause for user review before starting Category B (Identity acceptance & spec immutability). Dev stack left running for B's tests.
