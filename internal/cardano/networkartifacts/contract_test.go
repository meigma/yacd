package networkartifacts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContract(t *testing.T) {
	contract := Contract()

	assert.Equal(t, SchemaVersion, contract.SchemaVersion)
	assert.Equal(t, RequiredKeys(), contract.RequiredKeys)
	assert.Equal(t, OptionalKeys(), contract.OptionalKeys)
}

func TestKeysReturnCopies(t *testing.T) {
	required := RequiredKeys()
	required[0] = "mutated"
	assert.Equal(t, ConfigurationKey, RequiredKeys()[0])

	optional := OptionalKeys()
	optional[0] = "mutated"
	assert.Equal(t, DijkstraGenesisKey, OptionalKeys()[0])
}
