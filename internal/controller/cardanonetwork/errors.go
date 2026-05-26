package cardanonetwork

import (
	"fmt"

	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
)

// statusConditionError is the local alias for ctrlstatus.ConditionError used
// throughout the package. Reconcile uses errors.As against the concrete type
// to lift the carried Reason + Message into the CardanoNetwork status.
type statusConditionError = ctrlstatus.ConditionError

// unsupportedSpecError indicates the builder cannot translate a
// CardanoNetwork spec into a primary workload. The reconciler surfaces it as
// a Degraded condition with reason UnsupportedSpec rather than retrying.
type unsupportedSpecError struct {
	message string
}

// Error implements the error interface.
func (e unsupportedSpecError) Error() string {
	return e.message
}

// unsupportedSpec constructs an unsupportedSpecError with a formatted message.
func unsupportedSpec(format string, args ...any) unsupportedSpecError {
	return unsupportedSpecError{message: fmt.Sprintf(format, args...)}
}

// resourceConflict reports an unrecoverable conflict on an owned child
// (typically because another controller owns a same-name object). The
// reconciler requeues with backoff so the foreign object has time to be
// resolved by an operator.
func resourceConflict(format string, args ...any) statusConditionError {
	return ctrlstatus.NewConditionError(conditionReasonResourceConflict, format, args...)
}

// controllerOwnerConflict adapts the ctrlkit ownership-validation error into a
// resourceConflict for the reconciler's typed-error flow.
func controllerOwnerConflict(err error) error {
	return resourceConflict("%s", err.Error())
}

// unsupportedWorkloadChange reports that an existing owned object has drifted
// from desired in a field Kubernetes will not let us update in place
// (Deployment selector, RoleBinding roleRef, etc.). The reconciler treats it
// as a hard error and emits a Degraded condition.
func unsupportedWorkloadChange(format string, args ...any) statusConditionError {
	return ctrlstatus.NewConditionError(conditionReasonUnsupportedWorkloadChange, format, args...)
}

// unsupportedLocalnetChange reports that the accepted localnet fingerprint
// has changed after CardanoNetwork acceptance. The CR must be deleted and
// recreated to change localnet parameters.
func unsupportedLocalnetChange(format string, args ...any) statusConditionError {
	return ctrlstatus.NewConditionError(conditionReasonUnsupportedLocalnetChange, format, args...)
}

// missingLocalnetFingerprint reports that the primary node PVC has lost the
// localnet fingerprint annotation. The CR must be deleted and recreated to
// recover.
func missingLocalnetFingerprint(format string, args ...any) statusConditionError {
	return ctrlstatus.NewConditionError(conditionReasonMissingLocalnetFingerprint, format, args...)
}
