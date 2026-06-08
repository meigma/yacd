# Environment file

The environment file is the developer-facing YACD configuration document you
pass to the CLI with `-f/--file` (see [CLI reference](cli.md)). It is a single
YAML document with a fixed `apiVersion`/`kind` envelope and one field of
substance: `spec.network`, a Cardano network specification.

The CLI loads this file, validates it, and renders it into a `CardanoNetwork`
object that the operator reconciles. The document does **not** carry a name or
namespace. Identity is supplied on the command line (`yacd up NAME -n
NAMESPACE`), so one file can deploy under many names and namespaces.

## Document shape

```yaml
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network: {} # a CardanoNetworkSpec
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `apiVersion` | string | yes | Must equal `yacd.meigma.io/devconfig/v1alpha1`. |
| `kind` | string | yes | Must equal `Environment`. |
| `spec.network` | object | yes | A `CardanoNetworkSpec`. Decoded directly into the API type, so the document exposes the same fields the CRD exposes. See the [CardanoNetwork reference](cardanonetwork.md) for every network field, type, enum, and default. |

The envelope is intentionally thin. `spec.network` is the only field today;
the wrapper exists so future top-level fields can be added without breaking
existing documents.

For complete, copy-paste files per mode and profile, see the
[recipes](../recipes.md).

## Validation

The CLI rejects an invalid file before contacting the cluster. Validation runs
in three layers.

### Strict decoding (unknown fields rejected)

The file is parsed with a strict YAML decoder. Any key the schema does not
recognize is an error. A misspelled or misplaced field fails the load rather
than being silently ignored. The decoder names the offending key by its leaf
field name, not its full path.

```
parse developer config: error unmarshaling JSON: while decoding JSON: json: unknown field "nde"
```

### Envelope and shape checks

After decoding, the document is checked for envelope integrity and structural
consistency:

- `apiVersion` must equal `yacd.meigma.io/devconfig/v1alpha1`.
- `kind` must equal `Environment`.
- `spec.network.node.version` must be set.
- `spec.network.node.port` must be greater than 0.
- `spec.network.mode` must be `local` or `public`.
- In `local` mode, `spec.network.local` is required and `spec.network.public`
  must be absent.
- In `public` mode, `spec.network.public` is required and `spec.network.local`
  must be absent. `spec.network.public.profile` must be `preview`, `preprod`,
  or `mainnet`. A `bootstrap` block is allowed only for `mainnet`, where
  `bootstrap.mithril` is required.

Runtime-support checks also run here (supported eras, supported Ogmios/Kupo
images and version pairings, port-conflict detection, mainnet storage minimums,
and which chain APIs are allowed per mode). Those constraints belong to the
network spec; see the [CardanoNetwork reference](cardanonetwork.md).

### Explicit-field enforcement

Some network fields have CRD defaults. On a strongly-typed Go value the
decoder cannot tell whether the author wrote `port: 0` or omitted `port`
entirely, and a silently-defaulted value here would produce surprising runtime
behavior (for example, an unset `node.port` rendering a Service with port 0).
To prevent that, the CLI re-reads the raw YAML and requires these paths to be
written explicitly. A missing path fails with:

```
spec.network.node.port must be set explicitly in developer config
```

**Always required:**

- `spec.network.mode`
- `spec.network.node.version`
- `spec.network.node.port`

**Required when `mode: local`:**

- `spec.network.local.networkMagic`
- `spec.network.local.era`
- `spec.network.local.timing.slotLength`
- `spec.network.local.timing.epochLength`
- `spec.network.local.topology.pools.count`

**Required when `mode: public`:**

- `spec.network.public.profile`

**Required only when the parent block is present:**

| If you include... | You must also set explicitly |
| --- | --- |
| `spec.network.node.storage` | `spec.network.node.storage.size` |
| `spec.network.local.genesis` | `spec.network.local.genesis.profile` |
| `spec.network.chainAPI.ogmios` | `...ogmios.enabled`, `...ogmios.image`, `...ogmios.port` |
| `spec.network.chainAPI.kupo` | `...kupo.enabled`, `...kupo.image`, `...kupo.port` |

These rules cover only *presence* in the source. The accepted values, types,
and defaults for each field are documented in the
[CardanoNetwork reference](cardanonetwork.md). Chain-API blocks you omit
entirely keep their network-spec defaults; you opt into explicit-field
enforcement for a block only by including that block.

An ogmios or kupo block may also carry the optional `service` (ClusterIP or
NodePort) and `externalURL` fields to make the endpoint reachable from outside
the cluster; these are not required-explicit. See
[external access](cardanonetwork.md#external-access).
