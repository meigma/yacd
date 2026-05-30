---
id: 042
title: New session
started: 2026-05-29
---

## 2026-05-29 19:10 — Kickoff
Goal for the session: Not yet stated. Session opened via `session-new`; awaiting
the user's actual request.
Current state of the world:
- `master` is at `45c44f8` (PR #62, `yacd exec` in-pod verb). Working tree of the
  primary checkout is clean.
- Session 041 is still **in-progress** and mid-flight on Test Harness Phase 2
  (PRs #59–#62 merged; next documented step is PR5 = WB5 `yacd connect`). Its
  implementation worktree `feat/cli-connect-verb` (`.wt/feat-cli-connect-verb`)
  carries uncommitted changes (+95/−21) — PR5 work appears already started.
- On `/session-new`, I surfaced the active 041 state and asked whether to
  continue 041 or start fresh. The user chose to **start a new session 042** and
  leave 041 (and its worktree) untouched.
- Remaining `.journal/TEST_REPORT.md` findings noted by recent sessions: F0 and
  F2/F4.
Plan: Wait for the user's direction. Once a goal is set, survey task-relevant
skills, ground in the relevant code/docs, create an implementation worktree from
fetched `master` (NOT from 041's worktree), and start `moon run root:dev-up` from
that worktree if implementation work is involved.

## 2026-05-29 21:46 — Goal: F0 genesis-artifact redesign (design discussion, no code yet)
User's actual goal: rethink how genesis/config artifacts are handled (TEST_REPORT
F0 — mainnet public profile exceeds the ~1 MiB etcd ConfigMap cap). Two design
turns so far, both run as adversarial workflows (no code written):

1. Ran `wf_2012bfb8-8db` (7 agents): a verified local-vs-public genesis-artifact
   comparison. Verdict `accurate`. Headline: LOCAL artifacts are runtime-generated
   by an init container that patches an initially-empty `<net>-network-artifacts`
   ConfigMap; PUBLIC artifacts are `//go:embed`'d (or custom bundle) and written
   into the ConfigMap directly by the controller, mounted read-only at `/profile`.

2. User proposed a redesign: stop embedding; init container DOWNLOADS public
   artifacts from trusted Cardano sources (local still generates via
   cardano-testnet); write to PVC; run an in-Pod HTTP server to serve artifacts;
   consumers pull over HTTP (direct or via their own init+PVC); advertise URL(s)
   in status. Claim: "identical except how step 2 fetches."

   Ran `wf_3a953e1d-4d0` (7 agents: 5 critique lenses → synth → verify;
   verifier `minor-corrections`, 15 load-bearing claims confirmed at file:line).
   My assessment delivered to user:
   - Diagnosis + "bytes → PVC via init container" = RIGHT, keep.
   - DROP the in-Pod HTTP server: (a) doesn't solve producer side — node reads its
     own genesis off local disk (`containers.go:103-108,148`), can't fetch from a
     server it hosts; (b) in-cluster consumers don't need it — db-sync mounts the
     artifact ConfigMap as a volume by name (`cardanodbsync/resources.go:288-295`),
     controller reads only `status.Artifacts.DataHash` (`builder.go:319-320`);
     (c) couples consumer bootstrap to least-available workload (node Replicas:1,
     Recreate, RWO; mainnet Mithril init gate). The URL/server only has value for
     OUT-OF-CLUSTER consumers (the CLI on a laptop) — separate, deferrable feature,
     and if built it gets its OWN Deployment+Service, not the node Pod.
   - DECOUPLE runtime-download: nothing in YACD verifies `config.json`, which is the
     trust anchor pinning every genesis hash (incl. `CheckpointsFileHash`). Keep
     `//go:embed` for the F0 fix; download is a later opt-in that MUST carry
     operator-pinned sha256 + host allowlist + TLS + offline fallback.
   - "identical except fetch" LEAKS: format (configuration.yaml YAML vs config.json
     JSON under same key), artifact set (checkpoints/peer-snapshot/mithril vkeys
     public-only; dijkstra-genesis local-only), trust (self-consistent vs 3rd-party).
   - SECRET LANDMINE: local `/state` PVC holds KES/VRF/opcert + faucet UTxO keys
     (`containers.go:87-89,51,320-325`); any PVC-root serving/copy leaks them. Must
     keep the default-deny key allowlist (`ValidateConfigMapData`).
   - KEY SHARPENING: **F0 is mainnet-only.** Profile totals: preview ~93KB,
     preprod ~38KB, mainnet ~1.56MB (byron alone 1.05MB). Only mainnet breaches.
   - RECOMMENDED north star: init-container stages files to node PVC (embed public /
     generate local); small metadata ConfigMap = schemaVersion + per-file sha256
     manifest + DataHash stays in etcd as the reconcilable source of truth +
     integrity handshake; consumers fetch INDEPENDENTLY (own init) verifying vs the
     manifest, NOT pulling from node; Mithril stays separate; provenance under
     `status.Artifacts` not `status.Endpoints`.
   - MINIMAL FIRST SLICE (clears F0, breaks nothing — mainnet db-sync already
     rejected `public_network.go:19-21`): public mainnet node stages embedded bytes
     to PVC instead of ConfigMap.Data; replace byte ConfigMap with metadata/manifest
     ConfigMap; leave preview/preprod, consumer contract, mainnet db-sync gate, and
     download untouched.
   - OPEN QUESTIONS before code: (1) can cardano-node read --config/genesis from the
     PVC path like local vs current `/profile` ConfigMap mount? (2) is
     `cardano-testnet create-env` byte-deterministic? (if not, local consumers must
     copy, not regenerate).

Next: awaiting user decision on embed-vs-download + consumer-fetch + whether to
implement the minimal mainnet-only slice. Offered to write a file-by-file impl plan
against `internal/controller/cardanonetwork`. NO worktree/dev-stack yet (design
only).
