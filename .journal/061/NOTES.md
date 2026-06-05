---
id: 061
title: Faucet removal P4–P5 (cut over funding, delete faucet service, release + docs)
started: 2026-06-04
---

## 2026-06-04 07:36 — Kickoff

Goal for the session: continue the CLI-native-wallets / faucet-removal
re-architecture from session 059. **P1–P3 are done and merged** (PRs #95, #98,
#99, #97, #106). This session takes on **P4 (the breaking PR — cut over
funding to the CLI, then delete the faucet service + `spec.chainAPI.{faucet,
wallet}`)** and **P5 (faucet-free operator release + re-render the CLI's
embedded chart + docs)**, per `.journal/059/WALLET_REARCH_PLAN.md`.

Current state of the world:
- After P1–P3 the CLI fully manages wallets and funds them by building/signing/
  submitting txns directly against Ogmios/Kupo, spending from a genesis-funded
  `faucet` wallet (`<net>-wallet-faucet` Secret, generated once by the
  controller; funded at genesis via the `cardano-tools fund-genesis` init step).
- **The in-cluster faucet service still exists and is still used** by the
  controller's dev wallet (`spec.chainAPI.wallet`), which is still faucet-HTTP-
  funded. Nothing in P1–P3 deleted the faucet.
- `master` HEAD is `0b3a629` (PR #106, the `yacd wallet` verbs).
- cardano-tools is at `11.0.1-yacd.6` (genesis-fund verb), digest-pinned in
  `internal/cardano/toolsimage`.
- Operator last released `v0.1.1` (manager + faucet image + chart); the CLI
  embeds a digest-pinned render of `charts/yacd` (session 058 moved this to an
  in-memory render of the in-place `//go:embed`'d chart — `charts/embed.go`).

P4 scope (from the plan, breaking CRD change — pre-1.0, ephemeral devnets):
- `devnet`/`up`: create+fund a default wallet CLI-side so the funded-wallet UX
  survives; remove controller-side `spec.chainAPI.wallet` funding entirely.
- Delete the faucet service (`services/faucet/`, Dockerfile, ko-build-faucet,
  Tiltfile resource, release.yml faucet jobs, chart `faucet.image.*` +
  `--default-faucet-image`).
- Delete controller faucet wiring (faucetContainer, source-address init, faucet
  Service, `<net>-faucet-auth` Secret + rotation/hash/repair +
  `revokePrimaryFaucetExposure`, faucet readiness/status, `FaucetReady`,
  `spec.chainAPI.faucet` + defaults/validation).
- CLI: drop faucet-auth-token reads + the bearer-token trust gate; replace
  `requireFaucetReady` with the new "Ogmios+Kupo ready + funded source" gate.
- Rewrite Chainsaw (drop the faucet HTTP smoke), update examples + docs.
- Fold in the durable ogmigo/Apollo Ogmios-client cleanup if convenient (the
  GenesisParams ws-1006 trap — open thread from 059).

P5 scope: faucet-free operator release (release-please → release.yml),
re-render/re-pin the CLI's embedded chart to the new digests, CLI release, docs
update (coordinate with in-flight docs PR #91).

Plan: read `.journal/059/WALLET_REARCH_PLAN.md` (done) + the session-059 SUMMARY
(done). Next: set up an implementation worktree off fresh `origin/master`,
`moon run root:dev-up`, then design/execute P4 (likely with an adversarial
deletion-surface check before the breaking PR — workflow `wf_bb7e8066-c23`'s
`verify:deletion-surface` is the enumerated checklist). Awaiting the user's
go-ahead before substantive work.

## 2026-06-04 12:16 — P4 design + P4a implemented (awaiting human review)

Plan approved (`/Users/josh/.claude/plans/please-propose-a-plan-optimized-dusk.md`).
Design workflow `wf_6286065c-09e` (survey + 2 designs + adversarial critique) drove
3 locked decisions with the user:
1. **Funded-wallet UX = option B: "the faucet wallet IS the funded wallet."** No
   auto-created default wallet; devnet/info display the genesis-funded `faucet`
   wallet. P4 adds NO new funding code (the P3 `wallet topup --from faucet`
   primitive already exists). Revises the plan's "create+fund a default wallet"
   wording.
2. **Split P4 into PR-4a (cutover) + PR-4b (deletion).**
3. **P4 validated on the dev stack; the faucet-free operator release + embedded
   re-pin that makes `yacd devnet` work end-to-end is P5.** devnet/info wallet
   display MUST degrade gracefully when the faucet wallet Secret is absent (older
   embedded v0.1.1 operator) — now covered by tests.

Implementation worktree: `.wt/refactor-faucet-removal-p4a` (branch
`refactor/faucet-removal-p4a`, off master 0b3a629). dev stack up (own kind cluster).

**PR-4a DONE — commit `6cdb250` (unsigned; see auth note), 28 files +186/−1099.**
- Removed the controller dev wallet entirely: deleted `wallet_funding.go` + the
  dev-wallet half of `wallet.go`; removed `walletSettings`/`resolveWalletSettings`,
  the dev-wallet apply/status/condition wiring across builder/controller/status/
  conditions/delete/names/resources; API dropped `WalletSpec`/`WalletStatus`/
  `spec.chainAPI.wallet`/`WalletReady` (regenerated CRD+deepcopy).
- KEPT the genesis faucet wallet + shared Secret apply core + the whole faucet
  SERVICE (faucet service deletion is PR-4b).
- CLI: `lifecycle.Up` + `devnet` + `info` display the genesis faucet wallet via
  `cli/internal/wallet` `Store.Faucet` (graceful when absent); dropped
  `chainAPI.wallet` from devnet.yaml/init.yaml/examples/local + rewrote prose.
- Chainsaw: asserts the `<net>-wallet-faucet` Secret instead of the dev wallet;
  faucet HTTP service smoke preserved.
- Gates GREEN: `root:check`, `root:test` (envtest+unit), `root:test-e2e` (real
  Kind/Chainsaw, 186s PASS), `go build`, manager dep-boundary (`./cmd` clean of
  ogmigo/kugo/Apollo-tx-builder). Adversarial review `wf_26b14b10-333` (16 agents)
  found NO defects; added 2 graceful-absence coverage tests it suggested.

**Auth note (BOTH agent caches expired mid-session; worked during session-new):**
gpg signing fails (committed `--no-gpg-sign`; GitHub squash-merge signs server-side)
and the GitHub SSH key is not loaded (push blocked). To restore:
`ssh-add --apple-use-keychain ~/.ssh/id_ed25519_macbook` and unlock gpg. Branch is
NOT pushed yet.

Next: PAUSE for human review of PR-4a (per user instruction: pause before each
merge). After approval + auth restore: push branch, open PR, pause again before
merge. Then PR-4b (faucet service deletion + the builder.go re-gate to local-only
+ Chainsaw/RBAC/dbsync-test), then P5 (release).

## 2026-06-04 ~14:50 — PR-4b DONE (deletion + re-gate), gates green

PR-4a merged (squash `dfa9dd4`, PR #107). PR-4b built in worktree
`.wt/refactor-faucet-removal-p4b` (branch `refactor/faucet-removal-p4b`, off
master `dfa9dd4`). **Commit `167ea9b`** (unsigned; squash-merge re-signs),
**88 files +268/−7653** (bulk is the `services/faucet/` tree).

What 4b did:
- Deleted the in-cluster faucet **SERVICE** end to end: `services/faucet/`,
  faucet image (release.yml jobs, Tiltfile, ko-build-faucet.sh, chart
  faucet.image + kyverno + `--default-faucet-image`), and the CLI embedded-chart
  faucet image plumbing.
- Deleted controller faucet-service wiring: `faucet_auth.go`,
  `faucet_auth_watch.go`, faucetContainer, source-address init, faucet
  Service/auth Secret, FaucetReady, revokePrimaryFaucetExposure, faucet
  readiness/status/conditions/defaults, the faucet port-conflict branch, and the
  primarypod faucet port.
- **The re-gate (highest-risk edit):** `faucetWalletEnabled` /
  `resolveFaucetWalletSettings` now gate the genesis faucet **WALLET** on
  `Spec.Mode == Local` ALONE (dropped the old `faucet.enabled` dependency).
- API removed `FaucetSpec`/`FaucetStatus`/`ChainAPISpec.Faucet`/status.Faucet/
  endpoints.Faucet/FaucetReady (regenerated CRD+deepcopy+mocks).
- CLI dropped the faucet trust-gate residue (faucetTokenForHost, YACD_FAUCET_*,
  endpoints FaucetURL, info/list faucet, ConditionFaucetReady, devconfig faucet
  validation).
- Rewrote the Chainsaw smoke (dropped the faucet HTTP curl test; asserts the
  `<net>-wallet-faucet` Secret + a non-empty address; teardown disables kupo
  only); fixed the orphaned dbsync port-conflict test (8080 freed).

Gates GREEN: `go build ./...`, `go vet`, `root:check` (gofmt/lint/generated-
artifacts/helm/chainsaw-lint), `root:test` (envtest+unit incl. a NEW
`TestCardanoNetworkReconcilerReconcileGatesFaucetWalletOnLocalMode` direct-
reconcile test proving the re-gate: local→wallet Secret present, public→absent),
`root:test-e2e` (real Kind/Chainsaw, 1 passed — wallet-faucet Secret + funded
address, no faucet service, reaches Ready). Manager dep boundary `./cmd` clean
(only Apollo address/key subpkgs; no ogmigo/kugo/apollo-tx-builder). Faucet
token sweep: only the faucet WALLET, Cardano `*-keys` genesis artifacts, and
P5-deferred doc prose remain.

Adversarial review `wf_8a25ce64-ea7` (11 agents, 6 dimensions): 3 confirmed, 2
dismissed. Disposition:
- **Missing wallet-Secret Owns() watch (dismissed):** master's
  `Owns(&Secret, faucetAuthSecretEventPredicate)` only ever matched
  `-faucet-auth`; the wallet Secret (`-wallet-faucet`) was deliberately excluded
  then too. Not a 4b regression; recreating the create-once genesis wallet on
  external delete would mint an UNFUNDED address — watching is wrong by design.
- **RBAC list/watch on secrets now unused (actioned, per-controller only):**
  tightened cardanonetwork's marker to `get;create;patch;delete` (its only Secret
  watch — the auth-Secret Owns — is gone). The shared manager ClusterRole is the
  controller-gen UNION and CANNOT shrink: `cardanodbsync` legitimately needs
  list/watch (`Owns(&Secret)` + a Secret `Watches` handler). Chart unchanged;
  `TestManagerRBACMatchesControllerGen` green.
- **Chainsaw missing positive survival assert (actioned):** added an explicit
  `kubectl get secret <net>-wallet-faucet` check after the kupo-disable patch so
  a regression that deletes the wallet on a chain-API toggle is caught.

Dev-stack live `yacd wallet topup --from faucet` NOT run: the running dev stack
(`.run/yacd-dev`, pid 25886) is bound to the **4a** worktree (pre-4b code, still
has faucet-image); re-pointing it needs a disruptive `dev-down` (deletes the
Kind cluster) + `dev-up` rebuild. Chainsaw e2e already proves the 4b-specific
risk (re-gate) in a real cluster, and the topup tx path is unchanged P3 code, so
this is a user decision — surfaced at the review pause.

Next: push branch + open PR-4b, PAUSE for human review before merge (standing
instruction). Then P5 (faucet-free operator release → re-pin CLI embedded
manager digest → CLI release → docs PR #91).

## 2026-06-05 ~00:00 — P5 release DONE + devnet validated (Phases A–C)

PR-4b merged (squash `1652ac6`, PR #108). P5 planned (ultracode research
workflow `wf_f948275b-42b`); plan approved with a key decision: **tag/appVersion
pinning** instead of digest pinning, because operator+CLI are ONE coupled
release-please component (`.` = `yacd`), so a v0.2.0 CLI can't digest-pin its own
v0.2.0 operator (digest doesn't exist until the release builds).

**Phase A — PR #109 (merged `6a290bd`):** `refactor(cli):` drop
`defaultManagerDigest`; `Default()` sets only `Repository` so the chart's
`yacd.image` helper renders `repository:appVersion`. Since operator+CLI release
together, the appVersion tag always resolves to the matching published image.
Behavior change: `--set image.digest` still pins; `--set image.tag` now REPOINTS
(was shadowed by the old digest). Rewrote operator render/values + install tests
(version-agnostic: expected tag derived from the embedded chart's appVersion) and
install help/comment. root:check + root:test green.

**Phase B — v0.2.0 released:** release-please PR #87 (`chore(master): release
0.2.0`) merged (`aecf7ef`) → root + Chart.yaml version/appVersion → 0.2.0. Note:
release-please did NOT rebase #87 after #109, so the v0.2.0 changelog omits the
#109 tag-pin line (cosmetic; #109's code IS in v0.2.0 since it's on master and
the release commit squashes on top — user chose "merge as-is"). `release.yml`
succeeded; published the draft. Live: `ghcr.io/meigma/yacd:v0.2.0` (multi-arch)
+ OCI `chart:0.2.0` + CLI binaries; v0.2.0 is Latest.

**Phase C — devnet validated end-to-end** with the RELEASED CLI v0.2.0 binary
(`yacd 0.2.0 (aecf7ef)`, darwin/arm64) on k3d: `yacd devnet` → "Operator v0.2.0
ready" (operator Deployment image = `ghcr.io/meigma/yacd:v0.2.0`, confirming the
tag-pin), network ready, devnet output shows the genesis faucet **Wallet:**
address (empty with the old v0.1.1 operator). `yacd wallet add devnet --name
alice --topup 5000000 --await --from faucet` → tx `8eb5e791…` **Confirmed
on-chain**. `wallet list` shows alice (managed-by-cli), faucet excluded.
`yacd devnet down` cleaned up the k3d cluster. The ogmigo ws-1006 genesis-config
warning still prints (non-fatal; the documented durable follow-up).

Remaining: **Phase D docs** — README.md + DESIGN.md faucet/dev-wallet/`yacd
topup --address` prose is stale (fix on master; not in #91). PR #91 (jmgilman's
MkDocs site, 17 pages) is OPEN but predates P4b and modifies host-access.md +
adds developer/{funding,connecting-tools}.md + reference/{cli,cardanonetwork}.md
— the site docs should be fixed WITHIN #91 (rebase on v0.2.0 + rewrite the stale
wallet pages), not duplicated on master. **Phase E housekeeping** — proto-*.log
gitignore+delete, remove merged 4a/4b worktrees, dev-down (dev stack still on 4a)
at session close, optional publish of cardano-tools/testnet drafts, file the
ogmigo ws-1006 follow-up issue.
