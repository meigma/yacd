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

---

## D1 — Faucet auth Secret deletion: lying status + silent token rotation (high)

### Test
Bring up a local-mode CardanoNetwork with Ogmios + Kupo + faucet enabled.
Once `FaucetReady=True`, save the live auth-Secret token bytes for later
comparison, then `kubectl delete secret <network>-faucet-auth`. Sample CR
conditions and Pod state at 5 s intervals for 90 s. Then restart the
operator manager to short-circuit the 10-minute repair requeue, and
compare the regenerated token to the baseline.

### Failure
Two compounding bugs:

**BUG-A — lying status for the full repair window (worst case ~10 min).**
After the Secret is deleted, the kubelet's already-projected volume keeps
serving the cached token file in-memory, and the faucet binary holds the
token in memory from container start. So:
- Pod's `faucet` container stays `state=running` with unchanged
  `startedAt`; no restart; no `CreateContainerConfigError`; no
  `MountVolume.SetupFailed`.
- CR conditions stay `FaucetReady=True, Ready=True` for the full 90 s
  observation (and theoretically until the next reconcile fires).
- The controller HAS an honest condition message ready
  (`"Faucet auth Secret is missing"` with reason `PrimaryWorkloadMissing`)
  — but it only executes inside `Reconcile`, and the controller does not
  watch Secrets, so `Reconcile` does not fire on the Secret deletion. The
  next reconcile is the `faucetSecretRepairRequeueAfter = 10 * time.Minute`
  tick in `internal/controller/cardanonetwork/controller.go`.

Result: a ~10-minute window where `Ready=True` is mathematically incorrect
and the user has no signal anywhere that auth-state has diverged.

**BUG-B — silent token rotation on repair.** Once the 10-min requeue (or
a manager restart) does fire, `createFaucetAuthSecretWithToken` in
`internal/controller/cardanonetwork/faucet_auth.go` generates a fresh
random token on the not-found branch — no migration, no preservation, no
out-of-band store. Confirmed in D1c: baseline token sha256
`57fa6745…bc0d9`, regenerated token sha256 `230e0601…1bc91`. The
Deployment is patched with `deploymentOperation:"updated"` but the
pod-template-hash is unchanged, so the Pod does NOT roll. The running
faucet binary continues to authenticate against the original in-memory
token (call it A) while the API server now holds a different token (B).
Any user holding A keeps working against the live pod. Any operator
following `kubectl get secret ... -o jsonpath='{.data.token}'` to obtain
the "current" token gets B and discovers the system silently rejects A.

The truly bad case: a future pod roll (node reboot, image upgrade,
unrelated CR change that touches the pod-template, manager re-deploys
the workload) swaps the running token from A to B at an arbitrary later
time — silently invalidating every cached A in the user's environment
with no preceding signal anywhere. The CR is `Ready=True` throughout the
entire history. Faucet operators with cached credentials get auth
failures with no diagnostic.

Severity is **high** because:
- The faucet token is the only secret control gate on the only mutating
  endpoint YACD currently exposes (UTxO topup).
- Silent invalidation cannot be observed from CR status, events, or any
  user-accessible signal.
- Recovery requires three steps (Secret repair, Pod roll, user re-fetch)
  but the operator only does the first; the user is responsible for
  noticing the second and third (which the operator gives no hint about).
- Failure window is bounded by `faucetSecretRepairRequeueAfter`
  (10 minutes today) at minimum, and *unbounded* in the runtime-vs-API
  divergence regime — depending on when the pod next rolls.

Evidence: `.run/break-pass/d1/`

### Suggested Fixes

1. **Roll the Deployment whenever the auth Secret is created or rotated
   (preferred — smallest surgery, eliminates the silent A-vs-B
   divergence).** Stamp the auth Secret's resourceVersion (or a hash of
   the token bytes) onto the primary Deployment's pod-template annotation
   alongside the existing `network-artifacts-configmap-uid` stamp.
   Whenever `applyPrimaryFaucetAuthSecret` returns a `created`/`updated`
   OperationResult, the resourceVersion changes, the pod-template-hash
   changes, the Deployment rolls. The faucet container restarts and
   picks up token B from the freshly-projected volume. The controller's
   existing honest-message path then becomes correct: cached user tokens
   are invalidated immediately at the HTTP layer (faucet returns 401),
   not silently-then-eventually-broken at an arbitrary future moment.
   Code: `internal/controller/cardanonetwork/apply.go::applyPrimaryWorkloadResources`
   ordering, `faucet_auth.go::applyPrimaryFaucetAuthSecret`, and the
   annotation key in `annotations.go`. This is the cleanest fix and
   stands alone — no architecture change to the requeue model required.

