---
id: 056
title: New session
started: 2026-06-02
---

## 2026-06-02 17:27 — Kickoff
Goal for the session: not yet stated; session started via `session-new`, awaiting
the user's request.

Current state of the world:
- `master` at `db7887b` (session 055, PR #90), clean working tree.
- Local-lifecycle plan (`.journal/049/LOCAL_LIFECYCLE_PLAN.md`) core sequence is
  complete: `P1✅ P4✅ P5✅ P2✅ P3✅ P6✅`. `yacd devnet` all-in-one ships and is
  k3d live-proven. **Only P7 (hardening & UX) remains** of that plan.
- Operator releases `v0.1.0` / `v0.1.1` are published to GHCR; the GitHub *draft*
  release pages still await a human Publish.
- Carried threads: P7 (typed failure taxonomy, preflight, `devnet down --purge`,
  `--isolate-kubeconfig`, WSL2/ARM guards); release-please root PR #7 with the
  queued pre-1.0 PATCH bump; deterministic primary-sidecar manager-envtest
  refactor; TEST_REPORT F2/F4; test-harness `yacd-env` Action + examples/how-to;
  a dedicated docs session.

Plan: await the user's actual request before any substantive work. Dev stack not
yet started (will run `moon run root:dev-up` from an implementation worktree once
work is scoped).

## 2026-06-02 18:30 — Manual devnet test: P0–P7 PASS, P8a surfaced a HIGH-severity bug chain (F1/F2/F3)

Driving the full manual functional test (plan approved). Built `/tmp/yacd`. Isolated
each scenario under temp KUBECONFIG/XDG_STATE_HOME; one real-kubeconfig run reserved
for Session G.

PASSING so far:
- **P1** static gates: `--timeout 0/-5m` → "must be greater than 0"; `devnet`/`down`/`status`
  reject explicit `--kubeconfig`/`--context`/`YACD_*`. All exit 1.
- **P2** no record + empty kubeconfig → tier-3 ambient ("no configuration provided"), no
  managed notice, no record written.
- **P3/P4** cold k3d fetch (isolated XDG_DATA_HOME → fresh k3d v5.9.0 0755, real cache mtime
  untouched) + full cold bring-up converged to Ready. Node on yacd's pinned **v1.32.5+k3s1**
  (overrides k3d default v1.35.5). Record perms 0700/0600, priorContext empty. Exact
  stderr/stdout match. NOTE: Kupo port is **1442** (plan guessed 3001).
- **P5** operator SSA: CRDs Established=True, manager version label v0.1.1, image pinned to the
  v0.1.1 digest, field owner yacd-cli, gen=1. CORRECTION: the `yacd.meigma.io/install=operator`
  label IS present on CRDs (plan wrongly said absent); the real guarantee — CRDs excluded from
  the prune GVK set (`apply.go` `managedGVKs` omits CRD) — holds, so CRDs are never pruned.
- **P6** funded wallet **100,000 ADA on-chain** (single UTxO == fundedTxID), WalletReady=True,
  via in-pod `exec cardano-cli query utxo`.
- **P7** host-access: `info` shows the new Wallet block; in-pod `exec env` has the YACD_* URLs +
  CARDANO_NODE_SOCKET_PATH and **omits YACD_FAUCET_TOKEN**; `run` host env has loopback ephemeral
  URLs + **YACD_FAUCET_TOKEN present** (len 43); standalone `topup` **announces** the managed
  target then can't reach in-cluster DNS (expected); `topup` via `run`+`sh -c` **Confirmed
  on-chain** (+5 ADA → 2 UTxOs); `connect` writes token-free `.yacd/devnet/endpoints.json` and
  removes it on clean SIGINT. `list -A` shows devnet; `list` (no -A) defaults to `default` ns.
- **P8a** warm re-run is a true no-op (server-0 .Created + SSA generation unchanged). The
  recorded-kubeconfig HEALTH guard works: KUBECONFIG→empty did NOT recreate the cluster.
- **P13** explicit `--context` beats managed tier (no announce; bogus ctx errors w/o falling
  back to record). 

### BUG CHAIN F1→F2→F3 (HIGH) — KUBECONFIG drift between `devnet` runs can silently destroy chain state
Root: on the healthy no-op path `k3d.infoFor` returns `KubeconfigPath = defaultKubeconfigPath()`
(the *current ambient* KUBECONFIG), not the recorded path (`ensure.go:69-73`).
- **F1**: `Up` builds the operator installer + kube client from that ambient `info.KubeconfigPath`
  (`manager.go:88,103`). If the current KUBECONFIG lacks the managed context, the re-run does NOT
  delete the cluster (health probe used the recorded path) but **fails at "Installing operator"**
  (`context "k3d-yacd" does not exist`).
- **F2**: `Up` saves the record (`manager.go:82-83`) with that ambient `info.KubeconfigPath`
  **before** the operator step — so a drifted-KUBECONFIG re-run **overwrites the good record's
  kubeconfigPath with the wrong one, even though the run then fails**. Persisted corruption.
  (Observed live: record.kubeconfigPath flipped to the empty test kubeconfig; `devnet status` then
  failed with `context "k3d-yacd" does not exist`.)
- **F3 (the payload, HIGH)**: a corrupted/drifted record makes the NEXT `yacd devnet` health-probe
  through the wrong path. `statusVia` (`list.go:63-64`) swallows the probe ERROR into
  `Healthy=false`; `ensure.go` then hits the `default` branch and **delete+recreates the healthy
  cluster — silently destroying the funded wallet + localnet ledger**. Confirmed live: server-0
  container id `2c109ea9c657`→`a68cac09c8ba` with stderr only saying "Cluster yacd ready".
Realistic trigger: a developer who switches KUBECONFIG between `yacd devnet` invocations (common).
Suggested fix direction: on the no-op path return the RECORDED kubeconfig path (or have `buildRecord`
preserve `existing.KubeconfigPath` when the cluster was a healthy no-op), distinguish probe
"context-not-found" from "API unreachable" so a config error doesn't read as unhealth, and/or save
the record only after a successful Up.

State: the F3 confirmation also repaired the record; cluster is a fresh --bare k3d-yacd (no network).
Continuing P17b/P8b(heal)/P12(orphan) on it.

## 2026-06-02 18:55 — Manual devnet test COMPLETE (Sessions B–G + teardown)

Remaining adversarial scenarios all PASS:
- **P9 lock contention**: P2 (`--timeout 8s`) failed at exactly 8s with `acquire cluster lock:
  ... context deadline exceeded` while P1 (holder) ran on; exactly one cluster, valid record,
  clean down. (Minor cosmetic: doubled wrap `acquire cluster lock: acquire cluster lock:`.)
- **P10 Ctrl-C**: SIGINT during "Installing operator" → clean abort (`install operator: context
  canceled`); cluster created + record saved (mid-provision); re-run converged (operator 1/1),
  record uncorrupted. yacd handles SIGINT via context cancellation.
- **P11 short --timeout**: `--timeout 10s` killed the k3d create (`signal: killed`); rollback
  deleted the partial cluster (0 clusters/containers) and **no record saved** (Up returns before
  Save). Clean, no stranding. (Minor: raw `exec …k3d…: signal: killed:` message — P7 hardening.)
- **P15 capture/restore**: seeded current-context `my-prior-ctx` → up records `priorContext:
  my-prior-ctx`, switches to k3d-yacd; warm re-run KEEPS the real prior (never k3d-yacd); down
  prints `Restored kube context "my-prior-ctx"` and restores it. (Works because KUBECONFIG stayed
  constant and held the context — i.e. the non-drift path, contrast F1/F2/F3.)
- **P14 --bare**: operator only, no `Applying network`, no CardanoNetwork, clean down.
- **P16 one real ~/.kube/config run**: up added ONLY `+k3d-yacd` (14 prod/EKS contexts untouched),
  switched current to k3d-yacd, left no real `~/.local/state/yacd` (state isolated); down removed
  k3d-yacd and restored the context list **identically**.

### Finding F4 (MEDIUM) — devnet up/down from an UNSET current-context can leave kubectl on a PROD context
Baseline current-context was unset. After `devnet --bare` (switch to k3d-yacd) then `devnet down`,
current-context became `arn:aws:eks:…:cluster/platform` — a real PROD EKS context. Mechanism: with
an empty prior, yacd's restore is SKIPPED (`manager.go:220` guards `PriorContext != ""`; no
"Restored…" line printed), so `k3d cluster delete` repoints current-context to an arbitrary
remaining entry. A later ambient `kubectl`/`yacd` would then target prod. The plan only anticipated
"back to unset"; reality is worse. Fix direction: when prior is empty, yacd should explicitly UNSET
current-context on down (capture+restore the "unset" state) rather than let k3d pick.

