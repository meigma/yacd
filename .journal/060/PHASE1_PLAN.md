# Phase 1 — External-Access API + Operator (Ogmios/Kupo `service` + `externalURL`)

> Approved P1 implementation plan (session 060). Durable copy of the plan-mode
> artifact (`~/.claude/plans/` is transient). Implementation is **gated on
> session 059 landing first** (per the user) — build on post-059 master.
> Companion design: `EXTERNAL_ACCESS_DESIGN.md` (P1 of 3).

## Context

The yacd CLI can only reach a network's Ogmios/Kupo over an ephemeral `kubectl`
port-forward set up and torn down per command — slow for `yacd devnet` (same
machine) and pointless when a shared cluster already fronts the services with a
real ingress. The approved design (`EXTERNAL_ACCESS_DESIGN.md`) fixes this in
three phases. **This plan is P1 only: the API + operator changes.** No CLI, no
devnet wiring (those are P2/P3).

P1 adds, on `spec.chainAPI.ogmios` and `spec.chainAPI.kupo`:
- a `service` block — `type` (`ClusterIP` default | `NodePort`) + optional pinned
  `nodePort` — so the operator can render a host-routable NodePort Service;
- an `externalURL` string — the operator-asserted externally-reachable URL —
  mirrored additively into `status.endpoints.{ogmios,kupo}.externalURL`.

The CRD field only *advertises* the URL; P2 (devnet k3d `--port` + NodePort) makes
localhost actually route, and P3 (CLI resolver) reads `externalURL` and falls back
to forwarding. P1 is additive, backward-compatible, and **independent of the
session-059 faucet removal** (it touches only ogmios/kupo spec, their Service
builders, the shared Service mutator, and the ogmios/kupo status/validation
branches) — but 059 edits the *same files*, so build P1 on post-059 master to
avoid rebase conflicts.

**Confirmed design decisions (user-approved this session):**
- `externalURL` validation is **lenient**: absolute URL + non-empty host, scheme
  ∈ {ws, wss, http, https} for both. The CLI liveness probe (P3) is the real gate.
- The NodePort-preservation fix lives in the **shared `MutateService`** (guarded
  on `desired.Type == NodePort`; verified no-op for the only other caller, db-sync,
  which is ClusterIP).
- Conditional validation is **Go → Degraded `UnsupportedSpec`** (matching every
  existing chainAPI check), plus simple CRD markers (Enum on `type`, Maximum on
  `nodePort`).
- `externalURL` is a **sibling** of `service` (it is an advertisement, meaningful
  even with ClusterIP behind a user-run ingress), mirrored as a peer of `url`.

## Approach (commit-sized steps)

### 1. API types + codegen — `api/v1alpha1/cardanonetwork_types.go`

Add near the `OgmiosSpec`/`KupoSpec` block (~L382):

```go
// ChainAPIServiceType selects the Kubernetes Service type for a chain API endpoint.
// +kubebuilder:validation:Enum=ClusterIP;NodePort
type ChainAPIServiceType string

const (
	ChainAPIServiceTypeClusterIP ChainAPIServiceType = "ClusterIP"
	ChainAPIServiceTypeNodePort  ChainAPIServiceType = "NodePort"
)

// ServiceExposureSpec configures how a chain API Service is exposed.
type ServiceExposureSpec struct {
	// type selects the Kubernetes Service type. NodePort exposes the endpoint
	// on a static port on every node, which the local dev stack maps to the host.
	// +kubebuilder:default=ClusterIP
	// +optional
	Type ChainAPIServiceType `json:"type,omitempty"`

	// nodePort pins the node port when type is NodePort. When 0/omitted,
	// Kubernetes auto-assigns and the controller preserves the assignment.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=32767
	// +optional
	NodePort int32 `json:"nodePort,omitempty"`
}
```

Add to **both** `OgmiosSpec` and `KupoSpec` (after `Resources`), wording adjusted
per component:

```go
	// service configures how the Ogmios Service is exposed.
	// +optional
	Service *ServiceExposureSpec `json:"service,omitempty"`

	// externalURL is the operator-asserted, externally reachable URL for this
	// endpoint, mirrored into status.endpoints.ogmios.externalURL. Must be an
	// absolute ws/wss/http/https URL with a host.
	// +optional
	ExternalURL string `json:"externalURL,omitempty"`
```

Add to `ServiceEndpointStatus` (~L645):

```go
	// externalURL is the operator-asserted externally reachable URL, mirrored
	// from spec when set.
	// +optional
	ExternalURL string `json:"externalURL,omitempty"`
```

Then `moon run root:generate` (regenerates `api/v1alpha1/zz_generated.deepcopy.go`
+ `charts/yacd/crds/yacd.meigma.io_cardanonetworks.yaml`, embedded via
`charts/embed.go`). **Commit the generated files in this same commit** so the
`check.sh` drift guard (`git diff --exit-code -- api charts/yacd/crds`) passes.

