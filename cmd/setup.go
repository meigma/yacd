package main

import (
	"os"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/controller/cardanonetwork"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

// init registers the core Kubernetes types with the shared runtime scheme used
// by the manager plus YACD API types.
func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(yacdv1alpha1.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

// mustRegisterControllers registers every reconciler with the manager and
// exits the process on failure.
func mustRegisterControllers(mgr manager.Manager, options managerOptions) {
	exitOnError(registerControllers(mgr, options), "Failed to register controllers")
}

// registerControllers constructs and registers every reconciler with the
// manager.
func registerControllers(mgr manager.Manager, options managerOptions) error {
	err := (&cardanonetwork.CardanoNetworkReconciler{
		Client:             mgr.GetClient(),
		Reader:             mgr.GetAPIReader(),
		Scheme:             mgr.GetScheme(),
		DefaultFaucetImage: options.DefaultFaucetImage,
	}).SetupWithManager(mgr)
	if err != nil {
		return err
	}

	// +kubebuilder:scaffold:builder
	return nil
}

// mustRegisterHealthChecks registers the /healthz and /readyz endpoints with
// the manager and exits the process if either probe fails to register.
func mustRegisterHealthChecks(mgr manager.Manager) {
	exitOnError(mgr.AddHealthzCheck("healthz", healthz.Ping), "Failed to set up health check")
	exitOnError(mgr.AddReadyzCheck("readyz", healthz.Ping), "Failed to set up ready check")
}

// mustStartManager runs the controller-runtime manager with the standard
// signal handler attached and exits the process if Start returns an error.
func mustStartManager(mgr manager.Manager) {
	setupLog.Info("Starting manager")
	exitOnError(mgr.Start(ctrl.SetupSignalHandler()), "Failed to run manager")
}

// exitOnError logs the supplied message and terminates the process with a
// non-zero status when err is non-nil. It is the canonical failure shape for
// startup-time helpers in this package.
func exitOnError(err error, msg string) {
	if err == nil {
		return
	}

	setupLog.Error(err, msg)
	os.Exit(1)
}
