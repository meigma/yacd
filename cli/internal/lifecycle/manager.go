package lifecycle

import (
	"context"
	"fmt"

	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/meigma/yacd/cli/internal/render"
)

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

	m.Report.Step("Ensuring local cluster %q", cluster.ManagedName)
	info, err := m.Provisioner.EnsureCluster(ctx, cluster.DefaultSpec(o.Timeout))
	if err != nil {
		return Result{}, fmt.Errorf("ensure cluster: %w", err)
	}
	m.Report.Done("Cluster %q ready (context %q)", info.Name, info.Context)

	record, err := m.reconcileRecord(info, prior)
	if err != nil {
		return Result{}, err
	}
	if err := m.State.Save(record); err != nil {
		return Result{}, fmt.Errorf("save cluster state: %w", err)
	}

	m.Report.Step("Installing operator")
	installer, err := m.NewInstaller(info.KubeconfigPath, info.Context)
	if err != nil {
		return Result{}, fmt.Errorf("build operator installer: %w", err)
	}
	state, err := installer.EnsureOperator(ctx, operator.InstallSpec{})
	if err != nil {
		return Result{}, fmt.Errorf("install operator: %w", err)
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

// reconcileRecord builds the cluster record to persist, preserving the user's
// real prior context across re-runs. It is also the record-repair path: a
// missing record against a live cluster is rebuilt, and a stale one is
// corrected, because Up always saves a fresh record derived from the runtime.
func (m *Manager) reconcileRecord(info cluster.Info, prior string) (clusterstate.Record, error) {
	existing, found, err := m.State.Load()
	if err != nil {
		return clusterstate.Record{}, fmt.Errorf("load cluster state: %w", err)
	}

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
	}, nil
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

	record, found, err := m.State.Load()
	if err != nil {
		return fmt.Errorf("load cluster state: %w", err)
	}

	m.Report.Step("Deleting cluster %q", cluster.ManagedName)
	if err := m.Provisioner.DeleteCluster(ctx, cluster.ManagedName); err != nil {
		return fmt.Errorf("delete cluster: %w", err)
	}
	m.Report.Done("Cluster %q removed", cluster.ManagedName)

	if found && record.PriorContext != "" && record.PriorContext != cluster.ManagedContext {
		if err := m.RestoreContext(record.KubeconfigPath, record.PriorContext); err != nil {
			// Best-effort: the cluster is gone, so a dangling current-context is
			// recoverable and must not fail the teardown.
			m.Report.Substep("Could not restore kube context %q: %v", record.PriorContext, err)
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

	clusterStatus, err := m.Provisioner.Status(ctx, cluster.ManagedName)
	if err != nil {
		return Report{}, fmt.Errorf("cluster status: %w", err)
	}

	record, found, err := m.State.Load()
	if err != nil {
		return Report{}, fmt.Errorf("load cluster state: %w", err)
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
