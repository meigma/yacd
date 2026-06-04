package operator

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// WaitForReady polls the installer's OperatorState in namespace until the
// operator reports Ready or the context is done, returning the last observed
// State. An empty namespace defaults to DefaultNamespace. It is the shared
// readiness wait both the devnet lifecycle and the install command use after an
// EnsureOperator that returns not-ready (the SSA apply does not wait for the
// workload), so a first-run install can converge while the image pulls. The
// context bounds the wait: callers that want a deadline pass a context.
func WaitForReady(ctx context.Context, inst Installer, namespace string, pollInterval time.Duration) (State, error) {
	if namespace == "" {
		namespace = DefaultNamespace
	}

	var state State
	err := wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (bool, error) {
		current, err := inst.OperatorState(ctx, namespace)
		if err != nil {
			return false, err
		}
		state = current
		return state.Ready, nil
	})
	if err != nil {
		return state, err
	}

	return state, nil
}
