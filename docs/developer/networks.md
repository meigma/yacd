# Defining networks

This page collects task recipes for the network lifecycle: write an Environment
file, apply it, validate it without applying, inspect what is running, and tear
it down. Each `yacd` command keys off a `NAME` argument; the `NAME` becomes the
`CardanoNetwork` name and, unless you pass `--namespace`, the namespace as well,
so each environment lands in its own isolated namespace by default.

For what each Environment field means, see the
[Environment file reference](../reference/environment.md) and the
[CardanoNetwork reference](../reference/cardanonetwork.md). For complete,
copy-paste manifests, see [Recipes](../recipes.md). For every command flag and
default, see the [CLI reference](../reference/cli.md).

## Scaffold and apply an Environment

The quickest way to start is `yacd init`, which prints a fully-commented
Environment to stdout. Redirect it to a file:

```sh
yacd init > yacd.yaml
```

The generated `yacd.yaml` is a ready-to-run local devnet: a single-pool network
with the faucet and a pre-funded wallet enabled. Its commented blocks document
the rest of the API — chain-API image and port overrides, and a public/mainnet
alternative — so you grow the config by uncommenting a whole block at a time.
Edit it to taste, then apply it:

```sh
yacd up dev -f yacd.yaml
```

`yacd up` renders the Environment into a `CardanoNetwork` named `dev` in
namespace `dev`, creates the namespace if needed, server-side-applies the
network, and waits until it reports `Ready`. The identity (name and namespace)
comes from the command line, not the file. Use `--wait=false` to apply without
blocking, and `--timeout` to change the readiness deadline (default `12m`). See
the [CLI reference](../reference/cli.md) for the full flag set.

!!! note
    The `yacd init` template already spells these out, but if you write or trim a
    file by hand: `spec.network.mode`, `spec.network.node.version`, and
    `spec.network.node.port` must be written explicitly; the loader rejects a
    document that omits them. Local mode additionally requires
    `spec.network.local` (with `networkMagic`, `era`, `timing.slotLength`,
    `timing.epochLength`, and `topology.pools.count`). See the
    [Environment file reference](../reference/environment.md) for the required
    fields per mode.

## Validate without applying

Use `--dry-run` to render the manifest and print it to stdout without touching
the cluster. This runs the same Environment validation and rendering as a real
apply, so it catches envelope, field, and mode errors:

```sh
yacd up dev -f yacd.yaml --dry-run
```

The rendered `CardanoNetwork` is written to stdout; a `Dry run: rendered
CardanoNetwork dev/dev; no resources applied.` line is written to stderr. No
namespace is created and nothing is applied.

## Inspect running environments

List every `CardanoNetwork` across all namespaces:

```sh
yacd list
```

The table shows `NAME`, `NAMESPACE`, `MODE`, `READY`, and a comma-separated
`ENDPOINTS` summary (for example `ogmios,kupo,faucet`, or `-` when nothing is
published yet). `READY` reflects a fresh `Ready` condition, so a stale status is
reported as not ready.

Scope to a single namespace with `-n`, or emit machine-readable JSON with
`--json`:

```sh
yacd list -n dev
yacd list --json
```

Print full status and connection details for one environment:

```sh
yacd info dev
```

`yacd info` shows the network mode and magic, published endpoint URLs, faucet
and wallet status, and the `metav1.Condition` list. Add `--json` for a stable,
scriptable projection:

```sh
yacd info dev --json
```

## Tear down

Delete an environment and wait for it and its garbage-collected children to be
removed:

```sh
yacd down dev
```

Deletion is idempotent: a network that is already absent is reported as success.
Use `--wait=false` to issue the delete without blocking, and `--timeout` to
change the removal deadline (default `5m`).

## Run a public profile locally

Public mode connects a node to a real Cardano network instead of a synthetic
local chain. The `preview` and `preprod` test networks need no bootstrap and are
the recommended way to develop against public-network data.

To switch the scaffolded config to a public network, set `spec.network.mode:
public`, remove the `local:` block, and uncomment the `public:` block — the
`yacd init` template marks exactly what to change. Or save the preview manifest
from [Recipes](../recipes.md) as `yacd.yaml`. Then apply it the same way:

```sh
yacd up preview -f yacd.yaml
```

Switch to preprod by setting `spec.network.public.profile: preprod`. For both
profiles, `spec.network.public.bootstrap` must be absent; it is accepted only
for `mainnet`. Public profiles sync from the network, so allow extra time and
storage compared with a local network. The copy-paste preview and preprod
manifests live in [Recipes](../recipes.md).

## Run mainnet

!!! warning
    Mainnet is heavyweight and gated. A mainnet Environment **must** set
    `spec.network.public.bootstrap.mithril` (the loader rejects a mainnet
    document without it), provisions large persistent volumes, and bootstraps
    chain state from [Mithril](https://mithril.network). `yacd up` refuses to
    apply a mainnet network unless you pass `--allow-mainnet`; without it the
    command fails with an explicit error. A `--dry-run` still renders a mainnet
    manifest without the flag, but prints a warning and applies nothing.

Save the mainnet manifest from [Recipes](../recipes.md) as `yacd.yaml`, then
apply it with the gate flag:

```sh
yacd up mainnet -f yacd.yaml --allow-mainnet
```

Because the chain bootstrap and sync take far longer than a local or test
network, raise `--timeout` accordingly. See the
[CardanoNetwork reference](../reference/cardanonetwork.md) for the
`spec.public.bootstrap.mithril` fields and the
[Recipes](../recipes.md) page for the full mainnet manifest.
