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

const (
	conditionReady    = "Ready"
	conditionDegraded = "Degraded"
)

// WaitReady polls a CardanoNetwork until it is usable or fails.
func WaitReady(
	ctx context.Context,
	client Client,
	namespace string,
	name string,
	timeout time.Duration,
) (*yacdv1alpha1.CardanoNetwork, error) {
	var latest *yacdv1alpha1.CardanoNetwork
	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be greater than 0")
	}

	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		network, err := client.GetCardanoNetwork(ctx, namespace, name)
		if err != nil {
			return false, err
		}
		latest = network

		degraded := FreshCondition(network, conditionDegraded)
		if degraded != nil && degraded.Status == metav1.ConditionTrue {
			return false, fmt.Errorf("cardanonetwork %s/%s is degraded: %s: %s", namespace, name, degraded.Reason, degraded.Message)
		}

		ready := FreshCondition(network, conditionReady)
		if ready != nil && ready.Status == metav1.ConditionTrue {
			return true, nil
		}

		return false, nil
	})
	if err != nil {
		if latest != nil {
			ready := apimeta.FindStatusCondition(latest.Status.Conditions, conditionReady)
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

// FreshCondition returns a condition only when it observes the current generation.
func FreshCondition(network *yacdv1alpha1.CardanoNetwork, conditionType string) *metav1.Condition {
	condition := apimeta.FindStatusCondition(network.Status.Conditions, conditionType)
	if condition == nil {
		return nil
	}
	if condition.ObservedGeneration < network.Generation {
		return nil
	}

	return condition
}
