package networkartifacts

import (
	"slices"

	ctrlartifacts "github.com/meigma/yacd/internal/ctrlkit/artifacts"
)

// SchemaVersion identifies the CardanoNetwork artifact payload schema.
const SchemaVersion = "yacd.meigma.io/cardano-network-artifacts/v1alpha1"

// ConfigMap data keys published for a CardanoNetwork localnet.
const (
	ConfigurationKey   = "configuration.yaml"
	ByronGenesisKey    = "byron-genesis.json"
	ShelleyGenesisKey  = "shelley-genesis.json"
	AlonzoGenesisKey   = "alonzo-genesis.json"
	ConwayGenesisKey   = "conway-genesis.json"
	DijkstraGenesisKey = "dijkstra-genesis.json"
	PrimaryTopologyKey = "primary-topology.json"
	PlanManifestKey    = "yacd-localnet-plan.json"
	ConnectionKey      = "connection.json"
)

var requiredKeys = []string{
	ConfigurationKey,
	ByronGenesisKey,
	ShelleyGenesisKey,
	AlonzoGenesisKey,
	ConwayGenesisKey,
	PrimaryTopologyKey,
	PlanManifestKey,
	ConnectionKey,
}

var optionalKeys = []string{
	DijkstraGenesisKey,
}

// RequiredKeys returns the required ConfigMap data keys.
func RequiredKeys() []string {
	return slices.Clone(requiredKeys)
}

// OptionalKeys returns the optional ConfigMap data keys.
func OptionalKeys() []string {
	return slices.Clone(optionalKeys)
}

// Contract returns the generic ConfigMap artifact contract for CardanoNetwork
// artifacts.
func Contract() ctrlartifacts.Contract {
	return ctrlartifacts.Contract{
		SchemaVersion: SchemaVersion,
		RequiredKeys:  RequiredKeys(),
		OptionalKeys:  OptionalKeys(),
	}
}
