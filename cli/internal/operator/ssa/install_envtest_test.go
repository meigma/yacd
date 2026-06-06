package ssa

import (
	"context"
	"testing"

	"github.com/meigma/yacd/charts"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// newInstaller starts an envtest API server with NO preloaded CRDs and returns
// an installer wired to it, so EnsureOperator exercises the real CRD-apply and
// Established-wait paths.
func newInstaller(t *testing.T) (*installer, crclient.Client) {
	t.Helper()

	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	require.NoError(t, err, "start envtest")
	t.Cleanup(func() {
		assert.NoError(t, testEnv.Stop(), "stop envtest")
	})

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, apiextensionsv1.AddToScheme(scheme))

	c, err := crclient.New(cfg, crclient.Options{Scheme: scheme})
	require.NoError(t, err, "create client")

	return &installer{client: c, mapper: c.RESTMapper(), chart: charts.OperatorChart}, c
}

func TestEnsureOperatorInstallsFromEmbeddedChart(t *testing.T) {
	ctx := context.Background()
	inst, c := newInstaller(t)

	state, err := inst.EnsureOperator(ctx, operator.InstallSpec{})
	require.NoError(t, err)

	assert.True(t, state.Installed, "manager Deployment should exist")
	assert.Equal(t, embeddedChartAppVersion(t), state.Version, "version read from the manager Deployment label")
	// envtest has no kube-controller-manager, so the Deployment never becomes
	// Available; live readiness is proven by the P6 k3d e2e, not here.
	assert.False(t, state.Ready, "envtest cannot make a Deployment Available")

	// CRDs were applied and reached Established (EnsureOperator waits for it).
	crd := &apiextensionsv1.CustomResourceDefinition{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Name: "cardanonetworks.yacd.meigma.io"}, crd))
	assert.True(t, establishedTrue(crd), "CRD should be Established")

	// Namespaced objects were defaulted into yacd-system.
	deployment := &appsv1.Deployment{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: installNamespace, Name: "yacd-controller-manager"}, deployment))
	sa := &corev1.ServiceAccount{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: installNamespace, Name: "yacd-controller-manager"}, sa))

	// Cluster-scoped objects carry no namespace.
	clusterRole := &rbacv1.ClusterRole{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Name: "yacd-manager-role"}, clusterRole))
	assert.Empty(t, clusterRole.Namespace)

	// The manager Deployment carries the install prune label.
	assert.Equal(t, pruneLabelValue, deployment.Labels[pruneLabelKey])
}

func TestEnsureOperatorIsIdempotent(t *testing.T) {
	ctx := context.Background()
	inst, _ := newInstaller(t)

	_, err := inst.EnsureOperator(ctx, operator.InstallSpec{})
	require.NoError(t, err)

	state, err := inst.EnsureOperator(ctx, operator.InstallSpec{})
	require.NoError(t, err, "re-apply of an equal version must be a no-op")
	assert.True(t, state.Installed)
	assert.Equal(t, embeddedChartAppVersion(t), state.Version)
}

func TestEnsureOperatorPrunesStrayManagedObject(t *testing.T) {
	ctx := context.Background()
	inst, c := newInstaller(t)

	_, err := inst.EnsureOperator(ctx, operator.InstallSpec{})
	require.NoError(t, err)

	stray := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stray-metrics",
			Namespace: installNamespace,
			Labels:    map[string]string{pruneLabelKey: pruneLabelValue},
		},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 8080}}},
	}
	require.NoError(t, c.Create(ctx, stray))

	_, err = inst.EnsureOperator(ctx, operator.InstallSpec{})
	require.NoError(t, err)

	err = c.Get(ctx, crclient.ObjectKey{Namespace: installNamespace, Name: "stray-metrics"}, &corev1.Service{})
	assert.True(t, apierrors.IsNotFound(err), "stray labeled Service should be pruned")

	// A real chart Service is retained.
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: installNamespace, Name: "yacd-controller-manager-metrics-service"}, &corev1.Service{}))
}

