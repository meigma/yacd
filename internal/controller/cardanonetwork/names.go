package cardanonetwork

import (
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/primarypod"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
)

// primaryWorkloadName returns the DNS-label name of the primary node
// Deployment and its node-to-node Service. Both share a name because the
// Service selects the Deployment's Pods directly.
func primaryWorkloadName(network *yacdv1alpha1.CardanoNetwork) string {
	return primarypod.WorkloadName(network)
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

// primaryArtifactsServiceName returns the DNS-label name of the artifacts
// Service that exposes the cardano-tools serve sidecar.
func primaryArtifactsServiceName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "artifacts")
}

// primaryFaucetAuthSecretName returns the DNS-label name of the faucet auth
// Secret that carries the API token consumed by the faucet sidecar.
func primaryFaucetAuthSecretName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "faucet-auth")
}

// primaryWalletSecretName returns the DNS-label name of the developer wallet
// Secret that carries the bootstrapped payment key envelopes and address.
func primaryWalletSecretName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "wallet")
}

// primaryFaucetWalletSecretName returns the DNS-label name of the well-known
// faucet wallet Secret. Unlike the developer wallet, the faucet wallet is
// funded directly at genesis (not through the faucet service) and serves as the
// local funding source the CLI later spends from.
func primaryFaucetWalletSecretName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "wallet-faucet")
}

// nodeToNodeHost is the in-cluster DNS name of the primary node-to-node
// Service. It depends on the namespace, so it is derived from the network
// object rather than precomputed.
func nodeToNodeHost(network *yacdv1alpha1.CardanoNetwork) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", primaryWorkloadName(network), network.Namespace)
}
