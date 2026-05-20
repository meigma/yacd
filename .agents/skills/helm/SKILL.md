---
name: helm
description: Build, review, and maintain modern Helm v4 charts for Kubernetes applications and operators. Use when creating or editing Chart.yaml, values.yaml, values.schema.json, templates, helpers, CRDs, RBAC, chart tests, dependencies, OCI publishing, or when replacing Kustomize/Kubebuilder manifests with a centralized Helm chart.
---

# Helm Chart Work

Use Helm as the single, readable source of deployment manifests. Do not rebuild
Kustomize inside Helm with scattered override layers, post-renderer tricks, or
template indirection. Prefer a small working chart first, then tighten it from
rendered output and install behavior.

## Helm v4 Stance

- Target Helm v4 by default. Verify current behavior with `helm version`,
  `helm help`, and the Helm v4 docs before encoding new automation.
- Keep chart APIs on `apiVersion: v2` until Helm chart API v3 is actually
  needed and documented as stable for the use case.
- Expect Helm v4 installs to use server-side apply for new releases. Test
  upgrades, rollbacks, and ownership interactions in clusters where other
  controllers or GitOps tools may touch the same resources.
- Use v4 flag names in new automation: prefer `--rollback-on-failure` over
  deprecated `--atomic`, and `--force-replace` over deprecated `--force`.
- Treat post-renderers as plugins in v4. Avoid post-renderers for ordinary
  chart customization; make the chart itself readable instead.
- For OCI charts, log in with registry hostnames only, publish packaged `.tgz`
  charts, and prefer digest-pinned installs for high-trust environments.
- Avoid random template functions unless churn is intentional; templates rerun
  on upgrade and can cause avoidable rollouts.

## Chart Shape

- Keep the chart directory conventional: `Chart.yaml`, `values.yaml`,
  `values.schema.json`, `templates/`, optional `crds/`, optional `charts/`, and
  `.helmignore`.
- Use lowercase dashed chart names, SemVer chart versions, quoted
  `appVersion`, and `type: application` for deployable charts.
- Set `kubeVersion` when rendered APIs depend on a Kubernetes version range.
- Keep one Kubernetes resource per template file. Use dashed file names that
  include the kind, such as `controller-deployment.yaml`.
- Put reusable named templates in `_helpers.tpl`; namespace every `define`
  name with the chart name because defined templates are global.
- Do not set `metadata.namespace` in ordinary templates. Let the installer,
  GitOps controller, or `--namespace` decide the target namespace.

## Values

- Treat `values.yaml` as the chart's public API. Keep it small, documented,
  and backwards-compatible unless the chart version intentionally breaks it.
- Use lower-camel-case keys. Prefer shallow values; nest only when a group has
  meaningful cohesion and at least one non-optional field.
- Prefer maps over positional lists for user-addressable objects so overrides
  remain stable under `--set`.
- Quote strings in values files. Keep booleans and numeric Kubernetes fields as
  native types, and quote rendered strings in templates.
- Add `values.schema.json` early. Validate owned config objects strictly, but
  allow open maps for labels, annotations, node selectors, tolerations, and
  other Kubernetes extension points.
- Use `required` or `fail` sparingly and only for chart-specific invariants
  that schema cannot express cleanly.
- Keep secrets out of committed values. Accept existing Secret names or
  Kubernetes Secret manifests from a deliberate secret-management workflow.

## Templates

- Render boring YAML. Use `include`, `default`, `quote`, `toYaml`, and
  `nindent` for clarity; avoid `tpl` unless users explicitly need templated
  external configuration.
- Centralize common labels and selector labels in helpers. Keep selectors
  stable and exclude labels that change across chart or app versions.
- Apply recommended labels consistently:
  `app.kubernetes.io/name`, `app.kubernetes.io/instance`,
  `app.kubernetes.io/managed-by`, and `helm.sh/chart`.
- Put optional whole resources behind simple `*.enabled` booleans. Avoid deep
  inline conditionals that make rendered output hard to predict.
- Add checksum annotations for mounted ConfigMaps or Secrets when their changes
  must roll pods.
- Prefer template comments for chart-author notes and YAML comments only when
  the rendered comment helps an operator debug output.
- Use hooks only for real lifecycle exceptions and tests. Ordinary dependency
  ordering should come from Kubernetes reconciliation, not hook choreography.

## Operators And CRDs

- Put CRD declarations in `crds/` as plain YAML with no templating. Helm
  installs CRDs before templates, but it does not upgrade, roll back, or delete
  them.
- For operators with serious CRD lifecycle needs, prefer a separate CRD chart
  or documented admin-managed CRD upgrade step over hiding lifecycle risk in
  hooks.
- Keep generated CRDs and RBAC flowing from controller tooling into the chart
  through an explicit repository task. Do not hand-edit generated Kubernetes
  API surfaces to make a chart pass.
- Separate `rbac.create` from `serviceAccount.create`; default RBAC creation
  to true, and let `serviceAccount.name` bind workloads to either generated or
  preexisting ServiceAccounts.
- Use fixed image tags or digests, never `latest`, `head`, or `canary`.
- Keep rendered Pod specs compatible with the repo's security posture, resource
  expectations, probes, and controller-runtime manager flags.

## Dependencies

- Avoid dependencies for resources this chart owns directly. Use subcharts only
  for separable components with an independent lifecycle.
- Gate optional dependencies with `dependency.enabled`; use tags only when
  several dependencies form one optional feature.
- Prefer HTTPS or OCI repositories. Commit `Chart.lock` after dependency
  updates so CI and releases build the same dependency graph.
- Use conservative version ranges in `Chart.yaml`, then let `Chart.lock` pin
  the resolved versions for repeatable builds.
- Use a library chart only after multiple charts have real duplicated helpers.

## Validation

Run the smallest useful validation set before handing off chart changes:

```sh
helm dependency build <chart>
helm lint <chart>
helm template <release> <chart> --namespace <namespace> --debug
helm install <release> <chart> --namespace <namespace> --dry-run=client --server-side=false
helm package <chart>
```

Add representative values files for non-default modes and render each one. For
operator charts, also smoke a real install in Kind or the repo's e2e harness
when CRDs, RBAC, manager args, services, or webhook/metrics wiring changes.

## Source Refresh

Use the current Helm docs before changing policy-sensitive behavior:

- Helm 4 Overview: https://helm.sh/docs/overview/
- Chart Best Practices: https://helm.sh/docs/chart_best_practices/
- Chart format and CRD rules: https://helm.sh/docs/topics/charts/
- OCI registries: https://helm.sh/docs/topics/registries/
