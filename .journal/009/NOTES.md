---
id: 009
title: Session 009
started: 2026-05-22
---

## 2026-05-22 09:56 — Kickoff
Goal for the session: Start a new YACD journal session and wait for the actual implementation request.
Current state of the world: The personal journal worktree is clean and up to date on `journal/jmgilman`. The default checkout is clean on `master` at `8bf1b26` (`feat(cli): add developer environment CLI (#13)`). Required `git` and `worktrunk` skills are loaded, `.journal/TECH_NOTES.md` has been read, and recent closed summaries `006`, `007`, and `008` have been loaded. Session `008` closed the developer CLI foundation; the dev stack was stopped during that closeout and no implementation worktree has been selected for this session yet.
Plan: Wait for the user's actual request. For implementation work, select or create an isolated Worktrunk worktree from the current default branch, then run `moon run root:dev-up` once from that implementation worktree before making code changes unless the user explicitly waives the dev stack startup.

## 2026-05-22 13:42 — Faucet Boundary Discussion
Goal for the session: Begin phase 5 by discussing the faucet/topup component shape before implementation.
Current state of the world: Phase 4 is complete per `.journal/PLAN.md`, with the CLI now able to deploy and inspect a local `CardanoNetwork`. The emerging phase-5 direction is to keep faucet funding keys on disk inside the primary workload boundary instead of exporting them as Kubernetes Secrets. The likely prototype shape is a narrow faucet sidecar/API in the primary Pod, with the API acting as the controlled access path to signing/topup operations.
Plan: Continue the design conversation around the faucet API surface, binding/exposure model, wallet metadata needs, and how the CLI should call the service before selecting an implementation branch.

## 2026-05-22 14:00 — Kupo Direction
Goal for the session: Reassess Kupo's role in phase 5 rather than treating it only as an Apollo helper dependency.
Current state of the world: Kupo is a lightweight standalone chain indexer with an HTTP API and can run alongside Ogmios, so it has value for the YACD development environment outside Go/Apollo. The working direction is to consider Kupo as a sensible default sidecar/service next to Ogmios for local developer UTxO/address/index queries, while keeping the faucet itself narrow and still using the Pod as the signing-material boundary.
Plan: Shape phase 5 around an initial faucet service plus Kupo-backed lookup where useful, then decide whether Kupo should be wired into `CardanoNetwork` status and CLI connection output in the same slice or a follow-up.

## 2026-05-22 14:13 — Implementation Start
Goal for the session: Implement Kupo as a first-class `CardanoNetwork` chain API following the Ogmios sidecar pattern.
Current state of the world: Created implementation worktree `feat/kupo-chain-api` at `/Users/josh/code/meigma/yacd/.wt/feat-kupo-chain-api`. `moon run root:dev-up` completed successfully from that worktree, starting the shared `kind-yacd-dev`/Tilt stack with the UI at `http://localhost:10350/`.
Plan: Add the CRD fields, workload sidecar, owned Service/status/readiness behavior, CLI info output, devconfig validation, docs, and tests; then regenerate and run the full verification bundle.

## 2026-05-22 14:34 — Kupo Chain API Implemented
Goal for the session: Finish the first Kupo slice with runtime proof, not just API/schema wiring.
Current state of the world: The implementation worktree now adds `spec.chainAPI.kupo`, a default Kupo sidecar, owned `<network>-kupo` Service, `status.endpoints.kupo`, `KupoReady`, CLI `info` output, devconfig explicit-field validation, README/current-state text, unit/envtest coverage, and Chainsaw smoke coverage. The live smoke initially exposed that Kupo v2.11.0 needs writable `/tmp` while running under the restricted/read-only-root profile; the final workload now adds a Kupo-only `kupo-tmp` `emptyDir` alongside `kupo-db`.
Verification: `moon run root:generate`, `moon run root:test`, `moon run root:check`, `moon run root:test-e2e`, `git diff --check`, and `git diff --cached --check` all passed after the `/tmp` mount fix. The successful Chainsaw run proved `Ready=True`, `NodeReady=True`, `OgmiosReady=True`, `KupoReady=True`, `status.endpoints.kupo`, `yacd info --json`, an in-cluster Kupo `/matches?unspent` request, and the Kupo-disabled path with aggregate `Ready=True` while node/Ogmios remain ready.
Plan: Commit the implementation branch. Keep the dev stack running for subsequent phase-5 work unless explicitly asked to stop or close the session.

