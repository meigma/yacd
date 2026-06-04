package cardanonetwork

import (
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
)

const (
	// localnetFingerprintAnno is the annotation key carrying the accepted
	// localnet plan fingerprint on the primary PVC and Deployment pod
	// template. Drift between current and desired triggers a hard error
	// (PVC) or a Pod template hash roll (Deployment).
	localnetFingerprintAnno = ctrlannotations.LocalnetFingerprint

	// networkFingerprintAnno is the mode-neutral annotation key carrying the
	// accepted network fingerprint on owned resources.
	networkFingerprintAnno = ctrlannotations.NetworkFingerprint

	dbSyncSidecarRevisionAnno = ctrlannotations.DBSyncSidecarRevision
)

// cardanoNetworkOwnedAnnotations enumerates every annotation key this
// controller takes ownership of on its owned objects. mergeOwnedAnnotations
// preserves these keys on existing objects and discards every other
// annotation that has crept onto the live object.
//
// Add new owned annotations here so future extensions of mergeOwnedAnnotations
// pick them up automatically.
var cardanoNetworkOwnedAnnotations = []string{
	localnetFingerprintAnno,
	networkFingerprintAnno,
	ctrlannotations.RequestedStorageClass,
	dbSyncSidecarRevisionAnno,
}

// mergeOwnedAnnotations preserves the cardanonetwork-owned annotation set
// from current onto desired and discards any unrelated annotations that live
// on the cluster object but are not owned by this controller.
func mergeOwnedAnnotations(current map[string]string, desired map[string]string) map[string]string {
	return ctrlmetadata.MergeOwnedAnnotations(current, desired, cardanoNetworkOwnedAnnotations...)
}
