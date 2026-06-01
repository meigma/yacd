---
id: 050
title: F0 redesign — PR-D (remove report verb, pin cardano-tools digest, drop e2e build hack, DESIGN.md rewrite)
started: 2026-06-01
---

## 2026-06-01 14:05 — Kickoff

Goal for the session: land **PR-D**, the final slice of the F0 redesign. Per
session 048's SUMMARY, PR-D is cleanup + docs (no new runtime behavior), a
low-risk close to the F0 series. Four carried items:

1. **Remove the cardano-tools `report` verb** — it was the publisher's
   genesis-hash enrichment mechanism; `cardano-tools stage` (PR-C) took over, so
   `report` is dead. Grep `containers/cardano-tools` for the `report` subcommand
   + its `internal/` impl/tests; confirm nothing in the operator or init
   wrappers still invokes it before deleting.
2. **Pin the manager's cardano-tools image to a published digest** — today the
   manager defaults to a `yacd.N` tag (same pattern cardano-testnet uses). The
   sequencing gap (no-override `helm install` between an image PR merging and its
   release-please PR merging) is documented/accepted pre-1.0; digest pinning
   closes it. release-please cardano-tools PR **#76 is open** — coordinate the
   digest with whatever it publishes. Check
   `internal/controller/cardanodbsync/defaults.go` and the cardanonetwork
   init/sidecar wiring for the cardano-tools image ref.
3. **Drop the e2e build+load hack** — e2e currently builds cardano-testnet/tools
   images from source and loads them into Kind because the manager defaulted to
   not-yet-published tags. Once images are published + digest-pinned, e2e can
   pull instead of build-and-load. See `.dev/scripts/test-e2e.sh` + chainsaw
   setup.
4. **Rewrite the DESIGN.md ConfigMap prose** — DESIGN.md still describes the old
   artifact-ConfigMap transport that PR-B1 deleted. Update it to the
   serve-over-HTTP / PVC-staged model that now ships.

Current state of the world:
- PR-A (#75), PR-C (#77), PR-B1 (#78), PR-B2 (#79), versioning follow-up (#80),
  and the cardano-testnet release (#34 → tag `cardano-testnet/v11.0.1-yacd.5`)
  are all merged. Runtime data path is fully HTTP/PVC-based; the publisher is
  gone. master @ `ca24030`.
- Open release-please PRs: **#76** (`release cardano-tools 11.1.0-yacd.4`),
  **#7** (`release 1.0.0`, root). Also open: dependabot #43, #44.
- PR-D needs a release of cardano-tools (for the `report` removal + the digest
  to pin to), so expect a release-please cardano-tools PR as part of landing it.
  Apply the same `Release-As:` base-pinning discipline (base = packaged
  cardano-node version) if its base drifts. NOTE the cardano-tools versioning:
  base should equal the cardano-node version; #76 currently proposes
  `11.1.0-yacd.4` which looks drifted off `11.0.1` — verify.

Plan: review PR-D scope in the code, confirm with the user, then execute on a
fresh implementation worktree off master. Dev stack startup deferred until the
implementation worktree is selected and the user gives the go-ahead.

User asked me to review PR-D and report readiness before further instructions.
