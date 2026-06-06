package k3d

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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

	status, err := p.statusVia(ctx, bin, spec.Name, spec.KubeconfigPath)
	if err != nil {
		return cluster.Info{}, err
	}

	switch {
	case !status.Exists:
		return p.create(ctx, bin, spec)
	case status.Running && status.Healthy:
		// The cluster is healthy where the probe reached it: the recorded
		// kubeconfig (spec.KubeconfigPath), or the ambient default when there is
		// no record yet. Report that path so the operator/network clients and the
		// saved state record reference the file the context actually lives in,
		// rather than the current ambient KUBECONFIG (which may not list it).
		kubeconfigPath := spec.KubeconfigPath
		if kubeconfigPath == "" {
			kubeconfigPath = defaultKubeconfigPath()
		}
		return p.infoFor(spec.Name, kubeconfigPath), nil
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
	args := make([]string, 0, 10+2*len(spec.PortMappings))
	args = append(args,
		"cluster", "create", spec.Name,
		"--image", spec.K3sImage,
		"--wait",
		"--timeout", spec.Timeout.String(),
		"--kubeconfig-update-default",
		"--kubeconfig-switch-context",
	)
	// Publish each host->node port mapping through the serverlb so NodePort
	// Services answer on stable host ports. The loadbalancer forwards the host
	// port to the same node port, so the mapping target must be a NodePort.
	for _, m := range spec.PortMappings {
		args = append(args, "--port", fmt.Sprintf("%d:%d@loadbalancer", m.HostPort, m.NodePort))
	}

	if _, err := p.run(ctx, bin, args...); err != nil {
		// Best-effort rollback of any partially-created cluster, on a context
		// that survives the (possibly cancelled or timed-out) parent so cleanup
		// still runs.
		_, _ = p.run(context.WithoutCancel(ctx), bin, "cluster", "delete", spec.Name)
		// Report a clear cause when the parent context bounded the run, rather
		// than the raw "signal: killed" the killed subprocess surfaces.
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return cluster.Info{}, fmt.Errorf("create cluster %s: timed out after %s", spec.Name, spec.Timeout)
		case errors.Is(ctx.Err(), context.Canceled):
			return cluster.Info{}, fmt.Errorf("create cluster %s: cancelled", spec.Name)
		case isHostPortConflict(err):
			// A mapped host port is already bound on this machine. Surface a
			// clear cause rather than the raw docker bind error; automatic
			// fallback-port selection is left to local-lifecycle hardening.
			return cluster.Info{}, fmt.Errorf("create cluster %s: a mapped host port (%s) is already in use; free it and retry: %w", spec.Name, hostPortList(spec.PortMappings), err)
		default:
			return cluster.Info{}, fmt.Errorf("create cluster %s: %w", spec.Name, err)
		}
	}

	// On create/heal, k3d wrote the context into the default kubeconfig.
	return p.infoFor(spec.Name, defaultKubeconfigPath()), nil
}

// infoFor reports the Info for a created or healthy cluster, recording the
// kubeconfig path the caller knows the context lives in. The create/heal path
// passes defaultKubeconfigPath() (where --kubeconfig-update-default wrote it,
// honouring KUBECONFIG); the healthy no-op path passes the recorded kubeconfig
// so a later run is not pointed at the wrong file.
func (p *Provisioner) infoFor(name, kubeconfigPath string) cluster.Info {
	return cluster.Info{
		Name:           name,
		Context:        "k3d-" + name,
		KubeconfigPath: kubeconfigPath,
		Running:        true,
	}
}

// defaultKubeconfigPath resolves the kubeconfig file k3d's
// --kubeconfig-update-default writes to: the first KUBECONFIG entry when the env
// var is set, otherwise ~/.kube/config.
func defaultKubeconfigPath() string {
	return clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
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

// isHostPortConflict reports whether a create error looks like a published host
// port already being bound on the machine. The runner wraps k3d/docker stderr
// into the error, so the bind failure is matchable on the message. The markers
// are the docker port-allocation phrases on both Linux and macOS; the bare
// "bind:" prefix is intentionally NOT matched, since other bind failures (e.g.
// "bind: permission denied") are not host-port conflicts.
func isHostPortConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{"address already in use", "port is already allocated", "ports are not available"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// hostPortList formats the mappings' host ports for an error message, e.g.
// "1337, 1442".
func hostPortList(mappings []cluster.PortMapping) string {
	ports := make([]string, 0, len(mappings))
	for _, m := range mappings {
		ports = append(ports, strconv.Itoa(int(m.HostPort)))
	}
	return strings.Join(ports, ", ")
}
