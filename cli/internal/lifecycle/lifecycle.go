package lifecycle

import (
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/devconfig"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/operator"
)

// Manager composes the local-lifecycle ports into the up/down/status flows.
// Construct it in the command layer from the option factories; the zero
// Reporter and context seams are filled with safe defaults on first use.
type Manager struct {
	// Provisioner creates, heals, deletes, and inspects the managed cluster.
	Provisioner cluster.Provisioner

	// State persists the supplementary cluster record and holds the process
	// lock that serializes mutating operations.
	State clusterstate.Store

	// NewInstaller builds an operator installer bound to a kubeconfig and
	// context. It is a factory because the target is only known after the
	// cluster is provisioned. In production it is operator/ssa.New.
	NewInstaller func(kubeconfig, kubeContext string) (operator.Installer, error)

	// NewNetworks builds a kube client bound to a target. It is the same
	// factory the existing verbs use (the command layer's KubeClientFactory).
	NewNetworks func(kube.Config) (kube.Client, error)

	// K3dVersion is recorded in the cluster state for bookkeeping. The command
	// layer sets it from the pinned k3d release.
	K3dVersion string

	// Report receives stepwise progress. Nil selects a no-op reporter.
	Report Reporter

	// CaptureContext returns the current kubectl context before provisioning
	// switches it, so Down can restore it. Nil selects kube.CurrentContext.
	CaptureContext func() (string, error)

	// RestoreContext sets the kubectl context in the given kubeconfig. Nil
	// selects kube.SetCurrentContext.
	RestoreContext func(kubeconfigPath, context string) error
}

// UpOptions parameterizes a devnet bring-up.
type UpOptions struct {
	// Bare stops after the operator install, skipping the default network.
	Bare bool

	// Env is the developer environment rendered into the default network. The
	// command layer supplies the embedded default.
	Env devconfig.Environment

	// NetworkName and Namespace identify the default network.
	NetworkName string
	Namespace   string

	// Timeout bounds cluster creation and the network readiness wait.
	Timeout time.Duration
}

// DownOptions parameterizes a devnet teardown.
type DownOptions struct {
	// Timeout bounds the cluster deletion. Zero means no deadline beyond ctx.
	Timeout time.Duration
}

// Target is the resolved kube target the lifecycle operated against.
type Target struct {
	// Kubeconfig is the kubeconfig path, or empty for the default rules.
	Kubeconfig string

	// Context is the kubectl context name.
	Context string
}

// Result is the outcome of a successful Up.
type Result struct {
	// Target is the kube target the operator and network were applied to.
	Target Target

	// Cluster is the provisioned cluster info.
	Cluster cluster.Info

	// Operator is the operator state after the install reconcile.
	Operator operator.State

	// Network is the Ready default network, or nil when Bare.
	Network *yacdv1alpha1.CardanoNetwork
}

// Report is the unified devnet status view.
type Report struct {
	// Cluster is the observed cluster state.
	Cluster cluster.Status

	// Operator is the operator state; zero when the cluster is absent.
	Operator operator.State

	// Networks lists the CardanoNetworks across all namespaces; empty when the
	// cluster is absent.
	Networks []yacdv1alpha1.CardanoNetwork

	// Record is the tracked cluster record.
	Record clusterstate.Record

	// HasRecord reports whether a tracked record was found.
	HasRecord bool
}

// Reporter receives stepwise progress messages. The command layer writes them
// to stderr; tests use NopReporter. It is an interface so later phases can add
// finer sub-progress without changing the call sites.
type Reporter interface {
	// Step announces the start of a top-level step.
	Step(format string, args ...any)

	// Substep announces progress within the current step.
	Substep(format string, args ...any)

	// Done announces the completion of the current step.
	Done(format string, args ...any)
}

// NopReporter is a Reporter that discards all progress.
type NopReporter struct{}

// Step implements Reporter.
func (NopReporter) Step(string, ...any) {}

// Substep implements Reporter.
func (NopReporter) Substep(string, ...any) {}

// Done implements Reporter.
func (NopReporter) Done(string, ...any) {}
