# External Access URLs for Chain APIs — Design

Status: draft for review (session 060, 2026-06-03)
Author: agent + jmgilman

## 1. Problem

The yacd CLI can only reach a network's Ogmios/Kupo over an **ephemeral
`kubectl` port-forward** that it sets up and tears down *per command*. Every
`yacd topup`, every `yacd run` that needs chain access, pays the full cost of
resolving the primary Pod, dialing an SPDY port-forward, and waiting for it to
become ready — even when the cluster is sitting on the same machine (`yacd
devnet`) or already fronted by a real ingress (a shared cluster a platform team
stood up).

Port-forwarding is the most *versatile* transport — it assumes nothing about how
the cluster is reachable — so it must remain the fallback. But it should not be
the *only* path. When a faster, already-reachable URL exists, the CLI should use
it.

## 2. Current state (as of `5383f76`)

Grounding facts from the code, so the design is concrete:

- **Status shape.** `status.endpoints.{ogmios,kupo,faucet,nodeToNode,artifacts}`
  are each a `ServiceEndpointStatus{serviceName, port, url}`
  (`api/v1alpha1/cardanonetwork_types.go`). Exactly one URL per endpoint, always
  the in-cluster form: `ws://<svc>.<ns>.svc.cluster.local:1337` (ogmios),
  `http://<svc>.<ns>.svc.cluster.local:1442` (kupo). Built in
  `internal/controller/cardanonetwork/status.go:setEndpointStatus`; URL schemes
  are package constants in `defaults.go`.
- **Services are all ClusterIP** (`resources.go`), selecting the primary Pod,
  with named ports (`primarypod.PortName{Ogmios,Kupo,...}`). Nothing is
  host-reachable.
- **The CLI is hardwired to forward.** `cli/internal/cli/forward.go`
  (`connectNetwork`/`forwardEndpoints`/`forwardSpecs`) resolves the primary Pod
  (`kube.Client.PrimaryPodName`) and opens forwards (`kube.Client.Forward`,
  SPDY, kernel-assigned local ports). `envcontract.go:loopbackURL` then takes the
  published in-cluster URL and rewrites only host:port → `127.0.0.1:<localPort>`,
  preserving scheme. There is **no** code path that uses a URL directly.
- **`topup`** (`topup.go:resolveFaucetTransport`) forwards per-invocation unless
  `--faucet-url` is given. Flags `--faucet-url`, `--kupo-url` exist; env
  `YACD_OGMIOS_URL`/`YACD_KUPO_URL` are already defined (the `YACD_*` contract
  `yacd run` injects — `envcontract.go`).
- **devnet's k3d cluster has no host port mappings.**
  `cli/internal/cluster/k3d/ensure.go:create` runs `k3d cluster create <name>
  --image … --wait --timeout … --kubeconfig-update-default
  --kubeconfig-switch-context` — no `--port`, no loadbalancer config. k3d is
  pinned to **v5.9.0** (`toolbin/ghrelease/pin.go`).
- **The checked-in config is the Environment document** (`yacd.yaml`):
  `apiVersion: yacd.meigma.io/devconfig/v1alpha1`, `kind: Environment`,
  `spec.network` = a `CardanoNetworkSpec` (`cli/internal/devconfig/config.go`).
  `EnvironmentSpec` is deliberately a thin envelope. It is consumed by `up -f`
  and by `devnet` (embedded default); `topup`/`run`/`connect`/`info` do **not**
  read it.

## 3. Goals / non-goals

**Goals**

- Let the CLI reach Ogmios/Kupo over a directly-reachable URL when one exists,
  falling back to port-forwarding otherwise.
- Make `yacd devnet` fast by default — no background job, no `yacd connect`
  babysitting — by giving its Ogmios/Kupo stable `localhost` URLs that actually
  route.
- Let a platform team that fronts a shared cluster's Ogmios/Kupo with a real
  ingress declare those URLs once, so their developers' CLIs use them
  automatically.

**Non-goals**

- The operator does **not** create Ingress objects. It only advertises a URL
  someone else asserts. (Bringing-your-own ingress controller, host/path routing,
  and Ogmios-websocket ingress quirks are out of scope.)
- **Faucet** external access is out of scope here — see §8. Scope is Ogmios and
  Kupo.
