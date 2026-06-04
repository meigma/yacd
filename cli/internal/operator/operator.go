package operator

import "context"

// DefaultNamespace is the namespace the operator is installed into when the
// caller leaves InstallSpec.Namespace empty. It is the single source of truth
// for that default: the SSA adapter and the install command both defer to it,
// and it is no longer a hard pin — any valid DNS-1123 namespace renders
// correctly because the chart's RBAC subjects follow the Helm release namespace.
const DefaultNamespace = "yacd-system"

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

// Decision is the outcome of a dry-run install plan: the action the next
// EnsureOperator would take, plus the versions that drove it. It is what the
// install command prints under --dry-run without mutating the cluster.
type Decision struct {
	// Action is the install action Decide selected from the observed and
	// embedded versions.
	Action Action

	// InstalledVersion is the in-cluster operator version, or empty when the
	// operator is not installed.
	InstalledVersion string

	// TargetVersion is the embedded operator version this CLI would apply.
	TargetVersion string
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

	// Plan reports the action the next EnsureOperator would take for spec,
	// without mutating the cluster: it renders the embedded chart for the
	// target version, reads the install state in the resolved namespace, and
	// runs the same Decide policy. A would-refuse plan returns the typed Decide
	// error alongside the Decision, so callers can surface actionable guidance.
	Plan(ctx context.Context, spec InstallSpec) (Decision, error)

	// OperatorState reports the current install state of the operator in the
	// given namespace without mutating the cluster. An empty namespace defaults
	// to DefaultNamespace.
	OperatorState(ctx context.Context, namespace string) (State, error)
}