## 2026-05-22 14:51 — Review Feedback Addressed
Goal for the session: Review and fix the Kupo branch feedback without widening the slice into a full compatibility design.
Current state of the world: Accepted the Ogmios-disable and port-collision findings. When `spec.chainAPI.ogmios.enabled=false` and `spec.chainAPI.kupo` is omitted, Kupo now inherits disabled instead of making the spec unsupported, so the controller reaches the normal apply/delete path and removes both owned chain API Services. Explicit `kupo.enabled=true` with Ogmios disabled still fails as unsupported. The builder now rejects node/Ogmios/Kupo port collisions before rendering a broken Pod. For Kupo image overrides, the branch now rejects untagged, `latest`, and non-`vX.Y.Z` tags; a node/Ogmios/Kupo compatibility table remains deferred as originally planned.
Verification: `moon run root:test`, `moon run root:check`, `moon run root:test-e2e`, and `git diff --check` passed. The e2e cluster was cleaned up by the test script; the shared `yacd-dev` stack remains running.
Plan: Commit the review fixes on `feat/kupo-chain-api`.

## 2026-05-22 15:05 — Kupo Bounds Tightened
Goal for the session: Address the second Kupo review pass around compatibility and storage bounds.
Current state of the world: Upstream Kupo's README has a version compatibility table, but it does not include the runtime-proven `cardano-node 11.0.1` default pair used by this branch. Instead of inventing a broader table, the controller now only accepts the known default Kupo image `cardanosolutions/kupo:v2.11.0`; non-default Kupo images are unsupported until a grounded compatibility contract exists. The Kupo container now runs with `--prune-utxo`, the `/kupo` and `/tmp` `emptyDir` volumes have size limits, and the default Kupo container resources include an ephemeral-storage request/limit. Explicit Kupo resource overrides still replace the default container resources, while the volume size limits remain.
Verification: `moon run root:test`, `moon run root:check`, `moon run root:test-e2e`, and `git diff --check` passed. The e2e cluster was cleaned up; only the shared `yacd-dev` stack remains.
Plan: Commit the tightening patch on `feat/kupo-chain-api`.

## 2026-05-22 15:12 — PR Opened And CI Passed
Goal for the session: Publish the Kupo branch and verify GitHub CI.
Current state of the world: Pushed `feat/kupo-chain-api` to origin and opened PR #14, `feat(cardanonetwork): add kupo chain api`, against `master`: https://github.com/meigma/yacd/pull/14. The PR is open, ready for review, mergeable, and `mergeStateStatus` is `CLEAN`.
Verification: `gh pr checks 14 --watch --fail-fast` completed successfully. The `ci` check and Kusari Inspector passed; release dry-run checks were skipped for this branch.
Plan: Wait for human review/merge direction. Keep the shared `yacd-dev` stack running until explicit session close or shutdown request.

## 2026-05-22 15:17 — Kupo PR Merged
Goal for the session: Merge the approved Kupo PR.
Current state of the world: PR #14 was squash-merged into `master` at `b52e923069ef0e98457477ed6e4e0c35cc7be0a1`. The GitHub PR state is `MERGED`. The remote `feat/kupo-chain-api` branch was deleted explicitly after `gh pr merge --delete-branch` completed the remote merge but hit a local Worktrunk checkout conflict because `master` is already checked out in the primary worktree.
Verification: Confirmed PR #14 is merged, CI/Kusari checks were successful, and `origin/master` points at `b52e923069ef0e98457477ed6e4e0c35cc7be0a1`. The implementation worktree is clean.
Plan: Leave local Worktrunk cleanup, journal commit/push, and dev-stack shutdown to explicit session closeout.

