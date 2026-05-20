package main

import (
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	ctrl "sigs.k8s.io/controller-runtime"
)

// main is the manager entrypoint. It parses command-line options, wires the
// controller-runtime logger, constructs the manager, registers controllers and
// health checks, and blocks until the manager stops.
func main() {
	options := mustParseManagerOptions(os.Args[1:])
	ctrl.SetLogger(mustNewControllerLogger(options, os.Stderr))

	mgr := mustNewManager(options)
	mustRegisterControllers(mgr)
	mustRegisterHealthChecks(mgr)
	mustStartManager(mgr)
}
