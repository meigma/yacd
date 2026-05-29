package cardanonetwork

import (
	"fmt"

	controllerchildren "github.com/meigma/yacd/internal/controller/children"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	return ctrlstatus.NewConditionError(string(conditionReasonResourceConflict), format, args...)
}

// controllerOwnerConflict adapts the ctrlkit ownership-validation error into a
// resourceConflict for the reconciler's typed-error flow.
func controllerOwnerConflict(err error) error {
	return resourceConflict("%s", err.Error())
}

// childBeingDeleted adapts a terminating owned child into the
// CardanoNetwork condition contract. Reconciliation must fail closed until
// Kubernetes finishes deleting the old object.
func childBeingDeleted[T client.Object](current T, _ T) error {
	return controllerchildren.BeingDeleted(string(conditionReasonChildBeingDeleted), current)
}

// primaryStateLost reports that a missing primary state PVC cannot be safely
// recreated because another owned runtime object proves this CardanoNetwork
// already accepted durable state.
func primaryStateLost(format string, args ...any) statusConditionError {
	return ctrlstatus.NewConditionError(string(conditionReasonPrimaryStateLost), format, args...)
}

// unsupportedWorkloadChange reports that an existing owned object has drifted
// from desired in a field Kubernetes will not let us update in place
// (Deployment selector, RoleBinding roleRef, etc.). The reconciler treats it
// as a hard error and emits a Degraded condition.
func unsupportedWorkloadChange(format string, args ...any) statusConditionError {
	return ctrlstatus.NewConditionError(string(conditionReasonUnsupportedWorkloadChange), format, args...)
}

// unsupportedNetworkChange reports that the accepted mode-neutral network
// fingerprint has changed after CardanoNetwork acceptance. The CR must be
// deleted and recreated to change network parameters.
func unsupportedNetworkChange(format string, args ...any) statusConditionError {
	return ctrlstatus.NewConditionError(string(conditionReasonUnsupportedNetworkChange), format, args...)
}

// unsupportedLocalnetChange reports that the accepted localnet fingerprint
// has changed after CardanoNetwork acceptance. The CR must be deleted and
// recreated to change localnet parameters.
func unsupportedLocalnetChange(format string, args ...any) statusConditionError {
	return ctrlstatus.NewConditionError(string(conditionReasonUnsupportedLocalnetChange), format, args...)
}

// missingNetworkFingerprint reports that the primary node PVC has lost the
// mode-neutral network fingerprint annotation. The CR must be deleted and
// recreated to recover.
func missingNetworkFingerprint(format string, args ...any) statusConditionError {
	return ctrlstatus.NewConditionError(string(conditionReasonMissingNetworkFingerprint), format, args...)
}

// missingLocalnetFingerprint reports that the primary node PVC has lost the
// localnet fingerprint annotation. The CR must be deleted and recreated to
// recover.
func missingLocalnetFingerprint(format string, args ...any) statusConditionError {
	return ctrlstatus.NewConditionError(string(conditionReasonMissingLocalnetFingerprint), format, args...)
}
