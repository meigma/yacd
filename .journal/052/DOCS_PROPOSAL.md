# yacd Documentation Structure Proposal

Status: proposal (session 052, 2026-06-02)
Framework: [Diátaxis](https://diataxis.fr) · [Material for MkDocs](https://squidfunk.github.io/mkdocs-material/)
Decision inputs confirmed with the user: theme = Material for MkDocs; hosting = GitHub Pages via CI.

## 1. Purpose and scope

`yacd` ships two user-facing surfaces — a Kubernetes operator (CRDs and controllers) and a
companion `yacd` CLI — but has no user documentation. Today the only docs are `README.md`,
`DESIGN.md`, `docs/host-access.md`, and the two `containers/*/README.md` versioning notes. There
is no install guide, no CLI reference, no CRD reference, and no configuration reference.

This document proposes the information architecture and tooling for a documentation site. It is a
structural proposal, not the site itself. Building the pages is a follow-up for an implementation
worktree branched from `master`.

Scope of this proposal:

- The page set, each page's Diátaxis type, and what it contains.
- The MkDocs configuration ("most well-trodden" Material setup).
- How the site is built (uv + Moon) and published (GitHub Pages via CI).
- Which existing files feed each page.
- Writing and external-link conventions.
- A phased build plan and the open decisions worth confirming before building.

## 2. Audience model and Diátaxis mapping

The docs serve two audiences, as requested:

- **Developer** — uses yacd locally: the `yacd` CLI plus a local k3d cluster via `yacd devnet`.
- **Operator** — runs yacd in a remote Kubernetes cluster for shared development environments and
  automated testing, installing the operator with Helm.

The two overlap (a developer running against a shared cluster installs the operator; an operator
uses the same CLI verbs). The structure does not split everything down the middle.

How this maps onto Diátaxis:

- The two audience guides (**Developer Guide**, **Operator Guide**) hold only **tutorials and
  how-to guides**. Grouping how-to guides by user context is an intended Diátaxis adaptation, not
  a violation, because each page stays a single type.
- **Reference**, **Recipes**, and **Concepts** stay **generic and shared**. A CardanoNetwork field
  means the same thing regardless of who applies it, so per-audience reference would duplicate and
  drift. This is the single most important structural decision.

Guardrails that keep the hybrid honest:

1. Every page is exactly one Diátaxis type. The audience sections are "context for how-to guides,"
   not a place to dump everything for a persona.
2. Overlap is resolved with cross-links, never duplication.
3. The Home page and the generic Reference/Concepts sections are what bind the two halves; the
   guides link across the divide where a reader's path crosses it (for example, a developer
   targeting a remote cluster is routed to Operator → Installation).

## 3. Information architecture

Seventeen pages in a left sidebar with collapsing section groups (the Material default). The four
section headers ("Developer Guide", "Operator Guide", "Reference", "Concepts") are non-clickable
grouping labels. There are no per-section landing pages; the Home page does the audience routing.

| Nav | File | Type | Purpose |
|-----|------|------|---------|
| Home | `index.md` | Orientation | What yacd is, who-reads-what routing, external links, a `yacd devnet` teaser linking into the tutorial |
| Developer Guide ▸ Getting started | `developer/getting-started.md` | Tutorial | Zero to a funded local network |
| Developer Guide ▸ Defining networks | `developer/networks.md` | How-to | Create, inspect, and tear down networks from an Environment file |
| Developer Guide ▸ Funding accounts | `developer/funding.md` | How-to | Faucet, dev wallet, and on-chain confirmation |
| Developer Guide ▸ Connecting tools & tests | `developer/connecting-tools.md` | How-to | `run` vs `exec` vs `connect`, using `YACD_*` from tools and tests |
| Operator Guide ▸ Installation | `operator/installation.md` | How-to | Install, verify, upgrade, and uninstall the operator with Helm |
| Operator Guide ▸ DB-Sync indexing | `operator/db-sync.md` | How-to | Add a CardanoDBSync indexer |
| Operator Guide ▸ Testing & CI | `operator/testing.md` | How-to | Provision and tear down ephemeral environments in CI |
| Reference ▸ CLI | `reference/cli.md` | Reference | All verbs, flags, defaults, global flags, and the canonical `YACD_*` contract |
| Reference ▸ CardanoNetwork | `reference/cardanonetwork.md` | Reference | CRD spec, status, and conditions |
| Reference ▸ CardanoDBSync | `reference/cardanodbsync.md` | Reference | CRD spec, status, and conditions |
| Reference ▸ Environment file | `reference/environment.md` | Reference | The devconfig `Environment` envelope and decode rules |
| Reference ▸ Configuration | `reference/configuration.md` | Reference | Helm chart values and manager flags |
| Recipes | `recipes.md` | Reference | Consolidated copy-paste manifests |
| Troubleshooting | `troubleshooting.md` | How-to | Read conditions and resolve common failures |
| Concepts ▸ Architecture | `concepts/architecture.md` | Explanation | The operator + CLI model and how networks are built |
| Concepts ▸ Security model | `concepts/security.md` | Explanation | Token handling, RBAC scope, image verification |

