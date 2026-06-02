package k3d

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/meigma/yacd/cli/internal/cluster"
)

// clusterJSON is the subset of "k3d cluster list -o json" we depend on. k3d
// reports serversRunning as the count of running server nodes; the API server
// lives on a server node, so serversRunning >= 1 means the control plane is up.
type clusterJSON struct {
	Name           string `json:"name"`
	ServersRunning int    `json:"serversRunning"`
	ServersCount   int    `json:"serversCount"`
	AgentsRunning  int    `json:"agentsRunning"`
	AgentsCount    int    `json:"agentsCount"`
}

// list runs "k3d cluster list -o json" (without a name, which exits non-zero
// when absent) and returns the entry matching name, if any.
func (p *Provisioner) list(ctx context.Context, bin string, name string) (clusterJSON, bool, error) {
	stdout, err := p.run(ctx, bin, "cluster", "list", "-o", "json")
	if err != nil {
		return clusterJSON{}, false, fmt.Errorf("list k3d clusters: %w", err)
	}

	var clusters []clusterJSON
	if err := json.Unmarshal(stdout, &clusters); err != nil {
		return clusterJSON{}, false, fmt.Errorf("parse k3d cluster list: %w", err)
	}

	for _, c := range clusters {
		if c.Name == name {
			return c, true, nil
		}
	}

	return clusterJSON{}, false, nil
}

// statusVia derives Status from a cluster list, probing API health only when the
// cluster is running.
func (p *Provisioner) statusVia(ctx context.Context, bin string, name string) (cluster.Status, error) {
	entry, found, err := p.list(ctx, bin, name)
	if err != nil {
		return cluster.Status{}, err
	}
	if !found {
		return cluster.Status{Exists: false}, nil
	}

	status := cluster.Status{
		Exists:   true,
		Running:  entry.ServersRunning >= 1,
		Context:  "k3d-" + name,
		K3sImage: cluster.K3sImage,
	}
	if status.Running {
		status.Healthy = p.prober(ctx, "", status.Context) == nil
	}

	return status, nil
}

// Status reports the observed state of the named cluster.
func (p *Provisioner) Status(ctx context.Context, name string) (cluster.Status, error) {
	bin, err := p.resolver.Resolve(ctx)
	if err != nil {
		return cluster.Status{}, fmt.Errorf("resolve k3d: %w", err)
	}

	return p.statusVia(ctx, bin, name)
}
