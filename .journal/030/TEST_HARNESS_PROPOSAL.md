# YACD Test Harness — Design Proposal

Status: **proposal for review** (session 030). Not yet implemented.

This is the converged, human-authored design for using the YACD operator as a
local/CI Cardano test harness. It **refines** the workflow-generated analysis in
[`TEST_HARNESS_DESIGN.md`](./TEST_HARNESS_DESIGN.md) — that document holds the
candidate designs, the adversarial critiques, and the full alternatives record;
this document is the decided design and the points where we deviated from it.

## 1. Goal and scope

Provide thin, Kubernetes-native tooling to use the **existing** YACD operator as
a test harness for E2E (and similar) tests that need a *crafted* Cardano network
— specific funded addresses, deployed scripts, known UTxOs.

In scope: the CLI verbs and one GitHub Action needed to bring an environment up,
make it reachable, fund a known address, run an arbitrary test command against
it, and tear it down — driven by the **same spec locally and in CI**.

Explicitly **out of scope**: a test DSL, assertions, transaction construction, a
bespoke runner, or a snapshot/restore format. YACD provides the *environment*;
the developer's own test code is the *runner* and stays YACD-agnostic.

Hard constraints (unchanged from the brief):

1. Cover both manual local testing and CI.
2. Prefer specifications over manual tuning — local and CI differ as little as
   possible because both consume the same spec.
3. Developer UX first — if it is annoying, no one adopts it.
4. Stay k8s-centric; for local use assume the developer can run KinD or k3d.

## 2. Core model

Four ideas carry the whole design:

- **Spec is shape; identity is a CLI argument.** The spec describes *what* the
  network looks like. The network's *name* and *namespace* are runtime identity
  passed on the command line, not baked into the file. One spec file therefore
  deploys cleanly under many names/namespaces (parallel test shards, matrix
  runs, local vs CI) with zero edits. This is what makes "same spec everywhere"
  real.
- **Fresh-build only.** `up` creates, `down` destroys, re-run is `down` + `up`.
  No re-run idempotency is promised, which structurally removes the
  fixture-reconciliation / double-funding class of bugs. (Snapshot/restore is a
  possible later *cache* over this, not part of v1.)
- **The `YACD_*` environment-variable contract is the integration surface.**
  Tests read ordinary env vars (endpoints, network magic, faucet token); they
  never parse a YACD file format. This is the scope discipline that keeps the
  test runner arbitrary.
- **One port-forward engine, three ergonomics.** `run`, `connect`, and `exec`
  share the same access machinery and differ only in lifetime and reach (see §6).

## 3. The spec

The contract is the **existing** developer `Environment` document
(`cli/internal/devconfig`), with one **proposed breaking change**: drop the
`metadata` block. Identity (`name`, `namespace`) moves to the CLI. The
`apiVersion`/`kind` envelope stays for versioning. Because nothing is released
yet (the chart is still `0.0.0`), this change is free to make now.

```yaml
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:                          # == CardanoNetworkSpec
    mode: local
    node: { version: "11.0.1", port: 3001, storage: { size: 2Gi } }
    chainAPI:
      kupo:   { enabled: true, image: cardanosolutions/kupo:v2.11.0, port: 1442 }
      faucet:
        enabled: true
        port: 8080
        defaultSource: utxo1
        minTopUpLovelace: 1000000
        maxTopUpLovelace: 10000000000
    local:
      networkMagic: 42
      era: conway                   # start directly in Conway; no era traversal
      timing: { slotLength: 100ms, epochLength: 500 }   # compressed epochs
      topology: { pools: { count: 1 } }
      genesis: { profile: zero-fee-and-min-utxo }        # remove fee/min-UTxO friction
# Omit Ogmios when the test only needs Kupo + faucet, to fit the CI runner ceiling.
```

The only thing that varies between environments is the kubeconfig/context and
the `NAME`/`-n` arguments — never the spec content.

## 4. CLI vocabulary

A coherent, docker/kubectl-flavored verb set. Every verb is keyed on
`NAME [-n|--namespace <ns>]`. "Current" = ships today; "proposed" = new.

