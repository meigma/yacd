package cardanodbsync

import (
	"sort"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
)

// PrimarySidecarClaimSelection is the deterministic selection result for a
// set of same-network primarySidecar CardanoDBSync claims.
type PrimarySidecarClaimSelection struct {
	// Incumbent is the claim that is allowed to publish and attach
	// primary-sidecar material. Nil means no claim exists.
	Incumbent *yacdv1alpha1.CardanoDBSync

	// ConflictingPeerKeys names primarySidecar claims that are not the
	// incumbent and should report PlacementConflict.
	ConflictingPeerKeys []string
}

// SelectPrimarySidecarClaim selects the deterministic incumbent from
// non-deleting primarySidecar CardanoDBSync claims.
//
// SelectPrimarySidecarClaim treats the oldest claim as the incumbent, breaking
// ties by UID and then namespace/name. The input slice is never modified.
func SelectPrimarySidecarClaim(claims []yacdv1alpha1.CardanoDBSync) PrimarySidecarClaimSelection {
	if len(claims) == 0 {
		return PrimarySidecarClaimSelection{}
	}

	sorted := make([]yacdv1alpha1.CardanoDBSync, len(claims))
	copy(sorted, claims)
	sort.Slice(sorted, func(i, j int) bool {
		return primarySidecarClaimLess(sorted[i], sorted[j])
	})

	conflictingPeerKeys := make([]string, 0, len(sorted)-1)
	for i := 1; i < len(sorted); i++ {
		conflictingPeerKeys = append(conflictingPeerKeys, primarySidecarClaimKey(&sorted[i]))
	}

	return PrimarySidecarClaimSelection{
		Incumbent:           &sorted[0],
		ConflictingPeerKeys: conflictingPeerKeys,
	}
}

func primarySidecarClaimLess(left yacdv1alpha1.CardanoDBSync, right yacdv1alpha1.CardanoDBSync) bool {
	if !left.CreationTimestamp.Time.Equal(right.CreationTimestamp.Time) {
		return left.CreationTimestamp.Time.Before(right.CreationTimestamp.Time)
	}
	if left.UID != right.UID {
		return string(left.UID) < string(right.UID)
	}
	if left.Namespace != right.Namespace {
		return left.Namespace < right.Namespace
	}
	return left.Name < right.Name
}

func primarySidecarClaimKey(dbSync *yacdv1alpha1.CardanoDBSync) string {
	if dbSync == nil {
		return ""
	}
	if dbSync.Namespace == "" {
		return dbSync.Name
	}
	return dbSync.Namespace + "/" + dbSync.Name
}

func samePrimarySidecarClaim(left *yacdv1alpha1.CardanoDBSync, right *yacdv1alpha1.CardanoDBSync) bool {
	return left != nil &&
		right != nil &&
		left.Namespace == right.Namespace &&
		left.Name == right.Name
}
