// Package toolsimage holds the shared default reference for the YACD
// cardano-tools utility image.
//
// Both the CardanoNetwork and CardanoDBSync controllers stage Cardano network
// artifacts with the cardano-tools image (generate/fetch/serve/stage/sync).
// Keeping the repository, packaging revision, published digest, and reference
// formula in one controller-free package lets both controllers agree on the
// same default without duplicating the constants or importing each other.
package toolsimage

import (
	"fmt"
	"strings"
)

const (
	// Repository is the cardano-tools image repository.
	Repository = "ghcr.io/meigma/yacd/cardano-tools"

	// Revision is the YACD packaging revision suffix. The published image tag
	// is "<toolVersion>-<Revision>" (for example "11.0.1-yacd.5"), tracking the
	// upstream cardano-node version with a separate YACD packaging counter.
	Revision = "yacd.5"

	// Digest pins the built-in reference to a specific published manifest so a
	// no-override install always resolves the exact image YACD was tested
	// against, regardless of tag mutation. It is the multi-arch image index
	// digest of "<Repository>:<toolVersion>-<Revision>"; update it together with
	// Revision on every cardano-tools release.
	Digest = "sha256:d3283ca5fc925f6ec01f61a54371e5ad1934088614b7cde1e1e1915424662fc4"
)

// Reference resolves the cardano-tools image reference for a given Cardano tool
// version. A non-empty override (the manager's --default-cardano-tools-image
// flag) always wins. Otherwise the built-in reference
// "<Repository>:<toolVersion>-<Revision>" is returned, digest-pinned with
// "@<Digest>" when Digest is set so the registry resolves by digest rather than
// the mutable tag.
func Reference(override, toolVersion string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}

	ref := fmt.Sprintf("%s:%s-%s", Repository, toolVersion, Revision)
	if Digest != "" {
		ref += "@" + Digest
	}

	return ref
}
