package localnet

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const (
	fingerprintAlgorithm  = "sha256"
	manifestSchemaVersion = "yacd.meigma.io/localnet-plan/v1alpha1"
)

func computeFingerprint(inputs ManifestInputs) (Fingerprint, error) {
	payload, err := json.Marshal(inputs)
	if err != nil {
		return Fingerprint{}, err
	}

	sum := sha256.Sum256(payload)

	return Fingerprint{
		Algorithm: fingerprintAlgorithm,
		Value:     hex.EncodeToString(sum[:]),
	}, nil
}
