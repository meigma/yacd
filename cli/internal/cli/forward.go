package cli

import (
	"context"
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// connectedSession is a live host-access session shared by run and connect: the
// chain-API port-forwards plus the YACD_* environment a host process consumes.
// The caller owns its lifetime and must Close it.
type connectedSession struct {
	session kube.ForwardSession
	env     []string
}

// Close tears down the forwards and blocks until they stop.
func (c *connectedSession) Close() error { return c.session.Close() }

// Done is closed when the forwards stop for any reason (used by connect's
// supervision and run's lost-forward handling).
func (c *connectedSession) Done() <-chan struct{} { return c.session.Done() }

// Err reports why the forwards stopped; valid only after Done has fired.
func (c *connectedSession) Err() error { return c.session.Err() }

// connectNetwork establishes the shared host-access session for a ready
// network: it gates on readiness so callers get a clear "not ready" message
// instead of opaque forward errors, resolves the primary Pod, forwards the
// published chain-API endpoints, reads the faucet token when a faucet is
// published, and builds the loopback YACD_* environment. The returned session
// is live; the caller closes it.
func connectNetwork(ctx context.Context, kubeClient kube.Client, namespace string, name string) (*connectedSession, error) {
	network, err := kubeClient.GetCardanoNetwork(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	if err := requireReady(network, namespace, name); err != nil {
		return nil, err
	}

	specs := forwardSpecs(network)
	if len(specs) == 0 {
		return nil, fmt.Errorf("cardanonetwork %s/%s publishes no chain-API endpoints to forward", namespace, name)
	}

	podName, err := kubeClient.PrimaryPodName(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	session, err := kubeClient.Forward(ctx, namespace, podName, specs)
	if err != nil {
		return nil, err
	}

	faucetToken, err := faucetTokenForHost(ctx, kubeClient, network, namespace, name)
	if err != nil {
		_ = session.Close()
		return nil, err
	}

	env, err := hostEnv(network, session.LocalPort, faucetToken)
	if err != nil {
		_ = session.Close()
		return nil, err
	}

	return &connectedSession{session: session, env: env}, nil
}

// forwardSpecs returns the port-forward specs for a network's published
// chain-API endpoints. The remote port is the published Service port, which
// equals the primary Pod's container port by construction. node-to-node is
// intentionally excluded — host tooling does not speak that peer protocol.
func forwardSpecs(network *yacdv1alpha1.CardanoNetwork) []kube.PortForwardSpec {
	if network.Status.Endpoints == nil {
		return nil
	}
	endpoints := network.Status.Endpoints

	candidates := []struct {
		name     string
		endpoint *yacdv1alpha1.ServiceEndpointStatus
	}{
		{name: "ogmios", endpoint: endpoints.Ogmios},
		{name: "kupo", endpoint: endpoints.Kupo},
		{name: "faucet", endpoint: endpoints.Faucet},
	}

	var specs []kube.PortForwardSpec
	for _, candidate := range candidates {
		// Require both a port to forward and a published URL, so the spec set
		// stays in lockstep with the env hostEnv builds from the same endpoints.
		if candidate.endpoint == nil || candidate.endpoint.Port == 0 || strings.TrimSpace(candidate.endpoint.URL) == "" {
			continue
		}
		specs = append(specs, kube.PortForwardSpec{Remote: candidate.endpoint.Port, Name: candidate.name})
	}

	return specs
}

// requireFreshStatus fails fast when a network's published status cannot be
// trusted: a stale observedGeneration or a True Degraded condition. It is the
// shared preamble for the readiness gates (requireReady here and
// requireFaucetReady in topup.go) so the staleness/Degraded handling lives in
// one place.
func requireFreshStatus(network *yacdv1alpha1.CardanoNetwork, namespace string, name string) error {
	if network.Status.ObservedGeneration != network.Generation {
		return fmt.Errorf(
			"cardanonetwork %s/%s status is stale: observedGeneration=%d generation=%d",
			namespace, name, network.Status.ObservedGeneration, network.Generation,
		)
	}
	if degraded := kube.FreshCondition(network, kube.ConditionDegraded); degraded != nil && degraded.Status == metav1.ConditionTrue {
		return fmt.Errorf("cardanonetwork %s/%s is degraded: %s: %s", namespace, name, degraded.Reason, degraded.Message)
	}

	return nil
}

// requireReady fails fast unless the network's status is fresh and Ready is
// True, mirroring the gating the up and topup verbs use so host access produces
// a clear "not ready" message rather than opaque per-connection forward errors.
func requireReady(network *yacdv1alpha1.CardanoNetwork, namespace string, name string) error {
	if err := requireFreshStatus(network, namespace, name); err != nil {
		return err
	}
	ready := kube.FreshCondition(network, kube.ConditionReady)
	if ready == nil {
		return fmt.Errorf("cardanonetwork %s/%s is not ready: Ready condition is missing or stale", namespace, name)
	}
	if ready.Status != metav1.ConditionTrue {
		return fmt.Errorf("cardanonetwork %s/%s is not ready", namespace, name)
	}

	return nil
}

// faucetTokenForHost reads the faucet auth token so YACD_FAUCET_TOKEN can be set
// for host tooling, but only once the faucet is actually usable: it returns an
// empty token (and no error) when the network has no faucet or its FaucetReady
// condition is not fresh-and-True, so run/connect degrade gracefully on a
// not-yet-ready faucet instead of hard-failing. A faucet that reports ready but
// publishes no usable auth Secret is a real error.
func faucetTokenForHost(ctx context.Context, kubeClient kube.Client, network *yacdv1alpha1.CardanoNetwork, namespace string, name string) (string, error) {
	if network.Status.Endpoints == nil || network.Status.Endpoints.Faucet == nil {
		return "", nil
	}
	if ready := kube.FreshCondition(network, kube.ConditionFaucetReady); ready == nil || ready.Status != metav1.ConditionTrue {
		return "", nil
	}
	if network.Status.Faucet == nil || strings.TrimSpace(network.Status.Faucet.AuthSecretName) == "" {
		return "", fmt.Errorf("cardanonetwork %s/%s publishes a faucet endpoint but no auth Secret", namespace, name)
	}

	token, err := kubeClient.GetSecretValue(ctx, namespace, network.Status.Faucet.AuthSecretName, faucetAuthTokenKey)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(token), nil
}
