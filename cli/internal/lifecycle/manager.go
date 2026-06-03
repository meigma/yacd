package lifecycle

import (
	"context"
	"fmt"
	"time"

	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/meigma/yacd/cli/internal/render"
	"k8s.io/apimachinery/pkg/util/wait"
)

// operatorPollInterval is how often Up polls operator readiness after install.
const operatorPollInterval = 3 * time.Second

// defaults fills the Reporter and context seams with their production
// implementations when the caller left them nil.
func (m *Manager) defaults() {
	if m.Report == nil {
		m.Report = NopReporter{}
	}
	if m.CaptureContext == nil {
		m.CaptureContext = kube.CurrentContext
	}
	if m.RestoreContext == nil {
		m.RestoreContext = kube.SetCurrentContext
	}
}

// Up brings the managed devnet to a ready state: it provisions (or heals) the
// singleton cluster, installs (or upgrades) the operator, and — unless Bare —
// applies the default network and waits for it to become Ready. The whole
// mutating sequence runs under the cluster lock, so it is safe to re-run after
// an interruption: each step is idempotent and converges.
func (m *Manager) Up(ctx context.Context, o UpOptions) (Result, error) {
	m.defaults()

	// Bound the whole operation, including lock acquisition, by the timeout so a
	// stuck or concurrent run cannot hang past the advertised maximum.
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}

	release, err := m.State.Lock(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("acquire cluster lock: %w", err)
	}
	defer func() { _ = release() }()

	// Capture the prior context before EnsureCluster switches it to the managed
	// context; an unreadable kubeconfig just means there is nothing to restore.
	prior, err := m.CaptureContext()
	if err != nil {
		prior = ""
	}

	existing, found, err := m.State.Load()
	if err != nil {
		return Result{}, fmt.Errorf("load cluster state: %w", err)
	}

	// Probe an existing cluster's health through the kubeconfig the record says
	// it lives in, so a running cluster is not deleted just because the ambient
	// kubeconfig no longer references its context.
	spec := cluster.DefaultSpec(o.Timeout)
	if found {
		spec.KubeconfigPath = existing.KubeconfigPath
	}

	m.Report.Step("Ensuring local cluster %q", cluster.ManagedName)
	info, err := m.Provisioner.EnsureCluster(ctx, spec)
	if err != nil {
		return Result{}, fmt.Errorf("ensure cluster: %w", err)
	}
	m.Report.Done("Cluster %q ready (context %q)", info.Name, info.Context)

	record := m.buildRecord(info, prior, existing, found)
	if err := m.State.Save(record); err != nil {
		return Result{}, fmt.Errorf("save cluster state: %w", err)
	}

	m.Report.Step("Installing operator")
	installer, err := m.NewInstaller(info.KubeconfigPath, info.Context)
	if err != nil {
		return Result{}, fmt.Errorf("build operator installer: %w", err)
	}
	state, err := m.ensureOperatorReady(ctx, installer, o.Timeout)
	if err != nil {
		return Result{}, err
	}
	m.Report.Done("Operator %s ready", state.Version)

	target := Target{Kubeconfig: info.KubeconfigPath, Context: info.Context}
	if o.Bare {
		return Result{Target: target, Cluster: info, Operator: state}, nil
	}

	client, err := m.NewNetworks(kube.Config{Kubeconfig: info.KubeconfigPath, Context: info.Context})
	if err != nil {
		return Result{}, fmt.Errorf("build kube client: %w", err)
	}

	network, err := render.CardanoNetwork(&o.Env, o.NetworkName, o.Namespace)
	if err != nil {
		return Result{}, err
	}

	m.Report.Step("Applying network %q", o.NetworkName)
	if err := client.EnsureNamespace(ctx, o.Namespace); err != nil {
		return Result{}, err
	}
	if err := client.ApplyCardanoNetwork(ctx, network); err != nil {
		return Result{}, err
	}
	m.Report.Substep("Waiting for network %q to become ready", o.NetworkName)
	ready, err := kube.WaitReady(ctx, client, o.Namespace, o.NetworkName, o.Timeout)
	if err != nil {
		return Result{}, err
	}
	m.Report.Done("Network %q ready", o.NetworkName)

	return Result{Target: target, Cluster: info, Operator: state, Network: ready}, nil
}

// ensureOperatorReady installs or upgrades the operator and then waits for its
// manager Deployment to report Available, bounded by timeout. The SSA install
// applies the chart but does not wait for the workload, so a first-run install
// returns not-ready while the image pulls. Waiting here keeps the reported state
// and the "operator ready" progress honest, and makes `devnet --bare` return a
// usable operator rather than one that is still starting.
func (m *Manager) ensureOperatorReady(ctx context.Context, installer operator.Installer, timeout time.Duration) (operator.State, error) {
	state, err := installer.EnsureOperator(ctx, operator.InstallSpec{})
	if err != nil {
		return operator.State{}, fmt.Errorf("install operator: %w", err)
	}
	if state.Ready {
		return state, nil
	}

	m.Report.Substep("Waiting for the operator to become ready")
	err = wait.PollUntilContextTimeout(ctx, operatorPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		current, err := installer.OperatorState(ctx)
		if err != nil {
			return false, err
		}
		state = current
		return state.Ready, nil
	})
	if err != nil {
		return state, fmt.Errorf("operator did not become ready: %w", err)
	}

	return state, nil
}

