package cardanonetwork

import (
	"slices"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
)

// validateKupoImage rejects kupo images other than the single supported
// release. Kupo's wire format and chain assumptions are tightly coupled to
// the cardano-node version pair under test, so we do not accept arbitrary
// images here.
func validateKupoImage(settings kupoSettings) error {
	if !settings.enabled {
		return nil
	}
	if settings.image == defaultKupoImage {
		return nil
	}

	return unsupportedSpec("kupo image %q is not supported; supported image: %s", settings.image, defaultKupoImage)
}

// validatePrimaryWorkloadPorts rejects port conflicts across the primary
// node and the optional chain API sidecars. Each enabled sidecar must claim
// a distinct port.
func validatePrimaryWorkloadPorts(nodePort int32, ogmios ogmiosSettings, kupo kupoSettings, faucet faucetSettings) error {
	seen := map[int32]string{
		nodePort: cardanoNodePortName,
	}
	if ogmios.enabled {
		if owner, ok := seen[ogmios.port]; ok {
			return unsupportedSpec("ogmios port %d conflicts with %s port", ogmios.port, owner)
		}
		seen[ogmios.port] = ogmiosPortName
	}
	if kupo.enabled {
		if owner, ok := seen[kupo.port]; ok {
			return unsupportedSpec("kupo port %d conflicts with %s port", kupo.port, owner)
		}
		seen[kupo.port] = kupoPortName
	}
	if faucet.enabled {
		if owner, ok := seen[faucet.port]; ok {
			return unsupportedSpec("faucet port %d conflicts with %s port", faucet.port, owner)
		}
	}

	return nil
}

// validateFaucetSourceName rejects faucet defaultSource values that do not
// match the utxoN format (where N is a positive integer with no leading
// zero). cardano-testnet generates the matching key directory using this
// naming, so any drift would produce a non-functional faucet.
func validateFaucetSourceName(sourceName string) error {
	if !strings.HasPrefix(sourceName, "utxo") || len(sourceName) < len("utxo1") {
		return unsupportedSpec("faucet defaultSource must use the utxoN source name format")
	}
	digits := sourceName[len("utxo"):]
	if digits[0] == '0' {
		return unsupportedSpec("faucet defaultSource must use the utxoN source name format")
	}
	for _, char := range digits {
		if char < '0' || char > '9' {
			return unsupportedSpec("faucet defaultSource must use the utxoN source name format")
		}
	}

	return nil
}

// validateOgmiosCompatibility rejects ogmios/cardano-node image pairings the
// project has not validated. The pairing table lives in defaults.go.
func validateOgmiosCompatibility(nodeVersion string, settings ogmiosSettings) error {
	if !settings.enabled {
		return nil
	}

	compatibilityKey, err := ogmiosCompatibilityKey(settings.image)
	if err != nil {
		return err
	}

	supportedNodeVersions, ok := supportedOgmiosNodeVersions[compatibilityKey]
	if !ok {
		return unsupportedSpec("ogmios image tag %q is not supported", mustContainerImageTag(settings.image))
	}

	nodeVersion = strings.TrimSpace(nodeVersion)
	if slices.Contains(supportedNodeVersions, nodeVersion) {
		return nil
	}

	return unsupportedSpec(
		"ogmios %s.* is not supported with cardano-node %s; supported cardano-node versions: %s",
		compatibilityKey,
		nodeVersion,
		strings.Join(supportedNodeVersions, ", "),
	)
}

// acceptedNetworkFingerprintChanged reports whether the CardanoNetwork's
// accepted network fingerprint differs from a freshly computed one.
//
// When this returns true, builder validation skips the ogmios/node
// compatibility check because the CR is going to be rejected as
// UnsupportedNetworkChange anyway and we want the reconciler to surface
// that specific error rather than the (less actionable) compatibility error.
func acceptedNetworkFingerprintChanged(network *yacdv1alpha1.CardanoNetwork, networkFingerprint string) bool {
	if network.Status.Network == nil {
		return false
	}
	if network.Status.Network.NetworkFingerprint != "" {
		return network.Status.Network.NetworkFingerprint != networkFingerprint
	}
	return network.Status.Network.LocalnetFingerprint != "" &&
		network.Status.Network.LocalnetFingerprint != networkFingerprint
}

// ogmiosCompatibilityKey extracts the major.minor key (e.g. "v6.14") from
// an ogmios image tag. The ogmios project publishes vMAJ.MIN.PATCH tags;
// anything else is rejected as unrecognized.
func ogmiosCompatibilityKey(image string) (string, error) {
	tag, ok := containerImageTag(image)
	if !ok {
		return "", unsupportedSpec("ogmios image %q must include a supported release tag", image)
	}
	if !strings.HasPrefix(tag, "v") {
		return "", unsupportedSpec("ogmios image tag %q is not a supported release tag", tag)
	}

	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(parts) != 3 {
		return "", unsupportedSpec("ogmios image tag %q is not a supported release tag", tag)
	}
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return "", unsupportedSpec("ogmios image tag %q is not a supported release tag", tag)
		}
	}

	return "v" + parts[0] + "." + parts[1], nil
}

// imageRepository extracts the repository portion of an OCI image reference,
// stripping any tag and digest suffix.
func imageRepository(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	if repo, _, ok := strings.Cut(image, "@"); ok {
		return repo
	}
	// Distinguish "host:port/repo" (last colon before last slash, no tag)
	// from "repo:tag" (last colon after last slash). Only the latter has a
	// tag to strip.
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		return image[:lastColon]
	}

	return image
}

// containerImageTag extracts the tag from an OCI image reference. It returns
// false when the reference has no tag (digest-only references included).
func containerImageTag(image string) (string, bool) {
	withoutDigest, _, _ := strings.Cut(strings.TrimSpace(image), "@")
	lastSlash := strings.LastIndex(withoutDigest, "/")
	lastColon := strings.LastIndex(withoutDigest, ":")
	if lastColon <= lastSlash || lastColon == len(withoutDigest)-1 {
		return "", false
	}

	return withoutDigest[lastColon+1:], true
}

// mustContainerImageTag returns containerImageTag's tag or an empty string
// when the reference does not include a tag. Use only in error message
// formatting; do not use to derive a default.
func mustContainerImageTag(image string) string {
	tag, ok := containerImageTag(image)
	if !ok {
		return ""
	}

	return tag
}
