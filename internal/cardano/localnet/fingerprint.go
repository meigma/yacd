package localnet

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const (
	// fingerprintAlgorithm is the digest algorithm used for localnet plan
	// fingerprints.
	fingerprintAlgorithm = "sha256"

	// manifestSchemaVersion is the current manifest wire-format version.
	manifestSchemaVersion = "yacd.meigma.io/localnet-plan/v1alpha1"
)

// computeFingerprint returns a stable digest for normalized create-env inputs.
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
