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

---

## B1 — Status-fingerprint forgery permanently bricks CardanoNetwork (high)

### Test
Bring up a local-mode CardanoNetwork to `Ready=True` so both
`status.network.{network,localnet}Fingerprint` and the owned PVC's
`yacd.meigma.io/{network,localnet}-fingerprint` annotations are stamped with
the same accepted-identity hash. Then forge the status fingerprints via the
`status` subresource (with no spec change), and observe how the controller
reconciles its two sources of truth.

Three sub-tests run in sequence in the same CR:
1. **B1a** — patch `status.network.networkFingerprint` to `deadbeef-…`.
2. **B1b** — patch BOTH `networkFingerprint` and `localnetFingerprint` to
   forged values.
3. **B1c** — leave the forged status in place, then also overwrite the PVC's
   `yacd.meigma.io/localnet-fingerprint` annotation to a third bogus value
   (simulates an honest restore attempt against tampered status).

Sampling at 2 s for 30 s after each forgery; recovery probe at the end (restore
PVC annotation to baseline, then bump `spec.node.port` to force a generation
change and another reconcile pass).

### Failure
Two overlapping bugs:

**BUG-A — lying status with effectively unbounded persistence (B1a, B1b).**
The forged status fingerprint is silently retained for the full observation
window with `Ready=True / ReconcileSucceeded`. Root cause:
`For(&CardanoNetwork{}, ctrlbuilder.WithPredicates(predicate.GenerationChangedPredicate{}))`
filters out status-only updates, so the forgery never enqueues the CR.
`setNetworkIdentityStatus` (`status.go:167`) only runs after a successful
primary-workload apply, so the controller does not eagerly overwrite the
forged status. The lying-status window is bounded only by some *other*
unrelated event triggering a reconcile (owned-child churn, manager restart,
spec edit, etc.). Observers reading `status.network.networkFingerprint` in
the meantime see a value the controller never produced.

**BUG-B — permanently unrecoverable Degraded once status and PVC diverge (B1c).**
With forged status still in place, any subsequent reconcile rejects the plan
at `validateAcceptedNetworkFingerprint` (`callbacks.go:153`), which consults
*only* `status.network.{network,localnet}Fingerprint`. The CR enters
`Ready=False, Degraded=True, reason=UnsupportedLocalnetChange` with a message
telling the user to delete and recreate the CR. Restoring the PVC annotation
to its true baseline does NOT recover the CR — the status check fires first
and short-circuits before `validateLocalnetFingerprint` on the PVC ever runs.
Bumping `spec.node.port` to force a real reconcile (generation 1 → 2,
`observedGeneration` reaches 2) repeats the same rejection;
`setNetworkIdentityStatus` never runs because the validate step never
succeeds. The CR is bricked: no spec edit can resolve it because every
reconcile rejects on the stale forged status before the controller would
overwrite the status with the truth.

RBAC consideration: the
`+kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanonetworks/status,verbs=get;update;patch`
marker grants the controller (and anyone with that role) status-subresource
patch access. Admins are often granted status-subresource access independent
of spec access on the assumption that status is operator-internal and
non-destructive. B1 shows that assumption is false.

**UX-GAP.** Every condition message after B1c blames the user — *"localnet
inputs changed; delete and recreate to change network parameters"* — when no
spec changed and the actual cause is a forged status. A user who hit this via
tooling that round-trips status through a subresource patch (backup/restore,
GitOps tooling, kubectl edits to status) has no signal that the fix is to
recompute the status fingerprint rather than recreate the CR.

Severity is **high** because (a) recovery requires CR delete which loses
chain state and PVC data, (b) the attack vector is a single `kubectl patch
--subresource status` with a role that is commonly considered safe, (c) the
condition message actively misdirects the user.

Evidence: `.run/break-pass/b1/`

### Suggested Fixes

