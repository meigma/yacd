# yacd CLI — All-in-One Local Cluster Lifecycle (Design Proposal, first pass)

Status: **DRAFT for review** (session 049). Design only; no code written.
Author: session 049. Supersedes nothing; new capability.
Method: grounded in a code map of `cli/`, `charts/yacd/`, `.dev/`, and the release
tooling, plus k3d/Helm research, then an 8-agent adversarial refinement
(3 alternative command surfaces + 5 multi-axis critics). The adversarial
findings and how they reshaped the design are in §9.

---

## 1. User story & persona

> *"As a yacd user, I want to spin up a local Cardano network as fast as
> possible without having to worry about setup."*

Persona: a **Cardano app/dapp developer** who wants a working local devnet to
build against. They are **not** a Kubernetes operator. The only prerequisite we
may assume is **Docker**. After the network is up they want to *use* it — query
the tip, fund an address they own, run `cardano-cli`, point their app at
Ogmios/Kupo — and then tear it down cleanly.

The bar to beat is Yaci DevKit's `devnet`-style zero-config experience that
`DESIGN.md` already names as the target.

## 2. Goal & non-goals

**Goal.** One command takes a Docker-only machine to a running, *usable* local
Cardano devnet: provision a local k8s cluster → install the yacd operator →
create a default `CardanoNetwork` → wait until Ready → tell the user how to use
it. Idempotent, re-runnable, cleanly removable. Single runtime: **k3d** (decided
prior session — see `[[cli-local-runtime-k3d]]` memory).

**Non-goals (v1).**
- Multi-node local clusters (the operator rejects `pools.count != 1` for local).
- Public mainnet quickstart (DESIGN.md non-goal; already gated behind
  `--allow-mainnet` + a 300Gi floor).
- A local image registry / operator-image inner loop (that stays on the existing
  KinD + ctlptl + Tilt dev stack — see §10).
- Windows-native (the goreleaser matrix builds darwin+linux only; WSL2 is the
  Windows path — see §9.5).
- Replacing the KinD dev stack. The k3d path is the **end-user** path and runs
  in parallel, by design (KinD stays for controller testing fidelity).

## 3. The experience (recommended happy path)

The all-in-one verb is shown here as `yacd devnet` — **the name is an open
decision (§13)**. Everything else in the session is a deliberate recommendation.

```text
$ # Prereq the user already has: Docker running. Nothing else installed.
$ yacd devnet
==> Preparing local environment (first run — pulls images, ~2–4 min)
    Docker .................................. ok (24.0 GB free)
    Fetching k3d v5.8.3 (sha256 verified) ... ok
    Creating local cluster "k3d-yacd" ....... ok (k3s v1.33.x)
    Installing yacd operator (v0.6.0) ....... ready
==> Starting Cardano devnet "devnet" (node + Ogmios + Kupo + faucet)
    pulling cardano-node ............ ok
    waiting for first block ......... ready (31s)
==> devnet is ready.
    Ogmios          ws://127.0.0.1:1337
    Kupo            http://127.0.0.1:1442
    Funded wallet   addr_test1qz…   (100,000 ADA)   ← see §11
    Try:  yacd query tip devnet
          yacd topup devnet --address <addr> --lovelace 1000000000
          yacd run  devnet -- <your-app>      (env wired to the net)
          yacd devnet down                    (tear it all down)

$ yacd query tip devnet
{ "epoch": 0, "block": 4, "slot": 312, "syncProgress": "100.00" }

$ yacd run devnet -- my-dapp test
… app runs with YACD_OGMIOS_URL / YACD_KUPO_URL / YACD_FAUCET_URL set …

$ yacd devnet down
Deleting cluster "k3d-yacd" (chain data is ephemeral and will be discarded)… done
```

Warm runs (cluster already up) skip straight to the network step (~30s).

## 4. Mental model

Two layers, deliberately distinct:

- **Cluster** — a single, shared, long-lived, yacd-managed local k3d cluster
  (fixed context `k3d-yacd`). The "platform." Created once, reused.