The `Minimum=0`/`Maximum=32767` markers bound the field; the conditional "30000
floor only when NodePort" is enforced in Go (step 4) because a plain `Minimum=30000`
would reject the legal 0=auto-assign value.

### 2. Settings + resolvers — `internal/controller/cardanonetwork/settings.go`

Extend `ogmiosSettings` and `kupoSettings` with `serviceType corev1.ServiceType`,
`nodePort int32`, `externalURL string`. In `resolveOgmiosSettings` /
`resolveKupoSettings`: default `serviceType = corev1.ServiceTypeClusterIP`;
**before** the `!spec.Enabled` early return (L79/L121), reject a meaningless combo:
if disabled and (`spec.Service` requests NodePort or `spec.ExternalURL != ""`) →
`unsupportedSpec("ogmios service/externalURL set but ogmios is disabled")`. When
enabled and `spec.Service != nil`, map `Type` (`"NodePort"` → NodePort, else
ClusterIP) and copy `NodePort`; set `externalURL = strings.TrimSpace(spec.ExternalURL)`.

### 3. Service builders — `internal/controller/cardanonetwork/resources.go`

In `ogmiosService` (~L302) and `kupoService` (~L330): set `Type: settings.serviceType`
(replacing the hardcoded ClusterIP). On the single `ServicePort`, set
`NodePort: settings.nodePort` **only when** `serviceType == NodePort && nodePort != 0`;
otherwise leave it zero (so ClusterIP-desired still strips tampered NodePorts and
NodePort-with-auto lets k8s assign). Node-to-node/faucet/artifacts builders untouched.

### 4. Validation — `internal/controller/cardanonetwork/validate.go` + `builder.go`

Add Go validators returning `unsupportedSpec(...)`, wired into `chainAPISettings`
(`builder.go` ~L299, alongside `validateKupoImage`), for both ogmios and kupo:
- **nodePort/type coupling:** if `serviceType != NodePort && nodePort != 0` →
  `"<c> service nodePort is only valid when service.type is NodePort"`; if
  `serviceType == NodePort && nodePort != 0 && (nodePort < 30000 || nodePort > 32767)`
  → `"<c> service nodePort must be in the 30000-32767 range"` (defense-in-depth over
  the marker).
- **externalURL shape** (`net/url.Parse`, only when non-empty): not parseable, not
  `IsAbs()`, or empty `Host` → `"<c> externalURL must be an absolute URL with a host"`;
  scheme ∉ {ws,wss,http,https} → `"<c> externalURL scheme %q is not supported"`.
  (Lenient — same accepted set for both components.)

Do **not** add nodePort to `validatePrimaryWorkloadPorts` (that map is container/Service
*port* collisions; nodePort is a different number space the API server arbitrates).

### 5. NodePort preservation — `internal/ctrlkit/resources/resources.go` (RISKIEST)

`MutateService` currently does `current.Spec.Ports = desired.Spec.Ports`, wiping any
k8s-assigned NodePort each reconcile → thrash + churned host mapping. Fix it to honor
its own doc comment, name-keyed and guarded:

```go
current.Spec.Ports = mergeServicePorts(current.Spec.Ports, desired.Spec.Ports, desired.Spec.Type)
// ...
func mergeServicePorts(current, desired []corev1.ServicePort, desiredType corev1.ServiceType) []corev1.ServicePort {
	if desiredType != corev1.ServiceTypeNodePort {
		return desired // ClusterIP/etc: desired verbatim (clears tampered NodePorts)
	}
	assigned := map[string]int32{}
	for _, p := range current {
		if p.NodePort != 0 { assigned[p.Name] = p.NodePort }
	}
	merged := make([]corev1.ServicePort, len(desired)); copy(merged, desired)
	for i := range merged {
		if merged[i].NodePort == 0 {
			if np, ok := assigned[merged[i].Name]; ok { merged[i].NodePort = np }
		}
	}
	return merged
}
```

Match by `Name` (robust to reordering). Pinned desired NodePort wins (lets users
change the pin). Guard on `desired.Type == NodePort` keeps the existing
`...CorrectsPrimaryService/Ogmios/KupoServiceAndPreservesMetadata` tests
(`controller_test.go:1023/1083/1143`, desired=ClusterIP) green and is a verified
no-op for db-sync's ClusterIP services (`cardanodbsync/resources.go:380,447`).
Update the `MutateService` doc comment to state NodePort preservation is now real.

### 6. Status mirror — `internal/controller/cardanonetwork/status.go`

In `setEndpointStatus`, inside the existing `ogmiosService != nil` / `kupoService != nil`
branches (so it stays gated on the service being rendered/enabled), set
`...Ogmios.ExternalURL` / `...Kupo.ExternalURL` from the resolved settings'
trimmed `externalURL`, only when non-empty (keeps `omitempty`). Thread the two
strings into `setEndpointStatus` from the applied-status call site
(`patchPrimaryWorkloadAppliedStatus`) rather than re-reading raw spec, so the
"disabled → no endpoint" invariant is honored. The in-cluster `url` is unchanged
(additive). **Stale-on-degrade:** `setEndpointStatus` is skipped on the
Degraded/unsupported path (nodeService=nil), so a previously published `externalURL`
lingers — consistent with how `url` already behaves and safe because the P3 CLI
probes before trusting. Leave as-is; document the choice in the PR.