- `yacd connect` stays a forwarding tool for remote clusters; it is not
  reworked.

## 4. Design overview

A single new concept: **an asserted external-access URL on the CardanoNetwork**,
set by whoever provisions the network, read by the CLI.

The field is an *assertion of reachability*, not a derived fact. `localhost` is a
valid assertion for a co-located single-machine cluster (devnet); an ingress
DNS/LB URL is a valid assertion for a shared cluster. It is optional and empty by
default, so it is never wrong unless actively mis-asserted — and the CLI probes
it before trusting it (§4.3), so even a stale/mismatched value degrades safely to
forwarding.

Three components:

### 4.1 API + operator

On `spec.chainAPI.ogmios` and `spec.chainAPI.kupo`, add:

- `service`:
  - `type`: `ClusterIP` (default) | `NodePort`. Controls the rendered Service
    type. NodePort is what makes the service reachable from outside the cluster
    network (required for the devnet localhost path).
  - `nodePort` (optional, int32): pin the NodePort. Validated to the Kubernetes
    NodePort range **30000–32767** when `type: NodePort`; rejected otherwise.
- `externalURL` (optional, string): the asserted externally-reachable URL.
  Validated as an absolute URL with a host; scheme expectations match the service
  (`ws`/`wss` for Ogmios, `http`/`https` for Kupo).

Controller changes (`internal/controller/cardanonetwork`):

- `resources.go`: render `Service.Type` from `spec…service.type`; set
  `Service.Spec.Ports[0].NodePort` when pinned. Default path unchanged
  (ClusterIP).
- `status.go:setEndpointStatus`: **mirror** `spec…externalURL` into a new
  `status.endpoints.{ogmios,kupo}.externalURL` field, *additive* to the existing
  in-cluster `url`. (`ServiceEndpointStatus` gains an `externalURL string`.)
- Validation lives with the existing spec validation; envtest covers
  type/nodePort rendering, range rejection, externalURL mirroring, and the
  ClusterIP default.
- `moon run root:generate` to regen CRDs/deepcopy.

Why mirror to status rather than have the CLI read spec: status is the
conventional "how to reach me" surface, keeps the CLI reading one place
(`status.endpoints`), and lets the controller normalize. We are already in the
controller for the NodePort toggle, so the mirror is cheap.

### 4.2 devnet

devnet owns both the cluster *and* the default network spec, so it can pin
constants on both sides — no discovery, no experimental `k3d cluster edit`.

- **Pin** host ports and NodePorts as constants, e.g.
  - Ogmios: host `1337` ↔ nodePort `30137`
  - Kupo:   host `1442` ↔ nodePort `30442`
- **Create k3d with host port mappings** (`cluster/k3d/ensure.go:create`):
  `--port "1337:30137@loadbalancer" --port "1442:30442@loadbalancer"`. This is
  the stable, non-experimental create-time `--port` flag (format
  `[HOST:][HOSTPORT:]CONTAINERPORT[/PROTOCOL][@NODEFILTER]`). The serverlb
  proxies a host port to the *same port number on the server nodes*, which is why
  the target must be a NodePort.
- **Author the default network spec** (the embedded `devnet.yaml` / devnet's
  generated spec) with `chainAPI.{ogmios,kupo}.service.type: NodePort`, the
  pinned `nodePort`s, and `externalURL: ws://localhost:1337` /
  `http://localhost:1442`.

The CRD field only *advertises* the localhost URL; the k3d `--port` + NodePort
are what make it *route*. Both are required.

### 4.3 CLI resolution

Introduce a shared resolver in the forward path so every consumer benefits, not
just one command. For each of Ogmios/Kupo, resolve in precedence order:

1. explicit flag (`--ogmios-url` / `--kupo-url`)
2. ambient env (`YACD_OGMIOS_URL` / `YACD_KUPO_URL`, inside `yacd run`)
3. `status.endpoints.{ogmios,kupo}.externalURL` — **probe it** (short connect
   timeout); if reachable, use it