- **Network** — a `CardanoNetwork` CR in its own namespace. One CR per network.
  Networks come and go; the cluster persists. **One cluster hosts many
  networks.** (Not cluster-per-network — that is wasteful and contradicts the
  operator's namespaced-CRD, shared-cluster design.)

The all-in-one verb manages *both* layers in one shot. The existing primitives
(`up`, `down`, `info`, …) manage *networks* within whatever cluster they target.

## 5. Command surface (recommended)

**Unchanged, byte-for-byte** (the CI / Chainsaw / `yacd-env` Action contract —
see §7): `up NAME -f FILE`, `down NAME`, `list`, `info NAME`, `topup NAME`,
`run NAME`, `connect NAME`, `exec NAME`. None gain cluster-provisioning
behavior.

**New, additive:**

| Command | Purpose |
|---|---|
| `yacd devnet [NAME] [-f FILE]` | The all-in-one. Idempotently: ensure cluster → ensure/upgrade operator → apply network (embedded **default local devnet** if no `-f`; `NAME` defaults to `devnet`) → wait Ready → print endpoints + funded wallet + next steps. The **only** verb that provisions a cluster. Always uses the managed kubeconfig. |
| `yacd devnet down [NAME]` | Inverse: delete the k3d cluster (and its operator, networks, managed-kubeconfig context) in one shot. Warns that ephemeral chain data is discarded; `--keep-data` to preserve a bind-mounted host dir. |
| `yacd devnet status [NAME]` | One view of the hidden layer: Docker reachable? k3d binary present + pinned version? cluster up? operator version + Ready? network endpoints? The single place a confused user (or support) sees what yacd put on the machine. |
| `yacd query tip NAME` | Convenience: print the chain tip with the **published** network magic (no hard-coded `--testnet-magic 42`). Closes a real gap (§9.3). |

Deferred (not v1): a full `yacd cluster create|list|...` noun group (gold-plating
for a single managed cluster — fold lifecycle into `devnet`/`devnet down`/`status`);
`--persist`; registry; multi-node.

## 6. Key design decisions (the four forks, resolved)

| Fork | Decision | Why |
|---|---|---|
| **A. Overload `up` vs separate verb** | **Separate verb** (`devnet`). `up`/`down` untouched. | CI/Chainsaw select the cluster via *ambient* `KUBECONFIG` (no `--context` flag); the CLI can't distinguish ambient-env from default at the flag layer, so "provision unless explicit flag" breaks CI. Relaxing `up`'s `ExactArgs(1)` + required `-f` breaks pinned tests (`up_test.go`). A separate verb makes provisioning explicit at the verb boundary, not inferred from kubeconfig heuristics. |
| **B. Helm SDK vs pre-render + SSA** | **Pre-render manifests at build time → embed → server-side apply** via the controller-runtime client the CLI already has. | The repo already vendors SSA and the CLI already does `ApplyCardanoNetwork` via SSA. Helm's `crds/` are **install-once** (never upgraded) — a trap for a long-lived cluster across CLI upgrades. SSA lets the CLI own CRD-first ordering, in-place CRD upgrades, and label-based prune, and avoids the ~15 MB Helm-SDK import. |
| **C. Managed kubeconfig: isolated vs merge into `~/.kube/config`** | **Isolated.** yacd writes its own kubeconfig under an XDG path and **never** touches `~/.kube/config`. | k3d does *not* do this by default — `cluster create` clobbers `~/.kube/config` and switches its current-context. Mechanism: set `KUBECONFIG=<yacd-managed file>` in the **child k3d process env** before every shell-out (k3d then writes to that one file), keeping `--kubeconfig-switch-context` true *within that file*. Test invariant: `~/.kube/config` mtime unchanged after `yacd devnet`. |
| **D. k3d shell-out vs Go-library import** | **Shell-out** to a pinned, checksum-verified k3d binary. | Importing `github.com/k3d-io/k3d/v5` pulls ~115 modules incl. the full Docker engine SDK + docker/cli + client-go v0.30.2 (skews against the operator's controller-runtime client-go), forces Go ≥1.24.4, and adds logrus. Shell-out keeps yacd's module tree clean; the cost (binary fetch + CLI output parsing) is contained behind a port (§8). |

## 7. The `up` contract (why it stays safe)

The hard constraint: `up NAME -f FILE` is consumed by the CI e2e harness
(`.dev/scripts/test-e2e.sh` exports `KUBECONFIG` to a temp file and runs
`yacd up phase4-smoke -n yacd-smoke -f …` with **no** `--context`/`--kubeconfig`
flag), by `test/chainsaw/manager-smoke`, and by the planned `yacd-env` GitHub
Action — **always against an existing cluster**, context selected by ambient
`KUBECONFIG`.

Resolution: **`up`/`down` are not modified at all** — same args, same flags, same
`kube.NewClient`-against-resolved-context behavior, same
`EnsureNamespace`→`ApplyCardanoNetwork`→`WaitReady` path. Provisioning lives only
in `devnet`, which CI never calls. So the contract is byte-for-byte preserved
with zero test changes. `devnet` reuses the *same* apply+wait code internally
(no forked CR lifecycle), so the CI path and the end-user path can't drift.

**Targeting precedence** (resolved by one function; explicit always wins):

```
explicit --kubeconfig / --context / YACD_KUBECONFIG / YACD_KUBE_CONTEXT / KUBECONFIG-env
  >  the yacd-managed kubeconfig (if it exists)
  >  ~/.kube/config current-context
```

- `devnet` **always** uses the managed kubeconfig (deterministic; can never
  touch a real cluster).
- `up`/`info`/`run`/… use the precedence above. CI/automation always set an
  explicit target → unchanged. A dapp dev with a managed cluster present → all
  verbs seamlessly target it. This is a **bounded, additive** change to
  bare-`up` default targeting (it only differs from today when a managed cluster
  exists *and* no explicit target is given — a branch that didn't exist before).
  It is flagged as an open decision (§13).
- Every mutating verb prints the resolved target (context + server) to stderr so
  neither persona is surprised about which cluster they hit.

## 8. Architecture & overlay on current code

What we **reuse** (no change): `kube.Client` port + `Adapter`
(`cli/internal/kube`) builds `rest.Config` via `clientcmd` from
`--kubeconfig`/`--context`/`KUBECONFIG`/default with a `--context` override — so
targeting a freshly-created cluster needs **zero** changes to the port, just a
different context fed in. `ApplyCardanoNetwork`/`EnsureNamespace` are SSA and
reentrant. `run`/`connect`/`exec` use client-go SPDY port-forward/exec against
the API server, so they need **no** k3d ingress/serverlb/`-p` mapping. The
`render.CardanoNetwork` + `devconfig.Load` path is reused verbatim for the
embedded default network.

What we **add** (new packages, behind ports to preserve the repo's
ports-and-adapters discipline):

1. **`ClusterProvisioner` port** (mirrors `kube.Client`), injected via a factory
   on `Options` like `KubeClientFactory`, with a generated mock. Methods:
   `EnsureCluster(ctx, spec) (kubeconfigPath, context, error)`, `DeleteCluster`,
   `ClusterStatus`, plus a separate **`BinaryResolver`** port for the
   pinned/checksum-verified k3d fetch. The k3d shell-out adapter lives behind
   this; provisioning logic stays unit-testable without Docker.
2. **`EnsureCluster` state machine** (not a bare list-first guard): list → if
   present, health-probe (node list + API readiness on the managed context) → if
   present-but-unhealthy, delete+recreate → if absent, create with
   `--wait --timeout`. Treat create as a transaction: on any error, delete the
   partial cluster before returning. Layer a `kubectl wait`-equivalent on
   workloads after k3d's `--wait` (which only gates API-server readiness).
3. **Operator install via SSA**: embed pre-rendered chart manifests + CRDs
   (rendered in CI via `helm template` at the CLI's release version → `embed.FS`).
   Apply CRDs first, wait Established, then controllers/RBAC/SA, with an
   owner-label prune set. CRDs upgrade in place (unlike Helm's `crds/`).
4. **Cluster file lock** (`flock` on `$XDG_STATE_HOME/yacd/cluster.lock`) held
   across any cluster-mutating op (create/delete/upgrade + operator install),
   because the shared cluster is raced by parallel invocations / worktrees.
   Read-only verbs need no lock.
5. **Binary fetcher**: download `k3d-<os>-<arch>` from the pinned release tag,
   verify against a SHA256 **embedded in the CLI at build time** (not a
   runtime-fetched `checksums.txt` — see §9.5), pin the download host, reject
   redirects, fail closed, `chmod +x`, store at
   `$XDG_DATA_HOME/yacd/bin/k3d-<ver>`. GC older pinned binaries on fetch.

`devnet` then orchestrates: lock → preflight (Docker, disk) → ensure binary →
`EnsureCluster` → ensure/upgrade operator → reuse the `up` apply+wait path →
print results.

## 9. Adversarial review — top findings → resolutions

The full critic output is archived with this session. The findings that reshaped
the design:

**9.1 `up`-overload is unsafe (HIGH, multiple critics).** → Resolved by §6-A /
§7 (separate verb; `up` untouched; precedence table; never infer provisioning
from ambient env).

**9.2 Funded wallet is missing — the user story's "fund an address" is NOT
zero-config today (HIGH, user-need).** `topup` requires a user-supplied
`addr_test…`; there is no wallet/keygen in the CLI and no wallet field on the
CRD. A fresh-machine user cannot produce an address without first doing
`cardano-cli` key work (which itself needs the node socket via `yacd exec`). The
faucet's `defaultSource: utxo1` is the faucet's *own* genesis source, not a
destination the user owns. → **This is the most important gap for the story** and
is bigger than the CLI: it needs the operator to bootstrap a pre-funded named
wallet (DESIGN.md already anticipates "funded test wallets" / "generated wallet
sets"). See §11 — flagged as the top product dependency.

**9.3 Tip-query hint must use the live published magic (MEDIUM).** Hard-coding
`--testnet-magic 42` breaks if the default magic changes, and `exec` (no shell)
can't expand `$YACD_NETWORK_MAGIC`. → Add `yacd query tip NAME` that reads the
magic from the published status, and print magic-correct hints.

**9.4 Version skew / upgrade contract is unspecified (HIGH, completeness).** A
user who runs `devnet`, upgrades the CLI, and runs it again hits a long-lived
cluster with an *older* operator; "install/upgrade if absent" doesn't say what
happens, and Helm's `crds/`-once means CRD schema changes wouldn't migrate. →
Define it: stamp operator version (label/ConfigMap); on `devnet`/`status`,
compare embedded vs in-cluster — absent → install; older same-minor → upgrade the
operator Deployment (never silently migrate CR data) + print what changed;
newer/major-mismatch → refuse with an actionable message. A dedicated
`devnet upgrade` (future) is the only path that performs CRD upgrades, gated by a
confirmation naming affected networks. Add a skew check to the apply path so an
old CLI fails loudly against a newer CRD.

**9.5 Supply chain, paths, platforms (MEDIUM).**
- *Checksum trust*: embed the expected per-os/arch SHA256 at build time; verify
  against the embedded hash, not a `checksums.txt` fetched from the same place as
  the binary. The fetched binary talks to the Docker socket — broad privilege.
- *Windows*: declare WSL2 the supported Windows path (no `windows` GOOS in
  goreleaser); use `os.UserConfigDir`/`UserCacheDir` semantics with XDG honored
  when set, and an OS-aware lock.
- *ARM-mac*: the cardano-testnet image **is** built native arm64 — assert this
  multi-arch invariant + a CI guard, and preflight-warn if a user-supplied
  `spec.node.image` lacks a matching-arch manifest (else Docker silently emulates
  a consensus node — slow/unstable).

**9.6 Idempotency across non-idempotent layers (MEDIUM).** Only the network
apply is idempotent today; `k3d cluster create` is fatal on a duplicate name and
needs partial-create recovery; operator install needs real upgrade-or-install. →
Covered by the `EnsureCluster` state machine (§8.2), the cluster lock (§8.4), and
SSA install (§8.3). Add a test that kills provisioning mid-create and asserts the
next run recovers.

**9.7 Disk pressure is a likely first-run failure, not an edge case (MEDIUM).**
k3s storage lives inside the node container; Docker Desktop's VM disk is a fixed
ceiling; large chain data → DiskPressure eviction that reads as a random failure
to a non-k8s user. → Preflight `docker info`/`system df` and warn on low disk;
map `Evicted`/`DiskPressure` on the network pods to an actionable message
("Docker Desktop VM is out of disk — increase the disk image size or run
`yacd devnet down` to reclaim space").

**9.8 First-run "feels fast" is over-promised (HIGH, completeness).** Phase-0's
~27s was on KinD with warm images; first run additionally pays binary fetch + k3s
image pull + several hundred MB of Cardano/operator image pulls. The default `up`
timeout is already 12 min. → Don't promise 2 min; set expectations explicitly
("first run pulls images, a few minutes; subsequent ~30s"). Stream image-pull /
pod-status sub-progress (watch events + the operator's published sync-status
conditions) so a slow pull doesn't read as a hang. Keep 12 min as the hard
ceiling but show elapsed/expected.

**9.9 Uninstall/cleanup absent (HIGH).** New on-disk state (fetched binaries,
managed kubeconfig, Docker volumes) with no removal path. → `devnet down --purge`
/ `yacd uninstall` removes the cluster, managed kubeconfig, and all fetched
binaries; GC stale binaries on fetch; document the full footprint + the one
command that removes it.

**9.10 `down` stays network-scoped (MEDIUM).** Keep `down` `ExactArgs(1)`,
network-only. Cluster destruction only via `devnet down` (with confirmation +
chain-data-loss warning). Never let bare `down` act cluster-wide.

## 10. Coexistence with the KinD/ctlptl/Tilt dev stack

The operator dev loop stays on KinD (`kind-yacd-dev`, ctlptl registry
`yacd-registry:5005`, state under `.run/yacd-dev`, worktree-locked) — chosen for
native-kubeadm fidelity in controller testing. The end-user path uses k3d
(`k3d-yacd`, isolated managed kubeconfig). The two are **independent and may run
concurrently**; names don't collide and kubeconfigs are separate. A maintainer
running both has two single-node Docker clusters competing for the Docker Desktop
VM disk — `devnet status` should surface disk headroom. If a k3d registry is ever
added (operator-image inner loop only), pin its host port off `5005`. **Long-term
direction: dual indefinitely, by design** (different audiences, different fidelity
needs) — not convergence.

## 11. Critical product dependency: a funded wallet on day zero

The user story is only *met* when a Docker-only user can **receive funds** and
build against them. Today that's blocked (§9.2). The clean fix is operator-side
and already anticipated by DESIGN.md: the default devnet bootstraps at least one
**pre-generated, pre-funded named wallet** (operator writes keys into a Secret at
genesis), and the CLI surfaces its address + a key-export path in `info` and the
`devnet` next-step hints. This is a CRD/operator feature, not just CLI plumbing,
so it is the **top cross-cutting dependency** for the lifecycle to deliver the
story. The CLI lifecycle can ship before it (the user can hand-roll keys via
`exec`), but the "without worrying about setup" promise isn't real until it
exists. Decision needed on sequencing (§13).

## 12. Phased plan (proposed)

- **Slice 0 — provisioning core (no operator/network yet).** `BinaryResolver`
  (pinned fetch + embedded-checksum verify + XDG), `ClusterProvisioner` port +
  k3d adapter + `EnsureCluster` state machine, cluster lock, Docker/disk
  preflight, isolated managed kubeconfig. Verb: `yacd devnet status` +
  `devnet down`. Proves cluster up/down on the managed kubeconfig in isolation.
- **Slice 1 — operator install (SSA).** CI render of the chart → `embed.FS`;
  CRD-first SSA + prune; version stamp + skew check.
- **Slice 2 — the all-in-one.** `yacd devnet`: chain Slice 0 + 1 + the reused
  `up` apply path + embedded default local Environment + result/hint output +
  progress streaming. `yacd query tip`.
- **Slice 3 — usability hardening.** DiskPressure/Evicted mapper, uninstall
  `--purge` + binary GC, docs/quickstart + first-run banner, WSL2 validation,
  ARM multi-arch CI guard.
- **Dependency (parallel/ahead): funded-wallet bootstrap** (§11) — operator/CRD
  work; sequence vs Slice 2 is an open decision.

## 13. Open decisions for you

1. **Verb name.** `devnet` (recommended — names the artifact, won't be confused
   with the operator "dev stack") vs `dev up`/`dev down` (collides with "dev
   stack") vs `quickstart` (reads one-shot, not daily-driver) vs `start`/`local`
   (collide with `up` / `mode: local`).
2. **Funded-wallet sequencing (§11).** Build the operator-side funded-wallet
   bootstrap *before* the all-in-one (so the story is fully met on day one), or
   ship the lifecycle first and add the wallet next?
3. **Targeting precedence for bare `up` (§7).** Adopt "managed kubeconfig >
   `~/.kube/config`" so all verbs seamlessly follow `devnet` (recommended;
   CI/automation unaffected because they set explicit targets) — *or* the
   conservative variant where only the `devnet` family uses the managed
   kubeconfig and other verbs require `--context k3d-yacd`?
4. **`cluster` nouns.** Confirm deferring an explicit `yacd cluster create|list`
   group (folding lifecycle into `devnet`/`devnet down`/`status`) for v1.
5. **Chain-data persistence.** Confirm ephemeral-by-default for v1, with
   `--persist` (host bind-mount) deferred — acceptable given the funded-wallet/
   "build against it" use case wants persistence eventually?
