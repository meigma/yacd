package k3d

import (
	"context"
	"fmt"
	"strings"

	"github.com/meigma/yacd/cli/internal/cluster"
	"k8s.io/client-go/tools/clientcmd"
)

// EnsureCluster reconciles the managed cluster to spec. It is idempotent: absent
// creates, present-and-healthy is a no-op, present-but-unhealthy is deleted and
// recreated. It is not internally serialized; the caller holds the clusterstate
// lock.
func (p *Provisioner) EnsureCluster(ctx context.Context, spec cluster.Spec) (cluster.Info, error) {
	bin, err := p.resolver.Resolve(ctx)
	if err != nil {
		return cluster.Info{}, fmt.Errorf("resolve k3d: %w", err)
	}

	status, err := p.statusVia(ctx, bin, spec.Name)
	if err != nil {
		return cluster.Info{}, err
	}

	switch {
	case !status.Exists:
		return p.create(ctx, bin, spec)
	case status.Running && status.Healthy:
		return p.infoFor(spec.Name), nil
	default:
		// Present but not running, or running with an unreachable API server:
		// delete and recreate.
		if err := p.deleteCluster(ctx, bin, spec.Name); err != nil {
			return cluster.Info{}, fmt.Errorf("heal cluster %s: delete unhealthy: %w", spec.Name, err)
		}
		return p.create(ctx, bin, spec)
	}
}

// create runs "k3d cluster create" and rolls back a partial cluster on any
// error, returning the original create error.
func (p *Provisioner) create(ctx context.Context, bin string, spec cluster.Spec) (cluster.Info, error) {
	args := []string{
		"cluster", "create", spec.Name,
		"--image", spec.K3sImage,
		"--wait",
		"--timeout", spec.Timeout.String(),
		"--kubeconfig-update-default",
		"--kubeconfig-switch-context",
	}

	if _, err := p.run(ctx, bin, args...); err != nil {
		// Best-effort rollback of any partially-created cluster, on a context
		// that survives the (possibly cancelled or timed-out) parent so cleanup
		// still runs. The original create error is what we return.
		_, _ = p.run(context.WithoutCancel(ctx), bin, "cluster", "delete", spec.Name)
		return cluster.Info{}, fmt.Errorf("create cluster %s: %w", spec.Name, err)
	}

	return p.infoFor(spec.Name), nil
}

// infoFor reports the Info for a created/healthy cluster. k3d merges the context
// into the default kubeconfig via --kubeconfig-update-default.
func (p *Provisioner) infoFor(name string) cluster.Info {
	return cluster.Info{
		Name:           name,
		Context:        "k3d-" + name,
		KubeconfigPath: clientcmd.RecommendedHomeFile,
		Running:        true,
	}
}

// DeleteCluster deletes the named cluster, tolerating an absent one.
func (p *Provisioner) DeleteCluster(ctx context.Context, name string) error {
	bin, err := p.resolver.Resolve(ctx)
	if err != nil {
		return fmt.Errorf("resolve k3d: %w", err)
	}

	return p.deleteCluster(ctx, bin, name)
}

// deleteCluster runs "k3d cluster delete" and treats an absent cluster as
// success so teardown is idempotent.
func (p *Provisioner) deleteCluster(ctx context.Context, bin string, name string) error {
	_, stderr, err := p.runner.Run(ctx, bin, "cluster", "delete", name)
	if err != nil {
		if isNoClusterFound(stderr) {
			return nil
		}
		return fmt.Errorf("delete cluster %s: %w", name, err)
	}

	return nil
}

// isNoClusterFound reports whether k3d's stderr indicates the cluster did not
// exist, which the idempotent delete path tolerates.
func isNoClusterFound(stderr []byte) bool {
	return strings.Contains(strings.ToLower(string(stderr)), "no cluster")
}