| Verb | Signature | Behavior | Status |
|---|---|---|---|
| `up` | `yacd up NAME [-n ns] -f spec.yaml [--timeout 12m]` | Render the spec under `NAME`/`ns`, auto-create the namespace, server-side-apply the `CardanoNetwork`, block on `Ready`. Implemented as the existing `deploy` code path with `--wait` defaulted on (one code path, not a copy). | proposed |
| `down` | `yacd down NAME [-n ns]` | Delete the `CardanoNetwork`, wait for finalizer-driven teardown (`WaitGone`). Idempotent: `NotFound` = success. | proposed |
| `list` | `yacd list [-A \| -n ns] [--json]` | List `CardanoNetwork` instances via the CRD; project name/namespace/mode/ready/endpoints. | proposed |
| `info` | `yacd info NAME [-n ns] [--json]` | Status + published endpoints + conditions. | current |
| `connect` | `yacd connect NAME [-n ns]` | Foreground, supervised port-forwards to the chain-API services; writes loopback endpoint URLs to `.yacd/<network>/endpoints.json`; runs until Ctrl-C. (See §6.) | proposed |
| `run` | `yacd run NAME [-n ns] -- <cmd...>` | Establish scoped port-forwards, inject the `YACD_*` env, exec the command on the host, tear the forwards down when it exits. Primary test/CI path. | proposed |
| `exec` | `yacd exec NAME [-n ns] -- <cmd...>` | Exec the command **inside** the primary node Pod (kubectl-exec semantics) with in-pod env incl. the node socket — the path for socket-bound tools like `cardano-cli`. | proposed |
| `topup` | `yacd topup NAME [-n ns] --address ADDR --lovelace N [--await]` | Fund an arbitrary address via the faucet. `--await` polls Kupo for the resulting UTxO before returning (removes the submit-vs-confirm race). | current + `--await` proposed |
| `env` | `yacd env NAME [-n ns]` | Print the `YACD_*` block for `eval`/`export` from active `.yacd` state. | proposed (optional) |

Deprioritized: `connect --detach` and a paired `disconnect` (background-managed
forwards). It is a large step up in complexity (process supervision, stale-PID
cleanup, restart-on-death) for a local-only convenience; foreground `connect` is
the v1 core. See §6.

## 5. Identity and namespaces

- **Name-as-identity.** The positional `NAME` becomes the rendered
  `CardanoNetwork` name. `--namespace` is optional and **defaults to `NAME`**, so
  each network gets its own isolated namespace by default — a good fit for the
  fresh-build, one-network-per-test model.
- **DNS-1123.** `NAME` and `ns` must be valid Kubernetes names (lowercase
  alphanumeric + `-`). Invalid input (e.g. underscores) is rejected with a clear
  error rather than silently mangled, because child resource names derive from
  it.
- **Namespaces are auto-created** on `up`, and **stamped** with an ownership
  label (`app.kubernetes.io/managed-by: yacd` + a created-by-yacd marker) so a
  later teardown can tell what YACD created.
- **No namespace auto-*delete* in v1.** Deleting a namespace YACD did not create
  could destroy unrelated resources; this was a flagged risk. `down` deletes the
  `CardanoNetwork` and waits for its children to be garbage-collected; namespace
  cleanup is left to the cluster teardown (CI) or the developer (local). A
  guarded `--delete-namespace` that honors the ownership stamp may be added
  later.

## 6. Host access — `connect`, `run`, `exec`

All three solve the same underlying problem: the operator publishes
**cluster-internal** `*.svc.cluster.local` ClusterIP endpoints, which are not
reachable from a laptop or CI runner host. They differ in lifetime and reach.

### Shared engine

`connect` and `run` use client-go's port-forward (`k8s.io/client-go/tools/
portforward`), not a shelled-out `kubectl`. They forward the **chain-API
services** — Ogmios (WS), Kupo (HTTP), faucet (HTTP) — whichever are enabled.
Those services select the **primary node Pod**, so the forwards target one pod
on several container ports. Node-to-node TCP is *not* forwarded (a host test does
not speak that protocol). Local ports are chosen randomly by default (so
parallel networks do not collide); `.yacd/<network>/endpoints.json` is the source
of truth for which port was assigned. The written URLs are **loopback**
(`127.0.0.1:…`), which is why `yacd topup` works against them with no
`--trust-faucet-url` flag (the verified `isLoopbackHost` trust-gate exemption in
`topup_trust.go`).

### `connect` — persistent, foreground (v1)

For interactive local development: hold the network reachable across many ad-hoc
host processes (curl, a dApp dev server, repeated IDE test runs, a REPL).

```
$ yacd connect my-network
  Forwarding my-network (namespace my-network):
    YACD_OGMIOS_URL=ws://127.0.0.1:34521
    YACD_KUPO_URL=http://127.0.0.1:34522
    YACD_FAUCET_URL=http://127.0.0.1:34523
  Wrote .yacd/my-network/endpoints.json — ^C to disconnect
