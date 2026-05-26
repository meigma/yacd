package localnet

import (
	"path"
	"time"
)

// Spec defaults applied when the caller leaves fields zero.
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
)

// Generated environment filenames produced inside EnvDir.
const (
	// manifestFileName is the localnet plan marker filename under EnvDir.
	manifestFileName = "yacd-localnet-plan.json"

	// configFileName is the cardano-testnet-generated node config filename.
	configFileName = "configuration.yaml"
)

// DefaultSpec returns the YACD-recommended cardano-testnet create-env inputs
// used as the basis for zero-field overrides in BuildPlan.
func DefaultSpec() Spec {
	return Spec{
		NetworkMagic: defaultNetworkMagic,
		PoolCount:    defaultPoolCount,
		Timing: Timing{
			SlotLength:  defaultSlotLength,
			EpochLength: defaultEpochLength,
		},
		Paths: Paths{
			StateDir: defaultStateDir,
			EnvDir:   path.Join(defaultStateDir, "env"),
		},
		Tool: Tool{
			Binary: defaultToolBinary,
		},
	}
}
