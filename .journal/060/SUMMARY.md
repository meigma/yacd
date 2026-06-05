---
id: 060
title: External-access URLs for chain APIs — design + P1 (NodePort/externalURL) shipped
date: 2026-06-04
status: complete
repos_touched: [yacd]
related_sessions: [059, 058, 057, 049]
---

## Goal
Make the yacd CLI reach a local network's Ogmios/Kupo without an ephemeral
per-command `kubectl` port-forward — slow for `yacd devnet` (same machine) and
pointless when a shared cluster already fronts the services with a real ingress.
Design the approach end-to-end, then implement Phase 1 (the API + operator half).

## Outcome
**Met for the design and Phase 1.** Phases 2 and 3 are intentionally deferred
(see Open Threads).

- **Design (no code):** `.journal/060/EXTERNAL_ACCESS_DESIGN.md` — a 3-phase plan
  where the CardanoNetwork advertises a directly-reachable URL and the CLI reads
  it (probing first) before falling back to forwarding.
- **P1 shipped (PR #112, squash `dfa20b8`):** API + operator support for
  `service.{type,nodePort}` + `externalURL` on `spec.chainAPI.{ogmios,kupo}`,
  rendered NodePort Services, the additive `status.endpoints.*.externalURL`
  mirror, Go validation, and a fix to a NodePort-clobber bug in the shared
  `MutateService`. `root:generate`/`root:check`/`root:test` all green; merged
  with CI passing (ci, e2e, cardano-tools-image, Kusari).
- **Incidental fix (PR #111, squash `b61089b`):** master CI had been red since
  the 0.2.0 release (#87) because it bumped `Chart.yaml` appVersion to v0.2.0 but
  left six `cli/internal/operator/ssa` version "tripwire" tests asserting v0.1.1.
  Fixed as a separate test-only PR per the user's call.

Dev stack was **waived** for P1 (envtest-only; the user confirmed). No Kind/Tilt
or k3d cluster was started, so nothing to tear down.

## Key Decisions
- **External URL lives on the CardanoNetwork CRD, not a separate CLI config.**
  The user pushed back on an earlier layering objection: localhost is a *valid
  assertion* for a co-located devnet, and one CRD field unifies devnet + remote
  (a platform team asserts a real ingress URL). The field is an *advertisement*;
  making it routable is the provisioner's job (NodePort for devnet, ingress for
  shared clusters).
- **`externalURL` is a sibling of `service`, mirrored as a peer of `url`**
  (additive; the in-cluster `url` is unchanged). It is meaningful even with
  ClusterIP (a user-run ingress fronting the ClusterIP), so it is not nested
  under `service`.
- **Lenient externalURL validation** (absolute + host; `ws`/`wss`/`http`/`https`
  for both) — the CLI will probe before trusting (P3), so strict per-component
  schemes would only reject legitimate gateway/ingress topologies. This
  overrides the design doc's original "scheme matches the service" prose.
- **Validation is Go → `Degraded`/`UnsupportedSpec`** (matching every existing
  chainAPI check) for the conditional rules; CRD markers cover the unconditional
  parts (Enum on `type`, `Maximum=32767` on `nodePort` — NOT `Minimum=30000`,
  which would reject the legal `0`=auto-assign).
- **The `MutateService` fix went in the shared helper, guarded.** The shared
  mutator did `current.Spec.Ports = desired.Spec.Ports` wholesale, wiping a
  Kubernetes-assigned NodePort every reconcile (thrash) despite its doc comment
  claiming preservation. `mergeServicePorts` now preserves an assigned node port
  by port name when desired is NodePort and leaves it 0; a pinned value wins, and
  ClusterIP takes desired verbatim (still strips tampered ports). Verified no-op
  for the only other caller (db-sync, ClusterIP); the three existing
  `...CorrectsService...` tests stay green.
- **Node socket exposure was considered and rejected** (recorded in the design
  doc §6): n2n is the wrong protocol for dev tooling; n2c is a Unix socket that
  TCP forwarding can't carry; Ogmios already exposes node-to-client over
  WebSocket, so it covers the txn-submission use case.

## Changes (PR #112)
- `api/v1alpha1/cardanonetwork_types.go` — `ChainAPIServiceType` enum +
  `ServiceExposureSpec{type,nodePort}`; `Service`+`ExternalURL` on Ogmios/Kupo;
  `ExternalURL` on the shared `ServiceEndpointStatus` (also surfaces on the
  db-sync metrics/postgres endpoints — additive, unpopulated).
- `internal/controller/cardanonetwork/settings.go` — settings carry
  `serviceType`/`nodePort`/`externalURL`; shared `resolveServiceExposure`.
- `internal/controller/cardanonetwork/resources.go` — builders render
  `settings.serviceType` + `pinnedNodePort(...)`.
- `internal/controller/cardanonetwork/validate.go` +
  `builder.go` — `validateChainAPIServiceExposure` + `validateExternalURL`
  wired into `chainAPISettings`.
- `internal/controller/cardanonetwork/status.go` — mirror trimmed spec
  `externalURL` into endpoints (nil-guarded helpers; service-present branch only).
- `internal/ctrlkit/resources/resources.go` — `MutateService` +
  `mergeServicePorts` NodePort preservation.
- Generated: `zz_generated.deepcopy.go`, both CRDs.
- Tests: 3 ctrlkit unit tests (the thrash guard — envtest has no NodePort
  allocator), 6 builder reject rows, 2 controller reconcile tests, 1 manager
  envtest (externalURL mirror + pinned NodePort + CRD-marker rejection).
- PR #111 (test-only): `cli/internal/operator/ssa/{apply_test,install_envtest_test}.go`
  v0.1.1 → v0.2.0.

## Open Threads — EXPECTED NEXT STEPS (P2, P3)
The design (`.journal/060/EXTERNAL_ACCESS_DESIGN.md`) has two phases left. P1
landed the API/operator foundation they build on.

- **P2 — devnet plumbing (CLI, `yacd devnet`).** Make the localhost URL actually
  route. devnet owns both the k3d cluster and the default network spec, so pin
  constants on both sides:
  - Create the k3d cluster with host port mappings —
    `--port "1337:30137@loadbalancer"` and `--port "1442:30442@loadbalancer"`
    (stable, non-experimental create-time `--port`; the serverlb proxies the host
    port to the same node port, which is why NodePort is required) in
    `cli/internal/cluster/k3d/ensure.go`'s `create`.
  - Author the devnet default spec with `chainAPI.{ogmios,kupo}.service.type:
    NodePort` + the pinned nodePorts (30137/30442) and
    `externalURL: ws://localhost:1337` / `http://localhost:1442` (the new P1
    fields). devnet's localhost URLs are then deterministic.
  - **Needs a live k3d run to verify** (envtest can't prove host routing). Bring
    up the dev stack or a throwaway k3d for this phase.
  - Constraints to honor: nodePorts must be 30000-32767; pinned host ports
    (1337/1442) can clash with something already bound on the dev's machine —
    surface a clear error (relates to local-lifecycle P7 hardening).

- **P3 — CLI resolver (CLI).** Add a shared resolution chain in the forward path
  so every consumer benefits: explicit flag (`--ogmios-url`/`--kupo-url`) →
  ambient `YACD_OGMIOS_URL`/`YACD_KUPO_URL` env → `status.endpoints.*.externalURL`
  (probe with a short connect timeout, cache the verdict) → ephemeral
  port-forward fallback (today's behavior). The override layers (`YACD_*` env;
  topup/kupo flags) already exist; the new rung is "read + probe externalURL".
  Wire it into the funding/`run` paths (the CLI builds + submits funding txns
  directly over Ogmios/Kupo now that the faucet is gone — see session 059). The
  current force-forward lives in `cli/internal/cli/forward.go`
  (`forwardEndpoints`) and `envcontract.go:loopbackURL`; `connect` stays
  forward-only (it is the remote-cluster tool).

Other carried context:
- A parallel **session 061** is doing faucet-removal P4–P5 (release + docs);
  master may move. Re-fetch/rebase before P2/P3 work.
- Banked artifacts: `.journal/060/PHASE1_PLAN.md` (the executed P1 plan),
  `EXTERNAL_ACCESS_DESIGN.md` (full design incl. rejected alternatives).

## References
- PRs: #112 (P1, `dfa20b8`), #111 (master-red tripwire fix, `b61089b`). Both
  squash-merged to `master`.
- Design: `.journal/060/EXTERNAL_ACCESS_DESIGN.md`; plan:
  `.journal/060/PHASE1_PLAN.md`.
- Prior: `.journal/059/` (faucet removal — the dependency that landed first),
  `.journal/049/` (devnet lifecycle), `.journal/058/SUMMARY.md` (yacd install).

## Lessons
- **k3d create-time `--port` is stable; only `cluster edit --port-add` (mutating
  a running cluster) is experimental.** Pinning host+node ports at *create* time
  (devnet owns creation) sidesteps the experimental path entirely. The serverlb
  forwards a host port to the *same* node port, so the target must be a NodePort
  service (or Traefik ingress); plain ClusterIP is not host-reachable.
- **envtest's apiserver DOES allocate/accept NodePorts** (it is an apiserver
  registry function, not controller-manager), so a *pinned* NodePort is
  assertable in envtest. But envtest can't reproduce the *auto-assign* thrash
  (nothing re-allocates), so the `MutateService` preservation fix is guarded by a
  direct ctrlkit unit test, not envtest.
- **A green-looking master can be red.** The 0.2.0 release merged with CI failing
  (stale version tripwires); always check `gh run list --branch master` rather
  than assuming. Worth fixing pre-existing red as its own PR so the feature PR
  stays focused and reviewable.