### Single-source-of-truth rules

These prevent the most common docs-rot failure (the same fact in three places, drifting):

- Every CLI flag lives only in `reference/cli.md`. How-to guides link, they do not restate flags.
- The `YACD_*` variable table and the `endpoints.json` schema exist exactly once, in
  `reference/cli.md`. `connecting-tools.md` and `testing.md` link to it.
- CRD field tables live only in the Reference CRD pages. `environment.md` links to
  `cardanonetwork.md` instead of duplicating the network spec.
- Every copy-paste manifest lives in `recipes.md`, mirrored verbatim from `examples/`.

## 4. Per-page specification

### Home — `index.md` (orientation)

Leads with the answer: yacd is a Kubernetes-native manager for Cardano development environments,
delivered as an operator plus a CLI. States who should read what (developer vs operator) with two
clear links. Shows a single `yacd devnet` teaser, then links to the tutorial as the canonical
first run; it does not duplicate the steps. Carries the curated external-tools link list.
Sources: `README.md`, `DESIGN.md`.

### Developer Guide

**Getting started — `developer/getting-started.md` (tutorial).** The one canonical first-run path,
strictly procedural: install the CLI, run `yacd devnet`, inspect with `list`/`info`, run a query
with `exec`, fund an address with `topup`, then `yacd devnet down`. Every "why" links out to
Concepts. Opens with CLI installation so the tutorial is self-contained.
Sources: `cli/internal/cli/devnet.go`, `examples/local/yacd.yaml`.

**Defining networks — `developer/networks.md` (how-to).** Task recipes for the network lifecycle:
apply an Environment with `yacd up -f`, validate with `--dry-run`, inspect with `list`/`info`,
tear down with `down`. Covers running public profiles locally (preview/preprod) and the mainnet
caveats (`--allow-mainnet`, Mithril, storage minimums) in a clearly delimited section. Field
meaning lives in Reference; structures live in Recipes.
Sources: `cli/internal/cli/{up,down,list,info}.go`, `cli/internal/devconfig`, `examples/*/yacd.yaml`.

**Funding accounts — `developer/funding.md` (how-to).** Enable the faucet and dev wallet, fund an
address with `yacd topup`, and confirm on-chain with `--await`. The full topup flag set lives in
`reference/cli.md`; the trust-gate and host-only-token rationale lives in `concepts/security.md`.
Sources: `cli/internal/cli/topup*.go`, faucet/wallet specs.

**Connecting tools & tests — `developer/connecting-tools.md` (how-to).** Choose `run` vs `exec` vs
`connect` for a given goal, and use the `YACD_*` variables from tools and test runners. This page
supersedes `docs/host-access.md`; the canonical variable table and `endpoints.json` schema move to
`reference/cli.md` and are linked, not restated.
Sources: `docs/host-access.md` (migrate, then remove), `cli/internal/cli/{run,exec,connect}.go`,
`cli/internal/cli/envcontract.go`.

### Operator Guide

