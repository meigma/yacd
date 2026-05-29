
## 2026-05-29 — CLI fresh-build-primitive assessment
Reviewed cli/ against the 3-part fresh-build primitive (deterministic setup +
compressed-epoch localnet + clean ephemeral lifecycle). Verdict: ~70-80% exists;
the hard substrate is done, the clear gap is teardown + a thin CI wrapper.

Have (✅):
- Compressed/fast localnet substrate — the hard part, effectively done.
  `LocalNetworkSpec` (api/v1alpha1/cardanonetwork_types.go): `Era` (can start
  directly in conway, no era traversal), `Timing.SlotLength` (example uses
  100ms), `Timing.EpochLength` (example 500 slots => ~50s epoch), pool
  topology, and `GenesisProfile` presets (default/zero-fee/zero-min-utxo/both)
  ideal for friction-free test setup. Example: examples/local/yacd.yaml.
- Create + wait-ready: `yacd deploy -f f --wait --timeout` (cli/internal/cli/
  deploy.go) renders devconfig -> SSA CardanoNetwork -> kube.WaitReady
  (cli/internal/kube/wait.go) with correct Ready/Degraded/observedGeneration
  staleness handling.
- Connection discovery for harness: `yacd info NAME --json`
  (cli/internal/cli/info.go) emits node/ogmios/kupo/faucet endpoints +
  conditions.
- Funding known-spendable UTxOs: `yacd topup NAME --address <addr> --lovelace`
  (cli/internal/cli/topup.go) funds an ARBITRARY address => clean determinism
  model: test brings its own checked-in key, funds it via faucet, owns a stable
  spendable UTxO. Sidesteps "extract genesis keys".

Gaps (❌/⚠️):
- TEARDOWN missing (clearest gap). No `yacd destroy`/`down`, no Delete on the
  kube.Client port (interface only has DefaultNamespace/ApplyCardanoNetwork/
  GetCardanoNetwork/GetSecretValue, cli/internal/kube/client.go), no
  wait-for-deletion. Today teardown = `kubectl delete`. Small, well-scoped add.
- Ephemeral namespace lifecycle: CLI doesn't create/delete namespaces; CI
  manages externally or we add a flag.
- Setup determinism nuance: localnet genesis likely regenerates keys/addresses
  per run unless seeded => addresses not stable across runs; harness should
  discover dynamically OR use the BYO-key+topup model (preferred). UNVERIFIED
  whether cardano-testnet localnet genesis is reproducible — worth checking.
- GitHub Action: doesn't exist yet (expected; would be a thin wrapper over
  deploy/info/topup/destroy).
- Arbitrary chain-state setup (deploy scripts, build specific UTxOs) is NOT and
  should not be the CLI's job — user's setup script does it against the
  published Ogmios/Kupo endpoints. Correct boundary.

Implication: fresh-build is a `yacd destroy` (+ optional ns lifecycle) and one
GitHub Action away from a complete ephemeral CI loop — tiny compared to the
snapshot effort. Strongly reinforces fresh-build-first.

## 2026-05-29 — Launched test-harness design workflow
Per user request, launched a background Workflow (run wf_b8b8be33-2ec) to design
how to use the YACD operator as a local/CI Cardano test harness.
Structure: Design (3 divergent lenses: minimal-CLI+GitHub-Action /
single-spec+thin-runner / ecosystem-glue+template-repo) -> Challenge (adversarial
team of 3 per design: criteria-fit, technical-feasibility/assumption-correctness
[verifies repo + primary sources via web], adoption/friction) -> Synthesize (one
proposed design, must resolve all blocker/major findings, list rejected
alternatives) -> Report (markdown: proposal / why-how + criteria walkthrough /
alternatives considered / risks + next steps).
Hard criteria fed to agents: manual+CI, spec-over-tuning (minimal local<->CI
drift), UX-forward, k8s-centric (KinD/k3d). Scope limit: harness not framework.
Plan on completion: persist the report to .journal/030/ (likely
TEST_HARNESS_DESIGN.md), summarize proposal + key adversarial findings to user.

## 2026-05-29 — Workflow completed; report persisted
Workflow wf_b8b8be33-2ec done (14 agents, ~1.07M tokens, 9 critiques). Full
report written to .journal/030/TEST_HARNESS_DESIGN.md.

CHOSEN DESIGN: thin verbs over the EXISTING Environment spec — `yacd up`
(deploy w/ --wait defaulted, same code path), `yacd down` (new Delete+WaitGone
on kube port, idempotent on NotFound), `yacd connect` (port-forward published
ClusterIP svcs + write loopback endpoints file) — plus `topup --await` (poll
Kupo for the funded UTxO) and ONE first-party JS GitHub Action (yacd-env, post:
hook teardown, KinD + pinned helm chart + image-preload, access-mode default
in-cluster-job). Fresh-build-only lifecycle (no re-run idempotency). Snapshot
explicitly deferred as a possible later cache.

Adversarial team VERIFIED blockers (resolved in synthesis):
- ClusterIP svc.cluster.local endpoints unreachable from host => topup default
  URL fails. Fix: `yacd connect` rewrites to loopback; isLoopbackHost in
  topup_trust.go already exempts loopback from the trust gate (verified). CI
  in-cluster-job mode uses native DNS, no override.
- Chart is 0.0.0 / operator never released; OCI ref is
  oci://ghcr.io/meigma/yacd/chart. Pinned released chart-version is a REQUIRED
  action input + hard prerequisite (cut first release).
- Full KinD+localnet path has NEVER run in GH-hosted CI (ci.yml runs only
  check/test; root:test-e2e is wired but uninvoked). Fix: gating CI spike must
  prove bring-up-to-Ready + measure cold-start before shipping the action.
- connection.json carries only PrimaryNodeToNode (verified) — NOT ogmios/kupo/
  faucet. `yacd connect` sources endpoints from status (yacd info shape).
- topup returns on submission, not inclusion. Fix: topup --await polls Kupo.
- WaitGone must treat NotFound as success (unlike WaitReady) + surface stuck
  finalizers; needs IsNotFound sentinel on the port.

Rejected: spec-runner's new spec.test schema (extra surface, fail on
criteria-fit, racy annotation-idempotency); ecosystem-glue starter repo (6-8
tool template, maintenance/drift, adoption 2). Kept their good bits (in-cluster
Job mode; connection writer -> yacd connect; teardown ownerRef verification).

Make-or-break residual risk: CI runner ceiling (~2vCPU/7-8GB) vs ~10-12m
bring-up — UNVERIFIED until the gating spike runs. Also: chart release is a hard
blocker; topup --await depends on Kupo; clean ownerRef teardown unverified for
publisher RBAC + artifacts ConfigMap + PVCs.

Next: user review of the proposal before any implementation.