func TestEnsureOperatorRefusesNewerInstalledVersion(t *testing.T) {
	ctx := context.Background()
	inst, c := newInstaller(t)

	_, err := inst.EnsureOperator(ctx, operator.InstallSpec{})
	require.NoError(t, err)

	// Simulate a newer same-major operator already in the cluster.
	deployment := &appsv1.Deployment{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: installNamespace, Name: "yacd-controller-manager"}, deployment))
	deployment.Labels[versionLabel] = "v0.9.9"
	require.NoError(t, c.Update(ctx, deployment))

	state, err := inst.EnsureOperator(ctx, operator.InstallSpec{})
	require.ErrorIs(t, err, operator.ErrNewerOperator)
	assert.Equal(t, "v0.9.9", state.Version, "observed state is returned alongside the refusal")
}

func TestPlanOnEmptyClusterReportsInstallAndMutatesNothing(t *testing.T) {
	ctx := context.Background()
	inst, c := newInstaller(t)

	// Plan's load-bearing contract: it renders + reads + Decides without mutating
	// the cluster. On a fresh cluster it must report ActionInstall at the embedded
	// target version with no installed version and no error.
	decision, err := inst.Plan(ctx, operator.InstallSpec{})
	require.NoError(t, err)
	assert.Equal(t, operator.ActionInstall, decision.Action)
	assert.Equal(t, embeddedChartAppVersion(t), decision.TargetVersion)
	assert.Empty(t, decision.InstalledVersion, "nothing is installed yet")

	// And it applied nothing: no manager Deployment and no CRD exist afterward.
	err = c.Get(ctx, crclient.ObjectKey{Namespace: installNamespace, Name: "yacd-controller-manager"}, &appsv1.Deployment{})
	assert.True(t, apierrors.IsNotFound(err), "Plan must not create the manager Deployment")

	err = c.Get(ctx, crclient.ObjectKey{Name: "cardanonetworks.yacd.meigma.io"}, &apiextensionsv1.CustomResourceDefinition{})
	assert.True(t, apierrors.IsNotFound(err), "Plan must not apply CRDs")
}

func TestPlanRefusesNewerInstalledVersionWithoutMutating(t *testing.T) {
	ctx := context.Background()
	inst, c := newInstaller(t)

	// Seed a newer same-major operator so the policy refuses, then assert Plan
	// surfaces the typed refusal and the observed version while changing nothing.
	_, err := inst.EnsureOperator(ctx, operator.InstallSpec{})
	require.NoError(t, err)

	deployment := &appsv1.Deployment{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: installNamespace, Name: "yacd-controller-manager"}, deployment))
	deployment.Labels[versionLabel] = "v0.9.9"
	require.NoError(t, c.Update(ctx, deployment))

	decision, err := inst.Plan(ctx, operator.InstallSpec{})
	require.ErrorIs(t, err, operator.ErrNewerOperator)
	assert.Equal(t, operator.ActionRefuse, decision.Action)
	assert.Equal(t, "v0.9.9", decision.InstalledVersion)
	assert.Equal(t, embeddedChartAppVersion(t), decision.TargetVersion)

	// Plan did not touch the seeded Deployment: its version label still reads the
	// newer value, proving Plan never re-applied the embedded manifest set.
	after := &appsv1.Deployment{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: installNamespace, Name: "yacd-controller-manager"}, after))
	assert.Equal(t, "v0.9.9", after.Labels[versionLabel], "Plan must not overwrite the installed version")
}

func TestEnsureOperatorInstallsIntoRequestedNamespace(t *testing.T) {
	ctx := context.Background()
	inst, c := newInstaller(t)

	const ns = "elsewhere"
	state, err := inst.EnsureOperator(ctx, operator.InstallSpec{Namespace: ns})
	require.NoError(t, err)
	assert.True(t, state.Installed)

	// The Deployment and other namespaced objects land in the requested
	// namespace, and the rendered RBAC subjects follow it too.
	deployment := &appsv1.Deployment{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: ns, Name: "yacd-controller-manager"}, deployment))

	binding := &rbacv1.ClusterRoleBinding{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Name: "yacd-manager-rolebinding"}, binding))
	require.Len(t, binding.Subjects, 1)
	assert.Equal(t, ns, binding.Subjects[0].Namespace, "RBAC subject namespace follows the install namespace")
}

