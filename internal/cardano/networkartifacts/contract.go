package networkartifacts

import (
	"slices"
)

// SchemaVersion identifies the CardanoNetwork artifact payload schema.
const SchemaVersion = "yacd.meigma.io/cardano-network-artifacts/v1alpha1"

// Artifact data keys published for a CardanoNetwork.
const (
	ConfigurationKey         = "configuration.yaml"
	ByronGenesisKey          = "byron-genesis.json"
	ShelleyGenesisKey        = "shelley-genesis.json"
	AlonzoGenesisKey         = "alonzo-genesis.json"
	ConwayGenesisKey         = "conway-genesis.json"
	DijkstraGenesisKey       = "dijkstra-genesis.json"
	PrimaryTopologyKey       = "primary-topology.json"
	PlanManifestKey          = "yacd-localnet-plan.json"
	PublicProfileManifestKey = "yacd-public-profile.json"
	CheckpointsKey           = "checkpoints.json"
	PeerSnapshotKey          = "peer-snapshot.json"
	ConnectionKey            = "connection.json"
)

var requiredKeys = []string{
	ConfigurationKey,
	ByronGenesisKey,
	ShelleyGenesisKey,
	AlonzoGenesisKey,
	ConwayGenesisKey,
	PrimaryTopologyKey,
	ConnectionKey,
}

var optionalKeys = []string{
	DijkstraGenesisKey,
	PlanManifestKey,
	PublicProfileManifestKey,
	CheckpointsKey,
	PeerSnapshotKey,
}

// RequiredKeys returns the required artifact data keys.
func RequiredKeys() []string {
	return slices.Clone(requiredKeys)
}

// OptionalKeys returns the optional artifact data keys.
func OptionalKeys() []string {
	return slices.Clone(optionalKeys)
}
