package networkartifacts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSchemaVersion(t *testing.T) {
	assert.Equal(t, "yacd.meigma.io/cardano-network-artifacts/v1alpha1", SchemaVersion)
}

func TestKeys(t *testing.T) {
	assert.Equal(t, []string{
		ConfigurationKey,
		ByronGenesisKey,
		ShelleyGenesisKey,
		AlonzoGenesisKey,
		ConwayGenesisKey,
		PrimaryTopologyKey,
		PlanManifestKey,
		ConnectionKey,
	}, RequiredKeys())
	assert.Equal(t, []string{DijkstraGenesisKey}, OptionalKeys())
}

func TestKeysReturnCopies(t *testing.T) {
	required := RequiredKeys()
	required[0] = "mutated"
	assert.Equal(t, ConfigurationKey, RequiredKeys()[0])

	optional := OptionalKeys()
	optional[0] = "mutated"
	assert.Equal(t, DijkstraGenesisKey, OptionalKeys()[0])
}
