package main

import (
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestFoundationManagerConstruction proves the current operator shell can
// construct a controller-runtime manager against envtest, register its API
// types, and register the CardanoNetwork controller scaffold.
func TestFoundationManagerConstruction(t *testing.T) {
	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.Eventually(t, func() bool {
			return testEnv.Stop() == nil
		}, time.Minute, time.Second)
	})

	mgr, err := ctrl.NewManager(cfg, newManagerOptions(managerOptions{
		MetricsBindAddress:     "0",
		HealthProbeBindAddress: ":0",
		MetricsSecure:          true,
	}))
	require.NoError(t, err)
	require.NoError(t, registerControllers(mgr))

	_, _, err = scheme.ObjectKinds(&corev1.Pod{})
	require.NoError(t, err)

	_, _, err = scheme.ObjectKinds(&yacdv1alpha1.CardanoNetwork{})
	require.NoError(t, err)
}