**Installation — `operator/installation.md` (how-to).** Install the operator from the OCI Helm
chart (`oci://ghcr.io/meigma/yacd/chart`) with a pinned `--version`, choose a namespace, and verify
the manager is Ready. Includes clearly delimited sections for **Upgrading** (the Helm bundled-CRDs
caveat), **Metrics** (scrape and secure), optional **image verification** (Kyverno/cosign), and
**Uninstall**. Deep value semantics point to `reference/configuration.md`.
Sources: `charts/yacd/{Chart.yaml,values.yaml}`, `.github/workflows/release.yml`, `cmd/options.go`.

**DB-Sync indexing — `operator/db-sync.md` (how-to).** Add a CardanoDBSync indexer: choose managed
vs external Postgres (and supply the password Secret), choose a placement mode (dedicatedFollower
vs primarySidecar). Decisions and steps only; the full spec lives in `reference/cardanodbsync.md`
and the trade-off rationale in Concepts; manifests come from Recipes.
Sources: `api/v1alpha1/cardanodbsync_types.go`, `examples/cardanodbsync-*.yaml`.

**Testing & CI — `operator/testing.md` (how-to).** Provision an ephemeral network in CI: `up
--wait`, expose `YACD_*` to a test runner via `yacd run`, then tear down deterministically with
`down --wait`. References `connecting-tools.md` and `reference/cli.md` rather than restating them.
Sources: `cli/internal/cli/{up,down,run}.go`, `docs/host-access.md`.

### Reference (generic)

**CLI — `reference/cli.md`.** The single source of truth for every verb, argument, flag, and
default (for example `up --timeout 12m`, `down --timeout 5m`, `topup --await-timeout 2m`), the
global flags and their `YACD_*` env overrides, JSON output shapes, the canonical `YACD_*` contract
table (host vs in-pod) and `endpoints.json` schema, plus CLI install/verify and shell completion.
Sources: `cli/internal/cli/*.go`, `cli/internal/cli/root.go`, `cli/internal/cli/envcontract.go`.

**CardanoNetwork — `reference/cardanonetwork.md`.** Dry, complete description of the spec
(`mode`, `node`, `local`, `public`, `chainAPI`), the full status (endpoints, sync, wallet, faucet),
and every condition (`Ready`, `NodeReady`, `OgmiosReady`, `KupoReady`, `FaucetReady`,
`WalletReady`, `ArtifactsReady`, the sync conditions, `Degraded`). Mirror from the types and the
generated CRD.
Sources: `api/v1alpha1/cardanonetwork_types.go`, `charts/yacd/crds/yacd.meigma.io_cardanonetworks.yaml`.

**CardanoDBSync — `reference/cardanodbsync.md`.** The full spec including `database` (external and
managed, with `sslMode`, `passwordSecretRef`/`authSecretRef`, `parameters`), `placement` modes,
`config.runtime`, `config.ledgerBackend`, and the complete `config.insert` preset and per-table
option tree, plus status and conditions.
Sources: `api/v1alpha1/cardanodbsync_types.go`, `charts/yacd/crds/yacd.meigma.io_cardanodbsyncs.yaml`.

**Environment file — `reference/environment.md`.** The devconfig envelope (`apiVersion:
yacd.meigma.io/devconfig/v1alpha1`, `kind: Environment`, `spec.network` = a CardanoNetworkSpec),
the strict-decode behavior, and the fields that must be set explicitly. Links to
`cardanonetwork.md` for the embedded spec instead of duplicating it.
Sources: `cli/internal/devconfig/config.go`.

**Configuration — `reference/configuration.md`.** The Helm chart values and the manager binary
flags: image overrides (manager, cardano-testnet, cardano-tools, faucet), `manager.logFormat`/
`logLevel`/`extraArgs`, metrics and TLS, leader election, resources, security contexts, and the
optional Kyverno image-verification block.
Sources: `charts/yacd/values.yaml`, `cmd/options.go`.

### Recipes — `recipes.md` (reference, example collection)

Consolidated copy-paste manifests in labeled sections (Local networks, Public networks, DB-Sync),
each snippet with a one-line "use when" caption and a link to the relevant reference page. The
YAML mirrors `examples/` verbatim so it stays correct. The mainnet recipe is annotated with its
hard requirements (Mithril bootstrap, 300–500 GiB storage, `--allow-mainnet`) so a copied snippet
does not silently fail.
Sources: `examples/**`.

