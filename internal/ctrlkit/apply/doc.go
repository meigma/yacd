// Package apply contains shared reconciliation apply mechanics for
// controller-owned Kubernetes child objects.
//
// ApplyOwnedObject encodes the common owned-child contract used by reconcilers:
// callers build a fully-owned desired object, the helper creates it when
// missing, refuses same-name objects controlled by someone else, lets callers
// validate immutable live state before mutation, mutates only the fields the
// controller owns, and persists changes only when the semantic object changed.
//
// The desired object should already contain its controller reference and any
// labels or annotations that define the controller-owned identity. Defaulting is
// optional and runs against a deep copy of desired before create/read logic so
// API-defaulted desired state can be compared consistently with live state. If
// the object is missing, that defaulted desired copy is created directly;
// Validate and Mutate callbacks are not invoked on the create path.
//
// Mutation callbacks must preserve Kubernetes-assigned or externally-owned
// fields unless the controller intentionally owns them. Validate callbacks are
// the place to reject immutable drift such as selectors, role references,
// storage classes, accepted fingerprints, or other bootstrap identity. Owner
// conflicts should be mapped by callers into their status-facing error contract
// when the controller treats such conflicts as degraded user-visible state.
// Existing-object changes use merge patch by default; callers opt into full
// object update with UpdateModeUpdate when a resource's API semantics require
// it.
package apply
