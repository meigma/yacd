# Configuration

Reference for configuring the YACD operator. There are two layers:

1. **Helm chart values** — what you set in `values.yaml` (or `--set`) when you
   install the operator. Both `helm install` and `yacd install` consume these
   values; `yacd install` takes them via `-f`/`--set`/`--set-string` and
   validates them against the chart schema. They render the operator Deployment,
   RBAC, metrics Service, and the optional Kyverno image-verification policy.
2. **Manager flags** — the command-line flags the manager process accepts. The
   chart translates the relevant values into these flags automatically; set them
   directly only when running the manager binary outside the chart.

!!! note "Operator image with `yacd install`"
    With `yacd install`, the operator image is pinned to the chart's `appVersion`
    (the version the CLI embeds). Changing `image.*` is not the supported way to
    move the operator version — upgrade the CLI instead. The other operational
    values behave the same across both install methods.

This page documents the operator's own configuration. For the `yacd` CLI flags,
environment variables, and `endpoints.json` schema, see
[CLI reference](cli.md). For the per-network and per-db-sync resource fields, see
[CardanoNetwork reference](cardanonetwork.md) and
[CardanoDBSync reference](cardanodbsync.md).

## Helm chart values

Defaults below are from `charts/yacd/values.yaml`. Only user-relevant keys are
listed.

### Images

| Key | Default | Meaning |
| --- | --- | --- |
| `image.repository` | `ghcr.io/meigma/yacd` | Manager image repository. |
| `image.tag` | `""` | Manager image tag. Empty uses the chart `appVersion`. |
| `image.digest` | `""` | Manager image digest (`sha256:...`). When set, it takes precedence over `image.tag`. |
| `image.pullPolicy` | `IfNotPresent` | Manager image pull policy. |
| `cardanoTestnet.image.repository` | `""` | Overrides the cardano-testnet tools image (the create-env init container and the primary cardano-node container when `spec.node.image` is unset). Empty uses the operator's built-in versioned reference. |
| `cardanoTestnet.image.tag` | `""` | Tag for the cardano-testnet override. Applied only when `repository` is set. |
| `cardanoTestnet.image.digest` | `""` | Digest for the cardano-testnet override. When set, it takes precedence over the tag. |
| `cardanoTools.image.repository` | `""` | Overrides the cardano-tools utility image used for artifact staging containers. Empty uses the operator's built-in versioned reference. |
| `cardanoTools.image.tag` | `""` | Tag for the cardano-tools override. Applied only when `repository` is set. |
| `cardanoTools.image.digest` | `""` | Digest for the cardano-tools override. When set, it takes precedence over the tag. |
| `imagePullSecrets` | `[]` | Image pull secrets added to the manager pod. |

The `cardanoTestnet` and `cardanoTools` overrides only render a flag when
`repository` is set; otherwise the operator keeps its built-in
`<repo>:<toolVersion>-yacd.N` reference. These exist to run pre-release publisher
changes that the published tags do not yet contain.

### Manager runtime

| Key | Default | Meaning |
| --- | --- | --- |
| `replicaCount` | `1` | Number of manager replicas. |
| `manager.logFormat` | `json` | Log output format. Rendered into `--log-format`. One of `json`, `text`. |
| `manager.logLevel` | `info` | Minimum log level. Rendered into `--log-level`. One of `debug`, `info`, `warn`, `error`. |
| `manager.enableHTTP2` | `false` | Enable HTTP/2 for the metrics and webhook servers. Rendered into `--enable-http2` when `true`. |
| `manager.healthProbe.port` | `8081` | Health/readiness probe port. Rendered into `--health-probe-bind-address=:<port>`. |
| `manager.extraArgs` | `[]` | Extra raw arguments appended verbatim to the manager command line. |
| `leaderElection.enabled` | `true` | Enable controller-runtime leader election. Renders `--leader-elect` when `true`. |

!!! note
    The chart enables leader election by default (`leaderElection.enabled: true`),
    while the manager binary's `--leader-elect` flag defaults to `false`. The
    chart adds the flag for you.

### Metrics and TLS

