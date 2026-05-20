# Welcome to the Meigma Kubernetes Operator Template

This repository was generated from `template-k8s`, the standard starter for
Meigma Kubernetes operator projects. It is meant to give new repositories a
working baseline on day one: Kubebuilder and controller-runtime wiring, Moon
task orchestration, a Helm chart, envtest coverage, a Kind-backed Chainsaw
smoke path, repository security defaults, and a release pipeline for binaries,
container images, and an OCI Helm chart.

Delete this file after you finish the first-repository setup checklist below.
It is only here to orient the initial project owner.

## What This Template Provides

- A Go module at `github.com/meigma/template-k8s`.
- A Kubebuilder `go/v4` operator scaffold using controller-runtime.
- A sample `example.meigma.io/v1alpha1` `NginxDeployment` API.
- A sample controller that owns a same-named ConfigMap, Deployment, and
  ClusterIP Service, then projects fresh Deployment readiness into status.
- Manager startup through Kong flags, `slog`/`logr` logging, health checks,
  readiness checks, protected metrics, and webhook TLS plumbing.
- Operator-specific metrics and Kubernetes Events as examples of bounded,
  controller-owned observability.
- Moon tasks for generation, manifests, tests, linting, chart validation,
  local deploys, and Kind-backed e2e smoke tests.
- A Helm chart under `charts/template-k8s` with CRDs, RBAC, manager deployment,
  metrics service, values schema, and release-version markers.
- Release Please, GoReleaser, GHCR container publishing, OCI Helm chart
  publishing, and GitHub-native artifact attestations.
- Repository settings automation under `.github/repository-settings.toml` and
  `.github/scripts/configure_github_repo.py`.
- Repo-local agent guidance and lifecycle skills under `AGENTS.md`,
  `.session.md`, and `.agents/skills/`.

## How It Works

Moon is the main entrypoint for local development and CI:

```sh
moon run root:check
moon run root:test
```

For operator/API changes, the usual local path is:

```sh
moon run root:generate
moon run root:check
moon run root:test
```

The e2e smoke path builds the local manager image, loads it into Kind, installs
the Helm chart, and runs Chainsaw assertions:

```sh
moon run root:test-e2e
```

The release machinery is intentionally enabled in the template repository so
generated projects inherit a proven baseline for binary assets, container
images, OCI Helm charts, checksums, SBOMs, and attestations. The nominal
generated-project path is an operator that keeps all three artifact classes:
manager binary, container image, and Helm chart. If the new project changes
that shape, trim the release files before the first release.

## First Setup Checklist

1. Rename the Go module:

   ```sh
   go mod edit -module github.com/meigma/YOUR_REPO
   ```

2. Update Kubebuilder project metadata in `PROJECT`.

   Replace the template repository and API identity:

   - `domain`
   - `projectName`
   - `repo`
   - resource `domain`
   - resource `group`
   - resource `kind`
   - resource `path`

3. Choose the operator API shape.

   The sample API is intentionally a concrete nginx operator, not a generic
   abstraction. Replace it with the smallest real API that proves the new
   operator's reconciliation path.

4. Replace template identifiers:

   ```sh
   rg "template-k8s|template_k8s|github.com/meigma/template-k8s|example\\.meigma\\.io|NginxDeployment|nginx"
   ```

   Every match should either be rewritten for the new project or be a deliberate
   example in this file.

5. Rename or consciously keep the chart directory.

   If `charts/template-k8s` is renamed, update every matching Moon task,
   workflow, test, and generated CRD path. If it is kept, make sure the chart
   metadata and rendered resource names still describe the new operator.

6. Configure the release and repository settings for the new repository.

   ```sh
   uv run .github/scripts/configure_github_repo.py plan --repo OWNER/REPO
   uv run .github/scripts/configure_github_repo.py apply --repo OWNER/REPO
   ```

7. Refresh module metadata after code and import changes:

   ```sh
   go mod tidy
   ```

8. Run the full local check set:

   ```sh
   moon run root:check
   moon run root:test
   git diff --check
   ```

