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

## 2026-05-28 11:34 — Starting Category B; created TEST_REPORT.md
User reviewed Category A and asked for `.journal/TEST_REPORT.md` as a running log of confirmed failures: one section per failing test with test description, observed failure, and suggested fixes. Seeded the file with A3 and A4 entries. NOT-A-BUG tests are deliberately not included in TEST_REPORT.md — that file is the bug log; full pass record stays here in NOTES.md. Pushed as commit `6cb9a38`.

## 2026-05-28 11:42 — B1 BUG-B + BUG-A + UX-GAP: status-fingerprint forgery permanently bricks CardanoNetwork
Adversarial test B1 from the synthesized list: forge `CardanoNetwork.status.network.networkFingerprint` (and/or `localnetFingerprint`) via the status subresource and observe whether the controller (a) restores it, (b) accepts it silently, or (c) enters an unrecoverable Degraded state.

Setup: local-mode CardanoNetwork `b1-net` (Ready in 21s). Baseline fingerprints set identically on the CR `status.network.{network,localnet}Fingerprint` and on the primary node-state PVC annotations `yacd.meigma.io/{network,localnet}-fingerprint`. Three sub-tests run in sequence.

**B1a — forge `status.network.networkFingerprint` only:** the forged value `deadbeef-forged-network` is silently retained for the full 30s observation window. Deployment generation unchanged. The CR continues to report `Ready=True / ReconcileSucceeded` with a *lying* status fingerprint. Root cause: `For(&CardanoNetwork{}, ctrlbuilder.WithPredicates(predicate.GenerationChangedPredicate{}))` filters out status-only updates, so no reconcile is triggered by the forgery. `setNetworkIdentityStatus` (status.go:167) only runs after a successful primary-workload apply, so the controller never overwrites the forged status. This is BUG-A (lying status) with effectively unbounded persistence — only an unrelated reconcile trigger (owned-child event, manager restart, real spec edit) would refresh the field.

**B1b — forge both `networkFingerprint` and `localnetFingerprint`:** same outcome. Both forged values retained, `Ready=True`. Same root cause.

**B1c — forge the PVC localnet-fingerprint annotation while status is also forged:** this is where the bomb goes off. The controller's PVC apply detects the PVC annotation drift (the live annotation does not match the desired plan's fingerprint, as expected since both have been tampered with). `validateAcceptedNetworkFingerprint` (callbacks.go:153) runs *first* and consults *only* the CR status. The status says `localnetFingerprint=cafebabe-forged-localnet`; the freshly computed plan says `localnetFingerprint=8cf50c80…`. Mismatch → reject. `Ready=False`, `Degraded=True / UnsupportedLocalnetChange`, message "delete and recreate the CardanoNetwork to change network parameters." Restoring the PVC annotation to its baseline does *not* recover the CR: the status check fires before the PVC check, the forged status still says `cafebabe…`, and `setNetworkIdentityStatus` only runs *after* a successful workload apply — which never happens because validation rejects. Spec edits to `node.port` bump generation and re-trigger reconcile, but each reconcile re-rejects on the same status check. The CR is bricked.

Two sources of truth for accepted-identity, with the more easily forged one (`status.network.*` via subresource patch) acting authoritative. RBAC note: the `cardanonetworks/status` subresource patch verb is granted to the controller's own ServiceAccount per the kubebuilder marker (`+kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanonetworks/status,verbs=get;update;patch`), and is commonly granted independently to operators/admins. An admin who can edit *only* status (a posture often considered safer than spec edits) can brick the CR.

UX-GAP: every condition message blames the user — "localnet inputs changed; delete and recreate to change network parameters" — when the actual cause is a forged status field that no spec change can undo. Misleading for an honest user who hits this via tooling that scrambles status (e.g., a backup/restore tool that round-trips through subresource patches).

Evidence under `.run/break-pass/b1/`. Namespace cleanly torn down. Will add as TEST_REPORT.md section before moving to B2.

## 2026-05-28 11:49 — B2 UX-GAP: DB identity forgery brick is technically recoverable, but message lies
Adversarial test B2 from the synthesized list: forge `CardanoDBSync.status.database.acceptedIdentityFingerprint` via the status subresource and observe whether the controller goes Degraded, restores, or bricks the same way B1 did.

Setup: local CardanoNetwork `b2-net` (Ready in ~25s), CardanoDBSync `b2-dbsync` with managed Postgres. `acceptedIdentityFingerprint` was published in ~8s (set early, well before sync starts). Baseline FP `6c25b26f…`, PostgresReady=True, Degraded=False.

