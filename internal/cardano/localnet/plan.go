package localnet

import (
	"fmt"
	"path"
)

// BuildPlan normalizes spec and assembles the deterministic local testnet
// plan. The returned Plan carries the cardano-testnet create-env invocation,
// the stable generated-environment layout, the inputs fingerprint, and the
// JSON-serializable manifest init-container code writes for idempotency
// checks.
//
// BuildPlan treats zero-valued fields as defaults. Callers that need to express
// an explicit zero value should add that behavior at the adapter layer once a
// real scenario requires it.
func BuildPlan(spec Spec) (Plan, error) {
	spec, err := normalizeSpec(spec)
	if err != nil {
		return Plan{}, err
	}

	slotLength := formatSlotLength(spec.Timing.SlotLength)
	inputs := ManifestInputs{
		NetworkMagic: spec.NetworkMagic,
		PoolCount:    spec.PoolCount,
		EpochLength:  spec.Timing.EpochLength,
		SlotLength:   slotLength,
		EnvDir:       spec.Paths.EnvDir,
		ToolVersion:  spec.Tool.Version,
	}

	fingerprint, err := computeFingerprint(inputs)
	if err != nil {
		return Plan{}, fmt.Errorf("compute localnet fingerprint: %w", err)
	}

	return Plan{
		Spec:      spec,
		CreateEnv: buildCreateEnvInvocation(spec, slotLength),
		Layout: Layout{
			StateDir:     spec.Paths.StateDir,
			EnvDir:       spec.Paths.EnvDir,
			ConfigFile:   path.Join(spec.Paths.EnvDir, configFileName),
			ManifestFile: path.Join(spec.Paths.EnvDir, manifestFileName),
		},
		Fingerprint: fingerprint,
		Manifest: Manifest{
			SchemaVersion: manifestSchemaVersion,
			Inputs:        inputs,
			Fingerprint:   fingerprint,
		},
	}, nil
}