9. Delete this file:

   ```sh
   rm DELETE_ME.md
   ```

## Project Identity Checklist

- `go.mod`
  - Change `module github.com/meigma/template-k8s` to the new module path.

- `PROJECT`
  - Update Kubebuilder `domain`, `projectName`, `repo`, API `group`, `kind`,
    and API package `path`.
  - Keep this file generated/tool-owned in spirit; use it as Kubebuilder
    metadata, not as user-facing docs.

- `moon.yml`
  - Update project `title`, `description`, `owner`, and `maintainers`.
  - Update chart paths, release names, namespaces, Kind cluster names, default
    local image names, check paths, deploy/undeploy paths, and chart validation
    assertions when the chart or operator name changes.

- `README.md`
  - Replace bracketed placeholders with the real project name, API group,
    kind, namespace, repository, chart path, and examples.
  - Add the real license name after license files are added.

- `CHANGELOG.md`
  - Remove the template repository release history before the first real
    release, or replace it with the new project's initial release notes.

- `.release-please-manifest.json`
  - Reset the version state for the new repository before Release Please owns
    the first generated-project release.

## API And Reconciler Checklist

- `api/v1alpha1/groupversion_info.go`
  - Replace `example.meigma.io` with the real API group.
  - Update the package comment so it describes the new API group.

- `api/v1alpha1/nginxdeployment_types.go`
  - Replace `NginxDeployment`, `NginxDeploymentSpec`,
    `NginxDeploymentStatus`, and `NginxDeploymentList`.
  - Replace nginx-specific fields, defaults, comments, and validation markers.
  - Keep `metav1.Condition` status unless the new API has a clear reason not
    to expose conditions.
  - Keep bounded inline data rules if the new controller copies spec data into
    Kubernetes objects.

- `api/v1alpha1/zz_generated.deepcopy.go`
  - Do not edit this file by hand.
  - Regenerate it with `moon run root:generate` after API type changes.

- `internal/controller/nginxdeployment_controller.go`
  - Replace the sample nginx reconciler with the real controller.
  - Replace imports that point at `github.com/meigma/template-k8s`.
  - Replace `example.meigma.io` RBAC markers, resource names, status reasons,
    controller names, event names, labels, annotations, default image/config,
    child resource builders, and readiness projection logic.
  - Keep the useful patterns: controller references, `.Owns(...)` watches,
    freshness checks before trusting child status, bounded labels, and no-op
    status patches.

- `cmd/setup.go`
  - Register the real API scheme.
  - Wire the real reconciler, metrics, and event recorder names.
  - Update setup log context values that still say `nginxdeployment`.

- `cmd/manager.go`
  - Replace `leaderElectionID` with a unique value for the new operator, usually
    a stable suffix under the new API or company domain.

- `internal/controller/telemetry/metrics.go`
  - Rename metric namespace, subsystem, exported metric constants, help text,
    child resource labels, and initialized status reason series.
  - Keep metric labels finite. Do not add namespace, name, UID, image, or
    arbitrary spec values as labels.

- `internal/controller/telemetry/recorder.go`
  - Update event reasons and child resource display names for the new
    controller's owned objects.

## Helm Chart And Manifest Checklist

- `charts/template-k8s/Chart.yaml`
  - Update `description`, `home`, `sources`, and `maintainers`.
  - Decide whether to reset `version` and `appVersion` before the first release.
  - Keep `name: chart` only if the chart should continue publishing as
    `oci://ghcr.io/OWNER/REPO/chart`.

- `charts/template-k8s/values.yaml`
  - Update `image.repository`.
  - Update `kyverno.imageVerification.attestor.subjectRegExp` so optional
    Kyverno image verification trusts the generated repository's release
    workflow.
  - Add, remove, or rename values for real controller runtime options.
  - Keep fixed image tags or digests; do not default to `latest`.

- `charts/template-k8s/values.schema.json`
  - Update the schema title.
  - Keep the schema aligned with every public value in `values.yaml`.