2. **Add a labelled-selector watch on faucet auth Secrets to shrink the
   lying-status window from 10 minutes to seconds.** The TECH_NOTES
   rationale for not watching Secrets ("avoiding list RBAC") can be
   addressed with a `WithEventFilter` predicate that drops events for
   Secrets without the controller's standard label set, or with a
   `client.WithFieldSelector` on a sub-resource client. Either approach
   gives sub-second repair latency. Pair with (1) — without (1), faster
   repair still leaves the running-token-vs-API-token divergence.

3. **Preserve the previous token bytes on repair (heaviest, optional).**
   Requires either an in-memory LRU on the controller keyed by CR UID
   (lost on manager restart), an out-of-band Secret in the operator's
   own namespace, or a finalizer-based copy. This would let the operator
   recreate the *same* token bytes on Secret deletion, so cached user
   credentials keep working transparently. Useful only if option (1) is
   considered too disruptive — usually it isn't, because (1) makes the
   token rotation immediately visible at the HTTP layer, which is the
   correct UX (auth tokens are not supposed to silently persist after
   their Secret is deleted).

Option (1) is the smallest correct fix and removes BUG-B (silent
divergence) directly. Pairing with (2) collapses BUG-A's window from
10 minutes to seconds.

---

## D2 — PVC stuck Terminating: silent lying status + data loss on recovery (high)

### Test
Bring up a local-mode CardanoNetwork to `Ready=True`. Add a foreign
finalizer to the primary node-state PVC (`test.example.io/never-removed`)
and `kubectl delete` the PVC. It enters `Terminating` and stays there
because Kubernetes refuses to finalize while the finalizer is present and
the Pod still has the volume mounted. Observe CR conditions over 60 s.
Then remove the foreign finalizer and observe the recovery path.

### Failure
Two compounding bugs:

**BUG-A — silent lying status while PVC is Terminating.** The CR stays
`Ready=True / Degraded=False / NodeReady=True` for the full 66 s
observation window while the live PVC has a non-zero `deletionTimestamp`.
Root cause: `ApplyOwnedObject` (`internal/ctrlkit/apply/apply.go`)
does not check `DeletionTimestamp`. The reconciler's flow:
- `Get` returns the live (Terminating) PVC.
- Owner check passes (we own it).
- `validatePrimaryPersistentVolumeClaim`
  (`internal/controller/cardanonetwork/callbacks.go`) only checks
  localnet-fingerprint and storage drift — no `DeletionTimestamp` gate.
- `mutatePrimaryPersistentVolumeClaim` produces no diff.
- Reconcile logs `persistentVolumeClaimOperation:"unchanged"` and
  reports success.

No condition reflects the underlying problem. The operator log is silent.
A user reading `kubectl get cardanonetwork` cannot tell that the storage
they're "ready" on is mid-deletion.

**BUG-B — recovery silently destroys localnet data.** When the foreign
finalizer is removed and the PVC actually deletes, the controller's
recovery path technically works — within seconds a new PVC under the
same name appears via `ApplyOwnedObject`'s NotFound branch with the
correct localnet-fingerprint annotation. But the downstream consequences
are catastrophic:
- The kubelet binds the new (empty) volume into the still-running
  primary Pod.
- Container restarts do NOT re-run init containers. The cardano-testnet
  create-env init container — which populates `/state` with genesis,
  topology, and node config on first start — never re-fires.
- `cardano-node` enters CrashLoopBackOff with `Yaml file not found:
  /state/env/configuration.yaml`. ogmios + kupo go Unhealthy.
- Deployment refuses to spin a replacement Pod (`1 unavailable / 0
  terminating`). The new PVC sits Pending with `WaitForFirstConsumer`
  because the only consumer is the doomed Pod bound to the old (gone)
  PV.
- CR conditions stuck at `Ready=False / DeploymentProgressing`,
  `NodeReady=False / DeploymentProgressing`,
  `Degraded=False / ReconcileSucceeded`. **A user reading the CR cannot
  distinguish "rollout in progress" from "data permanently destroyed,
  manual pod delete required."**

Even if the user manually deletes the broken Pod (the natural next
recovery action), the init container re-runs and the new PVC is
populated to a fresh-genesis localnet — but the *original* localnet
state (any wallets the user funded, any chain progress) is gone. The
localnet-fingerprint validation that exists specifically to prevent
identity drift cannot help here because the freshly-init'd PVC carries
the same fingerprint (the localnet plan is deterministic). Validation
passes; data is gone.

