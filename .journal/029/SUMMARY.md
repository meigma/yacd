---
id: 029
title: Adversarial break-the-operator pass
date: 2026-05-28
status: complete
repos_touched: [yacd]
related_sessions: [028, 027, 026, 025]
---

## Goal
The session originally set out to continue dbsync work (snapshot/restore design from the open thread in session 028). Midway, the user pivoted to a focused adversarial test pass against the operator on the local Kind/Tilt dev stack. The redefined goal: surface "unexpected states" the operator can be driven into — infinite reconcile loops, silent acceptance of changes that break the runtime, unrecoverable error conditions, or lying status — explicitly excluding cleanly-declared error states where the controller correctly says "I am failing because X."

## Outcome
Goal met. Six categories (A–F) of probes ran against the live operator on Kind: **33 tests executed, 31 conclusive, 2 INCONCLUSIVE** (F1 and F3 blocked by an upstream finding uncovered orthogonally). Findings documented in `.journal/TEST_REPORT.md` as 10 structured entries (test → failure → suggested fixes) and chronologically in `.journal/029/NOTES.md`. No code changes were made; the session produced documentation only.

**10 findings classified by severity:**

| ID | Title | Severity |
|----|-------|----------|
| F0 | Mainnet artifact CM exceeds 1 MiB cap, mainnet cannot be created | high |
| A4 | Placement peer toggling severs stable primary-sidecar attachment | high |
| B1 | Status-fingerprint forgery permanently bricks CardanoNetwork | high |
| D1 | Faucet auth Secret deletion produces lying status + silent token rotation | high |
| D2 | PVC stuck Terminating: silent lie + localnet data loss on recovery | high |
| A3 | Artifact CM external corruption rolls primary Pod 1:1, no backoff | medium |
| B2 | CardanoDBSync DB identity forgery: recoverable brick but message demands CR delete | medium |
| B6 | Storage expansion failure on non-expandable class is invisible in CR status | medium |
| D6 | Managed Postgres auth Secret: advertised recovery path doesn't work | medium |
| F2+F4 | NodeReady message is uselessly generic for common Pod/PVC failures | medium |

Five high-severity findings touch security/data-integrity contracts:
- **F0** breaks the documented mainnet capability end-to-end with completely silent failure (no status, no events, no owned children). Discovered orthogonally to the synthesis list — the methodology paid for itself with this one finding.
- **A4** lets any second user with `cardanodbsync` create/edit permission keep an unrelated user's stable primary-sidecar attachment in a perpetual roll-detach-reattach cycle.
- **B1** turns a `status` subresource patch (a verb commonly granted to admins independent of spec access) into permanent CR bricking; the only recovery is CR delete which loses chain state.
- **D1** combines a 10-minute lying-status window with a silent token-rotation-without-pod-roll that produces an unbounded runtime-vs-API token divergence.
- **D2** has no `DeletionTimestamp` gate in the apply path (silent during stuck Terminating) AND the recovery path silently destroys localnet data when the PVC actually deletes.