- `charts/template-k8s/templates/_helpers.tpl`
  - Rename the `template-k8s.*` helper namespace.
  - Replace the default app name.
  - Rename custom resource role helper names such as
    `nginxDeploymentAdminRoleName`.

- `charts/template-k8s/templates/controller-deployment.yaml`
  - Confirm manager args match the real runtime flags.
  - Update helper references if the helper namespace changes.
  - Keep restricted-compatible security settings unless the new operator has a
    documented reason to change them.

- `charts/template-k8s/templates/kyverno-image-policy.yaml`
  - Update the policy name helper and default attestor subject if the chart or
    release workflow identity changes.
  - Keep it optional unless Kyverno is a hard prerequisite for the generated
    repository.

- `charts/template-k8s/templates/rbac-manager.yaml`
  - Replace the custom resource API group, resource, `/status` resource, and
    manager permissions with the real reconciler's reads and writes.
  - Keep RBAC markers and chart RBAC in sync through the chart RBAC drift test.

- `charts/template-k8s/templates/rbac-nginxdeployment-roles.yaml`
  - Rename this file if the sample API is renamed.
  - Replace custom resource admin/editor/viewer roles with the new API group and
    resource names, or delete these roles if the new project should not ship
    user-facing custom resource roles.

- `charts/template-k8s/templates/rbac-leader-election.yaml`,
  `rbac-metrics.yaml`, `rbac-metrics-auth.yaml`, `metrics-service.yaml`,
  `serviceaccount.yaml`, and `validate-values.yaml`
  - Update helper references if the helper namespace changes.
  - Keep names and labels aligned with the new chart identity.

- `charts/template-k8s/crds/*`
  - Regenerate with `moon run root:generate` after API marker changes.
  - Do not hand-edit generated CRDs except to diagnose generator output.

## Release And Repository Automation Checklist

- `.goreleaser.yaml`
  - Update `project_name`, build IDs, archive IDs, binary name, and `main` path
    if the manager command path changes.

- `.github/scripts/stage_release_assets.py`
  - Update the default `--binary-name` if the released manager binary is not
    named after the new repository.

- `.github/scripts/test_stage_release_assets.py`
  - Update expected release asset names, checksum fixtures, and binary-name
    assertions.

- `.github/workflows/release.yml`
  - Update `IMAGE_NAME`, `CHART_NAME`, `CHART_REF`, and `CHART_REPOSITORY`.
  - Update binary smoke-test names and temp file names.
  - Update OCI labels, especially image title and description.
  - Update Docker cache scopes.
  - Update Helm chart paths, rendered-output assertions, install examples, and
    release inspection summary commands.

- `.github/workflows/release-dry-run.yml`
  - Update image and chart refs.
  - Update binary validation names, dry-run image names, OCI archive names,
    cache scopes, chart paths, and rendered-output assertions.

- `.github/workflows/security-scan.yml`
  - Update local scan image tag, Docker cache scope, scan image ref, and SARIF
    category if the category should include the project name.

- `.github/workflows/release-please.yml`
  - Confirm release app variable and secret names.
  - Confirm the release app has protected-tag bypass before the first release.

- `release-please-config.json`
  - Update `package-name`.
  - Update the chart `extra-files` path if the chart directory is renamed.
  - Confirm the release type and changelog sections match the new project.

- `.github/repository-settings.toml`
  - Set `is_template` to the intended value for the generated repository.
  - Confirm `default_branch`.
  - Confirm the release app slug in the tag ruleset bypass.
  - Confirm required status check contexts match the workflows that remain.
  - Include `Helm Chart Dry Run` as a protected check if chart publication
    should be required before release PRs merge.

- `.github/dependabot.yml`
  - Confirm package ecosystems, labels, and schedules.
  - Add or remove ecosystems if the generated repository adds docs, frontend,
    Terraform, or other dependency surfaces.

## Local Development Stack Checklist

- `dev/ctlptl.yaml`
  - Rename the Kind cluster from `kind-template-k8s-dev`.
  - Rename the local registry from `template-k8s-registry` and choose an
    available local port if `127.0.0.1:5005` conflicts.

