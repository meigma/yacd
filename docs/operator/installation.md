# Installation

Install the YACD operator on a remote Kubernetes cluster with [Helm](https://helm.sh) from the OCI chart published to GitHub Container Registry. The chart deploys the controller manager, its RBAC and ServiceAccount, a secured metrics Service, and the `CardanoNetwork` and `CardanoDBSync` CRDs.

This page covers a remote install. For a local k3d cluster, the `yacd` CLI manages its own cluster lifecycle; see the [CLI reference](../reference/cli.md). For every chart value and manager flag, see the [configuration reference](../reference/configuration.md).

## Prerequisites

- A Kubernetes cluster, version 1.29 or later (the chart sets `kubeVersion: ">= 1.29.0-0"`).
- Helm 3.8 or later, which can pull OCI charts.
- `kubectl` configured against the target cluster.

## Install

Install the chart into a namespace, creating the namespace if it does not exist. Replace `<version>` with the release version you want (for example `0.1.1`) and `<namespace>` with your target namespace.

```sh
helm install yacd oci://ghcr.io/meigma/yacd/chart \
  --version <version> \
  --namespace <namespace> \
  --create-namespace
```

The chart name is `chart` and its version tracks the release version without the leading `v` (the published chart `0.1.1` ships `appVersion` `v0.1.1`). To discover the chart metadata before installing, run:

```sh
helm show chart oci://ghcr.io/meigma/yacd/chart --version <version>
```

The chart bundles the CRDs under `charts/yacd/crds/`, so Helm installs `cardanonetworks.yacd.meigma.io` and `cardanodbsyncs.yacd.meigma.io` as part of the release. You do not apply CRDs separately.

To override any value, pass `--set` or `-f values.yaml`. The full value surface (image, metrics, RBAC, leader election, manager logging, Kyverno, security context, scheduling) lives in the [configuration reference](../reference/configuration.md).

## Verify

Confirm the manager Deployment becomes Available:

```sh
kubectl rollout status deployment/yacd-controller-manager --namespace <namespace>
```

Confirm the CRDs are registered:

```sh
kubectl get crd cardanonetworks.yacd.meigma.io cardanodbsyncs.yacd.meigma.io
```

Operators drive networks with the same `yacd` verbs documented in the [CLI reference](../reference/cli.md). Once the manager is Available you can apply a `CardanoNetwork` or `CardanoDBSync`; see the copy-paste manifests in [Recipes](../recipes.md).

## Upgrading

Upgrade to a new chart version in place:

```sh
helm upgrade yacd oci://ghcr.io/meigma/yacd/chart \
  --version <version> \
  --namespace <namespace>
```

!!! warning "Helm does not upgrade bundled CRDs"
    The CRDs ship under the chart's `crds/` directory. Helm installs those CRDs on first install but **never upgrades or deletes them** on `helm upgrade` or `helm uninstall`. When a release changes a CRD schema, apply the updated definitions yourself before or after the upgrade:

    ```sh
    kubectl apply -f https://raw.githubusercontent.com/meigma/yacd/v<version>/charts/yacd/crds/yacd.meigma.io_cardanonetworks.yaml
    kubectl apply -f https://raw.githubusercontent.com/meigma/yacd/v<version>/charts/yacd/crds/yacd.meigma.io_cardanodbsyncs.yaml
    ```

    Review the release notes for the target version to confirm whether a CRD changed before applying.

## Metrics

The chart ships a secured metrics Service by default. The manager serves metrics over HTTPS (`--metrics-secure` defaults to true) behind the Kubernetes authn/authz filter, so scrapers must authenticate and carry RBAC to read the endpoint. The chart also creates a `ClusterRole` that grants the metrics-reader permission for that path.

The Service is a `ClusterIP` named `yacd-controller-manager-metrics-service`, exposing port `8443` by default. To scrape it, grant your scraper the metrics-reader role and target the Service over HTTPS. To turn metrics off or change the port and TLS material, see the `metrics.*` values in the [configuration reference](../reference/configuration.md).

## Supply-chain image verification (optional)

The release workflow attests the manager image, faucet image, and Helm chart with GitHub-native (Sigstore keyless) attestations. The chart includes an opt-in [Kyverno](https://kyverno.io) `ClusterPolicy` that verifies those attestations on admission. It is disabled by default (`kyverno.imageVerification.enabled: false`).

Enabling it requires a running Kyverno installation in the cluster. When enabled with no explicit `imageReferences`, the policy verifies the configured manager and faucet image repositories, requiring a keyless Sigstore attestation from the `meigma/yacd` release workflow. To enable it:

```sh
helm upgrade yacd oci://ghcr.io/meigma/yacd/chart \
  --version <version> \
  --namespace <namespace> \
  --set kyverno.imageVerification.enabled=true
```

The attestor issuer, subject pattern, Rekor URL, validation failure action, and image references are all configurable; see the `kyverno.imageVerification.*` values in the [configuration reference](../reference/configuration.md). To verify a pulled chart or image directly with the GitHub CLI instead of at admission time, use `gh attestation verify`.

## Uninstall

Remove the release:

```sh
helm uninstall yacd --namespace <namespace>
```

!!! warning "CRDs and custom resources are not removed"
    `helm uninstall` does not delete the CRDs installed from `crds/`, and it does not delete any `CardanoNetwork` or `CardanoDBSync` resources you created. Delete those resources first if you want the operator to tear down their owned runtime children, then remove the CRDs manually once nothing depends on them:

    ```sh
    kubectl delete cardanonetworks --all --all-namespaces
    kubectl delete cardanodbsyncs --all --all-namespaces
    kubectl delete crd cardanonetworks.yacd.meigma.io cardanodbsyncs.yacd.meigma.io
    ```

To remove the namespace if you created it solely for YACD:

```sh
kubectl delete namespace <namespace>
```
