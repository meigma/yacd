package ssa

import (
	"context"
	"testing"

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

	return &installer{client: c, mapper: c.RESTMapper(), manifests: Manifests}, c
}

func TestEnsureOperatorInstallsFromEmbeddedManifests(t *testing.T) {
	ctx := context.Background()
	inst, c := newInstaller(t)

	state, err := inst.EnsureOperator(ctx, operator.InstallSpec{})
	require.NoError(t, err)

	assert.True(t, state.Installed, "manager Deployment should exist")
	assert.Equal(t, "v0.1.1", state.Version, "version read from the manager Deployment label")
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
	assert.Equal(t, "v0.1.1", state.Version)
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

func TestEnsureOperatorRejectsForeignNamespace(t *testing.T) {
	ctx := context.Background()
	inst, _ := newInstaller(t)

	_, err := inst.EnsureOperator(ctx, operator.InstallSpec{Namespace: "elsewhere"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pinned")
}
