package wallet

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation"
)

func TestGenerateNameProducesFreeDNSSafeName(t *testing.T) {
	taken := map[string]struct{}{}
	for range 25 {
		name, err := GenerateName(taken)
		require.NoError(t, err)
		require.Empty(t, validation.IsDNS1123Label(name), "generated name must be a valid DNS-1123 label")
		assert.Equal(t, strings.ToLower(name), name, "generated name must be lowercased")
		_, exists := taken[name]
		require.False(t, exists, "generator must avoid collisions")
		taken[name] = struct{}{}
	}
}

func TestGenerateNameAvoidsTakenName(t *testing.T) {
	// With every adjective-noun pair but one already taken, the generator must
	// still find the single free name rather than returning a collision.
	taken := map[string]struct{}{}
	free := adjectives[0] + "-" + nouns[0]
	for _, adjective := range adjectives {
		for _, noun := range nouns {
			candidate := adjective + "-" + noun
			if candidate == free {
				continue
			}
			taken[candidate] = struct{}{}
			if len(taken) >= MaxManagedWallets-1 {
				break
			}
		}
		if len(taken) >= MaxManagedWallets-1 {
			break
		}
	}

	name, err := GenerateName(taken)
	require.NoError(t, err)
	_, collided := taken[name]
	assert.False(t, collided)
}

func TestGenerateNameReportsCapacity(t *testing.T) {
	taken := make(map[string]struct{}, MaxManagedWallets)
	for i := range MaxManagedWallets {
		taken[string(rune('a'+i))+"-wallet"] = struct{}{}
	}

	_, err := GenerateName(taken)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "capacity reached")
}

func TestWordlistsAreDNSSafeAndNonEmpty(t *testing.T) {
	require.NotEmpty(t, adjectives)
	require.NotEmpty(t, nouns)
	for _, words := range [][]string{adjectives, nouns} {
		for _, word := range words {
			assert.Empty(t, validation.IsDNS1123Label(word), "wordlist token %q must be DNS-1123 safe", word)
		}
	}
}