// buildRecord builds the cluster record to persist from the runtime info and
// the previously loaded record, preserving the user's real prior context across
// re-runs. It is also the record-repair path: a missing record against a live
// cluster is rebuilt, and a stale one is corrected, because Up always saves a
// fresh record derived from the runtime.
func (m *Manager) buildRecord(info cluster.Info, prior string, existing clusterstate.Record, found bool) clusterstate.Record {
	priorContext := prior
	// On a warm re-run the captured context is already the managed one; never
	// record that as the prior, or teardown would restore to it and strand the
	// user's real previous context.
	if priorContext == cluster.ManagedContext {
		priorContext = ""
	}
	// Preserve a real prior context recorded on an earlier run.
	if found && existing.PriorContext != "" && existing.PriorContext != cluster.ManagedContext {
		priorContext = existing.PriorContext
	}

	return clusterstate.Record{
		ClusterName:    info.Name,
		Context:        info.Context,
		PriorContext:   priorContext,
		K3dVersion:     m.K3dVersion,
		KubeconfigPath: info.KubeconfigPath,
	}
}

// Down deletes the managed cluster, restores the prior kubectl context, and
// clears the tracked record. It runs under the cluster lock and is idempotent:
// deleting an absent cluster is a no-op.
func (m *Manager) Down(ctx context.Context, o DownOptions) error {
	m.defaults()

	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}

	release, err := m.State.Lock(ctx)
	if err != nil {
		return fmt.Errorf("acquire cluster lock: %w", err)
	}
	defer func() { _ = release() }()

	// The runtime is authoritative; a corrupt or unreadable record must not
	// block teardown of the real cluster. Proceed without it and clear it below.
	record, found, err := m.State.Load()
	if err != nil {
		m.Report.Substep("Ignoring unreadable cluster state: %v", err)
		found = false
	}

	m.Report.Step("Deleting cluster %q", cluster.ManagedName)
	if err := m.Provisioner.DeleteCluster(ctx, cluster.ManagedName); err != nil {
		return fmt.Errorf("delete cluster: %w", err)
	}
	m.Report.Done("Cluster %q removed", cluster.ManagedName)

	// Return kubectl to its pre-devnet state. When a real prior context was
	// captured, restore it; when there was none (the user had no current-context
	// before devnet), clear it — otherwise k3d's `cluster delete` repoints
	// current-context to an arbitrary remaining entry (possibly a production
	// cluster), which the user never chose.
	if found && record.PriorContext != cluster.ManagedContext {
		if err := m.RestoreContext(record.KubeconfigPath, record.PriorContext); err != nil {
			// Best-effort: the cluster is gone, so a dangling current-context is
			// recoverable and must not fail the teardown.
			m.Report.Substep("Could not restore kube context %q: %v", record.PriorContext, err)
		} else if record.PriorContext == "" {
			m.Report.Done("Cleared kube context (no prior context to restore)")
		} else {
			m.Report.Done("Restored kube context %q", record.PriorContext)
		}
	}

	if err := m.State.Clear(); err != nil {
		return fmt.Errorf("clear cluster state: %w", err)
	}

	return nil
}

// Status reports the managed devnet state without mutating anything and without
// taking the lock. The cluster runtime is authoritative; the operator and
// network views are only gathered when the cluster exists.
func (m *Manager) Status(ctx context.Context) (Report, error) {
	m.defaults()

	// Load the cheap record first. With no record this CLI has not provisioned a
	// managed cluster, so report absent without resolving the pinned k3d binary
	// (which would fetch it on a cache miss) just to probe a runtime we have no
	// record of. A managed cluster always leaves a record, so the only case this
	// skips is an out-of-band cluster, which `yacd devnet` reconciles.
	record, found, err := m.State.Load()
	if err != nil {
		return Report{}, fmt.Errorf("load cluster state: %w", err)
	}
	if !found {
		return Report{Cluster: cluster.Status{Exists: false}}, nil
	}

	clusterStatus, err := m.Provisioner.Status(ctx, cluster.ManagedName, record.KubeconfigPath)
	if err != nil {
		return Report{}, fmt.Errorf("cluster status: %w", err)
	}

	report := Report{Cluster: clusterStatus, Record: record, HasRecord: found}
	if !clusterStatus.Exists {
		return report, nil
	}

	installer, err := m.NewInstaller(record.KubeconfigPath, clusterStatus.Context)
	if err != nil {
		return Report{}, fmt.Errorf("build operator installer: %w", err)
	}
	operatorState, err := installer.OperatorState(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("operator state: %w", err)
	}
	report.Operator = operatorState

	client, err := m.NewNetworks(kube.Config{Kubeconfig: record.KubeconfigPath, Context: clusterStatus.Context})
	if err != nil {
		return Report{}, fmt.Errorf("build kube client: %w", err)
	}
	networks, err := client.ListCardanoNetworks(ctx, "")
	if err != nil {
		return Report{}, fmt.Errorf("list networks: %w", err)
	}
	report.Networks = networks

	return report, nil
}
