
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

## 2026-05-29 — User UX refinements to the proposal (discussion)
User proposed three refinements; my assessment (detail in chat):

1. Env management: `yacd list` (List on CRD via kube port — clear yes);
   name-as-identity `yacd up NAME [-n ns]` and DROP metadata from the devconfig
   spec (spec = shape, name/ns = runtime identity → strengthens
   spec-over-tuning, enables parallel shards; breaking change but chart is 0.0.0
   so free now). Cautions: (a) DNS-1123 — user's `my_network` is ILLEGAL
   (underscore); use `my-network`. (b) auto-CREATE ns is safe; auto-DELETE needs
   an ownership stamp (managed-by + created-by-yacd label on the namespace) —
   never delete a pre-existing ns (matches the workflow's MAJOR finding).
   (c) default ns = name is a good predictable default.

2. Managed port-forward via `.yacd/` (mirrors .run/yacd-dev/): right direction,
   but "managed" must mean SUPERVISED (forwards die on pod restart/idle/blip →
   flaky tests), not just spawn+pidfile. Use client-go portforward, not shelling
   kubectl. Detached daemon = stale-state complexity.

3. `yacd run NAME -- <cmd>` (env-injection, aws-vault/doppler/nix pattern): the
   STRONGEST idea. Makes the harness invisible to the test runner (env vars,
   zero YACD awareness = scope discipline). Solves port-forward lifecycle for
   free (forwards scoped to the child, no daemon/PID/stale state). Can REPLACE
   the workflow's in-cluster-Job CI default and beat it on friction AND parity
   (env var name stable, value adapts host-loopback vs ClusterIP DNS).
   => connect = persistent forwards (dev); run = ephemeral scoped forwards
   (CI/tests, primary).

SHARP CAVEAT: the `cardano-cli` example mostly WON'T work via run —
cardano-cli uses the node UNIX SOCKET (--socket-path, local IPC), not TCP;
port-forward can't expose it. Socket-backed subcommands (query tip/utxo/params,
cli submit) fail; offline (address/key build) work. Host chain interfaces are
Ogmios(WS)/Kupo(HTTP) = TCP, forwardable. => add sibling `yacd exec NAME -- ...`
(kubectl-exec into node pod) for the socket case. Split: run = host+TCP env;
exec = in-pod+socket.

New load-bearing surface: the YACD_-prefixed env-var contract (NETWORK,
NAMESPACE, NETWORK_MAGIC, OGMIOS_URL, KUPO_URL, FAUCET_URL, FAUCET_TOKEN) —
becomes the stable API the endpoints file was; design deliberately. run with no
`-- cmd` => interactive $SHELL with env set.

Emerging vocabulary: up/down/list/info/connect/disconnect/run/exec/topup, all
keyed on NAME [-n ns]. Make-or-break risk unchanged (CI runner ceiling — gating
spike still needed). Next: fold into TEST_HARNESS_DESIGN.md or keep iterating on
vocabulary (run-vs-connect defaults, exact env var names). Awaiting user.

## 2026-05-29 — Drafted converged design proposal
User agreed with refinements (esp. run/exec) and deprioritized connect --detach
(too complex for v1; foreground connect only). Wrote
.journal/030/TEST_HARNESS_PROPOSAL.md — the converged human-authored design that
REFINES the workflow report (TEST_HARNESS_DESIGN.md stays as the analysis/
alternatives record). Captures: spec-is-shape/identity-is-CLI-arg (drop metadata
from devconfig), fresh-build-only, YACD_* env-var contract as the integration
surface, one port-forward engine with three ergonomics (connect foreground+
supervised / run scoped+env / exec in-pod+socket), the cardano-cli socket
caveat, name-as-identity + auto-created ownership-stamped namespaces (no
auto-delete v1), verb table (up/down/list/info/connect/run/exec/topup --await/
env), yacd-env JS GitHub Action, criteria fit, deferred/rejected, risks
(CI runner ceiling UNVERIFIED, chart 0.0.0 unreleased, topup --await needs Kupo,
ownerRef teardown unverified, exec assumes chain-API in primary pod), and
smallest-first next steps. Status: proposal for review; no code yet.

## 2026-05-29 — Drafted phased implementation plan
Wrote .journal/030/TEST_HARNESS_PLAN.md (companion to the proposal). High-level
work breakdown, no code. Six phases, risk-first ordering:
- P0 Validate feasibility (de-risk: CI cold-start go/no-go, teardown GC, host-
  access assumptions — uses today's tooling, no new verbs).
- P1 CLI foundation: identity-as-CLI-arg (breaking spec change) + up/down/list.
- P2 Host access + env-var contract: run/connect/exec + topup --await => LOCAL
  STORY COMPLETE.
- P3 Release operator/chart (parallel; gates P4).
- P4 CI integration: yacd-env Action => CI STORY COMPLETE.
- P5 Adoption: examples + Diataxis how-to.
Dependency flow + criteria-coverage milestones (end P2 = local; end P4 = CI;
end P5 = adoption-ready) included. Out-of-plan backlog: detached connect,
guarded ns auto-delete, in-cluster exec mode, k3d in action, snapshot cache.
Three journal artifacts now: TEST_HARNESS_DESIGN.md (workflow analysis/
alternatives), TEST_HARNESS_PROPOSAL.md (decided design), TEST_HARNESS_PLAN.md
(phased work). Status: all proposals for review; no code yet.

## 2026-05-29 17:30 — Close
Session 030 closed. Design-only session: NO code, NO implementation PR (all
output is journal documentation on journal/jmgilman, which is never merged to
master). User approved closeout ("LGTM").

Artifacts delivered to .journal/030/: TEST_HARNESS_DESIGN.md (adversarial
workflow report + rejected alternatives), TEST_HARNESS_PROPOSAL.md (decided
design), TEST_HARNESS_PLAN.md (six-phase plan), SUMMARY.md (postmortem).
INDEX.md row 030 added (complete); TECH_NOTES.md gained a pointer to the design.

Handoff: design awaits implementation. Start at Plan Phase 0 (validate CI
cold-start feasibility + teardown GC, using today's tooling) before building the
CLI verbs/Action. Make-or-break unknown = KinD+localnet readiness within a
hosted-runner budget. Operator/chart still unreleased (0.0.0) — gates the CI
story. Dev stack not owned by this session (concurrent sessions manage it).
