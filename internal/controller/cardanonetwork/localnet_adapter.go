package cardanonetwork

import (
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
)

const (
	// localnetStateDir is the durable state mount root used by the first
	// CardanoNetwork workload shape.
	localnetStateDir = "/state"

	// localnetEnvDir is the cardano-testnet create-env output directory used by
	// the first CardanoNetwork workload shape.
	localnetEnvDir = "/state/env"
)

// localnetSpecFromCardanoNetwork converts supported CardanoNetwork local-mode
// fields into the localnet package's cardano-testnet input shape.
func localnetSpecFromCardanoNetwork(network *yacdv1alpha1.CardanoNetwork) (localnet.Spec, error) {
	if network.Spec.Mode != yacdv1alpha1.CardanoNetworkModeLocal {
		return localnet.Spec{}, fmt.Errorf("mode %q is not supported", network.Spec.Mode)
	}
	if network.Spec.Local == nil {
		return localnet.Spec{}, fmt.Errorf("local spec is required")
	}
	if network.Spec.Public != nil {
		return localnet.Spec{}, fmt.Errorf("public spec is not supported with local mode")
	}

	local := network.Spec.Local
	if local.Era == yacdv1alpha1.CardanoEraBabbage {
		return localnet.Spec{}, fmt.Errorf("local era %q is not supported", local.Era)
	}
	if local.Genesis != nil {
		return localnet.Spec{}, fmt.Errorf("local genesis tuning is not supported")
	}
	if local.Topology.Pools.Defaults != nil {
		return localnet.Spec{}, fmt.Errorf("local pool defaults are not supported")
	}

	return localnet.Spec{
		NetworkMagic: local.NetworkMagic,
		PoolCount:    int(local.Topology.Pools.Count),
		Timing: localnet.Timing{
			SlotLength:  local.Timing.SlotLength.Duration,
			EpochLength: int(local.Timing.EpochLength),
		},
		Paths: localnet.Paths{
			StateDir: localnetStateDir,
			EnvDir:   localnetEnvDir,
		},
		Tool: localnet.Tool{
			Version: network.Spec.Node.Version,
		},
	}, nil
}