```

Foreground is the robust core: because it is a live process it can **supervise**
its forwards — detect a dropped connection (pod restart, idle timeout) and
re-establish — which a fire-and-forget background process cannot do without a
supervisor. You run it in one terminal and work in another. CI essentially never
uses `connect`.

`.yacd/` mirrors the repo's existing `.run/yacd-dev/` runtime-state pattern:
gitignored, per-network subdir, holds `endpoints.json`.

### `run` — ephemeral, scoped (primary test/CI path)

```
$ yacd run my-network -- go test ./e2e/...
```

`run` is literally *establish forwards → set `YACD_*` env → exec child → close
forwards on exit*. The forwards live exactly as long as the command, so there is
no daemon, no PID file, and nothing to leak. This is the workhorse for both
local test runs and CI, and it keeps the test runner entirely YACD-agnostic — it
just reads env vars.

`run` with no `-- cmd` drops into an interactive `$SHELL` with the env set (the
`nix develop` move).

### `exec` — in-pod, for socket-bound tools

`cardano-cli` talks to the node over a **Unix domain socket** (`--socket-path`,
local IPC), not TCP — so a port-forward cannot expose it, and `yacd run -- cardano-cli query tip`
would fail for any socket-backed subcommand. `exec` runs the command **inside**
the primary node Pod, where the socket is local, and sets `CARDANO_NODE_SOCKET_PATH`
+ `YACD_NETWORK_MAGIC` so:

```
$ yacd exec my-network -- cardano-cli query tip \
    --socket-path "$CARDANO_NODE_SOCKET_PATH" --testnet-magic "$YACD_NETWORK_MAGIC"
```

works. Rule of thumb: **`run` for Ogmios/Kupo/faucet tooling over TCP; `exec`
for `cardano-cli` and anything that needs the node socket.**

### The `YACD_*` environment-variable contract

This is the stable integration surface; design it deliberately and version it.

| Variable | Example (host / `run`) | Notes |
|---|---|---|
| `YACD_NETWORK` | `my-network` | |
| `YACD_NAMESPACE` | `my-network` | |
| `YACD_NETWORK_MAGIC` | `42` | |
| `YACD_OGMIOS_URL` | `ws://127.0.0.1:34521` | present if Ogmios enabled |
| `YACD_KUPO_URL` | `http://127.0.0.1:34522` | present if Kupo enabled |
| `YACD_FAUCET_URL` | `http://127.0.0.1:34523` | present if faucet enabled |
| `YACD_FAUCET_TOKEN` | `…` | from the faucet auth Secret; an ephemeral localnet token (low-risk to inject) |
| `CARDANO_NODE_SOCKET_PATH` | *(exec only)* | in-pod node socket path |

Under `exec` (in-pod), the URL values are the ClusterIP DNS forms instead of
loopback. The variable **names are stable**; only the values adapt to host vs
in-pod — which is precisely what makes the test transparent to where it runs.

## 7. CI — the `yacd-env` GitHub Action

One first-party reusable action wraps the same verbs against the same spec.

```yaml
- uses: meigma/yacd/.github/actions/yacd-env@vX     # PROPOSED
  with:
    spec: env.yaml            # the IDENTICAL file used locally
    name: dapp-e2e
    chart-version: <pinned>   # REQUIRED — a published operator/chart release
    wait-timeout: 12m
    test: go test ./e2e/...   # run via `yacd run` (scoped forwards + env)
    dump-on-failure: true
```

- Implemented as a **JavaScript action** with a `post:` hook, because JS post
  hooks run on cancellation (composite `if: always()` does not) — guaranteeing
  teardown even when the job is cancelled.
- `main`: provision KinD on the runner → `helm install` the operator chart at
  the **pinned released** version from `oci://ghcr.io/meigma/yacd/chart` with an
  explicit image-preload (`kind load`) for the manager, faucet, and
  cardano-testnet images (bounds cold-pull time, pins provenance) → `yacd up`
  the spec → `yacd run NAME -- <test>`.
- `post`: `yacd down` then delete the KinD cluster, on all exits.
- On failure, `dump-on-failure` collects `kubectl describe`/events/logs for the
  network's **child** pods (node/Kupo/faucet/init), not just the operator, as
  uploaded artifacts.

`yacd run` is the CI execution path (host process + scoped forwards). An
in-cluster-Job mode (test packaged as a Pod, native ClusterIP DNS, no forward)
remains a documented option for tests that cannot run on the runner host.

## 8. How it meets the criteria

1. **Manual + CI.** The same `up`/`run`/`topup`/`down` verbs and the same
   `env.yaml` run in both; CI only adds cluster creation + chart install.
2. **Spec over tuning.** Exactly one spec format, no CI-only override layer.
   Identity moved to CLI args (the thing that legitimately varies). Host access
   is solved by the `YACD_*` contract, whose values adapt while names stay
   fixed — so the test is identical across host and in-pod.
