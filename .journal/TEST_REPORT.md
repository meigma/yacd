# Adversarial Test Report

Running log of confirmed operator failures from the session-029 break-the-operator
pass. One failure per section. NOT-A-BUG outcomes are not recorded here — see the
session's `NOTES.md` for the full pass record.

Format per failure:
- Test ID + short name
- **Test** — what we exercised
- **Failure** — what we observed and why it qualifies as unexpected
- **Suggested Fixes** — concrete options with tradeoffs

Findings are grouped by category. Severity tags: **low**, **medium**, **high**.

---

## A3 — Artifact ConfigMap external corruption rolls primary Pod 1:1 (medium)

### Test
Bring up a local-mode CardanoNetwork to `Ready=True` so the controller publishes
the owned `<network>-network-artifacts` ConfigMap with verified data and a
stable primary Pod (`a3-net-node-*`). Then externally corrupt the CM's
`data.topology.json` key on a ~8-second cadence for 16 iterations (~130 s), and
watch the primary `Deployment` and the live Pod's ReplicaSet history.

Files touched directly with `kubectl patch`:
- `<network>-network-artifacts` ConfigMap (`internal/controller/cardanonetwork/apply.go`
  → `artifactConfigMapNeedsRecovery` / `applyNetworkArtifactsConfigMap`).
- Observed: primary `Deployment` (`a3-net-node`), pod-template annotation
  `yacd.meigma.io/network-artifacts-configmap-uid` and the resulting
  ReplicaSet/Pod churn.

### Failure
Sustained external corruption produces sustained pod rolls with no
operator-side backoff:

- 16 corruption iterations → 16 Deployment generation bumps (2 → 18).
- 16 owned-CM UID rotations; pod-template annotation correctly tracks each new
  UID (so the `Owns` + annotation contract is honest), but the rotation alone
  is what triggers the rollout.
- 16 new ReplicaSets created. Kubernetes RS GC trimmed to 11 once
  `revisionHistoryLimit` defaulted in.
- Each cycle takes the live Pod through `Pending → Running` (full restart),
  severing in-progress chain reads.
- Operator log shows `configMapOperation` alternating `updated` / `created`
  with `deploymentOperation:"updated"` on essentially every reconcile during
  the burst. No rate limit, no circuit, no backoff.
- Recovery is clean: as soon as corruption stops, `deploy_gen` freezes within
  one reconcile and remains stable for the full 90 s recovery window.

Severity is **medium** rather than high because the controller's behaviour on
each event is correct in isolation — drift must trigger republish, and the
republish via delete-and-recreate necessarily rotates the CM UID, which is the
intentional rollout trigger for pod-template freshness. The damage is gated on
sustained external pressure. With ~3 s per Pod roll on Kind + M4 Max, an
adversary patching every ~5 s holds availability under 50% indefinitely.

Evidence: `.run/break-pass/a3/`

### Suggested Fixes

1. **Skip the Deployment annotation update when the artifact data hash is
   unchanged (preferred).** Today the rollout is gated by CM UID; replace that
   gate with the CM's `yacd.meigma.io/artifact-data-hash` annotation. When
   delete-and-recreate produces an identical canonical content hash (the
   common case under adversarial corruption, because the controller restores
   exactly the same bundle), the Deployment annotation does not change and the
   Pod does not roll. Honest data changes (a real spec edit that changes the
   network plan) still bump the hash and roll the Pod, preserving the
   intended freshness semantics. Code: `internal/controller/cardanonetwork/apply.go`
   `setDeploymentArtifactConfigMapUID`, plus the matching annotation key in
   `annotations.go`.

2. **Coarse per-network rate limit on artifact-recovery rollouts.** Track the
   timestamp of the last artifact-recovery-driven Deployment update on the CR
   (e.g., in `status.artifacts` or via a controller-local LRU) and refuse to
   roll the primary Deployment more than once per N seconds for the same
   artifact-recovery reason. N could be a manager flag (default ~60 s). This
   bounds the damage even if option (1) misses an edge case — pod rolls
   become at most 1/minute regardless of corruption rate. Weakness: the
   controller still re-applies the CM on every event, so reconcile noise
   remains.

3. **Watch-side debouncer for owned-artifact-CM updates** (less surgical).
   Add a predicate on `Owns(&corev1.ConfigMap{})` that suppresses events whose
   `data-hash` annotation matches the last-seen value. Effectively turns the
   `Owns` watch into a hash-aware watch. Risk: subtle drift cases where the
   hash annotation lags the actual data could be missed; option (1) is
   strictly safer because it gates the *rollout*, not the *enqueue*.

Option (1) is the smallest behaviour change and addresses the failure mode
directly. Option (2) is a defense-in-depth backstop.

---

## A4 — Placement peer toggling severs stable primary-sidecar attachment (high)