- `Tiltfile`
  - Update the allowed Kubernetes context, Helm chart path, release name,
    namespace, image selector, and controller Deployment name.
  - Keep Tilt scoped to the local development context; use `ctlptl` tasks for
    cluster lifecycle.

- `.ko.yaml`
  - Confirm the manager entrypoint remains `./cmd`.
  - Confirm the local development base image matches the operator's runtime
    expectations.

- `dev/ko-build.sh`
  - Update the image build entrypoint when the manager package moves.

- `moon.yml`
  - Update `dev-*` task inputs and commands if the generated repository renames
    the chart, sample fixture, manager image, or cluster context.

## Test And Sample Checklist

- `internal/controller/nginxdeployment_controller_test.go`
  - Rename the test file and suite descriptions for the real controller.
  - Update import paths, sample API objects, expected child resources, status
    assertions, metrics/events, and helper names.
  - Keep manager-backed envtest coverage that proves `.For(...)`, `.Owns(...)`,
    watches, predicates, and indexes are actually wired.

- `internal/controller/suite_test.go`
  - Update API imports and CRD directory paths.
  - Keep `setup-envtest` usage through `moon run root:test`.

- `test/chart/rbac_test.go`
  - Update Helm release name, chart path, namespace, and expected manager role
    name.
  - Keep the chart RBAC comparison against controller-gen output.

- `test/chainsaw/chainsaw-config.yaml`
  - Rename the e2e test suite from `template-k8s-e2e`.

- `test/chainsaw/nginx-smoke/chainsaw-test.yaml`
  - Rename the directory and test.
  - Replace API group, kind, object names, namespace constants, image defaults,
    metrics checks, event checks, debug commands, and cleanup commands.
  - Keep Chainsaw focused on installed-operator behavior; do not copy the full
    envtest behavior matrix into e2e tests.

- `test/chainsaw/fixtures/example_v1alpha1_nginxdeployment.yaml`
  - Rename the fixture.
  - Replace API group, kind, labels, object name, spec fields, image defaults,
    config content, and sample response text.

## Agent Guidance Checklist

- `AGENTS.md` and `CLAUDE.md`
  - Update the repository overview, chart path, current API, owned resources,
    and generated CRD path.
  - Keep the Moon task front door and envtest/Chainsaw testing boundary unless
    the generated repository intentionally changes them.

- `.agents/skills/k8s-operator/SKILL.md`
  - Update sample snippets that mention `NginxDeployment`,
    `example.meigma.io`, `template-k8s`, or nginx if the generated repository
    keeps repo-local operator guidance.
  - Keep the skill concise and workflow-oriented. Move deeper reference
    material under `references/` only if it becomes necessary.

- `.session.md` and lifecycle skills under `.agents/skills/session-*`
  - Keep these files unless the generated repository will not use the local
    journal/session protocol.

## Artifact Shape Checklist

The default template ships all of these artifact classes:

- Go manager binaries from GoReleaser.
- A GHCR container image.
- An OCI Helm chart.

For the normal operator case, keep all three. If the generated repository
intentionally changes shape:

- Binary plus container plus chart: keep the default release shape and update
  names.
- Container plus chart only: remove GoReleaser release assets and binary dry-run
  checks if users should not install a standalone manager binary.
- Library or API-only module: remove container, Helm, GoReleaser, Chainsaw, and
  release publication pieces that no longer apply.
- No releases yet: remove or pause Release Please and release workflows until
  the repository has a real artifact contract.

Whenever a release job is removed, also remove its required check from
`.github/repository-settings.toml`.

## Final Validation

After the repository is renamed and the real API is in place, run:

```sh
go mod tidy
moon run root:generate
moon run root:check
moon run root:test
git diff --check
```

Then search for leftover template data:

```sh
rg "template-k8s|template_k8s|github.com/meigma/template-k8s|example\\.meigma\\.io|NginxDeployment|nginx"
```

Matches in generated-project code, manifests, tests, workflows, docs, or agent
guidance should be intentional. When only intentional references remain, delete
this file:

```sh
rm DELETE_ME.md
```
