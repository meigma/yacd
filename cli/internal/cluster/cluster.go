package cluster

import (
	"context"
	"time"
)

const (
	// ManagedName is the fixed name of the singleton managed cluster. yacd
	// neither takes nor implies a cluster name; everything derives from this.
	ManagedName = "yacd"

	// ManagedContext is the kubeconfig context k3d creates for the managed
	// cluster ("k3d-<name>").
	ManagedContext = "k3d-" + ManagedName

	// K3sImage is the pinned k3s image the managed cluster runs. It is k3d
	// v5.9.0's own default k3s version, so the pinned binary and image agree.
	// Bump it deliberately alongside a k3d upgrade.
	K3sImage = "docker.io/rancher/k3s:v1.32.5-k3s1"
)

// Host and node ports for the managed cluster's chain API exposure. The managed
// cluster is created with host->node port mappings (see DefaultPortMappings) so
// the devnet network's NodePort Ogmios/Kupo Services answer on stable host
// ports. These node ports MUST match the nodePorts the devnet default network
// spec pins, and these host ports MUST match its externalURLs; a CLI test
// cross-checks the two sides against these constants. The host ports mirror the
// in-cluster service ports for a familiar localhost:<port> URL.
const (
	// OgmiosHostPort is the host port the managed cluster maps to Ogmios.
	OgmiosHostPort int32 = 1337
	// OgmiosNodePort is the NodePort the devnet Ogmios Service pins; the
	// serverlb forwards OgmiosHostPort to this same node port.
	OgmiosNodePort int32 = 30137

	// KupoHostPort is the host port the managed cluster maps to Kupo.
	KupoHostPort int32 = 1442
	// KupoNodePort is the NodePort the devnet Kupo Service pins; the serverlb
	// forwards KupoHostPort to this same node port.
	KupoNodePort int32 = 30442
)

// PortMapping is a host-port to node-port mapping for the managed cluster. It is
// provisioner-agnostic; the adapter renders it into its own flag syntax (k3d's
// "--port HOST:NODEPORT@loadbalancer").
type PortMapping struct {
	// HostPort is the port published on the host.
	HostPort int32
	// NodePort is the NodePort the host port forwards to on the cluster nodes.
	NodePort int32
}

// DefaultPortMappings is the managed cluster's host->node port mappings: the
// stable host ports devnet's Ogmios/Kupo NodePort Services are reachable on.
var DefaultPortMappings = []PortMapping{
	{HostPort: OgmiosHostPort, NodePort: OgmiosNodePort},
	{HostPort: KupoHostPort, NodePort: KupoNodePort},
}

// Spec describes a managed-cluster provisioning request.
type Spec struct {
	// Name is the cluster name; the kubeconfig context is "k3d-<Name>".
	Name string

	// K3sImage is the pinned k3s image to run.
	K3sImage string

	// Timeout bounds cluster creation. Chain data is ephemeral; there is no
	// host bind-mount.
	Timeout time.Duration

	// KubeconfigPath is the kubeconfig the health probe reads to reach an
	// existing cluster's API. It is supplied from the saved state record so a
	// running cluster is not judged unhealthy (and deleted) merely because the
	// current ambient kubeconfig no longer references its context. Empty uses
	// the standard loading rules.
	KubeconfigPath string

	// PortMappings are host->node port mappings applied at cluster-create time
	// so NodePort Services answer on stable host ports. They are create-time
	// only (k3d's mutating "cluster edit --port-add" is experimental, and a
	// healthy cluster takes the EnsureCluster no-op path): a cluster created
	// before these mappings existed keeps its NodePort Services but no host
	// mapping, so its advertised localhost URLs do not route until it is
	// recreated (yacd devnet down && yacd devnet). The chain data is ephemeral,
	// so recreation is cheap, and the CLI resolver probes the advertised URL and
	// falls back to port-forwarding when it does not answer.
	PortMappings []PortMapping
}

// DefaultSpec returns the spec for the singleton managed cluster.
func DefaultSpec(timeout time.Duration) Spec {
	return Spec{Name: ManagedName, K3sImage: K3sImage, Timeout: timeout, PortMappings: DefaultPortMappings}
}

// Info describes a provisioned cluster.
type Info struct {
	Name           string
	Context        string
	KubeconfigPath string
	Running        bool
}

// Status is the observed state of a cluster.
type Status struct {
	Exists   bool
	Running  bool
	Healthy  bool
	K3sImage string
	Context  string
}

// Provisioner creates, heals, deletes, and inspects the managed cluster. It is
// idempotent and treats the runtime as authoritative. Implementations are not
// internally serialized; callers hold the clusterstate lock around mutating
// operations.
type Provisioner interface {
	// EnsureCluster reconciles the cluster to the spec: absent creates it,
	// present-and-healthy is a no-op, present-but-unhealthy is deleted and
	// recreated. A create that fails partway is rolled back before returning.
	EnsureCluster(ctx context.Context, spec Spec) (Info, error)

	// DeleteCluster deletes the named cluster. It is idempotent: an absent
	// cluster is treated as success.
	DeleteCluster(ctx context.Context, name string) error

	// Status reports the observed state of the named cluster without mutating
	// it. kubeconfigPath is the kubeconfig the health probe reads to reach the
	// cluster (the saved record's path); empty uses the standard loading rules.
	Status(ctx context.Context, name string, kubeconfigPath string) (Status, error)
}
