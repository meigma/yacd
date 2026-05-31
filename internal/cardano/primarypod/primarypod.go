// Package primarypod contains the shared primary Cardano node Pod vocabulary.
//
// CardanoNetwork owns primary Pod composition, while CardanoDBSync only needs
// enough primary Pod shape to validate sidecar placement and build selectors
// for DB Sync-owned Services. Keeping this package free of controller code lets
// both controllers share names, labels, and port ownership rules without a
// controller-to-controller import cycle.
package primarypod

import (
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
)

const (
	// CardanoNodeContainerName is the primary cardano-node container name.
	CardanoNodeContainerName = "cardano-node"

	// WorkloadNameSuffix is the suffix appended to the CardanoNetwork name for
	// the primary node Deployment and matching Service.
	WorkloadNameSuffix = "node"

	// DefaultNodePort is the primary node-to-node port default.
	DefaultNodePort int32 = 3001

	// DefaultOgmiosPort is the Ogmios sidecar port default.
	DefaultOgmiosPort int32 = 1337

	// DefaultKupoPort is the Kupo sidecar port default.
	DefaultKupoPort int32 = 1442

	// DefaultFaucetPort is the faucet sidecar port default.
	DefaultFaucetPort int32 = 8080

	// DefaultServePort is the cardano-tools serve sidecar port default. It is
	// deliberately not 8080 (the faucet default) so the always-on serve
	// container can coexist with the faucet on the primary Pod.
	//
	// 8090 IS registered in PortOwners now that the serve sidecar is exposed
	// on an owned Service: PortOwners feeds the CardanoDBSync sidecar placement
	// collision check, so a db-sync metrics port set to 8090 is correctly
	// rejected as a primary Pod conflict.
	DefaultServePort int32 = 8090

	// LabelAppName is the Kubernetes recommended application name label key.
	LabelAppName = "app.kubernetes.io/name"

	// LabelAppInstance is the Kubernetes recommended application instance label key.
	LabelAppInstance = "app.kubernetes.io/instance"

	// LabelAppComponent is the Kubernetes recommended component label key.
	LabelAppComponent = "app.kubernetes.io/component"

	// LabelAppManagedBy is the Kubernetes recommended managed-by label key.
	LabelAppManagedBy = "app.kubernetes.io/managed-by"

	// LabelCardanoNetwork is the YACD CardanoNetwork instance discriminator.
	LabelCardanoNetwork = "yacd.meigma.io/cardanonetwork"

	// LabelCardanoRole is the YACD workload role label key.
	LabelCardanoRole = "yacd.meigma.io/role"

	// LabelPrimaryNodeName is the application name value for primary node Pods.
	LabelPrimaryNodeName = "cardano-node"

	// LabelPrimaryRole is the component and role value for primary node Pods.
	LabelPrimaryRole = "primary-node"

	// PortNameNodeToNode is the primary node-to-node container port name.
	PortNameNodeToNode = "node-to-node"

	// PortNameOgmios is the Ogmios container port name.
	PortNameOgmios = "ogmios"

	// PortNameKupo is the Kupo container port name.
	PortNameKupo = "kupo"

	// PortNameFaucet is the faucet container port name.
	PortNameFaucet = "faucet"

	// PortNameServe is the cardano-tools serve container port name.
	PortNameServe = "serve"
)

// WorkloadName returns the DNS-label name of the primary node Deployment and
// its node-to-node Service.
func WorkloadName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, WorkloadNameSuffix)
}

// SelectorLabels returns the immutable selector labels for the primary node
// workload.
func SelectorLabels(network *yacdv1alpha1.CardanoNetwork) map[string]string {
	instance := ctrlnames.LabelValue(network.Name)

	return map[string]string{
		LabelAppName:        LabelPrimaryNodeName,
		LabelAppInstance:    instance,
		LabelAppComponent:   LabelPrimaryRole,
		LabelCardanoNetwork: instance,
		LabelCardanoRole:    LabelPrimaryRole,
	}
}

// PortOwners returns the effective primary Pod container ports and their
// owning component names. It intentionally mirrors CardanoNetwork sidecar
// defaulting so placement validation and primary Pod rendering agree.
func PortOwners(network *yacdv1alpha1.CardanoNetwork) map[int32]string {
	nodePort := network.Spec.Node.Port
	if nodePort == 0 {
		nodePort = DefaultNodePort
	}
	ports := map[int32]string{
		nodePort: PortNameNodeToNode,
		// The cardano-tools serve sidecar is always-on for the network modes
		// where it runs (local and curated public) and binds a fixed port, so
		// it permanently owns DefaultServePort in the primary Pod.
		DefaultServePort: PortNameServe,
	}
	if ogmiosEnabled(network) {
		ports[ogmiosPort(network)] = PortNameOgmios
	}
	if kupoEnabled(network) {
		ports[kupoPort(network)] = PortNameKupo
	}
	if faucetEnabled(network) {
		ports[faucetPort(network)] = PortNameFaucet
	}

	return ports
}

// ogmiosEnabled returns the effective Ogmios sidecar enablement for the
// primary Pod.
func ogmiosEnabled(network *yacdv1alpha1.CardanoNetwork) bool {
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Ogmios == nil {
		return true
	}

	return network.Spec.ChainAPI.Ogmios.Enabled
}

// kupoEnabled returns the effective Kupo sidecar enablement for the primary
// Pod. When omitted, Kupo follows the effective Ogmios setting.
func kupoEnabled(network *yacdv1alpha1.CardanoNetwork) bool {
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Kupo == nil {
		return ogmiosEnabled(network)
	}

	return network.Spec.ChainAPI.Kupo.Enabled
}

// faucetEnabled returns the effective faucet sidecar enablement for the
// primary Pod.
func faucetEnabled(network *yacdv1alpha1.CardanoNetwork) bool {
	return network.Spec.ChainAPI != nil &&
		network.Spec.ChainAPI.Faucet != nil &&
		network.Spec.ChainAPI.Faucet.Enabled
}

// ogmiosPort returns the effective Ogmios container port for the primary Pod.
func ogmiosPort(network *yacdv1alpha1.CardanoNetwork) int32 {
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Ogmios == nil || !network.Spec.ChainAPI.Ogmios.Enabled {
		return DefaultOgmiosPort
	}
	if network.Spec.ChainAPI.Ogmios.Port == 0 {
		return DefaultOgmiosPort
	}

	return network.Spec.ChainAPI.Ogmios.Port
}

// kupoPort returns the effective Kupo container port for the primary Pod.
func kupoPort(network *yacdv1alpha1.CardanoNetwork) int32 {
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Kupo == nil || !network.Spec.ChainAPI.Kupo.Enabled {
		return DefaultKupoPort
	}
	if network.Spec.ChainAPI.Kupo.Port == 0 {
		return DefaultKupoPort
	}

	return network.Spec.ChainAPI.Kupo.Port
}

// faucetPort returns the effective faucet container port for the primary Pod.
func faucetPort(network *yacdv1alpha1.CardanoNetwork) int32 {
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Faucet == nil || !network.Spec.ChainAPI.Faucet.Enabled {
		return DefaultFaucetPort
	}
	if network.Spec.ChainAPI.Faucet.Port == 0 {
		return DefaultFaucetPort
	}

	return network.Spec.ChainAPI.Faucet.Port
}
