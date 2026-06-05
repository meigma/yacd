package cardanonetwork

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
)

const (
	// nodePortRangeMin and nodePortRangeMax bound a pinned chain API node port.
	// They mirror the Kubernetes default service-node-port-range. The CRD marker
	// enforces the ceiling; this Go floor check is conditional so that 0 stays
	// legal as "let Kubernetes auto-assign".
	nodePortRangeMin int32 = 30000
	nodePortRangeMax int32 = 32767
)

// externalURLSchemes is the lenient set of schemes accepted for a chain API
// externalURL. The CLI probes the URL before trusting it, so this only rejects
// obviously wrong values rather than pinning a scheme per component.
var externalURLSchemes = []string{"ws", "wss", "http", "https"}

// validateChainAPIServiceExposure rejects service-exposure and externalURL
// settings the CRD markers cannot express: the conditional nodePort/type
// coupling, the nodePort floor, the externalURL shape, and any exposure set on
// a disabled sidecar. component is "ogmios" or "kupo".
func validateChainAPIServiceExposure(component string, enabled bool, service *yacdv1alpha1.ServiceExposureSpec, externalURL string) error {
	externalURL = strings.TrimSpace(externalURL)

	isNodePort := service != nil && service.Type == yacdv1alpha1.ChainAPIServiceTypeNodePort
	var nodePort int32
	if service != nil {
		nodePort = service.NodePort
	}

	if !enabled {
		if isNodePort || nodePort != 0 || externalURL != "" {
			return unsupportedSpec("%s service exposure requires %s to be enabled", component, component)
		}

		return nil
	}

	if nodePort != 0 && !isNodePort {
		return unsupportedSpec("%s service nodePort is only valid when service.type is NodePort", component)
	}
	if nodePort != 0 && (nodePort < nodePortRangeMin || nodePort > nodePortRangeMax) {
		return unsupportedSpec("%s service nodePort must be in the %d-%d range", component, nodePortRangeMin, nodePortRangeMax)
	}

	return validateExternalURL(component, externalURL)
}

// validateExternalURL rejects an externalURL that is not an absolute URL with a
// host or that uses an unsupported scheme. An empty value is allowed.
func validateExternalURL(component string, externalURL string) error {
	if externalURL == "" {
		return nil
	}

	parsed, err := url.Parse(externalURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return unsupportedSpec("%s externalURL must be an absolute URL with a host", component)
	}
	if !slices.Contains(externalURLSchemes, parsed.Scheme) {
		return unsupportedSpec("%s externalURL scheme %q is not supported; supported: %s", component, parsed.Scheme, strings.Join(externalURLSchemes, ", "))
	}

	return nil
}

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
// node, the optional chain API sidecars, and the always-on cardano-tools
// serve sidecar. Each must claim a distinct port. serveEnabled reserves the
// fixed serve port only for the networks that run the serve sidecar.
func validatePrimaryWorkloadPorts(nodePort int32, ogmios ogmiosSettings, kupo kupoSettings, serveEnabled bool) error {
	seen := map[int32]string{
		nodePort: cardanoNodePortName,
	}
	// The serve sidecar binds a fixed port whenever it runs, so it is reserved
	// up front and a user-set node or sidecar port that collides with it is
	// rejected.
	if serveEnabled {
		if nodePort == defaultServePort {
			return unsupportedSpec("node port %d conflicts with %s port", nodePort, servePortName)
		}
		seen[defaultServePort] = servePortName
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

// acceptedNetworkFingerprintChanged reports whether the accepted
// mode-neutral network fingerprint from owned runtime material differs from
// a freshly computed one.
//
// When this returns true, builder validation skips the ogmios/node
// compatibility check because the CR is going to be rejected as
// UnsupportedNetworkChange anyway and we want the reconciler to surface
// that specific error rather than the (less actionable) compatibility error.
func acceptedNetworkFingerprintChanged(acceptedIdentity acceptedNetworkIdentity, networkFingerprint string) bool {
	if acceptedIdentity.NetworkFingerprint != "" {
		return acceptedIdentity.NetworkFingerprint != networkFingerprint
	}
	return acceptedIdentity.LocalnetFingerprint != "" &&
		acceptedIdentity.LocalnetFingerprint != networkFingerprint
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