### Test
Bring up a local-mode CardanoNetwork to `Ready=True`. Apply two CardanoDBSyncs
against it, both pinned to `placement.mode: primarySidecar`. The first
(`a4-dbs-stable`) is the natural incumbent; the second (`a4-dbs-toggler`)
starts at `dedicatedFollower` to let the incumbent attach cleanly, then
toggles between `primarySidecar` and `dedicatedFollower` every 12 s for 10
cycles. Sample at 2 s for primary `Deployment.metadata.generation`, network's
`DBSyncAttachmentReady` condition, both DBSyncs' `SidecarMaterialReady`
conditions, and the live primary Pod's container set.

### Failure
The toggler reliably evicts the *unrelated* stable claimant's sidecar from the
primary Pod every cycle:

- 10 toggles → 10 primary `Deployment` generation bumps (2 → 12).
- 84 `Applied` reconcile log entries on the network during the 2-min burst,
  with `deploymentOperation:"updated"` on essentially every one.
- 10 real container-set changes on the live primary Pod: `cardano-db-sync`
  added on every `dedicatedFollower` toggle and removed on every
  `primarySidecar` toggle. The Pod's sidecar is severed, restarted, severed,
  restarted — every 12 s.
- Stable DBSync's `SidecarMaterialReady` flips True↔False 10 times with reason
  `PlacementConflict` / `applyBlocked`. The stable, pre-existing winner is
  dethroned by the late competitor on every cycle.
- Recovery is clean: with the toggler parked at `dedicatedFollower`, the
  attachment correctly snaps back and `deploy_gen` freezes.

Two root causes, both confirmed in code:

1. `internal/controller/cardanodbsync/placement.go:31-38` — `reconcilePlacement`
   treats `len(claims) > 1` as a **symmetric** block. Both claimants get
   `applyBlocked / PlacementConflict`. There is no winner-by-creation-time or
   winner-by-UID tiebreak.

2. `internal/controller/cardanonetwork/dbsync_sidecar.go:60-67` —
   `primaryDBSyncAttachment` returns no `Attachment` when `len(claims) != 1`,
   so `primaryWorkloadBuilder.Build(network)` renders the Deployment *without*
   the sidecar container. The PodTemplateSpec diff is a real rollout.

Severity is **high** because (a) the cardano-db-sync sidecar's node-socket
continuity and any in-progress db-sync work is destroyed every cycle, (b) the
attacker doesn't need a privileged role — they only need permission to create
or edit *their own* CardanoDBSync, (c) the victim has no observability into
which peer is causing the churn (see UX-GAP below), and (d) YACD's design
explicitly contemplates a hosted cluster shared by a team (`DESIGN.md`
"Goals"). The single-tenant local-dev posture today buys time, but the bug
becomes acute the moment multi-tenancy is real.

There is also a **UX-GAP** (G4 in the synthesis list, now sharpened): the
`PlacementConflict` condition message reads
`"CardanoNetwork %q has multiple primarySidecar CardanoDBSync claims; exactly
one primary-sidecar claim is allowed"`. It names neither the incumbent, the
conflicting claimants, nor which one the user should change. A user receiving
this on `a4-dbs-stable` has no signal that `a4-dbs-toggler` is the cause.

Evidence: `.run/break-pass/a4/`

### Suggested Fixes

1. **Stable-winner tiebreak in `primarySidecarClaims` (preferred).** Sort
   candidate claims deterministically — `CreationTimestamp` ascending, then
   UID ascending as a final tiebreaker — and treat `claims[0]` as the
   incumbent. Late competitors are rejected on themselves with
   `PlacementConflict`, but they do not dethrone the existing incumbent. The
   network's `primaryDBSyncAttachment` then continues to attach to the
   incumbent until the incumbent itself becomes non-attachable (deleted,
   spec-changed away from `primarySidecar`, etc.). Code:
   `internal/controller/cardanodbsync/placement.go` `primarySidecarClaims`
   + the consumer at `dbsync_sidecar.go:60-67`. Behaviour change is local and
   conservative: in the steady-state single-claim case nothing changes; the
   only visible effect is which claim wins when two arrive close together,
   and the new behaviour matches user intuition (first-come-first-served).

2. **Sharpen the `PlacementConflict` message.** Include the incumbent name and
   the list of conflicting peers in the condition `message`. Even without
   fix (1), this lets the victim diagnose the issue. With (1) in place, the
   message becomes informational — "you are not the incumbent; the incumbent
   is `<name>`; remove or change one to attach."

3. **Defense-in-depth: ignore peer-driven Deployment churn in the network's
   apply path.** Even with (1), a stable-winner change (e.g., the toggler
   genuinely promoted itself after the incumbent was deleted) should roll the
   Pod. But adding a debounce on the network-side `Watches(&CardanoDBSync{})`
   handler so that purely peer-status-driven enqueues don't cascade into
   primary Deployment patches when the chosen incumbent hasn't changed is a
   cheap additional guard. Most useful if you ever have to walk back (1).

Option (1) is the smallest correct fix and removes the failure entirely.
Option (2) ships independently and improves UX even before (1) lands.