### Troubleshooting — `troubleshooting.md` (how-to)

The highest-frequency task after first run, with no current owner. How to read `yacd info`
conditions and resolve common failures: a network not reaching Ready, mainnet missing a Mithril
bootstrap, an undersized PVC, an unreachable external Postgres, faucet enabled on a public network
(local-only), and a faucet/wallet dependency error. Generic to both audiences.
Sources: status conditions in `api/v1alpha1/*_types.go`, `cli/internal/cli/info.go`.

### Concepts (generic)

**Architecture — `concepts/architecture.md` (explanation).** The two-artifact model (operator +
CLI), CardanoNetwork as the primary resource with supporting-service CRDs, how local networks are
generated versus how public networks fetch and bootstrap, the served-artifacts design, and how the
host-access verbs bridge cluster-internal services. Frames the developer vs operator workflows.
Sources: `DESIGN.md`.

**Security model — `concepts/security.md` (explanation).** Why the faucet token is host-only and
never injected in-pod, the `topup` trust gate for custom faucet URLs, the cluster-scoped manager
RBAC, and the optional cosign/Kyverno image-verification path. A heavily cross-linked target for
`funding.md`, `connecting-tools.md`, and `installation.md`.
Sources: `cli/internal/cli/topup_trust.go`, `charts/yacd/templates/rbac-manager.yaml`, Kyverno
values, faucet auth Secret handling.

## 5. MkDocs setup

Material for MkDocs, configured for the well-trodden defaults. `mkdocs.yml` at the repo root,
`docs_dir: docs`. The left sidebar uses Material's default collapsing groups; `navigation.sections`
and `navigation.expand` are deliberately omitted because they would flatten or auto-expand the
groups rather than collapse them.

```yaml
site_name: yacd
site_description: Kubernetes-native Cardano development environments
repo_url: https://github.com/meigma/yacd
repo_name: meigma/yacd
docs_dir: docs

theme:
  name: material
  features:
    - navigation.top        # back-to-top button
    - navigation.footer     # previous/next links
    - navigation.tracking   # update the URL anchor while scrolling
    - toc.follow
    - content.code.copy      # copy button on code blocks
    - content.code.annotate
    - search.suggest
    - search.highlight

markdown_extensions:
  - admonition
  - attr_list
  - md_in_html
  - tables
  - toc:
      permalink: true
  - pymdownx.highlight:
      anchor_linenums: true
  - pymdownx.inlinehilite
  - pymdownx.snippets
  - pymdownx.superfences
  - pymdownx.tabbed:
      alternate_style: true   # tabs for macOS/Linux, managed/external Postgres, etc.

plugins:
  - search

nav:
  - Home: index.md
  - Developer Guide:
      - Getting started: developer/getting-started.md
      - Defining networks: developer/networks.md
      - Funding accounts: developer/funding.md
      - Connecting tools & tests: developer/connecting-tools.md
  - Operator Guide:
      - Installation: operator/installation.md
      - DB-Sync indexing: operator/db-sync.md
      - Testing & CI: operator/testing.md
  - Reference:
      - CLI: reference/cli.md
      - CardanoNetwork: reference/cardanonetwork.md
      - CardanoDBSync: reference/cardanodbsync.md
      - Environment file: reference/environment.md
      - Configuration: reference/configuration.md
  - Recipes: recipes.md
  - Troubleshooting: troubleshooting.md
  - Concepts:
      - Architecture: concepts/architecture.md
      - Security model: concepts/security.md
```

Colors and fonts are intentionally left at Material defaults; no palette configuration is required.

## 6. Build and deploy

- **Toolchain.** Pin `mkdocs-material` through `uv` (a small `docs/pyproject.toml` or a PEP 723
  inline script) and run MkDocs with `uv run`, so there is no global Python dependency and the
  build is reproducible.
- **Moon tasks.** Moon is the task front door; do not add Make. Add `root:docs`
  (`uv run mkdocs build --strict`) and `root:docs-serve` (`uv run mkdocs serve`). Keep the task
  surface small per repo convention.
