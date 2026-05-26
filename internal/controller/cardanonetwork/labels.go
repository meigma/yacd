package cardanonetwork

import (
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
)

// Label key strategy.
//
// Standard "app.kubernetes.io/*" keys are consumed by generic dashboards and
// kubectl tooling and stay aligned with the Kubernetes recommended label set:
//
//   - labelAppName captures the workload role (cardano-node for the primary
//     workload). It does NOT contain the CardanoNetwork instance name.
//   - labelAppInstance is the per-CardanoNetwork instance discriminator and
//     matches the CR name after DNS-label sanitization.
//   - labelAppComponent describes the workload's place in the topology
//     (primary-node today; a future secondary-node controller should reuse
//     this key with its own value rather than inventing a new one).
//   - labelAppManagedBy is always "yacd" for resources this operator owns.
//
// YACD-specific "yacd.meigma.io/*" keys are the canonical selectors the
// operator's own predicates and Service selectors use:
//
//   - labelCardanoNetwork is the CardanoNetwork instance discriminator,
//     mirroring labelAppInstance. Selectors should prefer this key because it
//     is owned by the YACD label vocabulary.
//   - labelCardanoRole describes the workload role within YACD's topology and
//     mirrors labelAppComponent for the same selector reason.
//
// Future workload types should follow the same shape: instance label tracks
// the parent CR, component/role label tracks the topology position, and the
// labelAppName value tracks the workload role. Do not invent a new instance
// or role key when extending the topology.
const (
	labelAppName        = "app.kubernetes.io/name"
	labelAppInstance    = "app.kubernetes.io/instance"
	labelAppComponent   = "app.kubernetes.io/component"
	labelAppManagedBy   = "app.kubernetes.io/managed-by"
	labelCardanoNetwork = "yacd.meigma.io/cardanonetwork"
	labelCardanoRole    = "yacd.meigma.io/role"

	// labelPrimaryNodeName is the labelAppName value for primary node
	// workloads.
	labelPrimaryNodeName = "cardano-node"

	// labelPrimaryRole is the labelAppComponent and labelCardanoRole value for
	// the primary node workload.
	labelPrimaryRole = "primary-node"
)

// primaryWorkloadSelectorLabels returns the label set used for both the
// primary workload Pod template selector and the matching Service selector.
// It must remain stable for the life of a CardanoNetwork because Kubernetes
// rejects selector drift on Deployments.
func primaryWorkloadSelectorLabels(network *yacdv1alpha1.CardanoNetwork) map[string]string {
	instance := ctrlnames.LabelValue(network.Name)

	return map[string]string{
		labelAppName:        labelPrimaryNodeName,
		labelAppInstance:    instance,
		labelAppComponent:   labelPrimaryRole,
		labelCardanoNetwork: instance,
		labelCardanoRole:    labelPrimaryRole,
	}
}

// primaryWorkloadLabels returns the full label set applied to every primary
// workload-owned object (Deployment, Services, PVC, RBAC, Secret). The set
// adds the managed-by label on top of the selector labels.
func primaryWorkloadLabels(network *yacdv1alpha1.CardanoNetwork) map[string]string {
	labels := primaryWorkloadSelectorLabels(network)
	labels[labelAppManagedBy] = "yacd"

	return labels
}
