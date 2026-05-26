package kube

import (
	"context"
	"fmt"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// pollInterval is the cadence at which WaitReady polls the CardanoNetwork
// status while the deadline has not been reached.
const pollInterval = 2 * time.Second

// WaitReady polls a CardanoNetwork through the Client port until it is Ready,
// becomes Degraded, or the deadline expires. The returned network is the
// latest observed value, which may be useful to callers regardless of
// outcome.
func WaitReady(
	ctx context.Context,
	client Client,
	namespace string,
	name string,
	timeout time.Duration,
) (*yacdv1alpha1.CardanoNetwork, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be greater than 0")
	}

	var latest *yacdv1alpha1.CardanoNetwork
	err := wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		network, err := client.GetCardanoNetwork(ctx, namespace, name)
		if err != nil {
			return false, err
		}
		latest = network

		// Degraded is terminal-for-now; surface the reason/message immediately.
		degraded := FreshCondition(network, ConditionDegraded)
		if degraded != nil && degraded.Status == metav1.ConditionTrue {
			return false, fmt.Errorf("cardanonetwork %s/%s is degraded: %s: %s", namespace, name, degraded.Reason, degraded.Message)
		}

		ready := FreshCondition(network, ConditionReady)
		if ready != nil && ready.Status == metav1.ConditionTrue {
			return true, nil
		}

		return false, nil
	})
	if err != nil {
		if latest != nil {
			// Prefer the latest Ready condition for a precise failure message,
			// distinguishing stale-status timeouts from true not-ready timeouts.
			ready := apimeta.FindStatusCondition(latest.Status.Conditions, string(ConditionReady))
			if ready != nil {
				if ready.ObservedGeneration < latest.Generation {
					return latest, fmt.Errorf(
						"cardanonetwork %s/%s did not become ready: Ready condition observed generation %d is older than current generation %d",
						namespace,
						name,
						ready.ObservedGeneration,
						latest.Generation,
					)
				}
				return latest, fmt.Errorf("cardanonetwork %s/%s did not become ready: %s: %s", namespace, name, ready.Reason, ready.Message)
			}
		}
		return latest, fmt.Errorf("wait for cardanonetwork %s/%s: %w", namespace, name, err)
	}

	return latest, nil
}
