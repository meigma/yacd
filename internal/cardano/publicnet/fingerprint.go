package publicnet

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

const fingerprintAlgorithm = "sha256"

func computeFingerprint(input fingerprintInput) (Fingerprint, error) {
	sort.Slice(input.Files, func(i, j int) bool {
		return input.Files[i].Key < input.Files[j].Key
	})

	payload, err := json.Marshal(input)
	if err != nil {
		return Fingerprint{}, err
	}

	sum := sha256.Sum256(payload)

	return Fingerprint{
		Algorithm: fingerprintAlgorithm,
		Value:     hex.EncodeToString(sum[:]),
	}, nil
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%s:%s", fingerprintAlgorithm, hex.EncodeToString(sum[:]))
}