- **CI.** A `.github/workflows/docs.yml` runs `mkdocs build --strict` as a pull-request check on
  changes to `docs/**` and `mkdocs.yml`, and on merge to `master` deploys with the GitHub-native
  Pages actions (`actions/configure-pages` + `actions/upload-pages-artifact` + `actions/deploy-pages`).

## 7. Content-sourcing map

| Page | Primary sources |
|------|-----------------|
| Home | `README.md`, `DESIGN.md` |
| Getting started | `cli/internal/cli/devnet.go`, `examples/local/yacd.yaml` |
| Defining networks | `cli/internal/cli/{up,down,list,info}.go`, `cli/internal/devconfig`, `examples/*/yacd.yaml` |
| Funding accounts | `cli/internal/cli/topup*.go`, faucet/wallet spec |
| Connecting tools | `docs/host-access.md` (migrate, remove), `cli/internal/cli/{run,exec,connect}.go`, `envcontract.go` |
| Installation | `charts/yacd/{Chart.yaml,values.yaml}`, `.github/workflows/release.yml`, `cmd/options.go` |
| DB-Sync | `api/v1alpha1/cardanodbsync_types.go`, `examples/cardanodbsync-*.yaml` |
| Testing & CI | `cli/internal/cli/{up,down,run}.go`, `docs/host-access.md` |
| reference/cli | `cli/internal/cli/*.go`, `root.go`, `envcontract.go` |
| reference/cardanonetwork | `api/v1alpha1/cardanonetwork_types.go`, generated CRD |
| reference/cardanodbsync | `api/v1alpha1/cardanodbsync_types.go`, generated CRD |
| reference/environment | `cli/internal/devconfig/config.go` |
| reference/configuration | `charts/yacd/values.yaml`, `cmd/options.go` |
| recipes | `examples/**` |
| troubleshooting | status conditions in `api/v1alpha1/*_types.go`, `cli/internal/cli/info.go` |
| concepts/architecture | `DESIGN.md` |
| concepts/security | `topup_trust.go`, `charts/yacd/templates/rbac-manager.yaml`, Kyverno values |

## 8. Writing and style guidelines

- Lead with the answer and the most common path; defer edge cases and detail.
- Concise, professional, active voice. No emoji. Minimal em-dashes.
- Every procedure includes copy-paste fenced command or code blocks.
- Use admonitions sparingly, only for real footguns: mainnet requirements, faucet being
  local-only, and the host-only faucet token.