**B2a — forge status fingerprint to `deadbeef-forged-db-identity`:** unlike B1's quiet network controller, the DBSync controller picked up the change within ~2s and flipped to `Ready=False / Degraded=True / UnsupportedDatabaseIdentityChange`. The likely reason it re-enqueued despite `GenerationChangedPredicate` is that managed Postgres apply is constantly producing owned-child events (Deployment progressing, Pod state changes); B1's quiet local-only network had no such background churn. Forged value persisted for the full 30s observation window — controller did NOT restore from the PVC annotation that still holds the truth (per the agent's read, `pvc.metadata.annotations["yacd.meigma.io/dbsync-database-identity"]`).

**B2b — spec bump (resources.limits.memory=256Mi) to force reconcile:** Degraded stayed. Generation went 1→2, observedGeneration caught up to 2. Reject message: *"CardanoDBSync database-affecting inputs changed from accepted identity; delete and recreate the CardanoDBSync with a fresh or compatible external database"*. Managed Postgres Deployment generation did NOT bump (the apply path is short-circuited before workload patch). So far: bricked from the user's perspective.

**B2c — recovery probe:** patched status back to baseline FP via subresource (16s no recovery because `GenerationChangedPredicate` still suppresses status-only re-enqueue), then bumped spec.resources.limits.memory=300Mi to force a real reconcile. Within 2s: Degraded=False, PostgresReady=True, observedGeneration=3. CR is fully recovered. No CR delete required.

So B2 is brick-then-recover, not brick-then-stuck (B1). Two architectural differences from B1 matter: (a) the DBSync controller already stamps the accepted identity onto the PVC annotation as a separate truth source, so the cluster *has* the correct value to recover from; (b) but `validateAcceptedDBSyncDatabaseIdentity` (`internal/controller/cardanodbsync/apply.go:184-194`) trusts `status.database.acceptedIdentityFingerprint` and only falls through to the PVC annotation when status is empty. The controller has the right anchor; it just refuses to use it when status is non-empty.

UX-GAP (severity-medium-but-load-bearing): the reject message tells the user to *"delete and recreate the CardanoDBSync with a fresh or compatible external database"*. The actual fix is a status patch back to the value still present on the PVC annotation, plus a benign spec bump. An honest user following the documented remediation will delete their CR and lose the managed Postgres data (and the dedicated follower-node PVC and the dbsync state PVC). The message also doesn't name which field drifted — image? db name? user? port? password key? — so even an expert user has no diagnostic signal that the divergence is in the status field rather than a real spec change.

Comparison to B1: both confirm "status subresource is a privileged write path that bypasses spec validation"; B1 bricks permanently because the network controller can't escape the loop, B2 bricks user-visibly but is recoverable for someone who already knows the trick. From a user's-eyes perspective both are bug-grade; from an architecture perspective the DBSync side is closer to a fix because the PVC anchor already exists.

Evidence under `.run/break-pass/b2/`. Namespace cleanly torn down.

## 2026-05-28 11:58 — B3 NOT-A-BUG: PR #46 defense holds because PVC annotation backfill works
Adversarial test B3 from the synthesized list: clear `status.database.acceptedPlacementMode` via subresource patch and immediately flip `spec.placement.mode` from `primarySidecar` to `dedicatedFollower`, replicating the session-026 failure mode the PR #46 fix was designed to prevent.

Setup: local CardanoNetwork `b3-net` + CardanoDBSync `b3-dbsync` at `primarySidecar` with managed Postgres. Baseline `acceptedPlacementMode=primarySidecar` set within ~8s. Crucially, the agent inventoried owned material and found the placement-mode annotation `yacd.meigma.io/dbsync-placement-mode=primarySidecar` stamped on PVC `b3-dbsync-dbsync-state`, ConfigMap `b3-dbsync-dbsync-config`, and Secret `b3-dbsync-dbsync-pgpass`.

B3-control (sanity): flip placement without clearing status. Within 2s `Degraded=True, reason=UnsupportedDatabaseIdentityChange`, message "CardanoDBSync placement changed from accepted placement 'primarySidecar' to 'dedicatedFollower'; delete and recreate the CardanoDBSync with a fresh or compatible database." Restoring spec to primarySidecar cleared Degraded within 2s. Defense works.

B3a — clear status, then flip: status clear succeeded (`acceptedPlacementMode` removed from `status.database`, only `acceptedIdentityFingerprint` and `authSecretName` remained). Spec was then flipped to `dedicatedFollower`. Within 2s `status.database.acceptedPlacementMode` was **back to `primarySidecar`** and Degraded fired with the same message. The controller backfilled from `acceptedDBSyncPlacementModeFromPVC` (`internal/controller/cardanodbsync/apply.go:325`) — the PVC annotation read path that runs whenever status is empty. No follower PVC was created, no dedicated db-sync Deployment appeared, the primary-sidecar bundle stayed exactly as it was, and `acceptedIdentityFingerprint` was preserved.

B3b — flip back: cleanly recovered. Same backfill pattern; database state untouched.

This is a really useful finding for the *other* tests in this category. The fix the B1 and B2 entries recommend ("fall through to the PVC annotation when validating accepted identity") **already exists in the codebase for this one field** (`acceptedPlacementMode`), implemented as `currentAcceptedDBSyncPlacementMode` at apply.go:271 → `acceptedDBSyncPlacementModeFromPVC` at apply.go:325. The fix for B1 is to add an analogous `acceptedNetworkFingerprintFromPVC` and use it in `validateAcceptedNetworkFingerprint`; the fix for B2 is to add `acceptedDBSyncDatabaseIdentityFromPVC` and use it in `validateAcceptedDBSyncDatabaseIdentity`. The codebase has the right pattern; it just hasn't been generalized across all three accepted-identity fields. Sharpening B1/B2 recommendations in TEST_REPORT.md.

Evidence under `.run/break-pass/b3/`. Namespace cleanly torn down.

## 2026-05-28 12:04 — B4 NOT-A-BUG: PVC-side localnet-FP tamper is cleanly detected and self-recovers
Adversarial test B4 from the synthesized list: forge the PVC's `yacd.meigma.io/localnet-fingerprint` annotation directly (status untouched, unlike B1c which forged both) and observe detection + recovery.

B4a (localnet-FP forgery on PVC, local mode): caught at `validateLocalnetFingerprint` (callbacks.go:184), Ready=False / Degraded=True / `UnsupportedLocalnetChange` within 2s. Message verbatim: *"CardanoNetwork localnet inputs changed for PVC break-b4/b4-net-node-state; delete and recreate the CardanoNetwork to change network parameters"* — names the PVC, which is enough for a user to find the drifted annotation. Generation didn't bump (the controller updates status but cannot advance through the PVC apply step).

B4b (recovery): restoring the PVC annotation cleared Degraded by the very first 2s sample. The `Owns(&corev1.PersistentVolumeClaim{})` watch fires on the annotation edit, re-enqueues reconcile, validation passes, Ready=True. No spec bump required.

B4d (network-FP forgery on PVC, local mode): silently overwritten by `mergeOwnedAnnotations` (annotations.go:46) during the normal PVC apply. No validation, no rejection — local mode treats localnet-FP as the canonical identity, and the network-FP annotation is derived. This is intentional and correct; a future reviewer comparing public-mode behavior should note that the network-FP annotation has different drift semantics in public mode, where it is the canonical identity (no localnet plan exists).

Key takeaway for the category: this is direct evidence that PVC-based validation works correctly when wired up — Owns watch + auto-recovery on annotation restore, clean error path, message names the affected PVC. The B1 + B2 fix recommendations (fall through to PVC annotation when status is empty) are well-supported by this result. The codebase already has the pattern working for one field (B3's `acceptedPlacementMode`) and proves the pattern works correctly at the PVC level (B4); the gap is that B1 and B2's identity checks bypass the PVC anchor in favor of forgeable status.

Evidence under `.run/break-pass/b4/`. Namespace cleanly torn down.

## 2026-05-28 12:11 — B5 NOT-A-BUG: image drift bounced in <2s, two side notes
Adversarial test B5 from the synthesized list: hand-edit the managed Postgres Deployment's pod-template image via `kubectl set image` and observe whether the controller restores it before a wrong-image Pod can run against the existing data dir.

B5a (postgres:16.4-alpine — major version downgrade): time-to-restore <2s. Only one ReplicaSet (`6fc9986c44`, image 17.2) ever existed; no 16.4 image was pulled. The Pod that came up after the rollover already used 17.2 because the operator's Mutate restored the template before the kubelet pulled the new image. Data-dir incompatibility scenario (BUG-B) never materialized.

B5b (postgres:17.4-alpine — minor version drift): same outcome, <2s restore. The Mutate is field-blind w.r.t. image semver — it replaces `Containers` wholesale via `ctrlresources.MutateDeployment` in `internal/controller/cardanodbsync/callbacks.go::mutateDBSyncDeployment`. Either drift direction is corrected identically.

B5c (adversarial label + cpu limit): cpu limit was restored in <2s (it's inside `containers[]` which is replaced wholesale). The adversarial pod-template label `adversarial=b5c` PERSISTED through the test window. Root cause: `ctrlmetadata.OverlayStringMap` is overlay-only — desired keys win on collision, but user-injected keys not in desired are preserved. This is the standard Kubernetes controller pattern (don't strip user metadata) but it does mean an outside actor with `update deployment` on the operator's namespace can stamp arbitrary labels onto the managed Postgres Pod and force a rollout. Low severity (no runtime behavior change) but a "stamp-and-force-rollout" attack surface worth knowing about.

UX-GAP (not promoted, low impact): the operator silently overrides `kubectl set image` with no info-level log line or transient condition. A user attempting to apply a Postgres minor-version upgrade by `kubectl set image` would see their change disappear and have no signal that the operator is the cause. A single info-level "restored Postgres image to spec (postgres:17.2-alpine)" log line on each Mutate-driven revert would help.

Evidence under `.run/break-pass/b5/`. Namespace cleanly torn down.

## 2026-05-28 12:17 — B6 BUG-A: storage expansion against non-expandable class is invisible in CR status
Adversarial test B6 from the synthesized list: patch `spec.node.storage.size` UP on a CardanoNetwork backed by a StorageClass without `allowVolumeExpansion` (the Kind default `standard` class).

Setup: local CardanoNetwork `b6-net` at 2Gi initially. Default StorageClass is `standard` with `allowVolumeExpansion=<unset>` (treated as false). Network Ready=True.

B6a (2Gi → 5Gi): CR generation went 1→2 but observedGeneration stayed at 1 for the full 30s window. Live PVC `spec.resources.requests.storage` stayed 2Gi; `status.capacity.storage` unchanged. Zero events emitted on the PVC (API server rejects the resize synchronously without recording an event). **CR conditions: `Ready=True / Degraded=False`, message `"Primary node, artifact publisher, and chain API resources are applied"` — the controller did not propagate the failure to status.** The only outward signal is the generation/observedGeneration delta. The actual error appears only in operator logs at ERROR level: `persistentvolumeclaims "b6-net-node-state" is forbidden: only dynamically provisioned pvc can be resized and the storageclass that provisions the pvc must support resize`. Logged 14 times in the first 80s under controller-runtime's exponential backoff.

B6b (revert 5Gi → 2Gi): observedGeneration jumps from 1 to 3 within ~10s, forbidden errors stop, CR returns to a fully-applied state. Recovery is clean.

B6c (2Gi → 3Gi): identical outcome to B6a — the rejection is not size-dependent, it's StorageClass-capability-dependent.

Code (per the agent's reads): `internal/ctrlkit/storage/storage.go:78-112` `PersistentVolumeClaimDriftFor` only flags `RequestedStorageClass`, `StorageClass`, `AccessModes`, and `StorageDecrease`. Storage *expansion* is intentionally allowed through. The controller PATCHes the PVC's `spec.resources.requests.storage`, the API server returns 403 Forbidden, the error bubbles out of Reconcile as a generic error — but `Ready`/`Degraded`/conditions are never updated to reflect it.

This is **BUG-A** under our bar: the operator silently swallows the failure as far as CR status is concerned while the live PVC stays at the old size. A user runs `kubectl get cardanonetwork b6-net` and sees `Ready=True`, believes the expansion worked, and walks away. Severity is **medium** because (a) recovery is clean (revert the size), (b) no data loss, but (c) the user-visible misinformation is real and the underlying Kubernetes error message is excellent and not surfaced anywhere a user typically looks.

Recovery is non-destructive: revert spec storage to baseline, controller proceeds normally. This is BUG-A, not BUG-B.

Fix direction: when `applyPrimaryPersistentVolumeClaim` returns a forbidden or `IsInvalid`-type API error on PVC resize, classify it as a typed `statusConditionError` (the controller already uses this pattern for other validate paths — see A4 / B1 / B2) with reason `StorageExpansionRejected` and a message that propagates the API server text. The user sees the "StorageClass must support resize" hint in `kubectl describe cardanonetwork` without needing operator-log access. The same pattern likely needs to apply to the DBSync controller's PVC apply paths (state PVC and managed Postgres PVC, both expandable in CRD spec).

Evidence under `.run/break-pass/b6/`. Namespace cleanly torn down.

## 2026-05-28 12:20 — Category B synthesis (pause for user review)
Six tests run; outcomes:

| Test | Theory | Verdict | Severity |
|------|--------|---------|----------|
| B1 | Status-FP forgery (CardanoNetwork) | BUG-B (perma-brick) + BUG-A (lying status) + UX-GAP | **high** |
| B2 | Status-FP forgery (CardanoDBSync DB identity) | UX-GAP (brick-but-recoverable) | medium |
| B3 | Accepted-placement-mode clear + flip | NOT-A-BUG (PVC annotation backfill works) | — |
| B4 | PVC localnet-FP annotation tamper | NOT-A-BUG (clean detect + auto-recover) | — |
| B5 | Managed-Postgres image drift | NOT-A-BUG (<2s Mutate restore, no wrong-image Pod) | — |
| B6 | Storage expansion onto non-expandable StorageClass | BUG-A (silent swallow) + UX-GAP | medium |

Three real findings:

**B1 — Status-fingerprint forgery permanently bricks CardanoNetwork (high).** A status-subresource patch (a verb commonly granted to admins) can drive the CR into an unrecoverable Degraded state. `validateAcceptedNetworkFingerprint` reads only from status, and `GenerationChangedPredicate` filters status-only updates so the controller never overwrites the forged value. Combined with PVC annotation drift, the CR is bricked permanently absent CR delete (which loses chain state). Documented in TEST_REPORT.md with three fix options; the cleanest is to fall through to the PVC annotation as the authoritative anchor.

**B2 — DB identity forgery brick is technically recoverable but message demands CR delete (medium).** Same architectural problem as B1 but the DBSync controller's owned-Postgres churn keeps reconciles firing so the user sees the Degraded immediately. Recovery is possible (status patch + spec bump) but the printed message says *"delete and recreate the CardanoDBSync with a fresh or compatible external database"* — an honest user following the documented remediation deletes the CR and loses all the managed Postgres + state PVC data. Critically, the controller already stamps the truth on the PVC annotation `yacd.meigma.io/dbsync-database-identity` and just refuses to consult it when status is non-empty.

**B6 — Storage expansion failure is invisible in CR status (medium).** When the underlying StorageClass doesn't allow expansion (Kind's default), the controller PATCHes the PVC, the API server returns Forbidden with an excellent error message, but the controller doesn't propagate it to conditions. The user sees `Ready=True / Degraded=False` and believes their resize worked. The signal exists only in `observedGeneration < generation` (subtle) and operator logs (inaccessible to typical users).

Three non-findings worth recording:

**B3** — PR #46's defense holds because `acceptedDBSyncPlacementModeFromPVC` (`apply.go:325`) backfills from the PVC annotation when status is empty. This is the *exact pattern* B1 and B2's fixes recommend — the codebase already has the right idea for one accepted-identity field but hasn't generalized it across all three.

**B4** — PVC localnet-FP annotation tamper is cleanly detected by `validateLocalnetFingerprint` and self-recovers on annotation restore via the `Owns(&corev1.PersistentVolumeClaim{})` watch. Direct evidence that PVC-anchored validation works correctly when wired up.

**B5** — Mutate restores the managed Postgres image in <2s; no wrong-image Pod ever runs against the existing data dir. Two side observations: (a) user-injected pod-template labels persist via `OverlayStringMap` overlay semantics — low-severity attack surface for label stamping + forced rollout, and (b) silent image revert with no info-level log line is a minor UX gap.

Category-B fix theme: **align all three accepted-identity validation paths around the PVC annotation as the source of truth.** The pattern already exists in the codebase (B3's backfill, B4's PVC-annotated check that works) — B1's brick and B2's UX-trap both vanish when their respective validate functions fall through to the PVC annotation. The B6 fix is independent: surface PVC API errors as typed `statusConditionError`s on the CR.

Pause for user review before starting Category C (Owned-child tampering: replicas=0, ownerReference strip, foreign-owned same-name child, SA edit, hand-edit non-mutated fields). Dev stack left running.

## 2026-05-28 12:32 — C1+C2+C3 UX-GAP cluster (silent override pattern)
Adversarial tests C1, C2, C3 from the synthesized list, combined into one agent run against a single local CardanoNetwork `c123-net` (Ready in ~20s) to save redundant network startup. Skipped C4 as redundant with the already-confirmed A3 BUG-B (sustained sidecar persistence would just re-confirm "no operator-side backoff").

**C1 (kubectl scale --replicas=0):** Mutate restored `spec.replicas` to 1 in <1s (faster than the 1s sampler could observe the 0). Owned-resource watch fired within ~10-300ms of the user write. Pod rollout took ~11s back to availableReplicas=1, with Ready briefly flipping False (reason DeploymentProgressing) then True. Generation went 1→3 (user patch +1, Mutate +1). The brand-new Pod that came up after the scale-down had the correct replica spec because the restore beat the kubelet's pod creation. Verdict: UX-GAP only — Mutate works perfectly; there's just no signal to the user that their `kubectl scale` was reverted.

**C2 (kubectl set serviceaccount default):** Mutate restored `spec.template.spec.serviceAccountName` to `c123-net-artifact-publisher` in <1s. The new Pod that came up after the SA-change-triggered rollout had the correct SA — no Pod ever ran with the wrong SA token (BUG-B not realized). Same ~11s rollout. Verdict: UX-GAP only — silent override.

**C3 (kubectl patch svc selector):** API server ACCEPTED the patch (Service selector is mutable). Mutate restored `spec.selector` in <1s via `MutateService` (`internal/ctrlkit/resources/resources.go:57-63`, `maps.Clone(desired.Spec.Selector)`). The EndpointSlice controller never observed the selector flip — all 30 1s samples showed 1 endpoint address, no blackout. CR conditions stayed Ready=True throughout. Verdict: NOT-A-BUG with UX-GAP caveat — restore was too fast for any in-cluster traffic to notice, but the user gets no signal their selector edit was reverted.

**Cross-test observation: the silent-override UX gap is consistent across all three field types and matches B5's same finding on `kubectl set image`.** When the operator wins a restore race, nothing in `kubectl describe cardanonetwork` or in events tells the user their `kubectl` write was undone. For a curated dev-environment operator the contract is fine (operator wins) but the visibility gap is real for a maintainer trying to figure out why their patch had no effect. Worth considering a Warning-level event or a transient condition reason like `OwnedSpecOverridden` — not promoted to a TEST_REPORT entry yet because it spans multiple tests and lacks a sharp single-mitigation surface; will revisit at end-of-category.

Evidence under `.run/break-pass/c123/`. Namespace cleanly torn down.

## 2026-05-28 12:38 — C5 NOT-A-BUG: sharp overlay/replace/passthrough boundary, no accumulation
Adversarial test C5 from the synthesized list: inject user-owned fields of various types into the live primary Deployment, force a CR spec bump to drive a fresh reconcile, repeat to test for accumulation.

The agent confirmed three field-handling categories with sharp boundaries (codified in `internal/controller/cardanonetwork/callbacks.go::mutatePrimaryDeployment` and `internal/ctrlkit/resources/resources.go::MutateDeployment`):

1. **Overlay (user keys persist, operator-desired keys win on conflict):** Deployment + pod-template labels and annotations via `OverlayStringMap`. The user can stamp arbitrary metadata that survives every reconcile.
2. **Replace (wholesale overwrite):** `Containers`, `InitContainers`, `Volumes`, `SecurityContext`, `ServiceAccountName`, `AutomountServiceAccountToken`. Both JSON-patch-appended containers (`c5-user-sidecar` main container, `c5-user-init` init container) were stripped within ~5s. The "extra container shadowing operator container" BUG-A is structurally impossible on this surface.
3. **Passthrough (not touched at all):** `Tolerations`, `ImagePullSecrets`, `HostAliases`, `NodeSelector`, `Affinity`, `TopologySpreadConstraints`, `PriorityClassName`. The mutator never reads or writes these, so user values stay verbatim. Pure pass-through with no merge, so accumulation across many reconciles is structurally impossible — confirmed by 5 rapid `spec.node.port` toggles producing identical list counts to a single injection.

The boundary persists across spec-change-driven reconciles (C5b confirmed all 7 metadata/passthrough fields survived a port bump while both injected containers got stripped again). No CR-condition or event signal that user-injected containers were stripped, but the Mutate semantics are stable.

UX observation (not promoted, same flavor as B5/C1/C2/C3): the user cannot tell from the CR spec/status which Deployment-level customizations will survive — knowing the overlay/replace/passthrough split requires reading the operator code. For a curated dev environment this is acceptable; documenting it in the chart README would help operators avoid wasted edits.

Evidence under `.run/break-pass/c5/`. Namespace cleanly torn down.

## 2026-05-28 12:40 — Category C synthesis (pause for user review)
Four tests run (C1, C2, C3, C5); C4 skipped as redundant with A3 (sustained sidecar persistence of CM corruption would re-confirm "no operator-side backoff" already documented).

| Test | Theory | Verdict | Severity |
|------|--------|---------|----------|
| C1 | Manual replicas=0 on primary Deploy | UX-GAP (silent override) | low |
| C2 | kubectl set serviceaccount on primary Deploy | UX-GAP (silent override) | low |
| C3 | Patch primary Service selector | NOT-A-BUG + UX-GAP caveat | — |
| C4 | Artifact CM sustained corruption | (skipped — redundant with A3 BUG-B) | — |
| C5 | User-injected fields persistence | NOT-A-BUG | — |

**Zero new TEST_REPORT entries from Category C.** All restore-race tests came back NOT-A-BUG (Mutate restores within <1s, faster than 1s sampling can observe; Endpoints never observably empty; no wrong-image/wrong-SA Pod ever runs).

**Cross-cutting UX finding (B5 + C1 + C2 + C3 + C5):** When the operator wins a restore race or silently strips user-appended fields, there is no user-facing signal (no event, no transient condition, no log line accessible to non-platform users). This is internally consistent with the operator's "I own these fields" contract but unfriendly to a maintainer who runs `kubectl scale` / `set image` / `set serviceaccount` / `patch svc selector` / JSON-appends a sidecar and then sees `Ready=True` with no indication their edit was reverted.

Two ways to address this if you ever want to:
- **Per-restore Warning event** on the affected object: `OwnedFieldRestored field=spec.replicas user_value=0 restored_value=1`. Cheap, visible in `kubectl describe`, no noise on the steady-state.
- **Chart README "what's user-customizable" docs** listing the three field categories — overlay (labels/annotations), replace (containers/volumes/SA/SecurityContext), passthrough (tolerations/affinity/imagePullSecrets/etc.) — so operators know which Deployment-level edits will stick.

Not promoting this to a TEST_REPORT.md section yet because it's a UX/docs pattern across many tests rather than a single concrete failure. If you want it formalized as an entry, I can add it as a "Cross-cutting findings" section at the bottom of TEST_REPORT.md.

Pause for user review before starting Category D (Owned-child deletion & ownership leaks: D1 faucet auth Secret deletion, D2 PVC stuck Terminating via foreign finalizer, D3 owner-reference strip, D4 artifact publisher RBAC deletion, D5 foreign-owned same-name child, D6 managed Postgres auth Secret delete, D7 no-finalizer cascade data loss). Dev stack left running.

## 2026-05-28 12:50 — D1 BUG-A + BUG-B + UX-GAP (high): faucet auth Secret deletion produces lying status + silent token invalidation
Adversarial test D1 from the synthesized list: delete the faucet auth Secret while the faucet is enabled and Ready, observe CR conditions during the 10-min repair window, then probe the repair-time token-rotation behavior via manager restart.

Setup: local CardanoNetwork `d1-net` with ogmios + kupo + faucet enabled. FaucetReady=True reached in ~20s. Baseline token captured.

**D1a — Secret deletion, 90s observation:**
- Secret stayed deleted for the full 90s window (no Secret watch; 10-min requeue not yet fired).
- Pod's faucet container `state=running` with `startedAt` unchanged across all 13 samples. **The kubelet's already-mounted projected volume keeps serving the cached Secret file even after the API Secret is deleted, and the faucet binary holds the token in memory from container start. No restart, no CreateContainerConfigError, no MountVolume.SetupFailed.**
- CR conditions stayed `FaucetReady=True, Ready=True` for the full 90s. Lying-status condition (verbatim): *"Faucet sidecar is available through its Service"*.
- Time-to-honest-status: NEVER within 90s. Worst case is `faucetSecretRepairRequeueAfter = 10*time.Minute`.

**D1b — Recovery via manager restart:**
- New manager pod Ready in 11s.
- Secret recreated within 0s of manager Ready (manager-startup reconcile is the practical fast-recovery path).
- FaucetReady "already True" — the lying status never flipped to False, so recovery is invisible at the condition level. The CR cannot distinguish "broken and we fixed it" from "never broke."
- **Pod restartCount=0. Same ReplicaSet, same Pod, same `startedAt`. The faucet binary still holds the original token in memory.**

**D1c — Token rotation byte-comparison:**
- Baseline token sha256: `57fa6745bf37446dbe5ffee97c9e2d978ad924422b5ac25307fcdb0be08bc0d9`
- Regenerated token sha256: `230e0601400ae6568201d867c6d334501cd0281dc44da0bfdf7cc9647ca1bc91`
- **Token rotated.** `createFaucetAuthSecretWithToken` generates a fresh random token on the not-found branch — no migration, no preservation, no out-of-band store. The TECH_NOTES prediction is confirmed: "silently invalidates any previously cached topup credentials."

The combined failure mode: API server holds token B, running faucet pod still authenticates against token A in memory, any user holding A is silently broken. A future pod roll (node reboot, an unrelated CR spec change that bumps pod-template-hash, image upgrade, etc.) would finally swap the running token to B — at that future point, ALL pre-deletion users get auth failures with no operator-side signal that token continuity was broken in the past. The CR is `Ready=True` throughout the entire history.

Severity high because (a) faucet token is the only secret control gate on the only mutating endpoint YACD currently exposes (UTxO topup), (b) silent invalidation cannot be observed from CR status, (c) recovery requires both Secret repair AND pod roll AND user re-fetch of the new token, but the operator only does the first.

Code references the agent identified:
- `internal/controller/cardanonetwork/controller.go` — `faucetSecretRepairRequeueAfter = 10 * time.Minute`
- `internal/controller/cardanonetwork/faucet_auth.go` — `createFaucetAuthSecretWithToken`
- The controller HAS an honest message ready (`"Faucet auth Secret is missing"` reason `PrimaryWorkloadMissing`) — it just never publishes it because the gating path is the live-read Secret check inside Reconcile, and Reconcile doesn't run during the 10-min gap.

Three fixes worth considering, in increasing surgery:
1. **Roll the Deployment whenever the faucet auth Secret is repaired** (smallest surgery, addresses the runtime-vs-API divergence). Stamp the auth Secret resourceVersion onto the pod-template annotation; whenever the controller creates a new auth Secret, the resourceVersion changes and the Deployment rolls, swapping the running faucet's in-memory token to the new one in seconds. This eliminates the silent A-vs-B divergence.
2. **Watch faucet auth Secrets via labelled selector** to shrink the lying-status window from 10 minutes to seconds. The TECH_NOTES rationale for not watching ("avoiding list RBAC") could be addressed with a label-selector watch on `app.kubernetes.io/name=cardano-network-faucet-auth` or similar that doesn't require full Secret list/watch in the namespace.
3. **Preserve the previous token bytes on repair** (heaviest, also out of scope for current architecture). Requires either keeping a controller-local LRU keyed by CR UID, an out-of-band store (k8s Secret in operator's own namespace), or a finalizer-based copy. Mostly addresses the historic-user-token-still-valid case but adds complexity.

Option (1) is the cleanest end-to-end fix: it makes the runtime token always match the API Secret, which makes the controller's existing honest-message path correct again (any cached user token is invalid as soon as the operator repairs — the user sees auth failure at HTTP layer, not silently-broken-then-eventually-broken). Pair with (2) to shrink the gap to seconds.

Evidence under `.run/break-pass/d1/`. Namespace cleanly torn down.

## 2026-05-28 13:00 — D2 BUG-A + BUG-B (high): PVC stuck Terminating + silent localnet data loss on recovery
Adversarial test D2 from the synthesized list: add a foreign finalizer to the primary node-state PVC, delete it, observe the stuck-Terminating window, then remove the foreign finalizer and observe the recovery.

Setup: local CardanoNetwork `d2-net` (Ready in 21s). PVC `d2-net-node-state` with baseline localnet-fingerprint annotation.

**D2a — PVC stuck Terminating (66s observation):**
- PVC `metadata.deletionTimestamp` present for full window; finalizers `["test.example.io/never-removed", "kubernetes.io/pvc-protection"]`. Live Pod stays Running (PV still mounted, pvc-protection finalizer blocks the volume detach).
- CR conditions: **no transition.** Ready=True, Degraded=False, NodeReady=True throughout. The CR cheerfully reports a fully healthy primary while the underlying PVC is mid-deletion.
- Operator log: ZERO errors. The reconciler reports `persistentVolumeClaimOperation:"unchanged"` because `ApplyOwnedObject` (`internal/ctrlkit/apply/apply.go`) does not check `DeletionTimestamp`. Get returns the live (Terminating) object, owner check passes, `validatePrimaryPersistentVolumeClaim` only checks localnet-fingerprint and storage drift (also no DeletionTimestamp gate), Mutate produces no diff, "unchanged" — success.

This is BUG-A. There is no detection path anywhere in the apply/readiness pipeline that looks at `DeletionTimestamp`, so the silent-lying-status window is unbounded as long as the foreign finalizer is held.

**D2b — Recovery via finalizer removal (67s observation):**
The controller's recovery code path technically works — within seconds of the PVC actually deleting, a new PVC under the same name appears via the NotFound branch of `ApplyOwnedObject`. But the downstream consequences are catastrophic:
- The kubelet binds the EMPTY new volume into the still-existing primary Pod (`d2-net-node-6fbff4d85f-ch6t7`).
- Container restarts don't re-run init containers. The cardano-testnet create-env init (which populates `/state` with localnet genesis material on first start) never re-runs.
- `cardano-node` enters CrashLoopBackOff with `Yaml file not found: /state/env/configuration.yaml`. ogmios/kupo go Unhealthy.
- The Deployment refuses to spin a replacement Pod (`1 unavailable / 0 terminating`). The new PVC sits Pending with `WaitForFirstConsumer` because the only consumer (the doomed Pod) is bound to the old PV.
- CR conditions stuck at `Ready=False / DeploymentProgressing`, `NodeReady=False / DeploymentProgressing`, `Degraded=False / ReconcileSucceeded`. **A user reading the CR cannot distinguish "rollout in progress" from "data was permanently destroyed, manual pod delete required."**

Even manually deleting the broken Pod (which I didn't probe but is the natural next user action) would only get the new PVC bound and the init container re-run — but the *original* localnet state (any wallets the user funded, any state since genesis) is gone. The localnet-fingerprint validation that exists specifically to prevent identity drift can't help because the freshly-init'd PVC carries the SAME fingerprint (the localnet plan is deterministic). Validation passes; data is gone.

This is BUG-B with high severity because it crosses the contract YACD makes about PVC stability: TECH_NOTES says explicitly that "A `CardanoNetwork` localnet is stable for its lifetime" and that "Delete and recreate the CR/PVC to change localnet parameters." D2 demonstrates that an external actor with `update pvc` permission (or a buggy admission webhook that mishandles finalizers) can destroy that stability without any operator-side detection or signal.

The agent identified two complementary fixes:
1. **DeletionTimestamp gate in the apply path.** When `Get` returns a child with `DeletionTimestamp != nil`, treat it as a typed `statusConditionError` with reason `PVCBeingDeleted` and a message naming the foreign finalizer(s). This collapses BUG-A's window: the CR goes Degraded immediately on PVC deletion, the user sees an honest message, they can resolve the finalizer or accept the consequences before recovery damage. Code: `internal/ctrlkit/apply/apply.go::ApplyOwnedObject` plus per-controller callback wrapping for the conditions surface.
2. **Pod-rotation on PVC recreation.** When the controller creates a new PVC via the NotFound branch on an OWNED PVC name (i.e., the controller previously created it and it has since vanished), it should also delete the consuming Pod so the init container re-runs and re-populates state. Code: `internal/controller/cardanonetwork/apply.go::applyPrimaryPersistentVolumeClaim` plus a paired Pod-delete in the same reconcile when `OperationResultCreated` and the previous reconcile had stamped the older PVC UID. Without this, BUG-B persists even with fix (1) — the user sees the honest message but the recovery still destroys data unless they know to manually delete the Pod.

Pairing (1) + (2): the user sees Degraded explaining the issue, can choose to abandon recovery (delete and recreate the whole CR) or to proceed with managed recovery (the controller deletes the Pod, init re-runs, the localnet state is rebuilt from genesis but at least is consistent with the fingerprint). For a "stable for its lifetime" contract this is the only correct posture: either tell the truth + manage recovery, or fail loudly so the user makes the destruction decision themselves.

Evidence under `.run/break-pass/d2/`. Namespace cleanly torn down.

## 2026-05-28 13:08 — D3 NOT-A-BUG (small UX-GAP): ownerReference strip is detected fast and clean
Adversarial test D3 from the synthesized list: strip the controller ownerReference from the primary Deployment via JSON patch, observe controller behavior, then delete the CR and see what survives.

Setup: local CardanoNetwork `d3-net` (Ready in 20s). Baseline `ownerReferences: [CardanoNetwork/d3-net controller=true blockOwnerDeletion=true]`.

D3a (strip ownerReference): within <2s the CR went `Ready=False / ResourceConflict`, `Degraded=True / ResourceConflict` with message *"resource break-d3/d3-net-node already exists without a controller owner"*. The operator did NOT try to take ownership back — `ApplyOwnedObject` correctly catches the stripped reference via `ValidateControllerOwner`, lifts it through `controllerOwnerConflict` into a `ResourceConflict` typed `statusConditionError`, and `handlePrimaryWorkloadApplyError` flips Degraded with no auto-reclaim. Pod stayed 3/3 Running. Deployment ownerReferences stayed empty — the operator does not patch them back, which is the correct posture for a GitOps-style pruning incident (auto-reclaim would silently overwrite intentional reparenting).

D3b (CR delete with orphan in place): CR deleted immediately (no finalizers). Kubernetes GC walked ownerReferences and collected the network artifact ConfigMap, ServiceAccount, Role, RoleBinding, primary Service, Ogmios Service, Kupo Service. The orphan Deployment, its ReplicaSet, its Pod, and its PVC survived. The PVC entered Terminating but stuck because the orphan Pod still mounts it (same volume-in-use pattern as D2 but here intentional, not adversarial).

D3c (functional probe of the orphan): orphan Deployment exists, Pod still Running, but the Service was CR-owned and GC'd. So the orphan is a leaked-workload-with-no-network-surface. Cluster-internal traffic can't reach it.

UX-GAP: the message *"resource X already exists without a controller owner"* correctly identifies the resource and problem class but stops short of telling the user how to recover (restore the ownerReference via `kubectl annotate`/`patch`, delete the Deployment, or recreate the CR with a different name). For a single-tenant dev environment recovery is straightforward for anyone who knows Kubernetes GC; for a less-savvy operator a message that includes the recovery steps would shave investigation time. Not promoted to a TEST_REPORT.md entry — the safety posture is correct and the only friction is a docs/message polish.

Evidence under `.run/break-pass/d3/`. Manual orphan-Deployment delete was required during cleanup (a user who hits this in practice would have the same step).

## 2026-05-28 13:14 — D4 NOT-A-BUG (mild UX-GAP): artifact-publisher RBAC recreates in <1s
Adversarial test D4 from the synthesized list: delete each of the three artifact-publisher RBAC resources (Role, RoleBinding, ServiceAccount) in sequence and observe Owns-watch recreation latency.

All three sub-tests showed identical clean behavior: deletion → new resource with a fresh UID visible in <1s of sampling resolution. Operator log shows the paired `"<resource>Operation":"created"` entry in the same wall-clock second as each delete. The running primary Pod (`UID d190353e...`, 0 restarts) was correctly unaffected — its init container had already projected its token and patched the artifact ConfigMap at startup, so the RBAC churn never triggers a Pod restart. Recreation protects future Pod starts, not the current one.

Same UX gap as the B5/C-cluster pattern: no condition flap, no Kubernetes Event, only the operator log notes the recreation (and even then mixes it into the omnibus "Applied CardanoNetwork primary workload" log line). Not promoted to TEST_REPORT.md — operator behavior is correct; the gap is consistent with the cross-cutting UX pattern already noted in NOTES.

Evidence under `.run/break-pass/d4/`. Namespace cleanly torn down.

## 2026-05-28 13:21 — D5 NOT-A-BUG (minor UX-GAP): foreign-owned same-name CM cleanly blocks reconcile, recovers via 1-min requeue
Adversarial test D5 from the synthesized list: pre-create a foreign-owner Deployment and a ConfigMap with the YACD network-artifacts name (`d5-net-network-artifacts`) owned by it, then apply the CardanoNetwork.

D5a (apply into contested namespace): CR went Degraded=True/ResourceConflict in <5s with verbatim message `"resource break-d5/d5-net-network-artifacts is already controlled by Deployment/foreign-owner"`. The artifact CM is the first step in `applyPrimaryWorkloadResources`, so the early return on conflict prevented creation of PVC, ArtifactPublisher SA/Role/RoleBinding, Deployment, Services, and the (disabled) faucet Secret. **Clean abort with no half-built artifacts.** Foreign CM was untouched.

D5b (recovery via foreign-owner deletion): foreign-owner deleted → Kubernetes GC removed the foreign CM in <5s. Operator's own artifact CM appeared between sample elapsed=33 and 38, recovery clocked at ~51s after the foreign-owner delete. Reading the operator log: first reconcile attempt was at 20:30:44 with the conflict; next requeue at 20:31:44 (still conflict); next at 20:32:44 — recovery matches the `resourceConflictRequeueAfter = 1 * time.Minute` tick within sub-second resolution.

Notable sub-finding: the `Owns(&corev1.ConfigMap{})` watch on the artifact CM does NOT shortcut recovery when a foreign-owned same-named CM is deleted. This is consistent with controller-runtime's `EnqueueRequestForOwner` filtering on the operator's UID — the foreign CM is owned by `foreign-owner`, not by the CardanoNetwork, so its deletion event doesn't enqueue the network CR. Worst-case recovery latency is one full requeue cycle (~60s).

UX-GAP: the message names both the contested object and the foreign controller — a user can immediately `kubectl get deploy foreign-owner` to investigate — but does not explicitly suggest a remediation ("delete the conflicting object or rename the CardanoNetwork"). Not severe; the situation is self-explanatory.

Not a TEST_REPORT.md entry — detection is correct, recovery is bounded, and tightening the recovery latency would require either custom event filtering on foreign CMs (out of pattern) or shrinking the 1-min interval (cost/noise tradeoff).

Evidence under `.run/break-pass/d5/`. Namespace cleanly torn down.

## 2026-05-28 13:34 — D6 BUG-B + UX-GAP (medium): auth-Secret recovery promise doesn't work as written
Adversarial test D6 from the synthesized list: delete the generated `<dbsync>-postgres-auth` Secret after the managed-Postgres database identity has been accepted, observe Degraded behavior, then attempt the recovery path the controller's own message advertises (restore the Secret with the original password bytes).

Setup: local CardanoNetwork + CardanoDBSync with managed Postgres. PostgresReady=True in ~11s after CardanoNetwork Ready. Baseline `acceptedIdentityFingerprint=bbeadb20…`. Saved the auth Secret's `data.password` bytes for restore.

**D6a — delete the auth Secret:**
- Controller did NOT silently regenerate (good — `ensureManagedPostgresAuthSecret` has a safety check `acceptedManagedPostgresIdentity != ""` that refuses post-acceptance regeneration). No BUG-A.
- Within 5s: PostgresReady=False with `reason=ManagedDatabaseSecretMissing`, Degraded=True with verbatim message: *"Managed Postgres generated auth Secret break-d6d7/d6-dbsync-postgres-auth is missing after database initialization; restore the original Secret or recreate the CardanoDBSync with a fresh database"*.
- The live Postgres Pod stayed Running with 0 restarts throughout the 60s window — pgdata holds the original password, the container's mounted credential is cached, Postgres keeps authenticating its existing connections. **Data is not lost.**
- The dbsync follower Deployment was scaled to 0 as part of the Degraded path.

**D6b — restore the auth Secret with original bytes (the controller's own advice):**
This is where the bug appears. Three compounding problems:
1. **The plain `kubectl apply` recreates the Secret without `ownerReferences`.** It is now an unowned same-name object.
2. **The controller refuses to adopt it.** `validateControllerOwner` rejects the orphan. The Degraded diagnosis flips from `ManagedDatabaseSecretMissing` to `ResourceConflict` with message *"resource break-d6d7/d6-dbsync-postgres-auth already exists without a controller owner"* — same pattern as D5 (foreign-owned same-name child) and D3 (stripped ownerReference), but here the "foreign" object is one the controller itself told the user to create.
3. **The flip doesn't happen until a generation bump.** Without an `authSecretRef` field-indexer match (the generated Secret isn't tracked by `cardanoDBSyncManagedDatabaseSecretNameField`), Secret recreation doesn't enqueue the CR. The user sees stale `ManagedDatabaseSecretMissing` until they bump spec to force reconcile — at which point the message changes and they're now told `already exists without a controller owner` instead of `missing`.

Result: the user is given an instruction that doesn't work, and the diagnosis changes mid-recovery so they think their first restore made things worse. The actual recovery paths are:
- (a) Hand-patch `ownerReferences` onto the restored Secret to point at the DBSync CR (undocumented).
- (b) Delete-and-recreate the CR (data loss).

This is BUG-B (recovery promise broken) plus a sharp UX-GAP (the message is actively misleading; the diagnosis-changes-on-its-own behavior makes troubleshooting harder).

The data is NEVER actually lost — Postgres pgdata holds the original password and the Pod keeps running. The bug is entirely in the operator's adoption rule plus the messaging that doesn't reflect it.

Two clean fixes worth considering:
1. **Auto-adopt unowned same-name Secrets whose contents pass the credential identity check (preferred).** When `applyManagedPostgresAuthSecret` finds an unowned same-name Secret AND its `data.password` bytes match the accepted credential identity (the controller stamps a `managedPostgresCredentialVersion` on the owned material), set the ownerReference and proceed. This honors the controller's own "restore the original Secret" advice without requiring the user to know about Kubernetes ownership semantics.
2. **Rewrite the message to require ownership.** Current: *"restore the original Secret or recreate the CardanoDBSync"*. Better: *"restore the auth Secret with the original password bytes AND set `metadata.ownerReferences` to point at this CardanoDBSync (UID=<uid>), or recreate the CardanoDBSync with a fresh database. The Postgres Pod is still running with the original credentials — no data has been lost yet."*

Option (1) is the cleanest and matches what users expect when they read the message. Option (2) is independently shippable as a quick win even if (1) is contentious.

## 2026-05-28 13:34 — D7 NOT-A-BUG (docs gap): no-finalizer cascade is fast, clean, and undocumented
Adversarial test D7 from the synthesized list: confirm that deleting a CardanoDBSync immediately destroys all owned children (managed Postgres data, state PVC, follower PVC, auth Secret, etc.) with no graceful shutdown opportunity.

Test observations (after D6b stabilized with the orphan-restored Secret):
- CR gone in <2s of `kubectl delete cardanodbsync` (no finalizer, `deletionTimestamp` set then immediately collected).
- All three PVCs (`d6-dbsync-postgres-state`, `d6-dbsync-dbsync-state`, `d6-dbsync-follower-state`) gone in <2s.
- Postgres Pod yanked with no graceful-shutdown log sequence — last log was a routine checkpoint at 20:44:57, no SIGTERM `received fast shutdown request` or shutdown checkpoint, then the Pod simply vanished. The kubelet just killed the container when the ReplicaSet was GC'd while the Pod still existed.
- CardanoNetwork (`d6-net`) and all its children (node Pod, services, ConfigMap, node PVC) untouched. Ownership graph is correctly scoped per CR.

The "orphan" auth Secret that survived the delete is an artifact of D6b's restore-without-ownerReferences — not normal behavior. A normally-owned generated Secret would have been GC'd with everything else.

Verdict NOT-A-BUG because this is the documented expectation: neither CRD installs a finalizer, the controller skips reconcile on `DeletionTimestamp != 0`, and Kubernetes GC handles the cascade. The behavior is correct and predictable.

UX-GAP / docs-gap worth recording but not promoting to TEST_REPORT.md: the chart README, the CRD field comments, and the `yacd deploy` CLI all assume the user knows that deleting a CardanoDBSync means immediate, irrecoverable data loss with no snapshot or grace window. For a managed-Postgres deployment with real data on it, this is exactly the kind of thing a 3am operator wants warned about. A one-liner in the CRD `Reasoning` doc or chart README would close the gap — "Deleting a CardanoDBSync immediately deletes all owned PVCs (database, state, follower) without finalizers. Take a Postgres dump before deletion if you need to preserve data."

Evidence under `.run/break-pass/d6d7/`. Namespace cleanly torn down.

## 2026-05-28 13:38 — Category D synthesis (pause for user review)
Seven tests run; outcomes:

| Test | Theory | Verdict | Severity |
|------|--------|---------|----------|
| D1 | Faucet auth Secret delete → lying status | BUG-A + BUG-B + UX-GAP | **high** |
| D2 | PVC stuck Terminating via foreign finalizer | BUG-A + BUG-B | **high** |
| D3 | Strip ownerReference from primary Deploy | NOT-A-BUG (minor UX) | — |
| D4 | Delete artifact-publisher RBAC (3 sub-tests) | NOT-A-BUG (mild UX) | — |
| D5 | Foreign-owned same-name child | NOT-A-BUG (minor UX) | — |
| D6 | Managed Postgres auth Secret delete + restore | BUG-B + UX-GAP | medium |
| D7 | No-finalizer CR delete cascade | NOT-A-BUG (docs gap) | — |

Three new TEST_REPORT entries (D1, D2, D6).

**D1 — Faucet auth Secret deletion: lying status + silent token rotation (high).** The kubelet's already-mounted projected volume keeps serving the cached token in-memory and the faucet binary holds it in memory, so deletion produces zero Pod-level signal. The controller has no Secret watch, so `Ready=True` stays lying for up to the 10-min repair requeue. When repair fires, the controller generates a *fresh* random token without rolling the Deployment — so the API Secret holds token B, the running faucet uses A in memory, and any user with A is silently broken. A future pod roll at an arbitrary time swaps the running token to B, silently invalidating cached A. Fix: roll the Deployment whenever the auth Secret is created or rotated (smallest surgery, eliminates A-vs-B divergence). Pair with a labelled-selector Secret watch to shrink the lying window from 10 min to seconds.

**D2 — PVC stuck Terminating + data loss on recovery (high).** Two compounding bugs. `ApplyOwnedObject` has no `DeletionTimestamp` gate, so the CR stays `Ready=True` while the live PVC is mid-deletion (BUG-A). When the foreign finalizer is removed and the PVC actually deletes, the controller's recovery path technically works (recreates on NotFound) — but the running Pod keeps the (now empty) volume mounted, the init container doesn't re-fire on container restart, cardano-node crash-loops against empty `/state`, the localnet data is permanently destroyed (BUG-B). The localnet-fingerprint validation can't help because the freshly-init'd PVC carries the same deterministic fingerprint. Fix: DeletionTimestamp gate in apply (BUG-A) + Pod-rotation on PVC recreation (BUG-B). This crosses the explicit "stable for lifetime" contract YACD makes in TECH_NOTES.

**D6 — Managed Postgres auth Secret recovery doesn't work as advertised (medium).** The controller correctly refuses to silently regenerate the password after acceptance (the safety check works). But the Degraded message advertises *"restore the original Secret"* — and a plain `kubectl apply` of the original bytes does NOT recover, because the controller refuses to adopt an unowned same-name Secret (`validateControllerOwner` rejects it, diagnosis flips to `ResourceConflict`). Worse, the flip doesn't happen until the user bumps spec to force reconcile (no field-indexer on the generated Secret), so the displayed reason changes by itself, making troubleshooting confusing. Data is never actually lost — Postgres pgdata holds the original password and keeps running. Fix: auto-adopt unowned same-name auth Secrets whose `data.password` matches the accepted credential identity (`managedPostgresCredentialVersion`); update the message to match.

Four non-findings:

**D3** — operator correctly detects ownerReference strip in <2s, refuses to auto-reparent (correct GitOps-safe posture), surfaces `ResourceConflict` with a specific message. On CR delete the orphan persists but its Service was GC'd — leaked workload with no network surface.

**D4** — Owns watches recreate all three artifact-publisher RBAC resources in <1s of deletion. Running Pod unaffected (init already published). Same silent-operator UX pattern as B5/C-cluster.

**D5** — foreign-owned same-name CM cleanly blocks reconcile with a specific `ResourceConflict` message naming both the contested object and the foreign controller. Recovery is via the 1-min `resourceConflictRequeueAfter` (the `Owns` watch doesn't shortcut on foreign-CM deletion because controller-runtime's `EnqueueRequestForOwner` filters by UID).

**D7** — cascade is fast (<2s), clean (CardanoNetwork untouched), and immediate (no graceful shutdown, no SIGTERM sequence in Postgres logs). This is by-design. Worth a one-liner in chart README / CRD docs warning users that CR delete = immediate data loss with no snapshot or grace window.

**Cross-cutting Category-D theme:** the operator's adoption rule (`validateControllerOwner` refuses to adopt unowned same-name children) is correct for security and GitOps safety, but it surfaces as a recovery papercut in three different scenarios: D3 (user-stripped ownerRef on operator's child), D5 (foreign-owned same-name child arriving before the CR), and D6 (user-restored Secret without ownerReferences). Each surfaces a different message:
- D3: *"resource X already exists without a controller owner"*
- D5: *"resource X is already controlled by Y"*
- D6: starts as `ManagedDatabaseSecretMissing`, then flips to D3's message after a generation bump.

The three messages are correct but don't tell the user the remediation. A small unification — "add this CR as the controller owner of `<name>` (e.g. by patching `metadata.ownerReferences`), or delete the conflicting object" — would help in all three cases. Not promoted to TEST_REPORT.md yet because the underlying behavior is correct; only the message would change.

Pause for user review before starting Category E (External dependencies — E1 custom-profile Secret with `binaryData` only, E2 external Postgres Secret finalizer, E3 CardanoNetwork delete with DBSync still referencing, E4 same-name network recreate with new UID, E5 external Postgres key removal). Dev stack left running.

## 2026-05-28 13:45 — E1 NOT-A-BUG (UX-GAP): Secret binaryData premise is impossible
Adversarial test E1 from the synthesized list: load a custom-profile bundle via Secret with all keys under `binaryData`. **The Kubernetes API server rejects Secrets with a top-level `binaryData` field — it only exists on ConfigMaps.** The synthesis's literal premise is structurally impossible for Secrets.

The agent ran the functionally equivalent worst case: an `Opaque` Secret with `data: {}`. Controller rejected within 1s with `Degraded=True / UnsupportedSpec` and message *"public custom profile bundle is empty"*. No owned resources created. Recovery via Secret update with proper `data` keys was immediate via the `customProfileSecretEventHandler` Watch.

UX-GAP: the message names neither the Secret, the missing keys, nor (more importantly) the bad-field placement. A user who actually placed keys under `stringData` instead of `data` (the realistic mistake) gets the same "empty bundle" message. The agent suggested a more specific check: when `Data` is empty but `BinaryData`/equivalent has entries, emit `"required keys %v are not present in .data (binaryData/stringData entries are not consulted)"`.

The agent caught a real follow-up: `publicCustomProfileConfigMapBundle` at `public_profile_source.go:64` ALSO reads only `configMap.Data` and ignores `configMap.BinaryData`. ConfigMaps do have a real `binaryData` field, so the analogous test on a ConfigMap is exploitable. Ran E1b immediately.

## 2026-05-28 13:48 — E1b NOT-A-BUG (UX-GAP): defense-in-depth catches binaryData on ConfigMap
Follow-up test for the ConfigMap analog of E1. The agent confirmed:
- `publicCustomProfileConfigMapBundle` does read only `Data` (synthesis claim correct).
- BUT the downstream `customArtifacts()` in `internal/cardano/publicnet/plan.go:312` defensively rejects `len(bundle.Files) == 0` with `"public custom profile bundle is empty"`.
- Result: Degraded=True / UnsupportedSpec within 1s. No owned resources. Recovery via watch was immediate when keys moved to `data`.

Same UX-GAP as E1: a user with all 6 required keys *visibly present* under `binaryData` sees the "empty bundle" message and is confused — `kubectl get cm` contradicts the operator's report. The defense-in-depth at the planner layer is correct (BUG-A and BUG-B both ruled out: no degenerate workload was ever rendered) but the diagnostics could be more specific.

Both E1 and E1b are NOT-A-BUG, recorded here for completeness. Not promoted to TEST_REPORT.md.

Evidence under `.run/break-pass/e1/` and `.run/break-pass/e1b/`. Namespaces cleanly torn down.

## 2026-05-28 13:57 — E2 NOT-A-BUG: DeletionTimestamp honored, owned pgpass not scrubbed (hardening note)
Adversarial test E2 from the synthesized list: add a foreign finalizer to the external Postgres password Secret, delete it (it enters Terminating with DeletionTimestamp set but the object is still gettable), and check whether `validateExternalDatabaseSecret` honors the DeletionTimestamp.

Setup: local CardanoNetwork + CardanoDBSync `e2-dbsync` with external Postgres pointed at unreachable `127.0.0.1:5432`. Secret `pg-external-pass` with `data.password=real-baseline-password`. Baseline: PostgresReady=False/PostgresUnavailable (connection refused), Degraded=False/ReconcileSucceeded. The owned `e2-dbsync-dbsync-pgpass` Secret was rendered with the baseline password.

E2a (add finalizer + delete): within ~5s Degraded=True with reason `ExternalDatabaseSecretMissing` and message *"External Postgres password Secret is deleting"*. The Terminating Secret's password was NOT reused downstream — operator log shows `pgPassSecretOperation: unchanged` across reconciles, no fresh re-render of the pgpass. `validateExternalDatabaseSecret` at `controller.go:616-621` correctly checks `!secret.DeletionTimestamp.IsZero()`.

E2b (remove finalizer, Secret truly deletes): Degraded stays True; reason stays `ExternalDatabaseSecretMissing`, message updates from "is deleting" to *"External Postgres password Secret does not exist"*. Clean transition.

Hardening observation (not promoted to TEST_REPORT.md): the controller-owned `e2-dbsync-dbsync-pgpass` Secret persists with the baseline password throughout. This is conventional Kubernetes operator behavior (don't tear down child workloads on dependency unavailability) and the data isn't being newly consumed (the Deployment is still running its existing Pod which crashloops against unreachable Postgres). But a future scenario where the user changes the external password and then accidentally deletes the new Secret could result in the operator's child pgpass holding a stale password the user thought was rotated away. Worth a security-review consideration: should `applyDBSyncPGPassSecret` scrub the owned pgpass when the upstream external Secret is missing/Terminating/invalid, or maintain it untouched? Default Kubernetes pattern is "untouched"; YACD's posture is the same. Flagging the choice rather than calling it a bug.

## 2026-05-28 14:00 — E5 UX-GAP: external Secret key removal detected fast but message is too vague
Adversarial test E5 from the synthesized list: remove the `password` key from the live external Postgres Secret via JSON patch, observe Degraded behavior and recovery.

E5a (remove password key): within ~5s Degraded=True with reason `ExternalDatabaseSecretInvalid` and message *"External Postgres password Secret does not contain the configured key"*. The detection works correctly — this is via the `len(secret.Data[passwordKey]) == 0` check inside `validateExternalDatabaseSecret`. But the message does not name (a) which Secret it consulted (`pg-external-pass`), nor (b) which key was expected (`password`). A user inspecting `kubectl describe cardanodbsync` sees a vague complaint and has to consult the CR spec to figure out what key they were supposed to have. Same pattern as the E1/E1b "bundle is empty" message.

E5b (restore key): within ~5s Degraded back to False/ReconcileSucceeded. Recovery is clean and not sticky.

UX-GAP not promoted to TEST_REPORT.md — the underlying detection is correct, recovery is clean, and the message-clarity issue is consistent with the cross-cutting Category-A/B "messages don't include identifiers" theme already noted (G6 in the synthesis list specifically called this out for the related `UnsupportedDatabaseIdentityChange`).

Evidence under `.run/break-pass/e2e5/`. Namespace cleanly torn down.

## 2026-05-28 14:10 — E3 NOT-A-BUG: network delete leaves DBSync orphan-but-correct
Adversarial test E3 from the synthesized list: delete the CardanoNetwork while a CardanoDBSync still references it.

Setup: local network + DBSync `e3-dbsync` with managed Postgres against `e3-net`. PostgresReady=True in ~6s. Baseline acceptedIdentityFingerprint set.

E3a: network deleted in <5s. DBSync conditions flipped to `Degraded=True/NetworkUnavailable, Ready=False/NetworkUnavailable` within 5s with message *"Referenced CardanoNetwork does not exist"*. Stable through 60s. The CardanoNetwork's owned children (its Deployment, Service, Ogmios/Kupo Services, network-artifacts CM, primary PVC, artifact-publisher RBAC) all GC'd correctly. The CardanoDBSync's owned children (managed Postgres Deployment 1/1 Running, db-sync Deployment scaled to 0, three PVCs totaling 14Gi, ConfigMap, two Secrets) ALL persisted — their ownerReferences point at the still-living CardanoDBSync, not the deleted network. This is the correct controller-runtime ownership chain.

E3b inventory: 9 surviving owned children attributed to `e3-dbsync`, 14Gi of PVCs holding storage indefinitely until the user deletes the DBSync separately.

NOT-A-BUG because the design contract is met: dependency unavailable → patch status, don't tear down workloads. The DBSync controller's early `patchDependencyUnavailableStatus` return at `controller.go:117-130` is intentional. Minor UX observation (not promoted): no condition or message mentions the 14Gi of dormant storage, so a user reading only `kubectl describe cardanodbsync` won't realize they need to act to free that storage. A trailing note in the `NetworkUnavailable` message like "owned managed Postgres workloads remain provisioned; delete this CardanoDBSync to release the storage" would help. Same UX pattern as D7's "no warning that delete = data loss" finding.

## 2026-05-28 14:13 — E4 NOT-A-BUG (architecturally elegant): same-name network recreate is safely rejected via artifact-hash identity binding
Adversarial test E4 from the synthesized list: after E3 leaves the DBSync orphaned with NetworkUnavailable, recreate the network with the same name + same spec. The fingerprint computation over spec is deterministic, so the new network produces the same `networkFingerprint`. Does the DBSync silently consume the new network's artifacts?

Critical observation from the agent's read of the fingerprint code: `databaseIdentityFingerprint` in `internal/cardano/dbsync/fingerprint.go:96-117` hashes `spec.NetworkArtifactHash` — which is the network's artifact `dataHash`, NOT its `networkFingerprint`. The `dataHash` is computed over the *actual generated artifact content*, which includes freshly-minted localnet keys/genesis seeds per init run. Result: a same-spec recreate produces a *different* artifact `dataHash` even though the `networkFingerprint` is identical.

Observed: baseline artifact dataHash `sha256:fe4a55...`, post-recreate artifact dataHash `sha256:f7fed4...`. The recomputed DB identity diverges from the previously accepted identity. `validateAcceptedDBSyncDatabaseIdentity` at `controller.go:288` fails closed within seconds (and before the new network even reaches Ready=True, because the controller can compute the new database identity from any reachable artifact state). DBSync went `Degraded=True/UnsupportedDatabaseIdentityChange` with message *"CardanoDBSync database-affecting inputs changed from accepted identity; delete and recreate the CardanoDBSync with a fresh or compatible external database"*. acceptedIdentityFingerprint NOT overwritten — controller correctly refuses.

This is architecturally elegant. The identity binding is replay-resistant *without* needing UID tracking on the network reference, because the high-entropy artifact content hash already provides cryptographic continuity. A malicious actor would need to control artifact generation deterministically (e.g., by supplying a malicious operator image that produces colliding keys) — not just replay a spec.

NOT-A-BUG. The condition message reflects the safe-by-default refusal. The user gets a clear signal that the network recreation invalidates their DBSync state.

Security note worth keeping for future TECH_NOTES: the localnet artifacts publisher in `internal/controller/cardanonetwork` generates fresh keys per init container run, which is what makes the network rebirth detectable here. If a future change shifted to deterministic key generation (e.g., for repeatable testing), this E4 protection would weaken — the artifact hash would also become deterministic, and same-spec network reuse would silently continue the DBSync. Worth a comment near `dataHash` to document the dependency.

Evidence under `.run/break-pass/e3e4/`. Namespace cleanly torn down.

## 2026-05-28 14:15 — Category E synthesis (pause for user review)
Six tests run (5 from synthesis + 1 follow-up E1b that the E1 agent identified as the actually-exploitable analog):

| Test | Theory | Verdict | Severity |
|------|--------|---------|----------|
| E1 | Custom-profile Secret with binaryData | NOT-A-BUG (synthesis premise impossible for Secrets) | — |
| E1b | Custom-profile ConfigMap with binaryData (follow-up) | NOT-A-BUG (empty-bundle defense-in-depth catches it) | — |
| E2 | External Postgres password Secret finalizer + delete | NOT-A-BUG (DeletionTimestamp honored) | — |
| E3 | CardanoNetwork delete while DBSync references it | NOT-A-BUG (clean NetworkUnavailable; orphans by design) | — |
| E4 | Same-name network recreate with new UID | NOT-A-BUG (artifact dataHash binding catches it) | — |
| E5 | External Postgres key removal | UX-GAP (message doesn't name Secret or key) | low |

**Zero new TEST_REPORT entries from Category E.** All five synthesized tests came back NOT-A-BUG; E1b confirms the related ConfigMap concern is also defended.

Three architectural strengths revealed:

1. **E4: artifact-hash identity binding is replay-resistant.** The DBSync's accepted database identity transitively binds to the network's artifact `dataHash` (high-entropy generated content) rather than the spec-derived `networkFingerprint`. Same-spec network rebirth produces a different dataHash → DBSync correctly refuses to silently continue. This was a structural concern in the synthesis (★ at 🟡 medium); the result raises confidence in the architecture.

2. **E2: DeletionTimestamp is honored on the external dependency path** at `validateExternalDatabaseSecret` (`controller.go:616-621`). The asymmetry the synthesis hinted at (managed-path checks DeletionTimestamp, external-path doesn't) does not exist — both paths check it.

3. **E1+E1b: defense-in-depth at the planner layer.** Even though the bundle readers ignore `binaryData`, the `customArtifacts()` empty-bundle check at the planner layer catches the case before any owned resources get rendered. Both ConfigMap and Secret paths are covered.

Three cross-cutting UX issues consistent with prior categories:
- E1/E1b: *"public custom profile bundle is empty"* doesn't name the source or which keys were missing.
- E5: *"External Postgres password Secret does not contain the configured key"* doesn't name the Secret (`pg-external-pass`) or the key (`password`).
- E3: `NetworkUnavailable` message doesn't mention the orphan storage the user should clean up.

All three are "message-clarity" improvements rather than functional bugs — collected here for future polish.

One hardening observation from E2 (not a bug): when the upstream external Secret is missing/Terminating/invalid, the controller-owned `<dbsync>-pgpass` Secret persists with the previously-validated password. Conventional Kubernetes operator behavior (don't tear down workloads on dep unavailability), but worth a security-review consideration: should the controller scrub the rendered pgpass when the upstream becomes invalid? Default Kubernetes pattern is "untouched"; YACD's posture matches. Flagging the choice rather than calling it a bug.

Pause for user review before starting Category F (Runtime / cluster lifecycle — F1 Mithril init container with non-mithril image, F2 non-existent node image, F3 invalid Mithril snapshot digest, F4 missing StorageClass, F5 faucet revoke/re-enable race, F6 operator pod kill mid-apply). Dev stack left running.

## 2026-05-28 14:25 — F1+F3 INCONCLUSIVE, but uncovered a separate **HIGH-severity finding**: mainnet artifact ConfigMap exceeds 1 MiB cap, mainnet cannot be created
The F1+F3 agent set out to test Mithril init container behavior on mainnet (F1: alpine image as Mithril, F3: invalid snapshot digest). Both tests blocked at a preceding step: the controller's reconcile fails when it tries to apply the mainnet network-artifacts ConfigMap because the bundle (Byron + Shelley + Alonzo + Conway genesis files + topology + config) exceeds the Kubernetes 1 MiB cap on ConfigMaps.

Observed via the agent's operator-log evidence: `Reconciler error ... ConfigMap "f1-net-network-artifacts" is invalid: []: Too long: may not be more than 1048576 bytes`. 34 such error log lines across the two CRs in a few minutes — controller-runtime retries with exponential backoff, never makes progress, never patches CR status.

User-visible impact: a CardanoNetwork applied with `mode=public, profile=mainnet` and any valid Mithril bootstrap config (i.e., the only currently-supported way to create a mainnet) has NO `.status` block. No conditions. No events on the CR. The CR exists in etcd as an unaccepted spec; the cluster has no operator-owned resources for it. A user running `kubectl describe cardanonetwork` sees the spec they applied and zero feedback — exactly the lying-quiet failure the synthesis was hunting for.

Severity HIGH: this breaks the mainnet capability entirely. Mainnet is a documented and tested-in-Chainsaw feature per recent sessions (027 added public profiles + mainnet bootstrap); the breakage was introduced (or always latent) and not caught because Chainsaw tests use preview/preprod, not mainnet. The CRD comment for `MithrilBootstrapSpec` says "bootstrap.mithril is required only when public.profile is mainnet" — i.e., the only mainnet code path goes through Mithril which goes through the artifact bundle that can't fit in a ConfigMap.

F1 verdict INCONCLUSIVE — the original concern about Mithril image-override silent-success was never reachable because the Pod never gets created. Agent's source review note worth keeping: `mithrilBootstrapInitContainer` in `internal/controller/cardanonetwork/init_container.go:21` runs `mithril-client cardano-db download` and the operator does **not** perform any post-init validation that `db/` was actually populated. So if a hostile or accidentally-misconfigured Mithril image override ever did get past the artifact-bundle blocker, the downstream cardano-node would silently start with an empty data dir. The synthesis concern is theoretically real; the test couldn't empirically confirm. Recommend a follow-up later either via envtest (mock the init container apply) or via a code review that adds post-init checks.

F3 verdict INCONCLUSIVE — same block. Cannot test snapshot-digest behavior because the Pod never starts.

Writing the mainnet-ConfigMap-too-large finding to TEST_REPORT.md immediately. Moving on to F2+F4.

Evidence under `.run/break-pass/f1f3/`. Namespaces cleanly torn down.

## 2026-05-28 14:33 — F2+F4 UX-GAP (medium): NodeReady message is uselessly generic for image-pull and PVC-binding failures
Adversarial tests F2 and F4 from the synthesized list: do CR conditions surface the underlying Kubernetes failure mode (image pull, PVC binding) usefully, or just show "Deployment not available"?

F2 (bad node image `ghcr.io/nope/nope:404`): kubelet correctly reports `ImagePullBackOff` with verbatim `"Back-off pulling image"` plus the underlying 403 from ghcr.io. CR conditions: `NodeReady=False / reason=DeploymentProgressing / message="Primary node Deployment is not available"`. No mention of "image", the image string, or "pull". User must `kubectl describe pod` to diagnose.

F4 (missing StorageClass `nope-not-a-real-class`): PVC has `ProvisioningFailed` event with verbatim `"storageclass nope-not-a-real-class not found"`. Pod has `FailedScheduling` event `"unbound immediate PersistentVolumeClaim"`. CR conditions: identical `NodeReady=False / reason=DeploymentProgressing / message="Primary node Deployment is not available"`. No mention of "storage", "PVC", "binding", or the class name.

Same root cause: the operator's `NodeReady` computation reads only `Deployment.status.availableReplicas` (or the `Available` Deployment condition) and translates that into a fixed message. It never inspects the underlying Pod's `containerStatuses[*].state.waiting.reason`, Pod events, or PVC binding status. Both F2 and F4 result in the same `DeploymentProgressing` boilerplate.

Verdict UX-GAP (medium severity). Not BUG-A (status correctly says `Ready=False`), not BUG-B (recovery should be clean on spec correction). The bug is purely "user-visible message is uselessly vague for two very common configuration mistakes." Same finding applies to `OgmiosReady` and `KupoReady` (sidecar containers in the same Pod inherit the image-pull surface; their messages reuse the same generic `DeploymentProgressing` reason).

Agent's concrete fix is small and contained: in the node-readiness check, when the primary Deployment is not Available, walk the latest ReplicaSet's Pods and scan `containerStatuses[*].state.waiting` for actionable reasons (`ImagePullBackOff`, `ErrImagePull`, `CreateContainerError`, `CreateContainerConfigError`, `CrashLoopBackOff`) plus `status.conditions[type=PodScheduled].status=False` (which surfaces the "unbound immediate PVC" message). Promote the most specific reason and a truncated message into the `NodeReady` condition. Example outputs:
- F2: `NodeReady=False reason=ImagePullBackOff message="cardano-node container is waiting: Back-off pulling image \"ghcr.io/nope/nope:404\""`
- F4: `NodeReady=False reason=PodUnschedulable message="primary node pod is unschedulable: unbound immediate PVC f4-net-node-state (storageclass \"nope-not-a-real-class\" not found)"`

Writing as a combined TEST_REPORT entry because both tests share the same root cause and fix.

Evidence under `.run/break-pass/f2f4/`. Namespaces cleanly torn down.

## 2026-05-28 14:42 — F5 NOT-A-BUG (compounds with D1): kupo toggle silently rotates faucet token
Adversarial test F5 from the synthesized list: with faucet + kupo enabled and Ready, race the kupo disable (which triggers `revokePrimaryFaucetExposure` → strips faucet container and deletes auth Secret) with kupo re-enable (which rebuilds), to test whether the operator's apply path can be caught in a half-state mid-revoke.

Setup: local CardanoNetwork with ogmios + kupo + faucet enabled. FaucetReady=True in ~26s. Baseline faucet auth Secret UID and token sha256 captured.

F5a (race — both patches as fast as possible): the final state converged correctly to `kupo=true + faucet enabled` with Deployment containers `cardano-node, ogmios, kupo, faucet`, auth Secret present, FaucetReady=True. No Pod restart (the Pod was a fresh ReplicaSet roll-out, not crash-restart). controller-runtime serializes reconciles for a given CR, so there's no in-flight overlap of revoke vs apply on the same object. The race didn't produce the half-state the synthesis worried about.

F5b (sequential timing): also clean — controller observed each intermediate state, ran revoke or rebuild appropriately, convergence ~7-10s. No stuck states.

**But here's the catch:** every transit through revoke→apply ROTATES the faucet auth token. F5a: `905462a3` → `77f5fe4d`. F5b spaced: → `abacf30c`. F5b2 recovery: → `85d457de`. The `createFaucetAuthSecretWithToken` path always generates a fresh random token when the Secret is absent (same code path as D1's BUG-B). The root cause is the same: the controller doesn't roll the Deployment when the auth Secret is recreated, so the *running* faucet pod's in-memory token diverges from the *API server's* Secret bytes. Any external consumer holding a cached token loses authentication silently the next time the running pod rolls.

Notable amplification of D1 scope: D1's failure mode requires a malicious or buggy actor to delete the Secret. F5 shows the same token rotation can happen as a side effect of a routine `kupo.enabled=false` → `kupo.enabled=true` config toggle, which is much more likely to happen by accident than a Secret deletion.

NOT-A-BUG for the race per se. The token rotation is a real concern but it's structurally identical to D1's BUG-B — the fix is the same (stamp the auth Secret resourceVersion onto the pod-template annotation so the Deployment rolls whenever the Secret rotates). Not duplicating into a separate TEST_REPORT entry; flagging in D1's entry that F5 amplifies the scope.

UX observation worth keeping: a user toggling kupo off-and-on silently invalidates the faucet auth token with no surfaced status condition warning. Same D1 root cause.

Evidence under `.run/break-pass/f5/`. Namespace cleanly torn down.

## 2026-05-28 14:48 — F6 NOT-A-BUG: apply path is robust to mid-flight manager kills
Adversarial test F6 from the synthesized list: kill the operator pod during a fresh CR apply, observe whether the new manager pod completes the half-applied state idempotently.

F6a (kill immediately after apply): the entire reconcile pass committed all 9 owned children before SIGTERM landed. The kubelet's graceful shutdown drains the manager only AFTER the in-flight reconcile finishes — so this test couldn't actually probe a half-applied state, it instead confirmed the manager doesn't *abandon* a reconcile mid-flight when killed. Children: artifact CM, artifact-publisher SA/Role/RoleBinding, PVC (Bound), Deployment with stamped artifact-CM UID, primary Service, Ogmios + Kupo Services. UID stamp matches live CM. CR Ready=True in ~27s.

F6b (kill, then apply CR while no manager is running): controller-runtime's manager queues the apply until leader-elect + caches are up, then processes cleanly. Full child set created in one pass. CR Ready=True in ~30s. No errors.

NOT-A-BUG. The apply path is robust to pod-kill timing because (a) each reconcile is sequential and atomic from the API server's perspective (sub-second partial-write windows), (b) `controllerutil.CreateOrUpdate`-style semantics tolerate pre-existing children. Forcing a true half-applied state would require injecting a delay between apply steps (e.g., a webhook on Deployment create) — not reachable via pod-kill alone.

Evidence under `.run/break-pass/f6/`. Namespace cleanly torn down.

## 2026-05-28 14:50 — Category F synthesis (pause for user review)
Six probes attempted, with the original F1/F3 blocked by an unexpected upstream finding:

| Test | Theory | Verdict | Severity |
|------|--------|---------|----------|
| F0 | (uncovered: mainnet artifact CM exceeds 1 MiB cap) | BUG-A (silent failure) | **high** |
| F1 | Mithril init with non-mithril image | INCONCLUSIVE (blocked by F0) | — |
| F2 | Non-existent node image | UX-GAP (generic "Deployment not available") | medium |
| F3 | Invalid Mithril snapshot digest | INCONCLUSIVE (blocked by F0) | — |
| F4 | Missing StorageClass | UX-GAP (same boilerplate as F2) | medium |
| F5 | Faucet revoke/re-enable race | NOT-A-BUG (race is clean; token rotation compounds with D1) | — |
| F6 | Operator pod kill mid-apply | NOT-A-BUG (apply path is robust) | — |

Two new TEST_REPORT entries (F0, F2+F4 combined).

**F0 — Mainnet artifact ConfigMap exceeds 1 MiB cap, mainnet cannot be created (high).** The mainnet bundle (Byron + Shelley + Alonzo + Conway genesis files + topology + node config) exceeds Kubernetes' 1 MiB hard cap on ConfigMaps. The reconcile fails at the artifact CM apply step with `ConfigMap "...network-artifacts" is invalid: Too long: may not be more than 1048576 bytes`. The CR ends up with NO `.status` block, no conditions, no events — completely silent failure. Mainnet support shipped in PR #47 but Chainsaw tests cover only preview/preprod, so this didn't surface in CI. Fix path: move large genesis files to per-file ConfigMaps + pre-flight size check + Chainsaw mainnet smoke test.

**F2+F4 — NodeReady message is uselessly generic for image-pull and PVC-binding failures (medium).** Both `ghcr.io/nope/nope:404` (ImagePullBackOff) and `storageClassName: missing` (PVC Pending / FailedScheduling) produce identical `NodeReady=False / reason=DeploymentProgressing / message="Primary node Deployment is not available"`. The operator reads only `Deployment.status.availableReplicas` and never inspects the underlying Pod containerStatuses, Pod events, or PVC binding state. Fix path: walk the latest ReplicaSet's Pods, scan `containerStatuses[*].state.waiting` for actionable reasons, promote to NodeReady. Same fix surface works for OgmiosReady/KupoReady.

Two non-findings:

**F5 — race is clean per se (controller-runtime serializes reconciles per-CR; final state converges in ~7-10s with 0 Pod restarts).** But every kupo toggle cycle rotates the faucet auth token because `createFaucetAuthSecretWithToken` always generates a fresh random token when the Secret is absent. This is structurally identical to D1's BUG-B — the controller doesn't roll the Deployment when the auth Secret rotates, so the running pod's in-memory token diverges from the API server's Secret bytes. Amplification of D1's scope: the silent token rotation can happen from a routine `kupo.enabled=false→true` config toggle, not just adversarial Secret deletion. Fix: same as D1 (stamp auth Secret resourceVersion onto pod-template annotation so the Deployment rolls on rotation).

**F6 — apply path is robust to mid-flight manager kills.** The kubelet's graceful shutdown drains the manager only AFTER the in-flight reconcile finishes, so SIGTERM doesn't actually interrupt mid-apply in practice. Even when the manager is killed before apply, the new manager processes the workqueue cleanly via controller-runtime's caches + leader election. Forcing a true half-applied state would require injecting a delay between apply steps (e.g., a webhook); not reachable via pod-kill alone.

Category F highlight: the **F0 finding is the biggest discovery of the entire break-pass so far** — not because it's the worst kind of bug (it's not data loss, like D1/D2), but because it's a completely silent breakage of a documented major feature that wasn't on the synthesis list and wasn't caught in CI. The break-pass methodology paid for itself just by surfacing F0.

Running totals after Categories A–F:
- Tests run: 33 (A:5, B:6, C:4, D:7, E:6, F:6) — 2 INCONCLUSIVE (F1, F3, blocked by F0), 31 conclusive
- TEST_REPORT entries: **10** (A3, A4, B1, B2, B6, D1, D2, D6, F0, F2+F4)
- High severity: **5** (A4, B1, D1, D2, F0)
- Medium severity: **5** (A3, B2, B6, D6, F2+F4)
- Architectural strengths revealed in passing: E4 (replay-resistant identity binding), E2 (DeletionTimestamp parity), E1/E1b (planner-layer defense-in-depth), B3 (PVC-annotation backfill works), F6 (graceful-shutdown apply integrity)

Pause for user review. The synthesis list also has G (UX-clarity) — those were noted to be addressed as a parallel sweep rather than a separate test category, and most have been covered organically across categories (multiple "messages don't name X" findings in B2, B6, D1, E5, F2/F4). Happy to do a dedicated G sweep if you want, or close out the pass here.

Dev stack still up.

## 2026-05-28 18:10 — Close
Session closed. 33 adversarial tests run across categories A–F: 31 conclusive, 2 INCONCLUSIVE (F1 and F3 blocked by the F0 finding uncovered orthogonally). Ten findings written to `.journal/TEST_REPORT.md` with structured test/failure/suggested-fix entries — five high-severity (A4, B1, D1, D2, F0) and five medium-severity (A3, B2, B6, D6, F2+F4). No code changes; documentation-only session.

Hand-off state:
- `.journal/029/SUMMARY.md` written with goal/outcome/decisions/changes/open-threads/references/lessons.
- `.journal/INDEX.md` updated with session 029 row.
- `.journal/TECH_NOTES.md` updated with a pointer to the TEST_REPORT.md known-issues catalog.
- `.journal/TEST_REPORT.md` is the durable deliverable — future implementation work touching the relevant code paths should consult it.
- All `.run/break-pass/<test-id>/` evidence files preserved in tree.
- Dev stack stopped via `moon run root:dev-down` from master (3s clean shutdown).
- No PR opened — this session produced no source code changes.
- Snapshot/restore design work from the pre-pivot phase of session 029 (commits `8047673`, `50ff28f`, and `.journal/SNAPSHOT_DESIGN.md`) remains in the journal as the original session's first artifact; session 030 (opened in parallel by a separate agent) appears to be resuming that work.

Worktrunk worktrees unchanged — no implementation worktree was created for this session (test-pass against master). The journal worktree at `.wt/journal-jmgilman` remains active for future session work.
