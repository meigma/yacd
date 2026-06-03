# Security model

YACD is a development environment manager, so its threat model is shaped by two
facts: the faucet is a live spending endpoint, and the operator runs with
cluster-wide reach. This page explains the deliberate security decisions that
follow from those facts: why the faucet auth token never leaves the host, why
`yacd topup` gates custom faucet URLs, what the cluster-scoped manager RBAC
means on a shared cluster, and how the chart's optional image-verification path
hardens the supply chain. For the concrete knobs, follow the links to the
how-to and reference pages.

## The faucet auth token is host-only

A network with the faucet enabled publishes a Kubernetes Secret holding a Bearer
token. Any request that spends from the faucet must carry that token, so the
token is a credential worth protecting.

YACD treats the token as **host-only**: it is read on the developer's machine,
used to authenticate faucet calls over a port-forward, and exposed to host
tooling through the `YACD_FAUCET_TOKEN` environment variable. It is **never**
injected into a process running inside the cluster.

That asymmetry is intentional. The in-pod execution path (`yacd exec`) builds
its `YACD_*` environment without the token. From the source comment that
documents the omission:

> It intentionally omits `YACD_FAUCET_TOKEN` — a Bearer token injected into the
> exec argv would land in apiserver audit logs and /proc, and in-pod tooling
> does not need it.

Two leak channels drive the decision:

- **apiserver audit logs.** Launching an in-pod process means handing an `exec`
  request to the Kubernetes API server. If the token rode in the argv or
  environment of that request, it would be captured verbatim in the cluster's
  audit log, where it persists and is visible to anyone with audit access.
- **`/proc`.** A process's argv and environment are readable through `/proc` by
  other processes in the same pod or by anything with host visibility, so a
  token placed there is exposed for the lifetime of the process.

In-pod tooling has no need for the token in the first place: it talks to the
cluster-internal Ogmios, Kupo, and node socket directly. Keeping the token on
the host removes the credential from both leak channels without losing any
capability.

!!! warning "The faucet is local-only"
    The faucet exposes a spending endpoint and is intended for local
    development networks only. The host-only token rule is part of why: it keeps
    a spending credential off the cluster's API surface.

## The `yacd topup` trust gate

`yacd topup` sends the faucet auth token to a faucet URL. By default that URL is
the one the `CardanoNetwork` published in its status, reached over a loopback
port-forward. The `--faucet-url` flag lets you override that destination, which
reintroduces the risk of sending a live credential somewhere it should not go.

A trust gate guards the override. It only engages when you actually point
`--faucet-url` at something that differs from the published URL and is not a
loopback target. When the URL matches what the cluster published, or resolves to
`localhost` or a loopback IP, the gate stays out of your way. Outside those
cases it defends three distinct attack vectors, each with its own
acknowledgement flag.

### Vector 1: token exfiltration to an attacker-supplied host

`--faucet-url` accepts any HTTP endpoint. If you (or a script you ran) pointed it
at an attacker-controlled host, the request would carry the Bearer token
straight to that host. The gate's first job is to notice that the destination is
neither the cluster-published URL nor a loopback target, and to refuse rather
than leak the token to an unknown host.

### Vector 2: accidental exposure via a non-loopback override

Even a legitimate remote operator may not realise that a custom `--faucet-url`
leaves the cluster boundary. A custom non-loopback destination is therefore
refused by default; sending the token there requires the explicit
acknowledgement `--trust-faucet-url`. The error names the Secret and the
destination host so you can confirm you meant it:

```text
refusing to send faucet auth Secret <ns>/<name> token to custom faucet URL host "<host>"; pass --trust-faucet-url to allow this destination
```

### Vector 3: plaintext eavesdropping

A trusted custom URL using `http://` would transmit the Bearer token in
cleartext, where any on-path observer can read it. The gate is HTTPS-only by
default even for a trusted destination, so an `http://` custom URL needs a second
acknowledgement, `--allow-insecure-faucet-url`, on top of `--trust-faucet-url`:

```text
refusing to send faucet auth Secret <ns>/<name> token to insecure custom faucet URL host "<host>"; pass --allow-insecure-faucet-url with --trust-faucet-url to allow HTTP
```

The two flags compound: trust authorises the destination, and the insecure flag
separately authorises plaintext to it. Neither one implies the other, so you
cannot send a token in cleartext to a remote host by accident. For the flag
defaults and the full `topup` surface, see the [CLI reference](../reference/cli.md);
for the funding workflow, see the [funding how-to](../developer/funding.md).

## Cluster-scoped manager RBAC

The chart binds the manager's ServiceAccount to a `ClusterRole`, not a
namespaced `Role`. The grant is broad by design: the operator manages
`CardanoNetwork` and `CardanoDBSync` objects and reconciles them into the core
Kubernetes workloads they need.

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
verbs include `create` and `delete` because the faucet auth Secret is a managed
object. Treat the operator as a cluster-wide actor when you decide where to
install it; an isolated development cluster is the intended home, not a
multi-tenant production cluster shared with untrusted workloads.

RBAC creation is on by default but optional (`rbac.create`), and the role and
binding names are templated so an operator can substitute externally managed
RBAC. See the [installation guide](../operator/installation.md) and the
[configuration reference](../reference/configuration.md) for those values.

## Supply-chain image verification

The release workflow attests the manager image, the faucet image, and the Helm
chart with GitHub-native (Sigstore keyless) attestations. The chart ships an
**opt-in** Kyverno `ClusterPolicy` that verifies the manager and faucet image
attestations at Pod admission time, closing the gap between "an image is signed
somewhere" and "only verified images run in this cluster".

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
