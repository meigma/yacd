package cardanodbsync

import (
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlnetworkartifacts "github.com/meigma/yacd/internal/controller/networkartifacts"
)

// validatePublicDBSyncSupport applies the intentionally narrow Slice 3 public
// db-sync runtime gate before any db-sync or Postgres workloads are applied.
func validatePublicDBSyncSupport(
	dbSync *yacdv1alpha1.CardanoDBSync,
	connection ctrlnetworkartifacts.Connection,
) error {
	if connection.Mode != yacdv1alpha1.CardanoNetworkModePublic {
		return nil
	}
	if effectivePlacementMode(dbSync) != yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower {
		return unsupportedSpec("public CardanoDBSync is supported only with dedicatedFollower placement")
	}
	if connection.Profile == yacdv1alpha1.PublicNetworkProfileMainnet {
		return unsupportedSpec("public mainnet CardanoDBSync is not supported until mainnet sizing and bootstrap support are implemented")
	}

	return nil
}