## 2026-05-22 15:50 — Faucet Source API Scaffold
Goal for the session: Start the first faucet service slice with a compile-tested service scaffold and source-wallet discovery API.
Current state of the world: Fast-forwarded local `master` to the merged Kupo commit and created Worktrunk branch `feat/faucet-service` at `/Users/josh/code/meigma/yacd/.wt/feat-faucet-service`. The old Kupo-owned Tilt stack prevented startup for the new worktree, so it was stopped with `moon run root:dev-down` from `feat/kupo-chain-api` and restarted successfully from `feat/faucet-service`. The branch now has `services/faucet` with a `yacd-faucet` Cobra/Viper root command, source discovery for `cardano-testnet` `utxo-keys`, JSON-only health/readiness/source HTTP endpoints, and Moon/check wiring for `services/**/*.go`. The implementation is committed as `144c7d8` (`feat(faucet): add source API scaffold`).
Verification: `go test ./services/faucet/...`, `go run ./services/faucet/cmd/yacd-faucet --version`, `moon run root:check`, `moon run root:test`, and `git diff --check` passed. `git diff --cached --check` also passed before commit.
Plan: Keep the dev stack running under `feat/faucet-service`. Next faucet slices can wire the service into the `CardanoNetwork` Pod/Service model, then add Kupo/Ogmios-backed top-up behavior.

## 2026-05-22 16:29 — Faucet Review Hardening
Goal for the session: Address the first faucet review pass before moving on to transaction/top-up behavior.
Current state of the world: The faucet now defaults to `127.0.0.1:8080` and the listen flag help makes non-loopback exposure explicit. Source API responses no longer expose server-side key paths. Source discovery now uses `os.Root`, rejects symlinked source directories and key files, and validates exact `GenesisUTxOVerificationKey_ed25519` / `GenesisUTxOSigningKey_ed25519` key types before reporting a source ready. The hardening patch is committed as `0beb2f8` (`fix(faucet): harden source discovery`).
Verification: `go test ./services/faucet/...`, `gosec ./services/faucet/...`, `govulncheck ./services/faucet/...`, `moon run root:check`, `moon run root:test`, `git diff --check master...HEAD`, `git diff --check`, and `git diff --cached --check` passed. `gosec` reports zero issues.
Plan: Keep the dev stack running under `feat/faucet-service` and continue with the next faucet slice after review.

## 2026-05-22 17:02 — Faucet Source Usability Tightening
Goal for the session: Address the second faucet review pass around source readiness, bounded reads, and conservative source names.
Current state of the world: The faucet source boundary now requires the documented `utxo.{addr,skey,vkey}` layout, validates `utxo.addr` as a testnet Bech32 address, validates each key `cborHex` as a CBOR byte string of the expected key length, caps source directory entries and source-file sizes, and restricts source names to `utxo[1-9][0-9]*`. The source API still exposes only non-secret metadata plus the address; signing key contents and server-side paths remain hidden. The tightening patch is committed as `69e55b2` (`fix(faucet): validate usable source files`).
Verification: `go test ./services/faucet/...`, `gosec ./services/faucet/...`, `govulncheck ./services/faucet/...`, `moon run root:check`, `moon run root:test`, `git diff --check master...HEAD`, `git diff --check`, and `git diff --cached --check` passed.
Plan: Keep the dev stack running under `feat/faucet-service`. The next integration slice needs to account for the current pinned `cardano-testnet` image not emitting `utxo.addr`, despite the Developer Portal documenting that file in the sandbox layout.

