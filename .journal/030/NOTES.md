---
id: 030
title: Bespoke snapshot/restore design
started: 2026-05-28
---

## 2026-05-28 14:00 — Kickoff
Goal for the session: not yet stated. Developer explicitly requested a new
session opened concurrently with the still-ongoing session 029, then will
provide the actual request.

Current state of the world:
- `master` is at `69a87d1` (`feat(cardanodbsync): support public sidecar
  placement`, PR #48), in sync with `origin/master`.
- Session 029 is **still ongoing** (intentionally), mid Category E adversarial
  break-pass testing. Its `NOTES.md` has uncommitted appended entries (E1/E1b
  ruled NOT-A-BUG / UX-GAP); left dirty on purpose per developer. This session
  was created without disturbing those changes — only `030/` was staged.
- Latest closed/summarized sessions: 028 (public db-sync primary sidecar, PR
  #48), 027 (public CardanoNetwork profiles + mainnet bootstrap, PR #47), 026
  (primary sidecar manual functional testing, PR #46).
- Dev stack: per session 029 notes it was left running. Not re-verified at
  kickoff; will confirm/start when implementation work begins.

Plan: await the developer's actual request before any substantive work.

## 2026-05-28 14:15 — Session goal set: bespoke snapshot/restore design review
Goal: design YACD's bespoke snapshot/restore feature for the dApp-testing use
case. User builds a localnet chain to a desired state (specific UTxOs, deployed
scripts), `yacd snapshot create` produces a self-contained archive, hosts it
over HTTP, and `CardanoNetwork.spec.restore.source.{url,sha256}` restores it
repeatedly in CI to catch regressions. Builds directly on
`.journal/SNAPSHOT_RESEARCH.md` (Mithril rejected for localnet fixtures;
bespoke format + slot/time re-anchoring is the path) and the
`.journal/SNAPSHOT_DESIGN.md` draft.

User asked for a critical review of the approach before committing to a design.
Headline points raised in chat (full reasoning there):
1. Plumbing (CLI -> HTTP artifact -> operator restore-by-URL) is sound and
   matches the design doc. The hard parts are Cardano-time + version semantics,
   not the transport.
2. Re-anchoring is the COMMON path, not an edge case: every CI restore happens
   at a later wall clock than create, so `systemStart` rewrite must be a
   first-class, every-restore operation. Make-or-break #1.
3. Node-version pinning is make-or-break #2: chain DB / ledger-state format is
   tied to a cardano-node version; restore must fail closed on version
   mismatch. A snapshot is a fixture pinned to a node version; node bump =>
   re-snapshot.
4. Use-case gaps under-emphasized in current draft: (a) spending keys for the
   funded UTxOs must travel with (or be coordinated alongside) the snapshot or
   the fixture is unspendable; (b) node + db-sync (+ Kupo) must be captured at
   the SAME tip (atomic checkpoint) or re-derive downstream; (c) producing vs
   frozen fixture — dApp tests submit txs, so this is a *producing* restore
   (KES/VRF/opcert + re-anchor mandatory); (d) restore must expose the
   re-anchor mapping (origSystemStart/newSystemStart/tipSlot) so tests can
   compute "what slot is now"; re-anchor preserves slot-relative but breaks
   absolute-time semantics.
5. Packaging: HTTP+sha256 is the right floor; OCI artifact (cosign/attestation,
   dedup, caching) is the natural home as a packaging mode, not a rewrite.
   CI-reliability concerns: artifact-host availability => flaky tests, auth for
   private fixtures, cold-start latency (download+unpack+reanchor+sync-to-tip).
6. Honest worth-it check: snapshot earns its complexity when setup cost >>
   restore cost or exact reproducibility matters. Mental model: setup script
   (source, checked in) -> snapshot (versioned build artifact, node-pinned) ->
   restore (fixture).

Next: await user's reaction / which forks to pursue (producing-vs-frozen,
include-db-sync, OCI-vs-plain-HTTP, key handling). No code yet; design phase.
Dev stack not started (design discussion only).