### Findings summary (for the user)
- **F1/F2/F3 (HIGH, one chain)**: KUBECONFIG drift between `devnet` runs → record's kubeconfigPath
  overwritten with a config lacking the managed context (F2, saved even on a failed run), then the
  NEXT `devnet` health-probes via that path, mis-reads healthy-as-unhealthy, and **silently
  delete+recreates the cluster, destroying chain state (F3)**. Root: no-op path returns ambient
  `info.KubeconfigPath` not the recorded one (`ensure.go:69-73`); `statusVia` conflates probe error
  with unhealth (`list.go:63-64`).
- **F4 (MEDIUM)**: up/down from unset current-context can silently leave kubectl on a prod context.
- **Cosmetic/UX (LOW, P7 territory)**: doubled lock-error wrap; raw `signal: killed` timeout message.
- **Plan/recon corrections**: Kupo port is 1442 (not 3001); operator install label IS on CRDs (CRDs
  excluded from prune set, so still never pruned — guarantee holds).

### Environment notes (not yacd bugs)
- First cold bring-up pulls the large `cardano-testnet`/`cardano-tools` images from ghcr.io;
  in-cluster DNS to ghcr.io was intermittently flaky (`lookup ghcr.io: Try again`,
  ImagePullBackOff) — the backoff eventually succeeded within 12m. A cold pull on flaky networking
  can approach/exceed the 12m default. Pre-pulling host-side + `k3d image import` is a reliable
  workaround; consider documenting / an image-preload option for the devnet UX (P7).