| Key | Default | Meaning |
| --- | --- | --- |
| `metrics.enabled` | `true` | Serve the metrics endpoint. When `false`, the chart renders `--metrics-bind-address=0` to disable it. |
| `metrics.port` | `8443` | Metrics container port. Rendered into `--metrics-bind-address=:<port>`. |
| `metrics.secure` | `true` | Serve metrics over HTTPS with Kubernetes authn/authz. Rendered into `--metrics-secure=<bool>`. |
| `metrics.service.create` | `true` | Create the metrics Service. |
| `metrics.service.port` | `8443` | Metrics Service port. |
| `metrics.service.annotations` | `{}` | Annotations on the metrics Service. |
| `metrics.tls.secretName` | `""` | Secret holding metrics server TLS material. When set, the chart mounts it and renders the `--metrics-cert-*` flags. |
| `metrics.tls.certPath` | `/certs/metrics` | Mount path for the metrics TLS Secret. Rendered into `--metrics-cert-path` when `secretName` is set. |
| `metrics.tls.certName` | `tls.crt` | Certificate filename. Rendered into `--metrics-cert-name`. |
| `metrics.tls.keyName` | `tls.key` | Private key filename. Rendered into `--metrics-cert-key`. |
| `webhook.tls.secretName` | `""` | Secret holding webhook server TLS material. When set, the chart mounts it and renders the `--webhook-cert-*` flags. |
| `webhook.tls.certPath` | `/certs/webhook` | Mount path for the webhook TLS Secret. Rendered into `--webhook-cert-path` when `secretName` is set. |
| `webhook.tls.certName` | `tls.crt` | Certificate filename. Rendered into `--webhook-cert-name`. |
| `webhook.tls.keyName` | `tls.key` | Private key filename. Rendered into `--webhook-cert-key`. |

### RBAC and service account

| Key | Default | Meaning |
| --- | --- | --- |
| `serviceAccount.create` | `true` | Create the manager ServiceAccount. |
| `serviceAccount.name` | `""` | ServiceAccount name. Empty derives the name from the release. |
| `serviceAccount.annotations` | `{}` | Annotations on the ServiceAccount. |
| `rbac.create` | `true` | Create the manager Role/RoleBinding and metrics auth RBAC. |
| `rbac.metricsReader.create` | `true` | Create the metrics-reader ClusterRole for scraping the protected metrics endpoint. |

### Scheduling and resources

| Key | Default | Meaning |
| --- | --- | --- |
| `resources.requests.cpu` | `10m` | Manager CPU request. (No limits set by default.) |
| `resources.requests.memory` | `64Mi` | Manager memory request. |
| `nodeSelector` | `{}` | Manager pod node selector. |
| `tolerations` | `[]` | Manager pod tolerations. |
| `affinity` | `{}` | Manager pod affinity. |
| `topologySpreadConstraints` | `[]` | Manager pod topology spread constraints. |
| `extraVolumes` | `[]` | Extra volumes added to the manager pod. |
| `extraVolumeMounts` | `[]` | Extra volume mounts added to the manager container. |

### Security contexts

| Key | Default | Meaning |
| --- | --- | --- |
| `podSecurityContext.runAsNonRoot` | `true` | Require the pod to run as a non-root user. |
| `podSecurityContext.seccompProfile.type` | `RuntimeDefault` | Pod seccomp profile. |
| `containerSecurityContext.allowPrivilegeEscalation` | `false` | Disallow privilege escalation. |
| `containerSecurityContext.capabilities.drop` | `[ALL]` | Linux capabilities dropped. |
| `containerSecurityContext.readOnlyRootFilesystem` | `true` | Mount the root filesystem read-only. |
| `containerSecurityContext.runAsNonRoot` | `true` | Require the container to run as a non-root user. |
| `containerSecurityContext.runAsUser` | `65532` | Container UID. |

### Labels and annotations

| Key | Default | Meaning |
| --- | --- | --- |
| `nameOverride` | `""` | Override the chart name component of generated names. |
| `fullnameOverride` | `""` | Override the full resource name prefix. |
| `commonLabels` | `{}` | Labels added to all chart resources. |
| `commonAnnotations` | `{}` | Annotations added to all chart resources. |
| `deploymentAnnotations` | `{}` | Annotations on the manager Deployment. |
| `podLabels` | `{}` | Labels on the manager pod. |
| `podAnnotations` | `{}` | Annotations on the manager pod. |

