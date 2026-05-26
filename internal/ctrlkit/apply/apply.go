package apply

import (
	"context"
	"fmt"

	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// UpdateMode selects how a changed object is persisted. The zero value uses
// UpdateModePatch.
type UpdateMode string

const (
	// UpdateModePatch persists changes with a merge patch from the pre-mutation
	// object.
	UpdateModePatch UpdateMode = "Patch"
	// UpdateModeUpdate persists changes with a full object update.
	UpdateModeUpdate UpdateMode = "Update"
)

// OwnedObjectOptions configures [ApplyOwnedObject].
type OwnedObjectOptions[T client.Object] struct {
	// Current is the empty object instance used for the live read.
	Current T
	// Default mutates a deep copy of desired before the object is read or
	// created.
	Default func(desired T) error
	// OwnerConflict maps a generic owner conflict into the caller's status
	// error contract.
	OwnerConflict func(error) error
	// Validate runs for existing objects after owner validation and before
	// mutation. It is not called for newly-created objects.
	Validate func(current T, desired T) error
	// Mutate copies reconciled fields from desired to current for existing
	// objects. It must preserve Kubernetes-assigned or externally-owned fields
	// the controller does not own, and is not called for newly-created objects.
	Mutate func(current T, desired T) error
	// UpdateMode selects patch or full update when current changes. The zero
	// value uses UpdateModePatch.
	UpdateMode UpdateMode
}

// ApplyOwnedObject reconciles the common create/read/owner-check/mutate/persist
// skeleton for controller-owned child objects. Missing objects are created from
// the defaulted desired copy directly; Validate and Mutate only run when an
// existing object is found and has the expected controller owner.
func ApplyOwnedObject[T client.Object](
	ctx context.Context,
	c client.Client,
	desired T,
	options OwnedObjectOptions[T],
) (controllerutil.OperationResult, T, error) {
	var zero T

	desiredCopy, err := cloneObject(desired)
	if err != nil {
		return controllerutil.OperationResultNone, zero, err
	}
	if options.Default != nil {
		if err := options.Default(desiredCopy); err != nil {
			return controllerutil.OperationResultNone, zero, err
		}
	}
	if err := ctrlmetadata.ValidateDesiredControllerOwner(desiredCopy); err != nil {
		return controllerutil.OperationResultNone, zero, err
	}

	current := options.Current
	err = c.Get(ctx, ctrlmetadata.ObjectKey(desiredCopy), current)
	if apierrors.IsNotFound(err) {
		if err := c.Create(ctx, desiredCopy); err != nil {
			return controllerutil.OperationResultNone, zero, err
		}

		return controllerutil.OperationResultCreated, desiredCopy, nil
	}
	if err != nil {
		return controllerutil.OperationResultNone, zero, err
	}

	if err := ctrlmetadata.ValidateControllerOwner(current, desiredCopy); err != nil {
		if options.OwnerConflict != nil {
			err = options.OwnerConflict(err)
		}
		return controllerutil.OperationResultNone, current, err
	}
	if options.Validate != nil {
		if err := options.Validate(current, desiredCopy); err != nil {
			return controllerutil.OperationResultNone, current, err
		}
	}

	before, err := cloneObject(current)
	if err != nil {
		return controllerutil.OperationResultNone, current, err
	}
	if options.Mutate != nil {
		if err := options.Mutate(current, desiredCopy); err != nil {
			return controllerutil.OperationResultNone, current, err
		}
	}

	if equality.Semantic.DeepEqual(before, current) {
		return controllerutil.OperationResultNone, current, nil
	}
	if options.UpdateMode == UpdateModeUpdate {
		if err := c.Update(ctx, current); err != nil {
			return controllerutil.OperationResultNone, current, err
		}
		return controllerutil.OperationResultUpdated, current, nil
	}
	if err := c.Patch(ctx, current, client.MergeFrom(before)); err != nil {
		return controllerutil.OperationResultNone, current, err
	}

	return controllerutil.OperationResultUpdated, current, nil
}

// cloneObject deep-copies obj and asserts the result back to T, returning an
// error when the runtime type does not match.
func cloneObject[T client.Object](obj T) (T, error) {
	cloned, ok := obj.DeepCopyObject().(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("clone %T: unexpected deep-copy type", obj)
	}

	return cloned, nil
}