## 2026-05-22 17:48 — Faucet Apollo Top-Up Slice
Goal for the session: Add the first real faucet transaction path while keeping HTTP handling, source-key loading, and Apollo/Ogmios/Kupo submission separated.
Current state of the world: The faucet now has `POST /v1/topups` for exact lovelace transfers from a selected `utxoN` source, defaulting to the configured `utxo1`. `sources.Store` keeps the public source API JSON-safe while adding a private funding read path that decodes the existing CBOR key envelopes into raw 32-byte hex for transaction submission. The new `topup` package owns request validation and orchestration, while `topup/apollo.Client` builds and submits transactions through Apollo with Ogmios and Kupo endpoints. The slice is committed as `3c10f3f` (`feat(faucet): submit top-ups with Apollo`).
Verification: `go test ./services/faucet/...`, `gosec ./services/faucet/...`, `govulncheck ./services/faucet/...`, `moon run root:check`, `moon run root:test`, `git diff --check master...HEAD`, `git diff --check`, and `git diff --cached --check` passed. `govulncheck` reports no called vulnerabilities; it notes required modules contain vulnerabilities that this code does not call.
Plan: Keep the dev stack running under `feat/faucet-service`. Full live top-up smoke, CRD/Pod injection, Dockerfile, Helm, Service exposure, and CLI client wiring remain follow-up slices.

## 2026-05-22 18:24 — Faucet Top-Up Review Fixes
Goal for the session: Address review feedback on faucet source/key correctness, concurrent spending, address validation, and accidental remote exposure.
Current state of the world: The faucet now validates source readiness by deriving the public key from `utxo.skey`, comparing it to `utxo.vkey`, deriving the enterprise testnet payment address from the verification key, and comparing that to `utxo.addr`. Testnet address validation now decodes the Bech32 payload and enforces testnet payment-address shape instead of only checking the prefix/checksum. Top-up submission serializes per source around the chain submitter, preventing concurrent requests from spending the same source UTxOs. The CLI now rejects non-loopback `--listen-address` values unless `--allow-remote-listen` is explicitly set. The review-fix patch is committed as `5bcdf9a` (`fix(faucet): harden top-up source handling`).
Verification: `go test ./services/faucet/...`, `go test -race ./services/faucet/...`, `gosec ./services/faucet/...`, `govulncheck ./services/faucet/...`, `moon run root:check`, `moon run root:test`, `git diff --check master...HEAD`, `git diff --check`, and `git diff --cached --check` passed. `govulncheck` again reports no called vulnerabilities and only unreachable vulnerabilities in required modules.
Plan: Keep the dev stack running under `feat/faucet-service`. A stronger auth/quota model should still be decided before making a faucet Service broadly reachable; this patch prevents accidental remote binding but does not add caller identity or per-caller quotas.

## 2026-05-22 19:02 — Faucet Top-Up Hardening
Goal for the session: Address the next top-up review pass without widening into Kubernetes Secret/Service or CLI client wiring.
Current state of the world: The faucet service now requires a startup-loaded bearer token file for `POST /v1/topups`, enforces `Authorization: Bearer ...` with constant-time hash comparison, rejects non-JSON mutating requests before body decode, adds a configurable minimum top-up amount, rejects source-address self-transfers, verifies the completed Apollo transaction still has exactly one destination output with the requested lovelace and no assets, submits signed CBOR directly through `ogmigo.SubmitTx` while checking protocol-level Ogmios errors, and tracks per-source pending spent inputs so later top-ups exclude UTxOs submitted but not yet reflected by the indexer.
Verification: `go test ./services/faucet/...`, `go test -race ./services/faucet/...`, `gosec ./services/faucet/...`, `govulncheck ./services/faucet/...`, `moon run root:check`, `moon run root:test`, `git diff --check master...HEAD`, and `git diff --check` passed. `govulncheck` reports no called vulnerabilities, with only unreachable vulnerabilities in required modules.
Plan: Commit the service-only hardening patch on `feat/faucet-service`. Future Kubernetes work should generate/mount the token Secret and add CLI token retrieval before exposing the faucet endpoint.