## Key Decisions
- Pivot session 029 mid-flight to break-pass rather than open a new session — preserved the in-flight snapshot-research notes by appending the new focus chronologically rather than rewriting.
- One subagent per test (with hard "no TaskCreate/TaskUpdate, run synchronously" rules added after A3's subagent left work mid-flight) — protected the main-thread context window across 33 tests.
- Promote to `TEST_REPORT.md` only when a finding has a concrete fix surface AND severity ≥ medium. UX nits across multiple tests with the same root cause (e.g., F2+F4) combined into single entries.
- Combine related setup-expensive tests onto one CR (C1+C2+C3 against one local network; D6+D7 against one managed-Postgres DBSync; E2+E5 against one external-Postgres Secret) to fit the M4 Max constraint.
- Skip C4 (sustained sidecar corruption of artifact CM) as redundant — A3 already confirmed 1:1 corruption-to-roll with no operator-side backoff; sustained variant would tautologically re-confirm.
- Run E1b (custom-profile ConfigMap with `binaryData`) as an ad-hoc follow-up after the E1 agent identified the original premise was structurally impossible for Secrets but exploitable for ConfigMaps. Confirmed defended via planner-layer defense-in-depth.

## Changes
- `.journal/029/NOTES.md` — full chronological session record with per-test entries, including the dev-stack mishap (Bash cwd silently drifted into `.wt/journal-jmgilman/` and brought up the OLD template-k8s stack), recovery, and cross-cutting UX themes
- `.journal/TEST_REPORT.md` — created and populated with 10 structured failure entries (test description, observed failure, suggested fixes per entry)
- `.run/break-pass/<test-id>/` — evidence files (sample TSVs, baseline/final YAMLs, operator logs) for all 33 tests, organized per test
- No source code changes

## Open Threads
- All 10 TEST_REPORT findings are documented but not fixed. Suggested fixes per entry are concrete but unimplemented.
- F0 (mainnet ConfigMap > 1 MiB) blocks F1 and F3 — those theorized concerns about silent Mithril bootstrap acceptance and opaque snapshot-failure messaging remain untested in this session. Re-run after F0 is fixed.
- Source-review note from the F1+F3 agent worth keeping: `mithrilBootstrapInitContainer` in `internal/controller/cardanonetwork/init_container.go:21` runs `mithril-client cardano-db download` and the operator does NOT perform any post-init validation that `db/` was actually populated. Worth a follow-up either via envtest or via a code review that adds post-init checks.
- Cross-cutting "silent operator override" UX pattern (B5, C1, C2, C3, C5) noted but not promoted to a single TEST_REPORT entry — spans many tests with no single sharp failure point. Could become an `OwnedFieldRestored` Warning event in a future session if you want it formalized.
- Cross-cutting adoption-rule papercuts (D3, D5, D6) — same `validateControllerOwner` rule produces three different unhelpful messages in three scenarios. Worth a unified guidance line.
- The original session-029 work on snapshot/restore design (committed at 8047673 and 50ff28f) was set aside when the user pivoted; `.journal/SNAPSHOT_DESIGN.md` exists from the earlier work and remains in tree. Decide whether to resume in a future session or treat as abandoned.
- Session 030 was started in parallel by a separate agent for "Bespoke snapshot/restore design" — appears to be picking up the snapshot work that was set aside in 029. Coordinate ownership with that session if appropriate.

## References
- `.journal/TEST_REPORT.md` — primary deliverable, 10 structured failure entries
- `.journal/029/NOTES.md` — full chronological session record (33 test entries plus dev-stack incident + cross-cutting themes)
- `.run/break-pass/` — per-test evidence files
- Session 027 (`.journal/027/SUMMARY.md`) — added the mainnet capability that F0 breaks
- Session 026 (`.journal/026/SUMMARY.md`) — added the accepted-placement-mode protection that B3 confirmed defeats the synthesized attack
- Session 028 (`.journal/028/SUMMARY.md`) — open thread on snapshot/restore that the original session 029 work was addressing before the pivot

## Lessons
- The break-pass methodology surfaced one major finding (F0) that wasn't on the original synthesis list and wasn't caught in CI. The synthesized list of theories is necessary but not sufficient; running real adversarial probes against the live operator can uncover orthogonal failure modes that pure code-review wouldn't catch.
- `predicate.GenerationChangedPredicate{}` on the primary `For()` filters out status-subresource updates — this is correct for ordinary reconciliation but creates a real attack surface when status fields are used as authoritative input by validation paths (B1, B2). Lining up status fields as "derived from PVC/owned-material annotations" rather than as authoritative sources removes the surface entirely. The pattern already exists in the codebase for one field (B3's `acceptedPlacementMode`); generalize.
- The `Owns` watch is a defensive shield against external resource-deletion attacks (E4 inference, D4 confirmation, A5 confirmation) — instantly recreating owned children faster than any external actor can take them down. But it doesn't help when the attack is on CONTENT (A3's CM data corruption) or on resources the operator doesn't own (D1's faucet auth Secret being externally deletable AND the operator not watching Secrets).
- Multiple findings (D1, F5) point at a common runtime-state-vs-API-state divergence problem when Secrets rotate without the consuming Deployment rolling. Stamping the consumed Secret's resourceVersion (or a hash of its contents) onto the pod-template annotation makes the Deployment roll on rotation, collapsing the divergence.
- `ApplyOwnedObject` has no `DeletionTimestamp` gate (D2). A child entering Terminating is treated as a valid existing object that just happens to produce no diff. Adding a typed `ChildBeingDeleted` error at the apply layer would fix D2's silent window and prevent class of similar attacks against any other owned child.
- For a developer-focused operator targeting Kind/dev environments (per DESIGN.md), generic "X not available" condition messages are user-hostile (F2+F4). Walking the parent Deployment's Pods for `containerStatuses[*].state.waiting.reason` plus `status.conditions[type=PodScheduled].status=False` would surface the actual kubelet/scheduler/provisioner error inside the CR's own readiness conditions, eliminating the need to drop into `kubectl describe pod/pvc` for the most common configuration mistakes.
- Chainsaw smoke tests for the mainnet profile would have caught F0 in CI. Coverage gap in addition to the code bug.
