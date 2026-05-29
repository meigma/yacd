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
// the object is missing, ValidateCreate may reject unsafe recreation before
// that defaulted desired copy is created. Validate and Mutate callbacks are not
// invoked on the create path, and ObjectDeleting blocks mutation of live
// objects whose deletion is already in flight.
package apply
