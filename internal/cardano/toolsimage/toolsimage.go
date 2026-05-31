// Package toolsimage holds the shared default reference for the YACD
// cardano-tools utility image.
//
// Both the CardanoNetwork and CardanoDBSync controllers stage Cardano network
// artifacts with the cardano-tools image (fetch/generate/serve/report). Keeping
// the repository, packaging revision, and reference formula in one
// controller-free package lets both controllers agree on the same default
// without duplicating the constants or importing each other.
package toolsimage

import (
	"fmt"
	"strings"
)

const (
	// Repository is the cardano-tools image repository.
	Repository = "ghcr.io/meigma/yacd/cardano-tools"

	// Revision is the YACD packaging revision suffix. The published image tag
	// is "<toolVersion>-<Revision>" (for example "11.0.1-yacd.0"), tracking the
	// upstream cardano-node version with a separate YACD packaging counter.
	Revision = "yacd.0"
)

// Reference resolves the cardano-tools image reference for a given Cardano tool
// version. A non-empty override (the manager's --default-cardano-tools-image
// flag) always wins; otherwise the built-in pinned reference
// "<Repository>:<toolVersion>-<Revision>" is used.
func Reference(override, toolVersion string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}

	return fmt.Sprintf("%s:%s-%s", Repository, toolVersion, Revision)
}
