package operator

import "context"

// Values carries optional install overrides. It is reserved for a later phase:
// the ssa adapter applies a build-time-rendered manifest with image references
// already pinned, so it does not consume Values today. The field is kept on
// InstallSpec to match the design contract and the lifecycle caller that will
// supply image overrides once configurable installs are supported.
type Values map[string]string

// InstallSpec describes a requested operator install.
type InstallSpec struct {
	// Namespace is the namespace the operator is installed into. This phase
	// pins it to the chart's render namespace; the adapter rejects any other
	// value because the chart's RBAC subjects are baked to that namespace.
	Namespace string

	// Version is the operator version the caller intends to install. It is
	// optional: when empty the adapter uses the version stamped into its
	// embedded manifests, which is the single source of truth.
	Version string

	// Values carries optional install overrides; reserved (see Values).
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
	// cluster.
	OperatorState(ctx context.Context) (State, error)
}