This crosses an explicit YACD design contract documented in TECH_NOTES:
*"A `CardanoNetwork` localnet is stable for its lifetime"* and *"Delete
and recreate the CR/PVC to change localnet parameters."* An external
actor with `update pvc` permission (or a buggy admission webhook that
mishandles finalizers) can destroy that stability with no operator-side
detection.

Severity is **high** because (a) localnet data loss is silent and
unrecoverable, (b) the only signal during the lying window is in the
PVC's `deletionTimestamp` (not surfaced anywhere a user typically
looks), (c) the recovery damage is invisible at the CR level — a user
who waits patiently for "rollout to finish" gets fresh-genesis instead
of their funded localnet, (d) the attack surface is broad (`update pvc`
is granted to many roles, and stuck finalizers are a known operational
pattern in clusters with admission webhooks, backup tools, or third-
party CSI drivers).

Evidence: `.run/break-pass/d2/`

### Suggested Fixes

1. **DeletionTimestamp gate in the apply path (preferred for BUG-A).**
   When `ApplyOwnedObject`'s `Get` returns a child with non-zero
   `DeletionTimestamp`, treat it as a typed `statusConditionError` with
   reason `PVCBeingDeleted` (or generically `ChildBeingDeleted`) and a
   message that names the finalizers blocking deletion. The CR
   immediately goes `Degraded=True` with an honest message. The user
   sees `kubectl describe cardanonetwork` and learns a finalizer is the
   problem; they can resolve it or accept the data loss before recovery
   damage occurs. Code: `internal/ctrlkit/apply/apply.go::ApplyOwnedObject`
   plus per-controller callbacks for the conditions surface (the
   pattern already exists for owner-conflict via `OwnerConflict`
   callback).

2. **Pod rotation on PVC recreation (required for BUG-B).** When the
   controller hits the NotFound branch for an owned PVC name that it
   previously stamped (i.e., the controller's prior reconcile saw an
   owned PVC with this name, now it's gone), the recovery path must
   also delete the consuming Pod so the init container re-runs and
   re-populates state. Code:
   `internal/controller/cardanonetwork/apply.go::applyPrimaryPersistentVolumeClaim`
   — extend the apply to return `OperationResultCreated` along with a
   signal to `applyPrimaryWorkloadResources` that the consuming Pod
   should be deleted in the same reconcile. Track "previously owned this
   PVC name" via a CR-status flag (or just via the existence of a prior
   `status.network.localnetFingerprint`, which implies the PVC was
   previously accepted).

