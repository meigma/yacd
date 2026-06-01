# cardano-testnet tools image

YACD's tools image built from official IntersectMBO `cardano-node` release
artifacts. It ships `cardano-node`, `cardano-cli`, and `cardano-testnet` plus the
`yacd-cardano-testnet-init` wrapper used by the operator's create-env init
container. The operator also uses it for the faucet source-address init, the
default `cardano-node` container, and the CardanoDBSync follower node.

## Versioning

The image tag is `<cardano-node-version>-yacd.<N>`, e.g. `11.0.1-yacd.5`:

- The **base** (`11.0.1`) tracks the upstream IntersectMBO `cardano-node`
  release the image is built from. The release workflow strips the `-yacd.<N>`
  suffix and downloads `cardano-node` at exactly this version, and the operator
  computes the image reference as `<node version>-yacd.<N>` — so the base **must**
  equal the packaged `cardano-node` version.
- The `-yacd.<N>` suffix is YACD's packaging revision: bump it for
  packaging-only changes (new wrapper behavior, slimming the image, etc.) that
  keep the same upstream `cardano-node` version.

release-please derives the next version from Conventional Commit types
(`fix`→patch, `feat`→minor, `!`→major), which would drift the base away from the
`cardano-node` version. To keep the base pinned to the upstream version, each
`cardano-testnet` release sets its exact version with a `Release-As:` footer on
the squash-merge commit:

- packaging-only change → `Release-As: <same node version>-yacd.<N+1>`
- upstream `cardano-node` bump → `Release-As: <new node version>-yacd.0`

When bumping the revision, also update the operator's default
(`cardanoTestnetImageRevision` in `internal/controller/cardanonetwork/init_container.go`
and `defaultFollowerNodeImageRevision` in `internal/controller/cardanodbsync/defaults.go`)
and the kind-loaded tag in `.dev/scripts/test-e2e.sh`.
