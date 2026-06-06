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

// chainEndpoints returns the published Ogmios/Kupo endpoints paired with their
// env keys and short names. node-to-node is excluded: it is a TCP peer
// protocol, not something host or in-pod test tooling speaks.
func chainEndpoints(network *yacdv1alpha1.CardanoNetwork) []chainEndpoint {
	if network.Status.Endpoints == nil {
		return nil
	}
	endpoints := network.Status.Endpoints

	return []chainEndpoint{
		{key: envOgmiosURL, name: "ogmios", endpoint: endpoints.Ogmios},
		{key: envKupoURL, name: "kupo", endpoint: endpoints.Kupo},
	}
}

// hostEnvFromURLs assembles the YACD_* environment for a host process
// (run/connect) from the already-resolved Ogmios and Kupo URLs: the identity
// variables plus one chain-URL variable per non-empty URL, in the fixed contract
// order. The resolver decides each URL (a probed externalURL, an override, or a
// loopback port-forward), so this builder is transport-agnostic.
func hostEnvFromURLs(network *yacdv1alpha1.CardanoNetwork, ogmiosURL string, kupoURL string) []string {
	env := identityEnv(network)
	if strings.TrimSpace(ogmiosURL) != "" {
		env = append(env, envOgmiosURL+"="+ogmiosURL)
	}
	if strings.TrimSpace(kupoURL) != "" {
		env = append(env, envKupoURL+"="+kupoURL)
	}

	return env
}

// endpointsDocument is the connection info connect writes to
// .yacd/<network>/endpoints.json and prints. Field names are stable across
// releases.
type endpointsDocument struct {
	Network      string `json:"network"`
	Namespace    string `json:"namespace"`
	NetworkMagic *int64 `json:"networkMagic,omitempty"`
	OgmiosURL    string `json:"ogmiosUrl,omitempty"`
	KupoURL      string `json:"kupoUrl,omitempty"`
}

// documentFromURLs builds the token-free connect document from the already-
// resolved Ogmios and Kupo URLs, the same values the host env carries.
func documentFromURLs(network *yacdv1alpha1.CardanoNetwork, ogmiosURL string, kupoURL string) endpointsDocument {
	doc := endpointsDocument{
		Network:   network.Name,
		Namespace: network.Namespace,
		OgmiosURL: strings.TrimSpace(ogmiosURL),
		KupoURL:   strings.TrimSpace(kupoURL),
	}
	if network.Status.Network != nil {
		doc.NetworkMagic = network.Status.Network.NetworkMagic
	}

	return doc
}

// loopbackURLs rewrites each forwarded chain endpoint's published URL onto its
// assigned local port, preserving each published scheme so a WebSocket endpoint
// keeps ws://. localPort reports the local port assigned to a remote container
// port and whether it was forwarded; an unforwarded or unpublished endpoint
// yields an empty string for that service. It is the forward path's adapter onto
// the URL-based builders.
func loopbackURLs(network *yacdv1alpha1.CardanoNetwork, localPort func(remote int32) (int, bool)) (ogmiosURL string, kupoURL string, err error) {
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
			return "", "", err
		}
		switch chain.name {
		case "ogmios":
			ogmiosURL = loopback
		case "kupo":
			kupoURL = loopback
		}
	}

	return ogmiosURL, kupoURL, nil
}

// podEnv assembles the YACD_* environment for an in-pod process (exec): the
// published ClusterIP URLs verbatim, the network magic, and the node socket
// path.
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
