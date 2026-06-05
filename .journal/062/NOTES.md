---
id: 062
title: External-access P2 (devnet plumbing) + P3 (CLI resolver)
started: 2026-06-05
---

## 2026-06-05 07:27 — Kickoff

Goal for the session: continue session 060's external-access work. P1 (the
API/operator half) shipped in PR #112 (`dfa20b8`). The remaining phases from
`.journal/060/EXTERNAL_ACCESS_DESIGN.md` are **P2 (devnet plumbing)** and **P3
(CLI resolver)**. The user asked me to review session 060, take note of the
remaining work, and pause before proceeding.

Current state of the world:
- **P1 is live on master** (PR #112, squash `dfa20b8`): `spec.chainAPI.{ogmios,
  kupo}` now carry `service.{type,nodePort}` (ClusterIP default | NodePort;
  nodePort 0=auto, CRD `Maximum=32767`, Go-enforced 30000 floor) and an
  optional `externalURL` string. The controller renders NodePort Services,
  mirrors `externalURL` additively into `status.endpoints.{ogmios,kupo}.
  externalURL`, validates Go→`UnsupportedSpec`/Degraded, and `MutateService`
  now preserves a k8s-assigned NodePort.
- **Faucet removal is fully done** (sessions 059+061, released as **v0.2.0**).
  The CLI funds via direct Ogmios/Kupo txns (`internal/cardano/tx`); there is
  no faucet HTTP service. So P3's resolver applies to the post-faucet funding
  path with no faucet-specific branch.
- master HEAD is `dfa20b8`. A concurrent docs branch (`docs/mkdocs-site`,
  PR #91) and the journal branch exist; nothing else is in flight that I know
  of, but re-fetch/rebase before any push (shared origin/master moves).

### Remaining work (from `.journal/060/SUMMARY.md` "Open Threads" + design §7)

**P2 — devnet plumbing (CLI, `yacd devnet`).** Make the localhost URL route.
devnet owns both the k3d cluster and the default network spec, so pin constants
on both sides:
- Create the k3d cluster with host port mappings —
  `--port "1337:30137@loadbalancer"` and `--port "1442:30442@loadbalancer"` —
  in `cli/internal/cluster/k3d/ensure.go`'s `create` (stable create-time
  `--port`; serverlb proxies host port → same node port, hence NodePort
  required). k3d pinned v5.9.0.
- Author the devnet default spec (`cli/internal/cli/devnet.yaml`, a byte copy
  of `examples/local/yacd.yaml`, drift-guarded by `TestDefaultDevnetEnvIsValid`)
  with `chainAPI.{ogmios,kupo}.service.type: NodePort` + pinned nodePorts
  (30137/30442) and `externalURL: ws://localhost:1337` /
  `http://localhost:1442`.
- **Needs a live k3d run to verify** host routing (envtest can't prove it).
- Constraints: nodePorts 30000–32767; pinned host ports (1337/1442) can clash
  with something already bound — surface a clear error (relates to local-
  lifecycle P7 hardening).

**P3 — CLI resolver (CLI).** Add a shared resolution chain in the forward path:
explicit flag (`--ogmios-url`/`--kupo-url`) → ambient `YACD_OGMIOS_URL`/
`YACD_KUPO_URL` env → `status.endpoints.*.externalURL` (probe with a short
connect timeout, cache the verdict) → ephemeral port-forward fallback (today's
behavior). Wire it into the funding/`run` paths. Current force-forward lives in
`cli/internal/cli/forward.go` (`forwardEndpoints`) and `envcontract.go:
loopbackURL`; `connect` stays forward-only (the remote-cluster tool).

Ordering: P2 before P3 (P3's resolver needs status to carry `externalURL`, and
a live P2 devnet is the smoke target for P3).

Plan: pause here per the user's instruction. Next, confirm scope/approach with
the user, then likely start with P2. Dev stack startup (`moon run root:dev-up`)
is the implementation-session prerequisite — but P2/P3 want a **k3d** devnet
(not the Kind/Tilt stack) for live verification; clarify which environment to
bring up when proceeding.
