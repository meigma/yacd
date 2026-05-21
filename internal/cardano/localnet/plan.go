package localnet

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultNetworkMagic is the local development testnet magic used when a
	// caller leaves NetworkMagic unset.
	defaultNetworkMagic = 42

	// defaultPoolCount is the generated stake pool count used by default.
	defaultPoolCount = 1

	// defaultEpochLength is the default number of slots per local epoch.
	defaultEpochLength = 500

	// defaultSlotLength is the default local slot duration.
	defaultSlotLength = 100 * time.Millisecond

	// defaultStateDir is the default durable state mount root inside the
	// cardano-testnet container.
	defaultStateDir = "/state"

	// defaultToolBinary is the default executable used for cardano-testnet.
	defaultToolBinary = "cardano-testnet"

	// manifestFileName is the localnet plan marker filename under EnvDir.
	manifestFileName = "yacd-localnet-plan.json"

	// configFileName is the cardano-testnet-generated node config filename.
	configFileName = "configuration.yaml"
)

// DefaultSpec returns the default local testnet create-env inputs used by YACD.
func DefaultSpec() Spec {
	stateDir := defaultStateDir

	return Spec{
		NetworkMagic: defaultNetworkMagic,
		PoolCount:    defaultPoolCount,
		Timing: Timing{
			SlotLength:  defaultSlotLength,
			EpochLength: defaultEpochLength,
		},
		Paths: Paths{
			StateDir: stateDir,
			EnvDir:   path.Join(stateDir, "env"),
		},
		Tool: Tool{
			Binary: defaultToolBinary,
		},
	}
}

// BuildPlan validates and normalizes spec into a deterministic local testnet
// plan.
//
// BuildPlan treats zero-valued fields as defaults. Callers that need to express
// an explicit zero value should add that behavior at the adapter layer once a
// real scenario requires it.
func BuildPlan(spec Spec) (Plan, error) {
	normalized, err := normalizeSpec(spec)
	if err != nil {
		return Plan{}, err
	}

	slotLength := formatSlotLength(normalized.Timing.SlotLength)
	inputs := ManifestInputs{
		NetworkMagic: normalized.NetworkMagic,
		PoolCount:    normalized.PoolCount,
		EpochLength:  normalized.Timing.EpochLength,
		SlotLength:   slotLength,
		EnvDir:       normalized.Paths.EnvDir,
		ToolVersion:  normalized.Tool.Version,
	}
	fingerprint, err := computeFingerprint(inputs)
	if err != nil {
		return Plan{}, fmt.Errorf("compute localnet fingerprint: %w", err)
	}

	layout := Layout{
		StateDir:     normalized.Paths.StateDir,
		EnvDir:       normalized.Paths.EnvDir,
		ConfigFile:   path.Join(normalized.Paths.EnvDir, configFileName),
		ManifestFile: path.Join(normalized.Paths.EnvDir, manifestFileName),
	}

	return Plan{
		Spec: normalized,
		CreateEnv: Invocation{
			Command: normalized.Tool.Binary,
			Args: []string{
				"create-env",
				"--num-pool-nodes", strconv.Itoa(normalized.PoolCount),
				"--testnet-magic", strconv.FormatInt(normalized.NetworkMagic, 10),
				"--epoch-length", strconv.Itoa(normalized.Timing.EpochLength),
				"--slot-length", slotLength,
				"--output", normalized.Paths.EnvDir,
			},
		},
		Layout:      layout,
		Fingerprint: fingerprint,
		Manifest: Manifest{
			SchemaVersion: manifestSchemaVersion,
			Inputs:        inputs,
			Fingerprint:   fingerprint,
		},
	}, nil
}

// normalizeSpec applies defaults, cleans paths, and validates the resulting
// localnet spec.
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

	stateDir, err := cleanAbsolutePath(spec.Paths.StateDir, "state dir")
	if err != nil {
		return Spec{}, err
	}
	spec.Paths.StateDir = stateDir

	if spec.Paths.EnvDir == "" {
		spec.Paths.EnvDir = path.Join(spec.Paths.StateDir, "env")
	}
	envDir, err := cleanAbsolutePath(spec.Paths.EnvDir, "env dir")
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
