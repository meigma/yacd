package operator

import "context"

// InstallSpec describes a requested operator install.
type InstallSpec struct {
	// Namespace is the namespace the operator is installed into. It is a real
	// render input: it sets the Helm release namespace, so the rendered RBAC
	// subjects, ServiceAccount, Role/RoleBinding, Service, and Deployment all
	// follow it. Empty defaults to "yacd-system"; any other value must be a
	// valid DNS-1123 label.
	Namespace string

	// Values carries typed install overrides merged onto the chart defaults
	// before rendering. The zero value renders the chart at its own defaults;
	// callers that want the pinned, digest-tagged baseline pass Default().
	Values Values
}

// State is the observed operator install state in a cluster.
type State struct {
	// Installed reports whether the operator manager Deployment exists.
	Installed bool

	// Ready reports whether the manager Deployment is Available for its
	// current generation. It can only be true against a cluster that runs
	// workload controllers (not envtest).
	Ready bool

	// Version is the installed operator version, read from the manager
	// Deployment's app.kubernetes.io/version label. Empty when not installed
	// or when the label is absent.
	Version string
}

// Installer installs or upgrades the YACD operator into a cluster and reports
// its state. Implementations reconcile against the cluster on every
// EnsureOperator call so the operation is idempotent and re-entrant.
type Installer interface {
	// EnsureOperator reconciles the cluster to the embedded operator version:
	// it installs when absent, upgrades an older same-major install, re-applies
	// an equal version to heal drift, and refuses a newer or major-mismatched
	// in-cluster version with an actionable error. It applies CRDs first
	// (waiting until Established), then the workload, under the CLI field owner,
	// and prunes objects no longer in the manifest set. The returned State
	// reflects the cluster after the apply.
	EnsureOperator(ctx context.Context, spec InstallSpec) (State, error)

	// OperatorState reports the current install state without mutating the
	// cluster. It reads the default install namespace ("yacd-system") until
	// this port grows a namespace argument; a non-default install (one created
	// with InstallSpec.Namespace set) is not yet visible to this read path.
	OperatorState(ctx context.Context) (State, error)
}
