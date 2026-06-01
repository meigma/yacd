package cardanodbsync

import (
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
)

const publicMainnetDBSyncUnsupportedMessage = "public mainnet CardanoDBSync is not supported until follower-node Mithril bootstrap or public mainnet primarySidecar support is implemented"

// validatePublicDBSyncSupport applies the intentionally narrow Slice 3 public
// db-sync runtime gate before any db-sync or Postgres workloads are applied.
//
// It reads the network mode and profile from the referenced CardanoNetwork spec
// rather than the parsed connection.json: the serve path no longer reads the
// network-artifacts ConfigMap, and the spec is the authoritative identity
// source for both transports. ValidatePrimarySidecarNetwork reads the same spec
// fields, so the two public gates stay consistent.
func validatePublicDBSyncSupport(
	dbSync *yacdv1alpha1.CardanoDBSync,
	network *yacdv1alpha1.CardanoNetwork,
) error {
	if network.Spec.Mode != yacdv1alpha1.CardanoNetworkModePublic {
		return nil
	}
	if network.Spec.Public == nil {
		return unsupportedSpec("public CardanoNetwork resources require spec.public")
	}
	if network.Spec.Public.Profile == yacdv1alpha1.PublicNetworkProfileMainnet {
		return unsupportedSpec(publicMainnetDBSyncUnsupportedMessage)
	}
	switch effectivePlacementMode(dbSync) {
	case yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower, yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar:
	default:
		return unsupportedSpec("public CardanoDBSync placement mode %q is not supported", effectivePlacementMode(dbSync))
	}

	return nil
}
