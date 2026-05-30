package cli

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
)

// The YACD_* environment variables are the harness's stable, versioned
// integration surface: tests read these instead of parsing any YACD file. The
// names are identical whether a command runs on the host (run/connect, over
// port-forwards) or inside the primary Pod (exec, over cluster DNS); only the
// values adapt. CARDANO_NODE_SOCKET_PATH is unprefixed because it is the name
// cardano-cli already expects.
//
// Contract version 1. Adding a variable is backward compatible; renaming or
// removing one is a breaking change to this contract.
const (
	envNetwork        = "YACD_NETWORK"
	envNamespace      = "YACD_NAMESPACE"
	envNetworkMagic   = "YACD_NETWORK_MAGIC"
	envOgmiosURL      = "YACD_OGMIOS_URL"
	envKupoURL        = "YACD_KUPO_URL"
	envFaucetURL      = "YACD_FAUCET_URL"
	envFaucetToken    = "YACD_FAUCET_TOKEN"
	envNodeSocketPath = "CARDANO_NODE_SOCKET_PATH"
)

// chainEndpoint pairs a chain-API endpoint's contract env key and short name
// with the endpoint the controller published in status, in the fixed order the
// contract emits.
type chainEndpoint struct {
	key      string
	name     string
	endpoint *yacdv1alpha1.ServiceEndpointStatus
}

// chainEndpoints returns the published Ogmios/Kupo/faucet endpoints paired with
// their env keys and short names. node-to-node is excluded: it is a TCP peer
// protocol, not something host or in-pod test tooling speaks.
func chainEndpoints(network *yacdv1alpha1.CardanoNetwork) []chainEndpoint {
	if network.Status.Endpoints == nil {
		return nil
	}
	endpoints := network.Status.Endpoints

	return []chainEndpoint{
		{key: envOgmiosURL, name: "ogmios", endpoint: endpoints.Ogmios},
		{key: envKupoURL, name: "kupo", endpoint: endpoints.Kupo},
		{key: envFaucetURL, name: "faucet", endpoint: endpoints.Faucet},
	}
}

// hostBinding is one forwarded chain endpoint exposed to the host: its contract
// env key, short name, and the loopback URL on the assigned local port.
type hostBinding struct {
	key  string
	name string
	url  string
}

// hostBindings builds the loopback bindings for the published chain endpoints
// that were forwarded, preserving each published scheme so a WebSocket endpoint
// keeps ws://. It is the single place the loopback rewrite happens, shared by
// the host env (run/connect) and the connect endpoints document. localPort
// reports the local port assigned to a remote container port, and whether that
// port was forwarded; unforwarded endpoints are skipped.
func hostBindings(network *yacdv1alpha1.CardanoNetwork, localPort func(remote int32) (int, bool)) ([]hostBinding, error) {
	var bindings []hostBinding
	for _, chain := range chainEndpoints(network) {
		if chain.endpoint == nil || strings.TrimSpace(chain.endpoint.URL) == "" {
			continue
		}
		local, ok := localPort(chain.endpoint.Port)
		if !ok {
			continue
		}
		loopback, err := loopbackURL(chain.endpoint.URL, local)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, hostBinding{key: chain.key, name: chain.name, url: loopback})
	}

	return bindings, nil
}

// hostEnv assembles the YACD_* environment for a host process (run/connect):
// the identity variables, a loopback URL per forwarded chain endpoint, and the
// faucet token when non-empty.
func hostEnv(network *yacdv1alpha1.CardanoNetwork, localPort func(remote int32) (int, bool), faucetToken string) ([]string, error) {
	bindings, err := hostBindings(network, localPort)
	if err != nil {
		return nil, err
	}

	env := identityEnv(network)
	for _, binding := range bindings {
		env = append(env, binding.key+"="+binding.url)
	}
	if strings.TrimSpace(faucetToken) != "" {
		env = append(env, envFaucetToken+"="+faucetToken)
	}

	return env, nil
}

// endpointsDocument is the token-free connection info connect writes to
// .yacd/<network>/endpoints.json and prints. Field names are stable across
// releases. It deliberately never carries the faucet token: the file is a
// checked-out, tool-readable artifact, and the loopback faucet URL is already
// trust-gate-exempt for yacd topup, which reads the token from the cluster.
type endpointsDocument struct {
	Network      string `json:"network"`
	Namespace    string `json:"namespace"`
	NetworkMagic *int64 `json:"networkMagic,omitempty"`
	OgmiosURL    string `json:"ogmiosUrl,omitempty"`
	KupoURL      string `json:"kupoUrl,omitempty"`
	FaucetURL    string `json:"faucetUrl,omitempty"`
}

// newEndpointsDocument builds the token-free connect document from the forwarded
// loopback bindings, reusing the same hostBindings rewrite as the host env.
func newEndpointsDocument(network *yacdv1alpha1.CardanoNetwork, localPort func(remote int32) (int, bool)) (endpointsDocument, error) {
	bindings, err := hostBindings(network, localPort)
	if err != nil {
		return endpointsDocument{}, err
	}

	doc := endpointsDocument{Network: network.Name, Namespace: network.Namespace}
	if network.Status.Network != nil {
		doc.NetworkMagic = network.Status.Network.NetworkMagic
	}
	for _, binding := range bindings {
		switch binding.name {
		case "ogmios":
			doc.OgmiosURL = binding.url
		case "kupo":
			doc.KupoURL = binding.url
		case "faucet":
			doc.FaucetURL = binding.url
		}
	}

	return doc, nil
}

// podEnv assembles the YACD_* environment for an in-pod process (exec): the
// published ClusterIP URLs verbatim, the network magic, and the node socket
// path. It intentionally omits YACD_FAUCET_TOKEN — a Bearer token injected into
// the exec argv would land in apiserver audit logs and /proc, and in-pod
// tooling does not need it.
func podEnv(network *yacdv1alpha1.CardanoNetwork, socketPath string) []string {
	env := identityEnv(network)
	for _, chain := range chainEndpoints(network) {
		if chain.endpoint == nil || strings.TrimSpace(chain.endpoint.URL) == "" {
			continue
		}
		env = append(env, chain.key+"="+strings.TrimSpace(chain.endpoint.URL))
	}
	if strings.TrimSpace(socketPath) != "" {
		env = append(env, envNodeSocketPath+"="+socketPath)
	}

	return env
}

// identityEnv returns the always-present identity variables shared by the host
// and in-pod environments: network name, namespace, and the network magic when
// the controller has published it.
func identityEnv(network *yacdv1alpha1.CardanoNetwork) []string {
	env := []string{
		envNetwork + "=" + network.Name,
		envNamespace + "=" + network.Namespace,
	}
	if network.Status.Network != nil && network.Status.Network.NetworkMagic != nil {
		env = append(env, envNetworkMagic+"="+strconv.FormatInt(*network.Status.Network.NetworkMagic, 10))
	}

	return env
}

// loopbackURL rewrites a published cluster URL onto 127.0.0.1:localPort. Only
// the host:port is replaced, so the scheme (ws:// endpoints stay ws://) and any
// path, query, or fragment carry through unchanged. The scheme is taken from
// the controller's published URL rather than hard-coded per service, so the
// contract stays faithful to what the operator exposed.
func loopbackURL(published string, localPort int) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(published))
	if err != nil {
		return "", fmt.Errorf("parse published endpoint URL %q: %w", published, err)
	}
	if parsed.Scheme == "" {
		return "", fmt.Errorf("published endpoint URL %q has no scheme", published)
	}
	parsed.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort))

	return parsed.String(), nil
}
