package cardanodbsync

import (
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlnetworkartifacts "github.com/meigma/yacd/internal/controller/networkartifacts"
)

const publicMainnetDBSyncUnsupportedMessage = "public mainnet CardanoDBSync is not supported until follower-node Mithril bootstrap or public mainnet primarySidecar support is implemented"

// validatePublicDBSyncSupport applies the intentionally narrow Slice 3 public
// db-sync runtime gate before any db-sync or Postgres workloads are applied.
func validatePublicDBSyncSupport(
	dbSync *yacdv1alpha1.CardanoDBSync,
	connection ctrlnetworkartifacts.Connection,
) error {
	if connection.Mode != yacdv1alpha1.CardanoNetworkModePublic {
		return nil
	}
	if connection.Profile == yacdv1alpha1.PublicNetworkProfileMainnet {
		return unsupportedSpec(publicMainnetDBSyncUnsupportedMessage)
	}
	switch effectivePlacementMode(dbSync) {
	case yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar:
	default:
		return unsupportedSpec("public CardanoDBSync placement mode %q is not supported", effectivePlacementMode(dbSync))
	}

	return nil
}