4. ephemeral port-forward (today's behavior — the fallback)

The probe is what makes trusting an unvalidated asserted URL safe: a stale
`localhost` carried to the wrong machine, or a typo'd ingress, simply fails the
probe and falls back to forwarding. Cache the verdict for the life of the
command/session so a multi-step flow does not re-probe.

Wiring: the resolution belongs in the shared `forwardEndpoints`/forward path so
`run` (which injects `YACD_*`) and `topup`'s Kupo `--await` path both pick it up.
`connect` stays forward-only.

## 5. Resolution precedence (summary)

```
flag  >  YACD_* env  >  status.externalURL (probed)  >  port-forward
```

devnet sets `externalURL`, so on a devnet the CLI hits `localhost:<pinned>`
directly with no forward. A shared cluster with a platform-asserted ingress URL
behaves identically. Anything without `externalURL` (or whose `externalURL`
fails the probe) keeps working exactly as today.

## 6. What this design rejects (and why)

- **A separate Viper CLI config file** (earlier proposal). Rejected: the
  CardanoNetwork field is a single source of truth that serves devnet *and*
  remote, read by the CLI in one place. No new config-file discovery /
  precedence / keying surface.
- **A long-lived reused port-forward / `connect`-then-`topup`.** Rejected by the
  user: requiring a background forward before `topup` is exactly the friction we
  are removing. `connect` is for remote clusters; on a local machine it is a
  workaround we are replacing.
- **Operator-created Ingress / k3s-bundled Traefik routing.** Deferred: requires
  an ingress-controller assumption and host/path routing, and Ogmios is a
  websocket — too much surface for v1. NodePort + k3d `--port` is the clean
  local path.
- **`k3d cluster edit --port-add` on a running cluster.** Avoided: it is marked
  experimental (it rebuilds the serverlb in place). Pinning ports at *create*
  time sidesteps it entirely.

## 7. Phased plan

Each phase is an independently reviewable, squash-mergeable PR, matching the
repo's slice-per-PR convention.

- **P1 — API + operator.** Add `service.{type,nodePort}` + `externalURL` to
  ogmios/kupo spec; `ServiceEndpointStatus.externalURL`; render Service type +
  nodePort; mirror externalURL to status; validation (nodePort range, URL shape);
  envtest matrix; `root:generate`. No CLI change yet (status field is additive
  and ignored by the current CLI).
- **P2 — devnet plumbing.** Pin host/node ports; k3d `--port` mappings in
  `ensure.go`; author the devnet default spec with NodePort + nodePorts +
  localhost `externalURL`. Live-verify on k3d that `localhost:1337/1442` answer
  before any CLI change.
- **P3 — CLI resolution.** Shared resolver (flag → env → probed externalURL →
  forward) in the forward path; wire into `run` and `topup` (`--await` Kupo).
  Unit tests for precedence + probe/fallback; live smoke on a P2 devnet
  confirming no port-forward is established when `externalURL` answers.

Ordering note: P1 before P2 (devnet's spec needs the new fields) before P3 (the
resolver needs status to carry `externalURL`). P1 is safe to merge alone.

## 8. Open threads / risks

- **Faucet + `topup`.** `topup` currently posts to the *faucet* service (and uses
  Kupo only for `--await`). This design does not give the faucet an
  `externalURL`, partly because the faucet auth token has a dedicated non-loopback
  trust gate (`topup_trust.go`) that an advertised external URL would have to
  interact with, and partly because **session 059's plan removes the faucet
  entirely** in favor of the CLI building/submitting funding txns directly over
  Ogmios/Kupo (`.journal/059/WALLET_REARCH_PLAN.md`, paused for review). If that
  lands, `topup` becomes a direct Ogmios/Kupo consumer and inherits this
  mechanism for free. Until then, the faucet leg of `topup` still forwards.
  Decision needed: leave faucet forwarding as-is (recommended), or also advertise
  a faucet externalURL with trust-gate handling.
- **Identity-stripping tension.** The Environment doc is identity-stripped so one
  spec deploys under many names. A single `externalURL` (especially a real
  ingress host) collides if the same spec is deployed under multiple names on one
  cluster. Acceptable for the target cases (devnet singleton; one canonical
  shared deployment), but worth noting.
- **Probe cost on the sad path.** Trying `externalURL` then falling back adds one
  failed-dial latency. Mitigated by a tight connect timeout + per-run caching.
- **NodePort host-port collisions.** Pinned host ports (1337/1442) could clash
  with something already bound on the developer's machine. devnet should surface
  a clear error rather than a confusing k3d failure (P2 detail; relates to P7
  hardening).
```
