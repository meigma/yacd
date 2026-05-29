package children

import (
	"strings"

	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BeingDeleted maps a live owned child with a deletionTimestamp into a
// status-facing condition error. The message names any finalizers because they
// are the usual reason Kubernetes has not completed deletion.
func BeingDeleted(reason string, current client.Object) ctrlstatus.ConditionError {
	finalizers := current.GetFinalizers()
	if len(finalizers) == 0 {
		return ctrlstatus.NewConditionError(
			reason,
			"Child %s is being deleted and cannot be reconciled until deletion completes",
			ctrlmetadata.ObjectKey(current),
		)
	}

	return ctrlstatus.NewConditionError(
		reason,
		"Child %s is being deleted and cannot be reconciled until deletion completes; blocking finalizers: %s",
		ctrlmetadata.ObjectKey(current),
		strings.Join(finalizers, ", "),
	)
}
