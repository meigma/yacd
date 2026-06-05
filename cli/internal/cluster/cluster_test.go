package cluster_test

import (
	"testing"
	"time"

	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/stretchr/testify/assert"
)

// TestDefaultSpecIncludesDefaultPortMappings ensures the singleton managed
// cluster is always provisioned with the chain API host port mappings, so its
// NodePort Ogmios/Kupo Services answer on stable host ports.
func TestDefaultSpecIncludesDefaultPortMappings(t *testing.T) {
	spec := cluster.DefaultSpec(time.Minute)

	assert.Equal(t, cluster.DefaultPortMappings, spec.PortMappings)
	assert.Equal(t, []cluster.PortMapping{
		{HostPort: cluster.OgmiosHostPort, NodePort: cluster.OgmiosNodePort},
		{HostPort: cluster.KupoHostPort, NodePort: cluster.KupoNodePort},
	}, cluster.DefaultPortMappings)
}

// TestDefaultPortMappingsUseValidNodePorts guards that the pinned node ports
// stay inside the Kubernetes NodePort range the operator's validation enforces
// (30000-32767); a value outside it would deploy a Degraded devnet network.
func TestDefaultPortMappingsUseValidNodePorts(t *testing.T) {
	for _, m := range cluster.DefaultPortMappings {
		assert.GreaterOrEqual(t, m.NodePort, int32(30000), "node port %d below range", m.NodePort)
		assert.LessOrEqual(t, m.NodePort, int32(32767), "node port %d above range", m.NodePort)
	}
}
