package wallet

import (
	"crypto/rand"
	_ "embed"
	"fmt"
	"math/big"
	"strings"
)

//go:embed adjectives.txt
var adjectivesData string

//go:embed nouns.txt
var nounsData string

// adjectives and nouns are the embedded name-part pools, parsed once at init.
// Every entry is a lowercase DNS-1123 token, so any adjective-noun join is a
// valid managed wallet name.
var (
	adjectives = splitWordlist(adjectivesData)
	nouns      = splitWordlist(nounsData)
)

// MaxManagedWallets is the soft ceiling on managed wallets per network. The name
// generator gives up and reports capacity rather than spinning once the taken
// set reaches this size, so a saturated network fails clearly instead of
// looping.
const MaxManagedWallets = 50

// nameGenerationAttempts bounds the re-roll loop. It is comfortably larger than
// MaxManagedWallets so a sparsely-collided pool still resolves to a free name,
// while a genuinely exhausted attempt budget surfaces as an error.
const nameGenerationAttempts = 200

// GenerateName returns a fresh adjective-noun wallet name that is absent from
// taken. The returned name is lowercased and DNS-1123-safe.
//
// It re-rolls on collision up to an internal attempt budget. When taken already
// holds MaxManagedWallets entries it reports the capacity ceiling, and when the
// budget is exhausted without finding a free name it reports that the namespace
// is too crowded to name a wallet automatically.
func GenerateName(taken map[string]struct{}) (string, error) {
	if len(taken) >= MaxManagedWallets {
		return "", fmt.Errorf("wallet name capacity reached: %d managed wallets already exist (limit %d)", len(taken), MaxManagedWallets)
	}

	for range nameGenerationAttempts {
		candidate, err := randomName()
		if err != nil {
			return "", err
		}
		if _, exists := taken[candidate]; exists {
			continue
		}

		return candidate, nil
	}

	return "", fmt.Errorf("could not generate a free wallet name after %d attempts; pick one with --name", nameGenerationAttempts)
}

// randomName joins a uniformly random adjective and noun with a hyphen.
func randomName() (string, error) {
	adjective, err := randomChoice(adjectives)
	if err != nil {
		return "", err
	}
	noun, err := randomChoice(nouns)
	if err != nil {
		return "", err
	}

	return adjective + "-" + noun, nil
}

// randomChoice returns a cryptographically uniform element of words.
func randomChoice(words []string) (string, error) {
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(words))))
	if err != nil {
		return "", fmt.Errorf("choose wallet name part: %w", err)
	}

	return words[index.Int64()], nil
}

// splitWordlist parses an embedded newline-delimited wordlist into a slice,
// dropping blank lines so a trailing newline does not yield an empty token.
func splitWordlist(data string) []string {
	lines := strings.Split(data, "\n")
	words := make([]string, 0, len(lines))
	for _, line := range lines {
		word := strings.TrimSpace(line)
		if word == "" {
			continue
		}
		words = append(words, word)
	}

	return words
}
