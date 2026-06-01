# cardano-tools image

YACD's Cardano artifact utility image. It ships the `yacd-cardano-tools` binary
plus the `cardano-node`, `cardano-cli`, and `cardano-testnet` release binaries on
a `distroless/static` base. The operator uses it for the artifact staging
containers on both controllers — the `serve` sidecar and the `stage`/`sync` init
containers that produce and consume the served network artifact directory.

Subcommands: `generate`, `fetch`, `serve`, `stage`, `sync`, `version`.

## Versioning

The image tag is `<cardano-node-version>-yacd.<N>`, e.g. `11.0.1-yacd.5`:

- The **base** (`11.0.1`) tracks the upstream IntersectMBO `cardano-node` release
  the image is built from. The release workflow strips the `-yacd.<N>` suffix and
  downloads `cardano-node` at exactly this version, and the operator computes the
  image reference as `<node version>-yacd.<N>` — so the base **must** equal the
  packaged `cardano-node` version.
- The `-yacd.<N>` suffix is YACD's packaging revision: bump it for packaging-only
  changes (new subcommand behavior, slimming the image, etc.) that keep the same
  upstream `cardano-node` version.

release-please derives the next version from Conventional Commit types
(`fix`→patch, `feat`→minor, `!`→major), which would drift the base away from the
`cardano-node` version. To keep the base pinned to the upstream version, each
`cardano-tools` release sets its exact version with a `Release-As:` footer on the
squash-merge commit:

- packaging-only change → `Release-As: <same node version>-yacd.<N+1>`
- upstream `cardano-node` bump → `Release-As: <new node version>-yacd.0`

When bumping the revision, also update the operator's default
(`Revision` in `internal/cardano/toolsimage/toolsimage.go`) and the kind-loaded
tag in `.dev/scripts/test-e2e.sh`. The manager default reference is digest-pinned
(`Digest` in the same package), so a release that publishes a new image should
update the pinned digest alongside the revision.