3. **UX.** A small, intuitive, docker/kubectl-flavored verb set; the test runner
   stays YACD-agnostic (reads env vars); child-pod diagnostics on failure address
   the k8s-debugging cliff.
4. **k8s-centric.** CRD via SSA, status-condition readiness/teardown gating,
   Helm-installed operator, KinD/k3d locally and KinD in CI. No non-k8s control
   plane.

## 9. What changed from the workflow report, and what was deferred/rejected

Refinements adopted on top of [`TEST_HARNESS_DESIGN.md`](./TEST_HARNESS_DESIGN.md):

- **Identity out of the spec** + `yacd list` + auto-created, ownership-stamped
  namespaces (default ns = name).
- **`run` vs `exec` split**, with the explicit `cardano-cli`/socket caveat (the
  report's `run` example would have failed for socket-backed subcommands).
- **`connect` defined precisely** as foreground + supervised; `--detach`
  deprioritized.
- **The `YACD_*` env-var contract** promoted to a first-class, versioned surface
  (the report's "endpoints file" generalized).

Deferred / rejected (full reasoning in the report):

- **`connect --detach` / background-managed forwards** — large complexity step
  (process supervision, stale state) for a local-only convenience; deferred.
- **Namespace auto-delete in v1** — destructive without ownership tracking;
  deferred behind the stamp.
- **Snapshot/restore format** — hard slot/time re-anchoring + node-version
  pinning; a possible later cache over fresh-build, not v1.
- **A new `spec.test` schema / `yacd test` runner** — extra config language;
  fresh-build + the env contract make it unnecessary.
- **A starter/template repo wiring 6–8 tools** — high maintenance/drift surface;
  conflicts with the repo's keep-the-surface-small rule.

## 10. Risks and open questions

- **CI runner ceiling vs bring-up time (make-or-break, UNVERIFIED).** The full
  KinD + localnet-to-Ready path has never run in GitHub-hosted CI. The repo's
  e2e budgets ~10–12m; the standard hosted runner is ~2 vCPU / 7–8 GB on the
  private tier. The 12m default and image-preload are mitigations, but a real
  cold-start number is needed before the action ships. **Gating spike required.**
- **Operator/chart is unreleased** (`0.0.0`, only `cardano-testnet/*` tags). A
  pinned-chart install cannot work until the first operator/chart release is
  cut. Hard prerequisite for the whole CI story.
- **`topup --await` depends on Kupo.** If a spec disables Kupo to save runner
  resources, `--await` needs an alternative confirmation source (Ogmios
  chainsync / node query) or must document that it requires Kupo.
- **Clean ownerRef teardown is unverified** for the artifact-publisher RBAC, the
  network-artifacts ConfigMap, and PVCs. The gating spike must assert all
  children are GC'd after `CardanoNetwork` delete; missing ownerRefs may need an
  operator finalizer.
- **Faucet source funding capacity / determinism** rests on the faucet
  `defaultSource` having enough genesis funds and on source-collision avoidance;
  document per-genesis-profile funded amounts.
- **`exec` reach.** `exec` assumes the chain-API containers (incl. the node
  socket) live in the primary node Pod; confirm against the rendered Pod before
  building it.
- **k3d** is supported by the cluster-agnostic CLI verbs but not exercised by the
  v1 action (KinD only).

## 11. Recommended next steps (smallest-first)

1. Add `Delete`, `WaitGone`, `List`, and an `IsNotFound` sentinel to the
   `kube.Client` port (`cli/internal/kube/client.go`); regenerate the mock via
   `moon run root:generate`.
2. Implement `yacd down` (`WaitGone`, idempotent on `NotFound`, surfaces stuck
   finalizers) and `yacd list`.
3. Implement `yacd up` (= `deploy` with `--wait` defaulted; name/namespace from
   args; auto-create + stamp the namespace) and `topup --await` (poll Kupo).
4. Define and document the `YACD_*` env-var contract.
5. Implement the shared port-forward engine; then `yacd run` (scoped + env),
   then `yacd connect` (foreground + `.yacd/` file), then `yacd exec` (in-pod +
   socket env). Optional `yacd env`.
6. Add a GitHub-hosted **gating CI job** running `moon run root:test-e2e` (KinD +
   localnet to Ready): capture cold-start timing, add a post-delete teardown
   assertion verifying all children are garbage-collected.
7. Cut the first operator + Helm chart release (resolve the `0.0.0` placeholder).
8. Build the `yacd-env` JavaScript GitHub Action; validate it in this repo's CI
   before documenting it for external consumers.
9. Ship `examples/e2e/` (spec, checked-in key, derive-address snippet) and a
   Diátaxis how-to that states fresh-build-only and documents `run`/`exec`.
