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
