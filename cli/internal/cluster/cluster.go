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
}

// DefaultSpec returns the spec for the singleton managed cluster.
func DefaultSpec(timeout time.Duration) Spec {
	return Spec{Name: ManagedName, K3sImage: K3sImage, Timeout: timeout}
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
