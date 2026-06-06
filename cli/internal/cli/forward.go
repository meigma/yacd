package cli

import (
	"context"
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// chainAccess is a resolved host-access handle for a network's Ogmios/Kupo: the
// host-usable URLs, the YACD_* environment a host process consumes (env), and
// the document connect writes and prints (endpoints). It carries a port-forward
// session only when an endpoint fell through to the forward fallback; session is
// nil when every endpoint resolved to a directly-reachable URL (a probed
// externalURL, an override, or an ambient YACD_* value). The caller owns its
// lifetime and must Close it.
type chainAccess struct {
	session   kube.ForwardSession
	env       []string
	endpoints endpointsDocument
	ogmiosURL string
	kupoURL   string
}

// Close tears down any forwards and blocks until they stop. It is a no-op when
// nothing was forwarded.
func (c *chainAccess) Close() error {
	if c.session == nil {
		return nil
	}

	return c.session.Close()
}

// Done is closed when the forwards stop for any reason (used by connect's
// supervision and run's lost-forward handling). With no forward it returns a nil
// channel, which never fires — so run does not report a lost connection for an
// access that holds nothing to lose.
func (c *chainAccess) Done() <-chan struct{} {
	if c.session == nil {
		return nil
	}

	return c.session.Done()
}

// Err reports why the forwards stopped; valid only after Done has fired. It is
// nil when nothing was forwarded.
func (c *chainAccess) Err() error {
	if c.session == nil {
		return nil
	}

	return c.session.Err()
}

// connectNetwork establishes the always-forward host-access handle connect uses:
// it gates on readiness so callers get a clear "not ready" message instead of
// opaque forward errors, resolves the primary Pod, forwards every published
// chain-API endpoint, and builds the loopback YACD_* environment. The returned
// handle is live; the caller closes it. run and the funding path instead go
// through resolveChainAccess, which prefers a directly-reachable URL.
func connectNetwork(ctx context.Context, kubeClient kube.Client, namespace string, name string) (*chainAccess, error) {
	network, err := kubeClient.GetCardanoNetwork(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	if err := requireReady(network, namespace, name); err != nil {
		return nil, err
	}

	return forwardAll(ctx, kubeClient, network, namespace, name)
}

// forwardAll forwards a ready network's published chain-API endpoints and returns
// the live handle with loopback URLs. It reads no Secret. The caller owns the
// returned session and must Close it; forwardAll closes it itself only when a
// later step here fails.
func forwardAll(ctx context.Context, kubeClient kube.Client, network *yacdv1alpha1.CardanoNetwork, namespace string, name string) (*chainAccess, error) {
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

	ogmiosURL, kupoURL, err := loopbackURLs(network, session.LocalPort)
	if err != nil {
		_ = session.Close()
		return nil, err
	}

	return &chainAccess{
		session:   session,
		env:       hostEnvFromURLs(network, ogmiosURL, kupoURL),
		endpoints: documentFromURLs(network, ogmiosURL, kupoURL),
		ogmiosURL: ogmiosURL,
		kupoURL:   kupoURL,
	}, nil
}

// forwardSpecs returns the port-forward specs for a network's published
// chain-API endpoints. The remote port is the published Service port, which
// equals the primary Pod's container port by construction. node-to-node is
// intentionally excluded — host tooling does not speak that peer protocol.
func forwardSpecs(network *yacdv1alpha1.CardanoNetwork) []kube.PortForwardSpec {
	var specs []kube.PortForwardSpec
	for _, chain := range chainEndpoints(network) {
		// Require both a port to forward and a published URL, so the spec set
		// stays in lockstep with the env and document built from the same
		// endpoints.
		if chain.endpoint == nil || chain.endpoint.Port == 0 || strings.TrimSpace(chain.endpoint.URL) == "" {
			continue
		}
		specs = append(specs, kube.PortForwardSpec{Remote: chain.endpoint.Port, Name: chain.name})
	}

	return specs
}

// requireFreshStatus fails fast when a network's published status cannot be
// trusted: a stale observedGeneration or a True Degraded condition. It is the
// shared preamble for requireReady so the staleness/Degraded handling lives in
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
