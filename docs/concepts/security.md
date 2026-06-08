# Security model

YACD is a development environment manager, and two facts shape its threat model:
wallet keys are real signing credentials, and the operator runs with cluster-wide
reach. This page explains the deliberate security decisions that follow: how
wallet keys are stored and signed with, what the cluster-scoped manager RBAC
means on a shared cluster, and how the chart's optional image-verification path
hardens the supply chain. For the concrete knobs, follow the links to the how-to
and reference pages.

## Wallet key custody

Funding a wallet means signing a real transaction, so wallet keys are credentials
worth protecting. YACD keeps custody simple and Kubernetes-native.

- **Keys live in labeled Kubernetes Secrets.** Each managed wallet is a
  `<network>-wallet-<name>` Secret in the network's namespace, holding the payment
  signing key, verification key, and address. The genesis-funded `faucet` wallet
  has the same shape (`<network>-wallet-faucet`) and is created and owned by the
  operator. Storing keys as Secrets means they inherit Kubernetes RBAC,
  encryption-at-rest, and backup rather than living in a bespoke store.
- **The CLI signs locally; the cluster never signs for you.** `yacd wallet` reads
  the source wallet's signing key, builds and signs the funding transaction on
  your machine, and submits it over Ogmios. There is no server-side signing
  endpoint and no faucet HTTP service to authenticate against, so no long-lived
  spending credential is exposed inside the cluster.
- **`export` writes keys deliberately.** `yacd wallet export` is the only path
  that puts raw key material on local disk; it writes `0600` files under a `0700`
  directory and never prints keys to stdout.

Because there is no in-cluster faucet service, there is no Bearer token to
distribute and no token-to-URL trust gate to reason about. Keys stay in Secrets,
and signing stays on the host.

## Cluster-scoped manager RBAC

The chart binds the manager's ServiceAccount to a `ClusterRole`, not a namespaced
`Role`. The grant is broad by design: the operator manages `CardanoNetwork` and
`CardanoDBSync` objects and reconciles them into the core Kubernetes workloads
they need.

The `ClusterRole` grants, across all namespaces:

- `configmaps` and `persistentvolumeclaims`: create, get, list, patch, update,
  watch
- `pods`: get, list
- `secrets`: create, delete, get, list, patch, watch
- `services`: create, delete, get, list, patch, update, watch
- `deployments` (`apps`): create, get, list, patch, update, watch
- `cardanonetworks` and `cardanodbsyncs` (`yacd.meigma.io`): get, list, watch,
  plus get, patch, update on their `/status` subresources

The implication for a **shared cluster** is direct: a YACD install can create,
read, and delete Secrets, Services, ConfigMaps, PVCs, and Deployments in any
namespace, because that is the scope the reconcilers operate at. The `secrets`
verbs include `create` and `delete` because wallet keys and other runtime
material are managed Secrets. Treat the operator as a cluster-wide actor when you
decide where to install it; an isolated development cluster is the intended home,
not a multi-tenant production cluster shared with untrusted workloads.

RBAC creation is on by default but optional (`rbac.create`), and the role and
binding names are templated so an operator can substitute externally managed
RBAC. See the [installation guide](../operator/installation.md) and the
[configuration reference](../reference/configuration.md) for those values.

## Supply-chain image verification

The release workflow attests the manager image and the Helm chart with
GitHub-native (Sigstore keyless) attestations. The chart ships an **opt-in**
Kyverno `ClusterPolicy` that verifies the manager image attestation at Pod
admission time, closing the gap between "an image is signed somewhere" and "only
verified images run in this cluster".

The policy is **disabled by default** (`kyverno.imageVerification.enabled:
false`) because it depends on a running [Kyverno](https://kyverno.io)
installation. When you enable it, Kyverno intercepts pod admission and rejects
images that do not carry a valid keyless Sigstore attestation matching the
configured attestor.

The attestor defaults encode trust in the `meigma/yacd` release pipeline rather
than in a long-lived key:

- **issuer** `https://token.actions.githubusercontent.com` — the OIDC identity
  must come from GitHub Actions.
- **subjectRegExp** restricts the signing workflow to `meigma/yacd`'s
  `release.yml` running on a `v<semver>` tag, so a signature produced by any
  other workflow or fork does not satisfy the policy.
- **Rekor** at `https://rekor.sigstore.dev` — the transparency log that records
  the keyless signing event.
- **attestation** of type `https://slsa.dev/provenance/v1` with build type
  `https://actions.github.io/buildtypes/workflow/v1` — the policy checks SLSA
  provenance, not just a bare signature.

Because the signing identity is keyless and tied to a specific workflow on a
release tag, there is no signing key to leak or rotate; trust flows from the
GitHub OIDC identity and the public transparency log. The issuer, subject
pattern, Rekor URL, validation failure action, and the set of image references
the policy applies to are all configurable under `kyverno.imageVerification.*`.
To enable and tune verification, see the [installation guide](../operator/installation.md);
for the full value surface, see the [configuration reference](../reference/configuration.md).
