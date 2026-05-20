package main

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	examplev1alpha1 "github.com/meigma/template-k8s/api/v1alpha1"
	"github.com/meigma/template-k8s/internal/controller"
	"github.com/meigma/template-k8s/internal/controller/telemetry"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

// init registers the core Kubernetes and example.meigma.io v1alpha1 types with
// the shared runtime scheme used by the manager.
func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(examplev1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// mustRegisterControllers constructs and registers every reconciler with the
// manager, wiring up controller telemetry against controller-runtime's
// metrics registry. It exits the process on failure.
func mustRegisterControllers(mgr manager.Manager) {
	controllerMetrics, err := telemetry.NewMetrics(crmetrics.Registry)
	exitOnError(err, "Failed to register controller metrics", "controller", "nginxdeployment")

	eventRecorder := mgr.GetEventRecorderFor("nginxdeployment-controller") //nolint:staticcheck
	err = (&controller.NginxDeploymentReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Telemetry: telemetry.NewRecorder(controllerMetrics, eventRecorder),
	}).SetupWithManager(mgr)
	exitOnError(err, "Failed to create controller", "controller", "nginxdeployment")
	// +kubebuilder:scaffold:builder
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

// exitOnError logs the supplied message together with the structured key/value
// pairs and terminates the process with a non-zero status when err is non-nil.
// It is the canonical failure shape for startup-time helpers in this package.
func exitOnError(err error, msg string, keysAndValues ...any) {
	if err == nil {
		return
	}

	setupLog.Error(err, msg, keysAndValues...)
	os.Exit(1)
}