3. **Sharpen the "stable for lifetime" contract with a CR-level
   refusal-to-recover stance (alternative to #2).** Instead of silently
   recreating the lost PVC, the controller could refuse — go Degraded
   with `LocalnetStateLost` and message *"Primary PVC was deleted
   external to the controller; the localnet state cannot be recovered.
   Delete and recreate the CardanoNetwork to start a fresh localnet."*
   This is more conservative than (2) but matches the documented
   "stable for lifetime" promise more honestly: the controller's
   acceptance contract says the localnet state survives; if it doesn't,
   the controller should not pretend it does. The user makes the
   destruction decision explicitly via CR delete.

Pair (1) with either (2) or (3). (1) alone leaves the BUG-B data-loss
window open. (2) is the better fit for development environments where
"oh just rebuild the localnet" is the expected response; (3) is the
better fit if the operator ever ships in a profile where state
persistence is contractually load-bearing.

---

## D6 — Managed Postgres auth Secret: advertised recovery path doesn't work (medium)

### Test
Bring up a local-mode CardanoNetwork + a CardanoDBSync with managed
Postgres (no `authSecretRef`, so the controller generates
`<dbsync>-postgres-auth`). Once `PostgresReady=True` and
`status.database.acceptedIdentityFingerprint` is set, save the auth
Secret's `data.password` bytes to a side file. Then
`kubectl delete secret <dbsync>-postgres-auth`. Observe the Degraded
behavior, then attempt the recovery path the controller's own message
advertises: restore the Secret with the original password bytes via
`kubectl apply`.

### Failure
The Degraded message tells the user to *"restore the original Secret or
recreate the CardanoDBSync with a fresh database"* — but the plain
`kubectl apply` restore path does not work as written. Three compounding
problems:

1. **The plain `kubectl apply` recreates the Secret without
   `ownerReferences`.** It is now an unowned same-name object.
2. **The controller refuses to adopt it.** `validateControllerOwner`
   rejects the orphan — the same code path D5 (foreign-owned same-name
   child) and D3 (stripped ownerReference) exercise. Here, however, the
   "foreign" object is one the controller itself instructed the user to
   create.
3. **The diagnosis changes mid-recovery.** Without an `authSecretRef`
   field-indexer match (the controller doesn't track its own generated
   auth Secret by name), Secret recreation doesn't enqueue the CR. The
   user sees the original `ManagedDatabaseSecretMissing` reason
   continue to display. Only after the user bumps spec to force a
   reconcile does the diagnosis flip to `ResourceConflict` with message
   *"resource break-d6d7/d6-dbsync-postgres-auth already exists without
   a controller owner"*. The user thinks their restore made things
   worse.

The actual recovery paths a user must discover the hard way are:
- (a) Hand-patch `metadata.ownerReferences` onto the restored Secret to
  point at the DBSync CR. This is undocumented.
- (b) Delete-and-recreate the CR. This is destructive — though notably
  the auth Secret deletion itself was NOT destructive (Postgres pgdata
  holds the original password and the Pod stays Running).

The data is never actually lost. The Postgres Pod stays Running with
the original password the entire time, because the kubelet's mounted
credential is cached and Postgres authenticates ongoing connections
against pgdata. The bug is entirely in the operator's adoption rule
plus messaging that doesn't reflect it.

Severity is **medium**:
- (+) The Postgres data is not at risk during the failure or the
  recovery attempt; this is a metadata-layer / contract-breach bug, not
  a data-loss bug.
- (–) The user is given instructions by the operator that do not work;
  troubleshooting becomes harder because the displayed reason changes
  by itself.
- (–) The recovery path that actually works (hand-patch ownerReferences)
  is undocumented and Kubernetes-arcane.
- (+) A motivated user can recover without CR delete; less-motivated
  users will reach for delete-and-recreate, which IS destructive (D7
  confirms immediate cascade with no grace).

Evidence: `.run/break-pass/d6d7/`

### Suggested Fixes

1. **Auto-adopt unowned same-name auth Secrets whose contents pass the
   credential identity check (preferred).** When
   `applyManagedPostgresAuthSecret` finds an unowned same-name Secret
   AND its `data.password` bytes match the accepted credential identity
   (the controller already stamps a `managedPostgresCredentialVersion`
   on the owned material per TECH_NOTES), set the ownerReference and
   proceed. This honors the controller's own *"restore the original
   Secret"* advice without requiring the user to know about Kubernetes
   ownership semantics. The credential-identity check protects against
   accepting an attacker-supplied Secret with a different password.
   Code: `internal/controller/cardanodbsync/managed_postgres.go`
   adopt-on-credential-match in the apply path.

2. **Rewrite the message to require ownership when (1) is rejected
   (independent of, or pairs with, (1)).** Current:
   *"Managed Postgres generated auth Secret `<name>` is missing after
   database initialization; restore the original Secret or recreate
   the CardanoDBSync with a fresh database."* Better, when (1) is
   implemented:
   *"Restore the auth Secret with the original password bytes; the
   controller will adopt it automatically if the password matches the
   accepted identity. If you no longer have the original bytes, the
   only recovery is recreating the CardanoDBSync (which deletes all
   managed Postgres data). The current Postgres Pod is still running
   with the original credentials — no data has been lost yet."*
   When (1) is NOT implemented, the message should at minimum require
   ownership: *"... set `metadata.ownerReferences` to point at this
   CardanoDBSync (UID=<uid>), or recreate the CardanoDBSync ..."*.

3. **Add a field-indexer for the generated auth Secret name (small
   correctness fix).** Today the indexer only covers external Postgres
   password Secrets and user-supplied managed `authSecretRef.name`,
   not the generated Secret. Adding it means Secret recreation
   enqueues the CR immediately and the displayed reason transitions
   cleanly (`Missing` → either `Adopted` if (1) is implemented, or
   `ResourceConflict` if not), eliminating the "diagnosis changes
   on its own" UX confusion. Code:
   `internal/controller/cardanodbsync/controller.go::SetupWithManager`
   field-indexer block.

Option (1) is the cleanest end-to-end fix and matches what users
expect from the displayed message. (2) is independently shippable as a
quick win even if (1) is contentious. (3) is a small correctness
improvement that pairs well with either.






