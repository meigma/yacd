package localnet

import (
	"fmt"
	"path"
	"strings"
)

// normalizeSpec trims input strings, applies defaults, cleans paths, and
// validates the resulting Spec. The returned Spec is the authoritative form
// used to build the invocation, layout, fingerprint, and manifest.
func normalizeSpec(spec Spec) (Spec, error) {
	defaults := DefaultSpec()

	if spec.NetworkMagic == 0 {
		spec.NetworkMagic = defaults.NetworkMagic
	}
	if spec.PoolCount == 0 {
		spec.PoolCount = defaults.PoolCount
	}
	if spec.Timing.SlotLength == 0 {
		spec.Timing.SlotLength = defaults.Timing.SlotLength
	}
	if spec.Timing.EpochLength == 0 {
		spec.Timing.EpochLength = defaults.Timing.EpochLength
	}
	if spec.Paths.StateDir == "" {
		spec.Paths.StateDir = defaults.Paths.StateDir
	}
	if spec.Tool.Binary == "" {
		spec.Tool.Binary = defaults.Tool.Binary
	}

	stateDir, err := normalizeContainerPath(spec.Paths.StateDir, "state dir")
	if err != nil {
		return Spec{}, err
	}
	spec.Paths.StateDir = stateDir

	// EnvDir defaults relative to the already-normalized StateDir so callers
	// can override either independently.
	if spec.Paths.EnvDir == "" {
		spec.Paths.EnvDir = path.Join(spec.Paths.StateDir, "env")
	}
	envDir, err := normalizeContainerPath(spec.Paths.EnvDir, "env dir")
	if err != nil {
		return Spec{}, err
	}
	spec.Paths.EnvDir = envDir

	spec.Tool.Binary = strings.TrimSpace(spec.Tool.Binary)
	spec.Tool.Version = strings.TrimSpace(spec.Tool.Version)

	if err := validateSpec(spec); err != nil {
		return Spec{}, err
	}

	return spec, nil
}

// normalizeContainerPath trims and cleans value as an absolute container path,
// returning a field-keyed error when value is relative or empty.
func normalizeContainerPath(value string, field string) (string, error) {
	cleaned := path.Clean(strings.TrimSpace(value))
	if cleaned == "." || !strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("%s must be an absolute container path", field)
	}

	return cleaned, nil
}