func TestEnsureOperatorRejectsInvalidNamespace(t *testing.T) {
	ctx := context.Background()
	inst, _ := newInstaller(t)

	_, err := inst.EnsureOperator(ctx, operator.InstallSpec{Namespace: "Not_Valid"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid install namespace")
}

// TestEnsureOperatorNamespaceFlexibility is the headline benefit of rendering
// the chart at install time: a non-"yacd-system" install keeps the namespaced
// objects and the RBAC subjects that reference them in agreement. It asserts
// the ServiceAccount, leader-election Role/RoleBinding, metrics Service, and
// Deployment all land in the requested namespace, AND that every RBAC subject
// (ClusterRoleBinding and RoleBinding) references that same namespace.
func TestEnsureOperatorNamespaceFlexibility(t *testing.T) {
	ctx := context.Background()
	inst, c := newInstaller(t)

	const ns = "yacd-test-ns"
	require.NotEqual(t, installNamespace, ns, "must install into a non-default namespace")

	state, err := inst.EnsureOperator(ctx, operator.InstallSpec{Namespace: ns})
	require.NoError(t, err)
	assert.True(t, state.Installed)
	assert.Equal(t, embeddedChartAppVersion(t), state.Version)

	// Namespaced objects land in the requested namespace.
	deployment := &appsv1.Deployment{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: ns, Name: "yacd-controller-manager"}, deployment))

	sa := &corev1.ServiceAccount{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: ns, Name: "yacd-controller-manager"}, sa))

	service := &corev1.Service{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: ns, Name: "yacd-controller-manager-metrics-service"}, service))

	leaderRole := &rbacv1.Role{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: ns, Name: "yacd-leader-election-role"}, leaderRole))

	leaderBinding := &rbacv1.RoleBinding{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Namespace: ns, Name: "yacd-leader-election-rolebinding"}, leaderBinding))

	// They are NOT in the default namespace (the old pin would have landed here).
	err = c.Get(ctx, crclient.ObjectKey{Namespace: installNamespace, Name: "yacd-controller-manager"}, &appsv1.Deployment{})
	assert.True(t, apierrors.IsNotFound(err), "nothing should land in the default namespace")

	// Every RBAC subject references the install namespace, so subjects and the
	// namespaced ServiceAccount they bind agree.
	managerBinding := &rbacv1.ClusterRoleBinding{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Name: "yacd-manager-rolebinding"}, managerBinding))
	assertSubjectsInNamespace(t, managerBinding.Subjects, ns)

	metricsAuthBinding := &rbacv1.ClusterRoleBinding{}
	require.NoError(t, c.Get(ctx, crclient.ObjectKey{Name: "yacd-metrics-auth-rolebinding"}, metricsAuthBinding))
	assertSubjectsInNamespace(t, metricsAuthBinding.Subjects, ns)

	assertSubjectsInNamespace(t, leaderBinding.Subjects, ns)
}

// assertSubjectsInNamespace asserts every ServiceAccount subject references the
// expected namespace, the agreement that lets RBAC bindings resolve the
// ServiceAccount that actually lives in the install namespace.
func assertSubjectsInNamespace(t *testing.T, subjects []rbacv1.Subject, namespace string) {
	t.Helper()
	require.NotEmpty(t, subjects, "binding must have subjects")
	for _, subject := range subjects {
		if subject.Kind != "ServiceAccount" {
			continue
		}
		assert.Equal(t, namespace, subject.Namespace, "ServiceAccount subject %q must reference the install namespace", subject.Name)
	}
}