## 2026-05-22 21:39 — Faucet Vertical Review Fixes
Goal for the session: Address review feedback on the Kubernetes faucet vertical and the Apollo transaction safety boundary.
Current state of the world: The `feat/faucet-service` branch now has follow-up commits `c943284` and `278c535`. Faucet is opt-in by default, unsupported faucet specs revoke owned Service/Secret/status exposure, `yacd topup` rejects stale or degraded status before reading the auth Secret, the faucet server reloads the mounted bearer token for each mutating request, the faucet sidecar only mounts `/state/env/utxo-keys` from the localnet PVC via `subPath`, and Apollo transaction validation now bounds fees, requires every input to be a loaded source UTxO, and verifies net source lovelace loss equals requested top-up plus fee.
Verification: `go test ./services/faucet/...`, `go test -race ./services/faucet/...`, `gosec ./services/faucet/...`, `govulncheck ./services/faucet/...`, `moon run root:check`, `moon run root:test`, `moon run root:test-e2e`, `git diff --check master...HEAD`, and `git diff --check` passed. The Chainsaw smoke included a valid authenticated faucet top-up after the narrowed mount change and cleaned up its test cluster.
Plan: Keep the shared dev stack running under `feat/faucet-service`. The branch is ready for the next review/PR step unless more faucet feedback arrives.

## 2026-05-23 09:36 — Faucet Security Review Follow-Up
Goal for the session: Address the remaining faucet security review findings without widening into namespace-scoped installs or split service accounts.
Current state of the world: Commit `608d8e3` (`fix(faucet): harden image and token trust boundaries`) now covers the faucet image in Kyverno's default image verification refs, constrains CR-level faucet image overrides to the configured default faucet repository, removes the controller's Secret watch and Secret list/update RBAC, moves all controller Secret reads to the live API reader, adds periodic faucet Secret repair requeues, adds explicit `yacd topup` trust flags for custom non-loopback faucet URLs, and rejects Ogmios tx-id mismatches after submit.
Verification: `moon run root:generate`, `go test ./services/faucet/...`, `go test -race ./services/faucet/...`, `gosec ./services/faucet/...`, `govulncheck ./services/faucet/...`, `moon run root:check`, `moon run root:test`, `moon run root:test-e2e`, `git diff --cached --check`, `git diff --check master...HEAD`, and `git diff --check` passed. The first e2e attempt exposed one remaining cached Secret read in faucet readiness status; after switching that read to the live API reader, the rerun passed and cleaned up its test cluster.
Plan: Keep the shared dev stack running under `feat/faucet-service`. The branch is clean at `608d8e3` and ready for the next review/PR step.

## 2026-05-23 10:03 — Faucet Dependency And Timeout Follow-Up
Goal for the session: Address the latest faucet review findings around the called `golang.org/x/net` vulnerability and unbounded HTTP server I/O.
Current state of the world: Commit `c47cec8` (`fix(faucet): bound server timeouts and update x/net`) bumps `golang.org/x/net` to `v0.55.0` and lets Go refresh companion `golang.org/x/*` modules. The faucet HTTP server now sets bounded read-header, full-read, write, and idle timeouts, with a focused unit test asserting those settings.
Verification: `go test ./services/faucet/...`, `go test -race ./services/faucet/...`, `go test -race ./cli/internal/cli`, `govulncheck ./...`, `moon run root:check`, `moon run root:test`, `gosec ./services/faucet/...`, `git diff --check master...HEAD`, `git diff --check`, and `git diff --cached --check` passed. `govulncheck ./...` reports zero called vulnerabilities.
Plan: Keep the shared dev stack running under `feat/faucet-service`. `moon run root:test-e2e` was not rerun for this narrow dependency/server-timeout follow-up.