- Honor the single-source-of-truth rules in section 3; how-to guides link rather than restate.
- Link out to external tools instead of re-explaining them:
  [Cardano](https://developers.cardano.org), [Ogmios](https://ogmios.dev),
  [Kupo](https://cardanosolutions.github.io/kupo/),
  [cardano-db-sync](https://github.com/IntersectMBO/cardano-db-sync),
  [Mithril](https://mithril.network), [Helm](https://helm.sh), [k3d](https://k3d.io),
  [Kubernetes](https://kubernetes.io), [Diátaxis](https://diataxis.fr).

## 9. Phased build plan

For a future implementation session in a worktree branched from `master`:

- **Phase 0** — scaffold `mkdocs.yml` and the `docs/` tree (each page an H1 plus its type label),
  the `uv` pin, the Moon tasks, and a green `mkdocs build --strict`.
- **Phase 1** — Reference pages and Recipes. These are fact extraction from the types, CRDs, and
  values; highest leverage, least prose.
- **Phase 2** — Developer Guide. Migrate the content of `docs/host-access.md`, then remove or
  redirect it.
- **Phase 3** — Operator Guide and Troubleshooting.
- **Phase 4** — Concepts, then a cross-link, external-link, and single-source-of-truth audit.
- **Phase 5** — enable the GitHub Pages CI deploy and link the site from `README.md`.

## 10. Open decisions

These are the only places where "fewest pages" trades off against completeness. The recommendation
is the more complete option in each case; each is easy to trim.

1. **Funding page.** Recommended: a dedicated `developer/funding.md` (a distinct, high-frequency
   goal with its own `topup` verb). Leaner alternative: merge it into `networks.md` as a "Funding"
   section.
2. **Troubleshooting page.** Recommended: include it (the highest-frequency task with no current
   owner). Alternative: defer to a later phase.
3. **Concepts split.** Recommended: separate Architecture and Security pages (security is a heavily
   cross-linked target). Leaner alternative: a single combined `concepts.md`.

Trimming all three would yield a 14-page site.

## 11. Verification

- `mkdocs build --strict` is the primary gate; it fails on broken internal links and nav problems.
- `mkdocs serve` to confirm the collapsing left sidebar and rendering.
- Confirm `docs/host-access.md` is fully migrated before removing it.
- Cross-check Reference pages against `api/v1alpha1/*_types.go`, `charts/yacd/values.yaml`,
  `cmd/options.go`, and `cli/internal/cli/*` so the reference does not drift from the code.

## Appendix: capability inventory (build reference)

Captured from the current `master` checkout so the build phase does not need to re-derive it.

**CLI verbs.** `devnet` / `devnet down` / `devnet status` (one-shot local stack; `--bare`,
`--timeout`); `up NAME -f FILE` (`--dry-run`, `--allow-mainnet`, `--wait` default true,
`--timeout` 12m); `down NAME` (`--wait`, `--timeout` 5m); `list` (`-A`, `--json`); `info NAME`
(`--json`); `topup NAME --address --lovelace` (`--source`, `--faucet-url`, `--trust-faucet-url`,
`--allow-insecure-faucet-url`, `--await`, `--await-timeout` 2m, `--kupo-url`, `--json`);
`run NAME [-- cmd]`; `connect NAME`; `exec NAME -- cmd`. Global flags `--kubeconfig`, `--context`,
`-n/--namespace`, `--log-level`, `--log-format`, each with a `YACD_*` env override.

**`YACD_*` contract (version 1).** `YACD_NETWORK`, `YACD_NAMESPACE`, `YACD_NETWORK_MAGIC`,
`YACD_OGMIOS_URL`, `YACD_KUPO_URL`, `YACD_FAUCET_URL`, `YACD_FAUCET_TOKEN` (host-only, never
in-pod). `exec` also sets `CARDANO_NODE_SOCKET_PATH=/ipc/node.socket`. Documented today in
`docs/host-access.md`.

**Environment file.** `apiVersion: yacd.meigma.io/devconfig/v1alpha1`, `kind: Environment`,
`spec.network` is a CardanoNetworkSpec. Strict-decoded; surprising-when-defaulted fields
(`mode`, `node.version`, `node.port`, local `timing`/`topology`, `era`) must be explicit.

**CRDs (`yacd.meigma.io/v1alpha1`, namespaced).**

- CardanoNetwork: `mode` local|public; `node{version,port,storage,resources}`;
  `local{networkMagic, era babbage|conway, timing{slotLength,epochLength}, topology.pools.count,
  genesis.profile}`; `public{profile preview|preprod|mainnet, bootstrap.mithril (required for
  mainnet; storage default 500Gi, minimum 300Gi)}`; `chainAPI{ogmios (default on), kupo (default
  on), faucet (opt-in, local-only), wallet (opt-in, needs faucet + kupo)}`.
- CardanoDBSync: `networkRef.name` (same namespace); `database` external{host,port,db,user,
  passwordSecretRef,sslMode} or managed{image,db,user,authSecretRef?,storage,parameters};
  `placement.mode` dedicatedFollower (default) | primarySidecar;
  `config{runtime, ledgerBackend lsm|inmemory, insert.preset full|only_utxo|only_governance|
  disable_all, full insert_options tree}`.

**Examples.** `examples/local/yacd.yaml`, `examples/public-{preview,preprod,mainnet}/yacd.yaml`
(Environment docs); `examples/cardanodbsync-{managed,external}-postgres.yaml`,
`examples/public-preprod/cardanodbsync-managed-postgres.yaml`,
`examples/public-preview/cardanodbsync-primary-sidecar-managed-postgres.yaml` (raw CRDs).

**Distribution.** Operator via the OCI Helm chart `oci://ghcr.io/meigma/yacd/chart` (bundles CRDs,
cluster-scoped RBAC, the manager Deployment, metrics). CLI as GitHub Release binaries built by
goreleaser (darwin/linux × amd64/arm64); no Homebrew or `go install` channel yet. The CLI is a
kubeconfig client and requires the operator installed in the target cluster.
