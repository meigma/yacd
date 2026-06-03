---
id: 058
title: New session
started: 2026-06-03
---

## 2026-06-03 10:16 — Kickoff (superseded)
Session started via `session-new` but never received a request; world-state
below was stale before any work began. Re-initialized in the entry that follows.

## 2026-06-03 11:43 — Kickoff (re-initialized)
Goal for the session: not yet stated — session (re)started via `session-new`;
awaiting the user's request.

Current state of the world:
- `master` at `b611645` (PR #93, session 057: all-namespaces list, self-
  forwarding topup, `yacd init`), clean.
- Local-lifecycle plan core is **complete**: `P1✅ P4✅ P5✅ P2✅ P3✅ P6✅`;
  only **P7 (hardening & UX)** remains (typed failure taxonomy, Docker/disk
  preflight, `devnet down --purge`, `--isolate-kubeconfig`, WSL2/ARM guards,
  first-run banner, devnet image preload/preflight).
- `yacd devnet` all-in-one local k3d lifecycle shipped + manually functional-
  tested (sessions 055/056); wallet funded 100k ADA on-chain verified.
- Operator releases live: `v0.1.1` (manager/faucet/chart), embedded in the CLI
  SSA install. release-please root PR #87 open (`release 0.1.2`); GitHub draft
  releases still await a human Publish (GHCR artifacts already public).
- Open PRs: #91 (`docs/mkdocs-site`, docs site — owes the session-057 doc fixes:
  `list -A`, `topup` form), #87 (release-please 0.1.2), #44/#43 (dependabot).
- Other carried threads: deterministic primary-sidecar manager-envtest refactor;
  TEST_REPORT F2/F4; test-harness `yacd-env` Action + examples/how-to.

Plan: await the user's actual request before doing substantive work. Dev-stack
startup (`moon run root:dev-up`) is deferred until an implementation worktree is
selected and the task is known.

## 2026-06-03 13:00 — Feature design: managed test wallets (analysis + decisions)
Request: add **managed test wallets** to YACD — generate / list / topup / export
named wallets, gated to faucet-enabled local networks. The user's original framing
was "extend the faucet to be a wallet container, keys held by the faucet on a
SQLite DB on the local PVC; CLI list/manage/export; wordlist names; gated off on
public networks." Asked for analysis + recommendation first (no code yet).

Ran an 11-agent analysis workflow (6-subsystem survey + 5 design-dimension
analysis; ~957k tokens). Load-bearing findings:
- The faucet is deliberately **stateless**: read-only root FS, CGO_ENABLED=0,
  distroless/static, nonroot, **NO writable volume**. The only PVC is
  cardano-node's `<net>-node-state`, mounted **read-only** into the faucet (for
  the genesis utxo keys). So "SQLite on the existing PVC" is the *largest* path
  (new dedicated faucet PVC + writable mount + relaxed hardening + pure-Go driver
  — `mattn/go-sqlite3` can't link; even `modernc.org/sqlite` is overkill vs
  bbolt/JSON for a handful), not the simple one. Colocating on the node PVC is
  wrong (inherits `PrimaryStateLost`; node-state loss strands all wallet funds).
- ~80% already exists: the single funded dev wallet (`spec.chainAPI.wallet`,
  session 053) generates an ed25519 key ONCE into a K8s **Secret**
  (`payment.skey`/`payment.vkey`/`address`), funds via faucet `/v1/topups`,
  confirms via Kupo, never regenerates. Keygen + cardano-cli-compatible address
  derivation = `internal/cardano/wallet` (golden-tested, **already imported by
  the faucet**). Multi-wallet is a generalization, not a new subsystem.

**Decisions (user-confirmed):**
- **Signing:** fund + export keys only — no server-side signing → the faucet
  never needs key custody.
- **Custody:** CLI-side **Kubernetes Secrets**; faucet + controller **UNCHANGED**
  (faucet gets **NO ServiceAccount**). The CLI does keygen + Secret CRUD under the
  user's own kubeconfig (same creds it already uses for `GetSecretValue` / apply);
  funds via the existing `/v1/topups`. Avoid the faucet+SA middle ground (broad
  Secret RBAC + export-over-HTTP = blast-radius expansion; cuts against the
  deliberate "no faucet Secret list RBAC" posture + DESIGN.md "not a general
  wallet platform").
- **Seed wallets:** CLI-imperative only for v1; declarative
  `spec.chainAPI.faucet.seedWallets[]` (controller-owned, generate-once) deferred.
- **Scale:** NOT a driver. Secrets handle low-hundreds fine (on k3d, Secrets are
  *already* kine→SQLite); the real bottleneck at scale is funding throughput
  (N on-chain txs, serialized per funding UTxO) — identical in both designs. For
  >100s, users go direct to the node/Ogmios/Kupo. v1 gets a **sane soft ceiling**
  (~dozens) with a message pointing to direct access; **no `--count` / no batch
  funding in v1**.
- **Lifecycle (my default, unobjected):** wallet Secrets owned by the
  CardanoNetwork (ownerRef) → cascade-delete on `yacd down` (ephemeral devnet;
  funds vanish on teardown anyway).

**v1 surface (all CLI; no faucet/controller/CRD change; no new third-party deps):**
- `yacd wallet add NET [--name N] [--topup L] [--await]` → generate via
  `internal/cardano/wallet` → create labeled Secret (ownerRef→network) → optional
  fund via `/v1/topups`, self-heal on confirm (mirror dev-wallet FundedTxID).
- `yacd wallet list NET [--json]` → list Secrets by label.
- `yacd wallet topup NET WALLET L [--await]` → today's `topup` keyed by selector.
- `yacd wallet export NET WALLET [--out DIR] [--force]` → read Secret → write
  `0600` `.skey`/`.vkey`/`.addr` in the gitignored `.yacd/<ns>/<net>/wallets/...`.
- `yacd wallet remove NET WALLET` → delete the Secret.
- Reuse `requireFaucetReady` (public-network gate falls out free); self-forward
  transport like `topup` (no `yacd run` wrapper). Add a tiny pubkey-hex accessor
  to `internal/cardano/wallet.Material`. New `kube.Client` methods: create / list
  / delete labeled wallet Secrets. Embed an adjective-noun wordlist for names.

**Open design sub-decisions (recommended defaults):** wallet selector = second
positional (`yacd wallet topup NET WALLET L`); name stored as a (lowercased,
DNS-1123) label for free selector lookup + annotation for display, so no separate
index is needed; clearly distinguish the new managed wallets from the existing
controller `status.wallet` dev wallet; pick the exact soft-ceiling number.

**Next:** (pending user OK) write a short design doc + phased plan to
`.journal/058/`, then implement on a fresh impl worktree (start dev stack then).
Analysis artifacts: workflow run `wf_1c108c99-0f2`.
