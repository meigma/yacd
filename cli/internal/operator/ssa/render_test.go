package ssa

import (
	"sort"
	"testing"

	"github.com/meigma/yacd/charts"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestRenderDigestPinnedImages asserts the default render is digest-pinned: the
// manager container image carries the manager digest and the
// --default-faucet-image arg carries the faucet digest.
func TestRenderDigestPinnedImages(t *testing.T) {
	const (
		wantManagerImage = "ghcr.io/meigma/yacd@sha256:5d53ca824dacad39c482dc93edfd2db4a65d5803f43dce5b18b1a7482b0f8e21"
		wantFaucetArg    = "--default-faucet-image=ghcr.io/meigma/yacd/faucet@sha256:826f8d52f0a4b0f607e2293cf72a8217de27700b5e5f1b35e1af86ef18fd3f66"
	)

	deployment := findObject(t, renderDefault(t, installNamespace), "Deployment", "yacd-controller-manager")

	containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	require.NoError(t, err)
	require.True(t, found, "deployment must declare containers")
	require.Len(t, containers, 1)

	manager, ok := containers[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, wantManagerImage, manager["image"], "manager image is digest-pinned")

	args, found, err := unstructured.NestedStringSlice(manager, "args")
	require.NoError(t, err)
	require.True(t, found, "manager must carry args")
	assert.Contains(t, args, wantFaucetArg, "faucet digest is threaded into --default-faucet-image")
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

// TestRenderImageTagOverrideKeepsDigestPin proves the headline Model-A invariant
// end to end through the renderer: with a user --set image.tag override present
// (folded through Extra exactly as the install path does), the manager container
// image is STILL the embedded digest, because the chart renders repository@digest
// and Default()'s digest survives the deep-merge. This guards against a future
// ToHelmValues merge change silently letting a tag override repoint the image.
func TestRenderImageTagOverrideKeepsDigestPin(t *testing.T) {
	const wantManagerImage = "ghcr.io/meigma/yacd@sha256:5d53ca824dacad39c482dc93edfd2db4a65d5803f43dce5b18b1a7482b0f8e21"

	objs, err := renderWithExtra(t, map[string]any{"image": map[string]any{"tag": "v9.9.9"}})
	require.NoError(t, err)

	deployment := findObject(t, objs, "Deployment", "yacd-controller-manager")
	containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	require.NoError(t, err)
	require.True(t, found, "deployment must declare containers")
	require.Len(t, containers, 1)

	manager, ok := containers[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, wantManagerImage, manager["image"],
		"a --set image.tag override must be shadowed by the surviving digest pin")
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
