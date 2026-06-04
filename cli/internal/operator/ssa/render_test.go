package ssa

import (
	"sort"
	"testing"

	"github.com/meigma/yacd/charts"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart/loader"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// objectIdentity is "Kind/name", the stable identity used to assert the rendered
// object set independent of render order (engine.Render returns a Go map).
func objectIdentity(obj *unstructured.Unstructured) string {
	return obj.GetKind() + "/" + obj.GetName()
}

func renderDefault(t *testing.T, namespace string) []*unstructured.Unstructured {
	t.Helper()
	objs, err := render(charts.OperatorChart, namespace, operator.Default().ToHelmValues())
	require.NoError(t, err)
	return objs
}

// TestRenderDefaultObjectSet asserts the embedded chart at operator.Default()
// renders exactly the 12 expected objects (both CRDs included), the headline
// invariant the install pipeline relies on.
func TestRenderDefaultObjectSet(t *testing.T) {
	objs := renderDefault(t, installNamespace)

	got := make([]string, 0, len(objs))
	for _, obj := range objs {
		got = append(got, objectIdentity(obj))
	}
	sort.Strings(got)

	want := []string{
		"ClusterRole/yacd-manager-role",
		"ClusterRole/yacd-metrics-auth-role",
		"ClusterRole/yacd-metrics-reader",
		"ClusterRoleBinding/yacd-manager-rolebinding",
		"ClusterRoleBinding/yacd-metrics-auth-rolebinding",
		"CustomResourceDefinition/cardanodbsyncs.yacd.meigma.io",
		"CustomResourceDefinition/cardanonetworks.yacd.meigma.io",
		"Deployment/yacd-controller-manager",
		"Role/yacd-leader-election-role",
		"RoleBinding/yacd-leader-election-rolebinding",
		"Service/yacd-controller-manager-metrics-service",
		"ServiceAccount/yacd-controller-manager",
	}
	sort.Strings(want)

	assert.Equal(t, want, got, "rendered object set must be exactly the expected 12")
}

// TestRenderDefaultImageUsesAppVersionTag asserts the default render pins the
// manager image to the chart's appVersion (repository:appVersion). The expected
// tag is read from the embedded chart so the test stays correct across release
// bumps rather than hardcoding a version.
func TestRenderDefaultImageUsesAppVersionTag(t *testing.T) {
	wantManagerImage := "ghcr.io/meigma/yacd:" + embeddedChartAppVersion(t)

	got := managerImage(t, renderDefault(t, installNamespace))
	assert.Equal(t, wantManagerImage, got, "manager image defaults to repository:appVersion")
}

// TestRenderPresenceOfCoreObjects double-checks the metrics Service and the
// three ClusterRoles are present (the RBAC surface the operator needs).
func TestRenderPresenceOfCoreObjects(t *testing.T) {
	objs := renderDefault(t, installNamespace)

	findObject(t, objs, "Service", "yacd-controller-manager-metrics-service")
	findObject(t, objs, "ClusterRole", "yacd-manager-role")
	findObject(t, objs, "ClusterRole", "yacd-metrics-auth-role")
	findObject(t, objs, "ClusterRole", "yacd-metrics-reader")
}

// renderWithExtra renders the chart at Default() with the given Extra override
// layer folded in, exercising the same ToHelmValues -> render path the install
// command uses for -f/--set values.
func renderWithExtra(t *testing.T, extra map[string]any) ([]*unstructured.Unstructured, error) {
	t.Helper()
	vals := operator.Default()
	vals.Extra = extra
	return render(charts.OperatorChart, installNamespace, vals.ToHelmValues())
}

// TestRenderOverrideReachesDeployment proves a user override (replicaCount=2 via
// Extra) flows through ToHelmValues and the renderer into the manager
// Deployment's spec.replicas, confirming -f/--set values actually take effect.
func TestRenderOverrideReachesDeployment(t *testing.T) {
	objs, err := renderWithExtra(t, map[string]any{"replicaCount": 2})
	require.NoError(t, err)

	deployment := findObject(t, objs, "Deployment", "yacd-controller-manager")
	// The rendered YAML round-trips numbers through the unstructured decoder, so
	// spec.replicas arrives as a float64; compare its numeric value.
	replicas, found, err := unstructured.NestedFieldNoCopy(deployment.Object, "spec", "replicas")
	require.NoError(t, err)
	require.True(t, found, "deployment must declare replicas")
	assert.EqualValues(t, 2, replicas, "override replicaCount must reach the rendered Deployment")
}

// TestRenderImageDigestOverridePinsDigest proves an explicit image.digest
// override still produces a digest-pinned render: the chart's yacd.image helper
// prefers digest over tag, so a deliberate digest pin remains available even
// though the default now tracks the chart appVersion.
func TestRenderImageDigestOverridePinsDigest(t *testing.T) {
	const digest = "sha256:5d53ca824dacad39c482dc93edfd2db4a65d5803f43dce5b18b1a7482b0f8e21"
	const wantManagerImage = "ghcr.io/meigma/yacd@" + digest

	objs, err := renderWithExtra(t, map[string]any{"image": map[string]any{"digest": digest}})
	require.NoError(t, err)

	assert.Equal(t, wantManagerImage, managerImage(t, objs),
		"an explicit image.digest override pins the manager image to repository@digest")
}

// TestRenderImageTagOverrideRepointsImage proves a --set image.tag override now
// repoints the manager image to repository:tag. With the default install no
// longer digest-pinned, a tag override is honored rather than shadowed; it is an
// unsupported configuration, not blocked here.
func TestRenderImageTagOverrideRepointsImage(t *testing.T) {
	const wantManagerImage = "ghcr.io/meigma/yacd:v9.9.9"

	objs, err := renderWithExtra(t, map[string]any{"image": map[string]any{"tag": "v9.9.9"}})
	require.NoError(t, err)

	assert.Equal(t, wantManagerImage, managerImage(t, objs),
		"a --set image.tag override repoints the manager image")
}

// embeddedChartAppVersion loads the embedded operator chart and returns its
// declared appVersion, so image-pin assertions track the chart rather than a
// hardcoded release.
func embeddedChartAppVersion(t *testing.T) string {
	t.Helper()
	files, err := bufferedFiles(charts.OperatorChart)
	require.NoError(t, err)
	ch, err := loader.LoadFiles(files)
	require.NoError(t, err)
	require.NotEmpty(t, ch.Metadata.AppVersion, "chart must declare an appVersion")
	return ch.Metadata.AppVersion
}

// managerImage returns the single manager container image string from a rendered
// object set, failing the test if the Deployment or container is missing.
func managerImage(t *testing.T, objs []*unstructured.Unstructured) string {
	t.Helper()
	deployment := findObject(t, objs, "Deployment", "yacd-controller-manager")
	containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	require.NoError(t, err)
	require.True(t, found, "deployment must declare containers")
	require.Len(t, containers, 1)
	manager, ok := containers[0].(map[string]any)
	require.True(t, ok)
	image, ok := manager["image"].(string)
	require.True(t, ok, "manager container must declare an image string")
	return image
}

// TestRenderSchemaInvalidOverrideFails proves a schema-violating override fails
// fast at render time with a schema-validation error rather than producing a
// malformed object set. The chart schema types replicaCount as an integer, so a
// string value is rejected before any template renders.
func TestRenderSchemaInvalidOverrideFails(t *testing.T) {
	_, err := renderWithExtra(t, map[string]any{"replicaCount": "not-an-int"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema validation",
		"a schema-violating override must fail with a schema-validation error")
}

// findObject returns the single object of the given kind/name, failing the test
// if absent.
func findObject(t *testing.T, objs []*unstructured.Unstructured, kind, name string) *unstructured.Unstructured {
	t.Helper()
	for _, obj := range objs {
		if obj.GetKind() == kind && obj.GetName() == name {
			return obj
		}
	}
	t.Fatalf("object %s/%s not found in rendered set", kind, name)
	return nil
}