1. **Treat the PVC annotation as the single source of truth for accepted
   identity; rebuild status from it on every reconcile (preferred).** Today
   the validate step reads from status and the PVC annotation is a redundant
   on-disk copy. Invert: have the controller read accepted identity from the
   PVC annotation, validate against that, and overwrite `status.network.*`
   from the live annotation at the top of every reconcile. The PVC is
   harder to forge (admins generally do not have free-form `update pvc`
   permission), and aligning the validate path with the storage-backed
   truth removes the two-sources-of-truth divergence entirely. Status
   becomes informational/derived. Code: `internal/controller/cardanonetwork/{callbacks,status,apply}.go`.
   **This pattern already exists in the codebase** — see
   `internal/controller/cardanodbsync/apply.go:271,325`
   (`currentAcceptedDBSyncPlacementMode` → `acceptedDBSyncPlacementModeFromPVC`),
   which B3 confirmed defeats the analogous status-clear attack on
   `acceptedPlacementMode`. The fix here is to add the symmetric
   `acceptedNetworkFingerprintFromPVC` helper and call it from
   `validateAcceptedNetworkFingerprint` whenever
   `status.network.*Fingerprint` is empty.

2. **Eagerly overwrite forged status fingerprints in every reconcile
   (defense-in-depth).** Even if (1) is too disruptive, the controller can
   compute the expected status fingerprint from the desired plan (or the
   PVC annotation) at the *start* of each reconcile and patch status to
   match if it has drifted. Combined with (3), the lying-status window
   collapses to a single reconcile cycle.

3. **Add a status-change watch / drop GenerationChangedPredicate on the
   primary For() (small).** Today, status-only changes do not enqueue the
   CR, so forged status persists until something else fires. Switching the
   predicate to also enqueue on status fingerprint changes, OR adding a
   periodic resync ticker on the controller, bounds the lying-status window
   to the resync period. Tradeoff: more reconcile noise; pair with (2) so
   each extra reconcile is cheap.

4. **Sharpen `UnsupportedLocalnetChange` message.** Include the observed CR
   `status.network.localnetFingerprint`, the PVC annotation value, and the
   freshly computed plan fingerprint in the condition `message`. This lets
   any user (or operator-on-call) immediately see when status diverges from
   PVC + plan and points at "your status is wrong, not your spec." Cheap
   to ship independently.

Option (1) removes the bug at the root by collapsing the two sources of
truth into one. Option (2)+(3) is a smaller-blast-radius alternative that
makes forged status self-healing but keeps the current architecture.

---

## B2 — CardanoDBSync DB identity forgery: recoverable brick, but message demands CR delete (medium)

### Test
Bring up a local-mode CardanoNetwork and a CardanoDBSync with managed Postgres.
Once `status.database.acceptedIdentityFingerprint` is published (~8 s, well
before sync starts), forge it via the `status` subresource to
`deadbeef-forged-db-identity` and observe. Then bump a benign spec field
(`spec.resources.limits.memory`) to force a reconcile. Then try to recover
by restoring the status fingerprint and bumping spec again.

### Failure
Two sub-failures:

**B2a/B2b — bricked from the user's perspective.** Within ~2 s of the
status patch the CR flips to `Ready=False / Degraded=True /
UnsupportedDatabaseIdentityChange`. Forced reconciles via spec bumps repeat
the rejection. The reject message reads verbatim: *"CardanoDBSync
database-affecting inputs changed from accepted identity; delete and
recreate the CardanoDBSync with a fresh or compatible external database"*.

The controller already stores the true accepted identity on the managed
Postgres PVC annotation (`yacd.meigma.io/dbsync-database-identity`), but
`validateAcceptedDBSyncDatabaseIdentity`
(`internal/controller/cardanodbsync/apply.go:184-194`) consults *only*
`status.database.acceptedIdentityFingerprint` when that field is non-empty.
The cluster holds the right value; the controller refuses to consult it.

**B2c — technically recoverable, but only by an expert who knows the trick.**
Restoring the status fingerprint to the true value via another status patch
is not enough on its own: `GenerationChangedPredicate{}` still suppresses
status-only re-enqueue, so the restored status sits unnoticed for ~16 s.
Combining the status restore with a benign spec bump (which DOES bump
generation past the predicate) clears the Degraded within 2 s and the CR
fully recovers — no CR delete required.

An honest user following the printed message ("delete and recreate") will
delete the CR. That cascades through Kubernetes garbage collection (the CRs
have no finalizers) and drops the managed Postgres PVC, the follower-node
PVC, the dbsync state PVC, and the generated `<dbsync>-postgres-auth`
Secret. The "fresh database" the user is told to bring is themselves; the
forgery has effectively forced a data-loss recovery.

