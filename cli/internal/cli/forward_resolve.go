package cli

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
)

// endpointProbeTimeout bounds the reachability probe of a published externalURL.
// It is short so the sad path — an externalURL set but unreachable — adds only a
// brief failed-dial latency before falling back to a port-forward.
const endpointProbeTimeout = 2 * time.Second

// chainOverrides carries explicit per-endpoint URL overrides (the resolver's
// highest-precedence rung), supplied by command flags. An empty field means "no
// override for this endpoint".
type chainOverrides struct {
	OgmiosURL string
	KupoURL   string
}

// resolveChainAccess resolves host-usable Ogmios and Kupo URLs for an already-
// fetched, already-readiness-gated network, opening a port-forward only for the
// endpoints that fall through to the last rung. Per endpoint the precedence is:
//
//  1. an explicit override (a command flag),
//  2. an ambient YACD_OGMIOS_URL / YACD_KUPO_URL (e.g. inside `yacd run`),
//  3. the operator-asserted status.externalURL, when the prober finds it reachable,
//  4. an ephemeral port-forward to loopback (today's behaviour, the fallback).
//
// Endpoints resolved by rungs 1–3 need no forward; only the remainder are
// forwarded, in a single Forward call. The returned handle's session is nil when
// nothing was forwarded.
func (commandContext *commandContext) resolveChainAccess(
	ctx context.Context,
	kubeClient kube.Client,
	network *yacdv1alpha1.CardanoNetwork,
	namespace string,
	name string,
	overrides chainOverrides,
) (*chainAccess, error) {
	overrideByName := map[string]string{"ogmios": overrides.OgmiosURL, "kupo": overrides.KupoURL}
	resolved := map[string]string{}
	var toForward []chainEndpoint

	for _, chain := range chainEndpoints(network) {
		if direct := commandContext.resolveDirectURL(ctx, chain, overrideByName[chain.name]); direct != "" {
			resolved[chain.name] = direct
			continue
		}
		// No directly-reachable URL: forward it, but only when it is actually
		// forwardable (published with a port and URL). An unpublished endpoint
		// is simply absent, exactly as before P3.
		if chain.endpoint != nil && chain.endpoint.Port != 0 && strings.TrimSpace(chain.endpoint.URL) != "" {
			toForward = append(toForward, chain)
		}
	}

	session, err := commandContext.forwardSubset(ctx, kubeClient, namespace, name, toForward, resolved)
	if err != nil {
		return nil, err
	}

	ogmiosURL := resolved["ogmios"]
	kupoURL := resolved["kupo"]

	return &chainAccess{
		session:   session,
		env:       hostEnvFromURLs(network, ogmiosURL, kupoURL),
		endpoints: documentFromURLs(network, ogmiosURL, kupoURL),
		ogmiosURL: ogmiosURL,
		kupoURL:   kupoURL,
	}, nil
}

// resolveDirectURL returns the directly-reachable URL for one chain endpoint via
// the override, ambient-env, and probed-externalURL rungs, or "" when none apply
// and the endpoint must be forwarded. chain.key is the endpoint's YACD_* env var
// name, so the ambient rung reads exactly the variable the contract publishes.
func (commandContext *commandContext) resolveDirectURL(ctx context.Context, chain chainEndpoint, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	if ambient := strings.TrimSpace(os.Getenv(chain.key)); ambient != "" {
		return ambient
	}
	if chain.endpoint != nil {
		if external := strings.TrimSpace(chain.endpoint.ExternalURL); external != "" {
			if commandContext.endpointProber(ctx, external) == nil {
				return external
			}
		}
	}

	return ""
}

// forwardSubset forwards the given endpoints (a subset of the network's chain
// APIs) in one Forward call and records each one's loopback URL in resolved. It
// returns a nil session when there is nothing to forward, so an all-direct
// resolution holds no forward open.
func (commandContext *commandContext) forwardSubset(
	ctx context.Context,
	kubeClient kube.Client,
	namespace string,
	name string,
	toForward []chainEndpoint,
	resolved map[string]string,
) (kube.ForwardSession, error) {
	if len(toForward) == 0 {
		return nil, nil
	}

	specs := make([]kube.PortForwardSpec, 0, len(toForward))
	for _, chain := range toForward {
		specs = append(specs, kube.PortForwardSpec{Remote: chain.endpoint.Port, Name: chain.name})
	}

	podName, err := kubeClient.PrimaryPodName(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	session, err := kubeClient.Forward(ctx, namespace, podName, specs)
	if err != nil {
		return nil, err
	}

	for _, chain := range toForward {
		local, ok := session.LocalPort(chain.endpoint.Port)
		if !ok {
			continue
		}
		loopback, err := loopbackURL(chain.endpoint.URL, local)
		if err != nil {
			_ = session.Close()
			return nil, err
		}
		resolved[chain.name] = loopback
	}

	return session, nil
}

// connectChain is run's entry point: it fetches the network, gates on readiness
// (so a not-ready network yields a clear message rather than opaque resolve
// errors), and resolves chain access with the given overrides.
func (commandContext *commandContext) connectChain(
	ctx context.Context,
	kubeClient kube.Client,
	namespace string,
	name string,
	overrides chainOverrides,
) (*chainAccess, error) {
	network, err := kubeClient.GetCardanoNetwork(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	if err := requireReady(network, namespace, name); err != nil {
		return nil, err
	}

	return commandContext.resolveChainAccess(ctx, kubeClient, network, namespace, name, overrides)
}

// probeEndpointReachable is the default EndpointProber: it reports whether
// rawURL's host:port accepts a TCP connection within endpointProbeTimeout. It is
// scheme-agnostic (ws/wss/http/https all reduce to a TCP dial), so a stale
// localhost carried to the wrong machine, or a typo'd ingress, simply fails and
// the resolver falls back to port-forwarding.
func probeEndpointReachable(ctx context.Context, rawURL string) error {
	address, err := dialAddress(rawURL)
	if err != nil {
		return err
	}

	dialer := net.Dialer{Timeout: endpointProbeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("probe %s: %w", rawURL, err)
	}

	return conn.Close()
}

// dialAddress derives the host:port to dial from an absolute URL, inferring the
// default port from the scheme when the URL omits one.
func dialAddress(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", fmt.Errorf("parse externalURL %q: %w", rawURL, err)
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("externalURL %q has no host", rawURL)
	}
	port := parsed.Port()
	if port == "" {
		port = defaultPortForScheme(parsed.Scheme)
		if port == "" {
			return "", fmt.Errorf("externalURL %q has no port and an unknown scheme %q", rawURL, parsed.Scheme)
		}
	}

	return net.JoinHostPort(host, port), nil
}

// defaultPortForScheme maps the lenient chain-API URL schemes to their default
// TCP port, or "" when the scheme is unrecognised.
func defaultPortForScheme(scheme string) string {
	switch strings.ToLower(scheme) {
	case "ws", "http":
		return "80"
	case "wss", "https":
		return "443"
	default:
		return ""
	}
}
