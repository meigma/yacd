package generate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

// fakeHasher records the genesis kinds it is asked to hash and returns a
// deterministic "hash-<kind>" value.
type fakeHasher struct {
	calls []GenesisKind
}

func (f *fakeHasher) HashGenesis(_ context.Context, kind GenesisKind, _ string) (string, error) {
	f.calls = append(f.calls, kind)
	return "hash-" + string(kind), nil
}

func TestEnrichGenesisHashesAddsMissingHashes(t *testing.T) {
	t.Parallel()

	config := "ByronGenesisFile: byron-genesis.json\n" +
		"ShelleyGenesisFile: shelley-genesis.json\n" +
		"AlonzoGenesisFile: alonzo-genesis.json\n" +
		"ConwayGenesisFile: conway-genesis.json\n"

	hasher := &fakeHasher{}
	out, err := EnrichGenesisHashes(t.Context(), "/state/env",
		map[string]string{networkartifacts.ConfigurationKey: config}, hasher)
	require.NoError(t, err)

	enriched := out[networkartifacts.ConfigurationKey]
	for _, hashField := range []string{"ByronGenesisHash", "ShelleyGenesisHash", "AlonzoGenesisHash", "ConwayGenesisHash"} {
		assert.Contains(t, enriched, hashField)
	}
	assert.ElementsMatch(t,
		[]GenesisKind{GenesisKindByron, GenesisKindShelley, GenesisKindAlonzo, GenesisKindConway},
		hasher.calls)
}

func TestEnrichGenesisHashesSkipsPresentHashes(t *testing.T) {
	t.Parallel()

	config := "ConwayGenesisFile: conway-genesis.json\nConwayGenesisHash: already-set\n"

	hasher := &fakeHasher{}
	out, err := EnrichGenesisHashes(t.Context(), "/state/env",
		map[string]string{networkartifacts.ConfigurationKey: config}, hasher)
	require.NoError(t, err)

	assert.Equal(t, config, out[networkartifacts.ConfigurationKey], "content is returned unchanged")
	assert.Empty(t, hasher.calls, "no hashing when every referenced hash is present")
}

func TestEnrichGenesisHashesRejectsEscapingGenesisPath(t *testing.T) {
	t.Parallel()

	config := "ConwayGenesisFile: ../../etc/passwd\n"

	_, err := EnrichGenesisHashes(t.Context(), "/state/env",
		map[string]string{networkartifacts.ConfigurationKey: config}, &fakeHasher{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stay within the localnet environment")
}

func TestEnrichGenesisHashesNoConfigurationIsNoop(t *testing.T) {
	t.Parallel()

	out, err := EnrichGenesisHashes(t.Context(), "/state/env", map[string]string{}, &fakeHasher{})
	require.NoError(t, err)
	assert.Empty(t, out)
}