The message also names *no specific field*. A user who doesn't know status
was tampered with cannot tell whether the controller is complaining about
image / database name / user / port / password key / ledger backend, etc.

Severity is **medium** rather than high because (a) an expert can recover
without data loss, (b) the bug requires an actor with
`cardanodbsyncs/status` patch permission, (c) the controller's anchor
(PVC annotation) is correct and just one code change away from being
consulted. But for any user following the on-screen guidance the effect is
the same as B1: data-loss CR delete.

Evidence: `.run/break-pass/b2/`

### Suggested Fixes

1. **Fall through to the PVC annotation when validating accepted identity
   (preferred).** Change `validateAcceptedDBSyncDatabaseIdentity` to read
   the PVC annotation as the authoritative anchor and treat the CR-status
   field as a derived cache that the controller writes but does not trust
   for validation. Forged status becomes a no-op; the controller will
   overwrite it on the next reconcile-with-apply, and the CR never goes
   Degraded. This is the analogous fix to B1's option (1) but smaller
   because the PVC anchor already exists and is already trusted at
   workload apply time. Code: `internal/controller/cardanodbsync/apply.go:184-194`
   `validateAcceptedDBSyncDatabaseIdentity`, plus the cleanup of any other
   readers of `status.database.acceptedIdentityFingerprint` that should be
   re-pointed at the annotation. **The pattern already exists in the
   same file for the placement-mode field** — see `apply.go:271,325`
   (`currentAcceptedDBSyncPlacementMode` →
   `acceptedDBSyncPlacementModeFromPVC`), which B3 confirmed defeats the
   analogous status-clear attack on `acceptedPlacementMode`. Generalize
   it to `acceptedDBSyncDatabaseIdentityFromPVC` and call it whenever
   `status.database.acceptedIdentityFingerprint` is empty *or* (defense
   in depth) whenever it diverges from the PVC annotation value.

