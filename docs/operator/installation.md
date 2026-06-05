# Installation

The YACD operator can be installed two equivalent ways: with the `yacd` CLI, which renders and applies the bundled chart directly (no Helm required), or with [Helm](https://helm.sh) from the OCI chart published to GitHub Container Registry. Either way deploys the controller manager, its RBAC and ServiceAccount, a secured metrics Service, and the `CardanoNetwork` and `CardanoDBSync` CRDs.

This page covers installing onto an existing cluster. For a local [k3d](https://k3d.io) cluster, the `yacd` CLI manages its own cluster lifecycle — `yacd devnet` installs the operator for you; see the [CLI reference](../reference/cli.md). For every chart value and manager flag, see the [configuration reference](../reference/configuration.md).

## Prerequisites

- A Kubernetes cluster, version 1.29 or later (the chart sets `kubeVersion: ">= 1.29.0-0"`).
- `kubectl` configured against the target cluster.
- For `yacd install`: the [`yacd` CLI](../reference/cli.md#install) on your `PATH` (no Helm needed).
- For the Helm path: Helm 3.8 or later, which can pull OCI charts.

## Install

=== "yacd install"

    `yacd install` renders the operator's bundled chart in memory and applies it to the cluster your kubeconfig points at. It installs the operator when absent and upgrades it otherwise, then waits for the manager to become ready.

    ```sh
    yacd install --namespace <namespace>
    ```

    The namespace defaults to `yacd-system` and is created if it does not exist. `--wait` (default true) blocks until the manager Deployment is Available, up to `--timeout` (default `5m`). Preview the action without changing the cluster with `--dry-run`. The operator version is the one this CLI embeds; to move it, upgrade the CLI. Pass operational value overrides with `-f`/`--set`/`--set-string` — see the [CLI reference](../reference/cli.md#install).

=== "Helm"

    Install the chart into a namespace, creating it if it does not exist. Replace `<version>` with the release version you want (for example `0.2.0`) and `<namespace>` with your target namespace.

    ```sh
    helm install yacd oci://ghcr.io/meigma/yacd/chart \
      --version <version> \
      --namespace <namespace> \
      --create-namespace
    ```

    The chart name is `chart` and its version tracks the release version without the leading `v` (the published chart `0.2.0` ships `appVersion` `v0.2.0`). To inspect the chart metadata before installing:

    ```sh
    helm show chart oci://ghcr.io/meigma/yacd/chart --version <version>
    ```

    Override values with `--set` or `-f values.yaml`.

Both methods bundle the CRDs `cardanonetworks.yacd.meigma.io` and `cardanodbsyncs.yacd.meigma.io`, so you do not apply CRDs separately. The full value surface (image, metrics, RBAC, leader election, manager logging, Kyverno, security context, scheduling) lives in the [configuration reference](../reference/configuration.md).

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

=== "yacd install"

    Re-run `yacd install` against the cluster. It upgrades an older same-major install and re-applies an equal version to heal drift; it refuses a newer or major-mismatched in-cluster version with actionable guidance. Because the operator version is pinned to the CLI, **upgrade the CLI to move the operator version**, then re-run:

    ```sh
    yacd install --namespace <namespace>
    ```

=== "Helm"

    Upgrade to a new chart version in place:

    ```sh
    helm upgrade yacd oci://ghcr.io/meigma/yacd/chart \
      --version <version> \
      --namespace <namespace>
    ```

!!! warning "Helm does not upgrade bundled CRDs (Helm path only)"
    `yacd install` re-renders the CRDs from the embedded chart, so they travel with the CLI version. Helm, by contrast, installs the CRDs under the chart's `crds/` directory on first install but **never upgrades or deletes them** on `helm upgrade` or `helm uninstall`. When a release changes a CRD schema, apply the updated definitions yourself before or after the Helm upgrade:

    ```sh
    kubectl apply -f https://raw.githubusercontent.com/meigma/yacd/v<version>/charts/yacd/crds/yacd.meigma.io_cardanonetworks.yaml
    kubectl apply -f https://raw.githubusercontent.com/meigma/yacd/v<version>/charts/yacd/crds/yacd.meigma.io_cardanodbsyncs.yaml
    ```

    Review the release notes for the target version to confirm whether a CRD changed before applying.

## Metrics

The chart ships a secured metrics Service by default. The manager serves metrics over HTTPS (`--metrics-secure` defaults to true) behind the Kubernetes authn/authz filter, so scrapers must authenticate and carry RBAC to read the endpoint. The chart also creates a `ClusterRole` that grants the metrics-reader permission for that path.

The Service is a `ClusterIP` named `yacd-controller-manager-metrics-service`, exposing port `8443` by default. To scrape it, grant your scraper the metrics-reader role and target the Service over HTTPS. To turn metrics off or change the port and TLS material, see the `metrics.*` values in the [configuration reference](../reference/configuration.md).

## Supply-chain image verification (optional)

The release workflow attests the manager image and Helm chart with GitHub-native (Sigstore keyless) attestations. The chart includes an opt-in [Kyverno](https://kyverno.io) `ClusterPolicy` that verifies those attestations on admission. It is disabled by default (`kyverno.imageVerification.enabled: false`).

Enabling it requires a running Kyverno installation in the cluster. When enabled with no explicit `imageReferences`, the policy verifies the configured manager image repository, requiring a keyless Sigstore attestation from the `meigma/yacd` release workflow. To enable it:

=== "yacd install"

    ```sh
    yacd install --namespace <namespace> \
      --set kyverno.imageVerification.enabled=true
    ```

=== "Helm"

    ```sh
    helm upgrade yacd oci://ghcr.io/meigma/yacd/chart \
      --version <version> \
      --namespace <namespace> \
      --set kyverno.imageVerification.enabled=true
    ```

The attestor issuer, subject pattern, Rekor URL, validation failure action, and image references are all configurable; see the `kyverno.imageVerification.*` values in the [configuration reference](../reference/configuration.md). To verify a pulled chart or image directly with the GitHub CLI instead of at admission time, use `gh attestation verify`.

## Uninstall

!!! note "`yacd uninstall` is not yet available"
    Removing the operator is a manual step today; the path depends on how you installed it.

=== "yacd install"

    Delete the install namespace (which removes the manager Deployment, ServiceAccount, Services, and namespaced RBAC) and the chart's cluster-scoped RBAC:

    ```sh
    kubectl delete namespace <namespace>
    kubectl delete clusterrole,clusterrolebinding -l app.kubernetes.io/name=yacd
    ```

=== "Helm"

    Remove the release:

    ```sh
    helm uninstall yacd --namespace <namespace>
    ```

!!! warning "CRDs and custom resources are not removed"
    Neither path deletes the CRDs or any `CardanoNetwork` or `CardanoDBSync` resources you created. Delete those resources first if you want the operator to tear down their owned runtime children, then remove the CRDs once nothing depends on them:

    ```sh
    kubectl delete cardanonetworks --all --all-namespaces
    kubectl delete cardanodbsyncs --all --all-namespaces
    kubectl delete crd cardanonetworks.yacd.meigma.io cardanodbsyncs.yacd.meigma.io
    ```