PASS tally: P1,P2,P3,P4,P5,P6,P7,P8a/b,P9,P10,P11,P12a/b/c,P13,P14,P15,P16,P17b — all behaviors as
specified. Teardown: machine restored to P0 baseline (0 yacd artifacts; standup-demo + oidc-smoke +
14 contexts + binary cache all intact; current-context unset). Dev stack (Tilt/KinD) was never
needed for this k3d-path test and was not started.

## 2026-06-02 19:30 — Fixes implemented + verified (branch fix/devnet-kubeconfig-handling)

Plan approved (full hardening + read-path Status change). Implemented on worktree
`.wt/fix-devnet-kubeconfig-handling` (off origin/master), CLI-only:
- **Fix 1 (root, F1/F2/F3):** `k3d.infoFor(name, kubeconfigPath)` — no-op branch returns the
  RECORDED `spec.KubeconfigPath` (fallback to default when empty); create/heal returns the
  default (where k3d wrote). `cluster/k3d/ensure.go`.
- **Fix 2 (defense):** typed `probeConfigError` from `defaultProbe` for kubeconfig/context load
  failures; `statusVia` returns it as a HARD error (abort) instead of `Healthy=false`, so a
  bad/missing kubeconfig never delete+recreates a healthy cluster. Genuine /healthz failure →
  heal (unchanged). `cluster/k3d/k3d.go`, `list.go`.
- **Fix 3 (read path):** `Provisioner.Status(ctx, name, kubeconfigPath)` (interface + adapter
  + mock regen); `lifecycle.Manager.Status` and `cli/orphan.go` pass `record.KubeconfigPath`.
- **Fix 4 (F4):** `Down` clears current-context (RestoreContext(path,"")) when prior is empty,
  reporting "Cleared kube context (no prior context to restore)". `lifecycle/manager.go`;
  verified `kube.SetCurrentContext(path,"")` blanks current-context (new `context_test.go`).
- **Fix 5 (cosmetic):** dropped the duplicate `acquire cluster lock:` wrap in
  `clusterstate/file/lock.go`; friendly "timed out after X"/"cancelled" message in `k3d.create`.

Tests added: k3d (no-op recorded path + fallback, config-error abort, Status path, context-error
message), lifecycle (Down empty-prior clear), kube (SetCurrentContext clear). `root:generate`
(provisioner mock), `root:check`, `root:test` all green.

**Live re-test (rebuilt binary, isolated dirs) — all FIXED:**
- F1/F2/F3: bring up `--bare`; drift KUBECONFIG→empty; re-run `devnet` → **exit 0 no-op**,
  record kubeconfigPath UNCHANGED, server-0 id stable (no recreate). (vs 056: corrupt+recreate.)
- Fix 2: corrupt record kubeconfigPath → context-less file; `devnet` → **aborts** `ensure
  cluster: probe cluster yacd health: load kubeconfig for k3d-yacd: context "k3d-yacd" does not
  exist`, server-0 id stable (no destroy).
- Fix 3: KUBECONFIG drifted, `devnet status` → `Cluster: k3d-yacd (running) (healthy)` via the
  recorded path.
- F4: empty-prior `down` → "Cleared kube context", recorded kubeconfig current-context UNSET.
Machine restored to baseline (0 yacd clusters, no real state dir, current-context unset, 14
contexts + standup-demo + oidc-smoke intact).

Diff: 13 files, +256/-51 (cli/internal only; mock regen). NOT yet committed/pushed — awaiting
go-ahead on the PR.