2. **Rewrite the reject message to name the drifting field and the
   non-destructive recovery (independent of #1).** Today's message is
   actively harmful when the divergence is a forged status. New message
   shape: "CardanoDBSync accepted database identity differs from the
   current spec on field(s) `<list>`. Inspect `status.database.acceptedIdentityFingerprint`
   vs `<pvc>.metadata.annotations[yacd.meigma.io/dbsync-database-identity]`
   — if status was patched directly without a spec change, restore status
   from the PVC annotation and re-apply spec to trigger reconcile. If the
   spec change is intentional, recreate the CR with a fresh or compatible
   database." This is verbose but accurate, and only fires on a real
   identity divergence (rare).

3. **Drop GenerationChangedPredicate, or add status-FP-change enqueue
   (defense in depth).** Same trade-off as B1 option (3): more reconcile
   noise versus tighter loop between forgery and detection. Pairs well
   with (1) so each extra reconcile is cheap and self-healing.

Option (1) is the cleanest fix and lines up with the analogous B1
suggestion. Option (2) is independently shippable and stops the most
costly user mistake even before (1) lands.

---

## B6 — Storage expansion failure on non-expandable StorageClass is invisible in CR status (medium)

### Test
Bring up a local-mode CardanoNetwork with `spec.node.storage.size=2Gi`
backed by the cluster's default StorageClass. On Kind that's `standard`
(local-path provisioner) which has `allowVolumeExpansion=<unset>` —
effectively disabled. After `Ready=True`, patch the storage size up
(2Gi → 5Gi). Sample at 2 s for 30 s. Then revert (5Gi → 2Gi) and observe
recovery. Then repeat with a smaller increment (2Gi → 3Gi) to confirm the
behaviour is StorageClass-capability-dependent, not size-dependent.

### Failure
The controller silently swallows the PVC expansion failure as far as CR
status is concerned:

- CR `metadata.generation` correctly bumps (1 → 2 on the patch).
- CR `status.observedGeneration` stays at 1 for the full 30 s window — the
  reconcile keeps failing inside `applyPrimaryPersistentVolumeClaim`.
- Live PVC `spec.resources.requests.storage` stays at 2Gi (API server
  rejected the resize patch synchronously). `status.capacity.storage` is
  unchanged.
- CR conditions: `Ready=True / Degraded=False` with the stale message
  `"Primary node, artifact publisher, and chain API resources are
  applied"`. The failure is not reflected in any condition.
- No PVC events are emitted (Kubernetes rejects the resize patch with a
  synchronous Forbidden — no event recorded).
- The actual error appears **only** in the operator logs:
  `persistentvolumeclaims "b6-net-node-state" is forbidden: only
  dynamically provisioned pvc can be resized and the storageclass that
  provisions the pvc must support resize`. Logged at ERROR level, ~14 hits
  in the first 80 s under controller-runtime's exponential backoff, then
  every 20–40 s.

The 2Gi → 3Gi attempt produces an identical outcome. Behaviour is
independent of the requested size — it depends entirely on the
StorageClass's `allowVolumeExpansion` flag.

Reverting the storage size to the original (B6b) recovers the CR
within ~10 s: `observedGeneration` catches up, forbidden errors stop, and
the controller resumes normal reconciles. No data loss, no orphan
resources.

Severity is **medium** because (a) recovery is clean and non-destructive,
(b) no data is lost, but (c) the user-visible misinformation is real and
the underlying Kubernetes error message is excellent. A user runs
`kubectl get cardanonetwork` and sees `Ready=True`, believes the
expansion took effect, and walks away. The signal that something is wrong
is `observedGeneration < metadata.generation` (subtle) plus operator
logs (usually inaccessible to non-platform users).

Code path (per the agent's read):
`internal/ctrlkit/storage/storage.go:78-112`
(`PersistentVolumeClaimDriftFor`) only flags class drift, decrease,
access-mode change, and `RequestedStorageClass`. Expansion is
intentionally allowed through. The controller PATCHes the PVC,
the API server returns Forbidden, the error bubbles up unmodified through
`applyPrimaryPersistentVolumeClaim` / Reconcile, and condition writes are
gated on the typed-error path (`statusConditionError`) which this
particular API error never satisfies.

This pattern likely also affects the CardanoDBSync controller's PVC apply
paths (state PVC and managed Postgres PVC), both of which are user-
configurable expandable; B6 only tested the network's primary PVC.

Evidence: `.run/break-pass/b6/`

### Suggested Fixes

1. **Classify Forbidden/Invalid PVC PATCH errors into a typed
   `statusConditionError` (preferred).** When `applyPrimaryPersistentVolumeClaim`
   (and the equivalent DBSync paths) gets back an `apierrors.IsForbidden`
   or `apierrors.IsInvalid` from the PVC update, wrap it as
   `statusConditionError{Reason: "StorageExpansionRejected", Message:
   <api server text>}` and let the existing `handlePrimaryWorkloadApplyError`
   surface it to `Degraded` with the rich underlying message. The API
   server's text already names the resize requirement explicitly; just
   propagating it ends the silent-failure problem and gives users a
   `kubectl describe` answer. Code:
   `internal/controller/cardanonetwork/apply.go::applyPrimaryPersistentVolumeClaim`
   error handling; same for `internal/controller/cardanodbsync/apply.go`
   PVC apply paths.

2. **Pre-flight check for `allowVolumeExpansion` (defense in depth).**
   Before issuing the PATCH, the controller could look up the bound PVC's
   StorageClass and read its `allowVolumeExpansion` field. If false, fail
   the reconcile early with a *different* typed status condition
   reason (`StorageClassDoesNotSupportExpansion`) so the user sees the
   issue before the operator hammers the API server with a doomed PATCH.
   Useful for log-noise reduction; option (1) is the more important fix
   because (2) is also vulnerable to admission webhooks or CSI driver
   quirks that the StorageClass flag doesn't capture.

3. **Apply the same fix to the CardanoDBSync state and managed Postgres
   PVC paths.** The bug is structural to "PVC PATCH error returned
   unmodified"; the same surface exists for any expandable PVC the
   operator owns. Bundle (1) with a sweep across all PVC apply paths.

Option (1) is the smallest correct fix and removes the silent-failure
window. Option (2) is a small QoL improvement on top.



