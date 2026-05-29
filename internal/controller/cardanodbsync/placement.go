package cardanodbsync

import (
	"context"
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	conditionReasonPlacementConflict conditionReason = "PlacementConflict"
)

// reconcilePlacement gates CardanoDBSync reconciliation on the effective
// placement mode. Dedicated-follower placement preserves the existing runtime
// path; primary-sidecar placement proceeds only for the deterministic
// same-network incumbent.
func (r *CardanoDBSyncReconciler) reconcilePlacement(
	ctx context.Context,
	dbSync *yacdv1alpha1.CardanoDBSync,
) (bool, error) {
	switch effectivePlacementMode(dbSync) {
	case yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower:
		return true, nil
	case yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar:
		claims, err := r.primarySidecarClaims(ctx, dbSync.Namespace, dbSync.Spec.NetworkRef.Name)
		if err != nil {
			return false, err
		}
		selection := SelectPrimarySidecarClaim(claims)
		if selection.Incumbent == nil {
			return true, nil
		}
		if !samePrimarySidecarClaim(dbSync, selection.Incumbent) {
			return false, r.patchWorkloadApplyBlockedStatus(
				ctx,
				dbSync,
				conditionReasonPlacementConflict,
				placementConflictMessage(dbSync.Spec.NetworkRef.Name, selection),
			)
		}

		return true, nil
	default:
		return false, r.patchWorkloadApplyBlockedStatus(
			ctx,
			dbSync,
			conditionReasonUnsupportedSpec,
			fmt.Sprintf("unsupported db-sync placement mode %q", effectivePlacementMode(dbSync)),
		)
	}
}

// primarySidecarClaims lists non-deleting CardanoDBSync resources in namespace
// that request primary-sidecar placement for networkName.
func (r *CardanoDBSyncReconciler) primarySidecarClaims(
	ctx context.Context,
	namespace string,
	networkName string,
) ([]yacdv1alpha1.CardanoDBSync, error) {
	if namespace == "" || networkName == "" {
		return nil, nil
	}

	dbSyncs := &yacdv1alpha1.CardanoDBSyncList{}
	if err := r.List(ctx, dbSyncs,
		client.InNamespace(namespace),
		client.MatchingFields{cardanoDBSyncNetworkRefNameField: networkName},
	); err != nil {
		return nil, err
	}

	claims := make([]yacdv1alpha1.CardanoDBSync, 0, len(dbSyncs.Items))
	for _, candidate := range dbSyncs.Items {
		if !candidate.DeletionTimestamp.IsZero() {
			continue
		}
		if effectivePlacementMode(&candidate) == yacdv1alpha1.CardanoDBSyncPlacementModePrimarySidecar {
			claims = append(claims, candidate)
		}
	}

	return claims, nil
}

// effectivePlacementMode returns the defaulted placement mode for a
// CardanoDBSync resource.
func effectivePlacementMode(dbSync *yacdv1alpha1.CardanoDBSync) yacdv1alpha1.CardanoDBSyncPlacementMode {
	if dbSync == nil || dbSync.Spec.Placement == nil || dbSync.Spec.Placement.Mode == "" {
		return yacdv1alpha1.CardanoDBSyncPlacementModeDedicatedFollower
	}

	return dbSync.Spec.Placement.Mode
}

// placementConflictMessage returns the user-facing condition message for a
// non-incumbent primarySidecar claim on the same CardanoNetwork.
func placementConflictMessage(networkName string, selection PrimarySidecarClaimSelection) string {
	incumbent := primarySidecarClaimKey(selection.Incumbent)
	if incumbent == "" {
		incumbent = "<unknown>"
	}
	conflicts := strings.Join(selection.ConflictingPeerKeys, ", ")
	if conflicts == "" {
		conflicts = primarySidecarClaimKey(selection.Incumbent)
	}

	return fmt.Sprintf("CardanoNetwork %q already has primarySidecar incumbent %q; conflicting CardanoDBSync claims must use dedicatedFollower placement or be removed: %s", networkName, incumbent, conflicts)
}