`commonLabels` and `podLabels` must not set the chart's reserved label keys
(`app.kubernetes.io/name`, `app.kubernetes.io/instance`,
`app.kubernetes.io/managed-by`, `app.kubernetes.io/version`, `helm.sh/chart`,
`control-plane`); the chart fails rendering if they do.

### Kyverno image verification

This optional block renders a [Kyverno](https://kyverno.io) image-verification
policy that requires the operator images to carry a verifiable Sigstore
signature and SLSA provenance attestation. It is disabled by default and
requires Kyverno to be installed in the cluster.

| Key | Default | Meaning |
| --- | --- | --- |
| `kyverno.imageVerification.enabled` | `false` | Render the image-verification policy. |
| `kyverno.imageVerification.name` | `""` | Policy name. Empty derives the name from the release. |
| `kyverno.imageVerification.validationFailureAction` | `Enforce` | Policy action when verification fails (`Enforce` or `Audit`). |
| `kyverno.imageVerification.webhookTimeoutSeconds` | `30` | Admission webhook timeout for the policy. |
| `kyverno.imageVerification.imageReferences` | `[]` | Image reference globs the policy applies to. Empty falls back to the manager repository (with `:*` and `@*`). |
| `kyverno.imageVerification.attestor.issuer` | `https://token.actions.githubusercontent.com` | Keyless OIDC issuer for the signing identity. |
| `kyverno.imageVerification.attestor.subject` | `""` | Exact OIDC subject (certificate identity). |
| `kyverno.imageVerification.attestor.subjectRegExp` | `^https://github\.com/meigma/yacd/\.github/workflows/release\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$` | OIDC subject regular expression. |
| `kyverno.imageVerification.attestor.rekor.url` | `https://rekor.sigstore.dev` | Rekor transparency log URL. |
| `kyverno.imageVerification.attestation.type` | `https://slsa.dev/provenance/v1` | Predicate type of the required attestation. |
| `kyverno.imageVerification.attestation.buildType` | `https://actions.github.io/buildtypes/workflow/v1` | Expected build type in the provenance. |

## Manager flags

Flags accepted by the manager binary, from `cmd/options.go`. The chart sets the
relevant flags for you; configure these directly only when running the manager
outside the chart.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--metrics-bind-address` | `0` | Metrics endpoint bind address. `0` disables the metrics server. The chart sets `:<metrics.port>` when metrics are enabled. |
| `--health-probe-bind-address` | `:8081` | Liveness/readiness probe bind address. |
| `--leader-elect` | `false` | Enable controller-runtime leader election. |
| `--metrics-secure` | `true` | Serve metrics over HTTPS with Kubernetes authn/authz. Negatable as `--no-metrics-secure`. |
| `--enable-http2` | `false` | Enable HTTP/2 for the metrics and webhook servers. Off by default to avoid the HTTP/2 rapid-reset CVEs. |
| `--log-format` | `json` | Log output format. One of `json`, `text`. |
| `--log-level` | `info` | Minimum log level. One of `debug`, `info`, `warn`, `error`. |
| `--metrics-cert-path` | `""` (unset) | Directory holding the metrics server certificate material. Empty disables the certificate watcher. |
| `--metrics-cert-name` | `tls.crt` | Metrics certificate filename within `--metrics-cert-path`. |
| `--metrics-cert-key` | `tls.key` | Metrics private key filename within `--metrics-cert-path`. |
| `--webhook-cert-path` | `""` (unset) | Directory holding the webhook server certificate material. Empty disables the certificate watcher. |
| `--webhook-cert-name` | `tls.crt` | Webhook certificate filename within `--webhook-cert-path`. |
| `--webhook-cert-key` | `tls.key` | Webhook private key filename within `--webhook-cert-path`. |
| `--default-cardano-testnet-image` | `""` | Override the cardano-testnet image used for the create-env init container and the default cardano-node container. Empty uses the built-in versioned reference. |
| `--default-cardano-tools-image` | `""` | Override the cardano-tools image used for artifact staging containers. Empty uses the built-in versioned reference. |