### 7. Tests

- **`internal/ctrlkit/resources/resources_test.go`** (the bug-catching guard — envtest
  can't reproduce the thrash since it has no NodePort allocator): add
  `TestMutateServicePreservesAssignedNodePort` (current NodePort 31234 + desired 0 →
  31234), `...AppliesPinnedNodePort` (desired 30500 wins), `...ClusterIPDropsNodePort`
  (desired ClusterIP → 0). Keep `TestMutateServicePreservesClusterIP` green.
- **`builder_test.go`** `TestPrimaryWorkloadBuilderRejectsUnsupportedInput` table rows:
  nodePort-without-NodePort (ogmios+kupo), externalURL bad-scheme, not-absolute,
  missing-host, disabled+externalURL-set.
- **`controller_test.go`** fake-client reconcile tests: renders NodePort service with a
  pinned nodePort; preserves an auto-assigned nodePort across a 2nd reconcile (update
  the service to simulate k8s assignment, reconcile, assert unchanged). Confirm the
  three existing correction tests still pass.
- **`controller_envtest_test.go`**: extend `ogmiosEndpointMatches`/`kupoEndpointMatches`
  (or add helpers) to assert `status.endpoints.{ogmios,kupo}.externalURL` mirroring with
  `wss://...`/`https://...`; add a CRD-level rejection case (`nodePort: 100` rejected by
  the embedded CRD's `Maximum`/marker) proving the markers reached the served CRD.

### 8. Regenerate + verify

`moon run root:generate` (already in step 1), then the verification recipe below.

## Critical files

- `api/v1alpha1/cardanonetwork_types.go` — new `ServiceExposureSpec`/`ChainAPIServiceType`,
  `Service`+`ExternalURL` on Ogmios/Kupo, `ExternalURL` on `ServiceEndpointStatus`.
- `internal/ctrlkit/resources/resources.go` — `MutateService` NodePort preservation
  (`mergeServicePorts`).
- `internal/controller/cardanonetwork/{settings.go,resources.go,validate.go,builder.go,status.go}`
  — resolvers, Service render, Go validation, status mirror.
- Generated: `api/v1alpha1/zz_generated.deepcopy.go`, `charts/yacd/crds/yacd.meigma.io_cardanonetworks.yaml`.
- Tests: `internal/ctrlkit/resources/resources_test.go`,
  `internal/controller/cardanonetwork/{builder_test.go,controller_test.go,controller_envtest_test.go}`.

## Reuse / conventions

- Enum + nested-pointer-subspec convention mirrors `CardanoNetworkMode` and
  `CardanoDBSyncPlacementSpec` (`*CardanoDBSyncPlacementSpec`, `+optional`+`omitempty`).
- Validation mirrors the existing `unsupportedSpec(...)` idiom in `validate.go` /
  `builder.go` (`validateKupoImage`, `"kupo requires ogmios to be enabled"`).
- `setEndpointStatus` scheme constants in `defaults.go` (`ogmiosServiceURLType="ws"`,
  `kupoServiceURLType="http"`) are untouched — they describe the in-cluster `url`.
- `primarypod.PortOwners` does not interact with Service type — no change.
- No network-identity fingerprint includes these fields — toggling them never trips
  `UnsupportedNetworkChange`. `values.schema.json` (operator chart values) is unrelated
  to the CRD — no change.

## Verification

1. `moon run root:generate` — regenerate; confirm `git status` shows only the intended
   deepcopy + CRD diffs; commit them with the API change.
2. `moon run root:check` — gofmt/lint clean, `controller-gen object` no-diff,
   `TestManagerRBACMatchesControllerGen`, and the `git diff --exit-code -- api charts/yacd/crds`
   drift guard all green; chart still lints/templates with the new CRD.
3. `moon run root:test` (sets `KUBEBUILDER_ASSETS`) — proves: the ctrlkit unit test
   catches the thrash fix (fails pre-fix); the fake-client reconcile test preserves an
   auto-assigned nodePort across reconciles; the envtest mirrors `externalURL` into status
   against a real API server with the embedded CRD; the CRD marker rejects an out-of-range
   nodePort; the three existing Service-correction tests still pass.
4. Live NodePort allocation + no-thrash is only observable in a real cluster — defer that
   to P2's k3d/Chainsaw path; P1's guarantee is the unit + envtest coverage above.

## Execution prep (when 059 has landed)

Per `.session.md`: create a fresh Worktrunk implementation worktree off the fetched
post-059 `master` (`wt switch --create --base master feat/chainapi-external-access`),
run `moon run root:dev-up` once, implement the steps above, and integrate via a single
squash-merged PR titled `feat(cardanonetwork): add NodePort/externalURL service exposure`.
This is P1 of 3 (`EXTERNAL_ACCESS_DESIGN.md`); P2 devnet + P3 CLI follow.
