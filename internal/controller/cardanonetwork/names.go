package cardanonetwork

import (
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
)

const (
	// primaryNodeNameSuffix is the suffix appended to the CardanoNetwork name
	// for the primary node Deployment and matching Service.
	primaryNodeNameSuffix = "node"
)

// primaryWorkloadName returns the DNS-label name of the primary node
// Deployment and its node-to-node Service. Both share a name because the
// Service selects the Deployment's Pods directly.
func primaryWorkloadName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, primaryNodeNameSuffix)
}

// primaryNodeStatePVCName returns the DNS-label name of the PVC that backs the
// primary node's durable state (cardano-node database, generated localnet
// environment, faucet UTXO keys).
func primaryNodeStatePVCName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "node-state")
}

// primaryOgmiosServiceName returns the DNS-label name of the ogmios Service.
func primaryOgmiosServiceName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "ogmios")
}

// primaryKupoServiceName returns the DNS-label name of the kupo Service.
func primaryKupoServiceName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "kupo")
}

// primaryFaucetServiceName returns the DNS-label name of the faucet Service.
func primaryFaucetServiceName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "faucet")
}

// primaryFaucetAuthSecretName returns the DNS-label name of the faucet auth
// Secret that carries the API token consumed by the faucet sidecar.
func primaryFaucetAuthSecretName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "faucet-auth")
}

// networkArtifactsConfigMapName returns the DNS-label name of the ConfigMap
// the init container publishes with the localnet environment artifacts
// downstream controllers consume.
func networkArtifactsConfigMapName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "network-artifacts")
}

// artifactPublisherServiceAccountName returns the DNS-label name of the
// ServiceAccount the init container uses to publish the artifact ConfigMap.
func artifactPublisherServiceAccountName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "artifact-publisher")
}

// artifactPublisherRoleName returns the DNS-label name of the Role that
// allows the artifact publisher to patch its ConfigMap and nothing else.
func artifactPublisherRoleName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "artifact-publisher")
}

// artifactPublisherRoleBindingName returns the DNS-label name of the
// RoleBinding that grants the artifact publisher ServiceAccount its Role.
func artifactPublisherRoleBindingName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "artifact-publisher")
}

// nodeToNodeHost is the in-cluster DNS name of the primary node-to-node
// Service. It depends on the namespace, so it is derived from the network
// object rather than precomputed.
func nodeToNodeHost(network *yacdv1alpha1.CardanoNetwork) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", primaryWorkloadName(network), network.Namespace)
}

// nodeToNodeURL is the in-cluster tcp:// URL the artifact ConfigMap publishes
// for the primary node-to-node endpoint.
func nodeToNodeURL(network *yacdv1alpha1.CardanoNetwork) string {
	return fmt.Sprintf("tcp://%s:%d", nodeToNodeHost(network), network.Spec.Node.Port)
}
