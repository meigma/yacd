package networkartifacts

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	cardanonetworkartifacts "github.com/meigma/yacd/internal/cardano/networkartifacts"
	corev1 "k8s.io/api/core/v1"
)

// Connection is the typed, validated view of a CardanoNetwork connection.json
// artifact consumed by dependent controllers.
type Connection struct {
	Mode                 yacdv1alpha1.CardanoNetworkMode
	Profile              yacdv1alpha1.PublicNetworkProfile
	NetworkName          string
	NetworkMagic         int64
	RequiresNetworkMagic bool
	NetworkFingerprint   string
	NodeToNodeHost       string
	NodeToNodePort       int32
	NodeToNodeURL        string
	Files                map[string]string
}

// ConnectionResult reports whether connection.json is valid for a referenced
// CardanoNetwork.
type ConnectionResult struct {
	Ready      bool
	Connection Connection
	Message    string
}

type connectionDocument struct {
	SchemaVersion     string             `json:"schemaVersion"`
	Network           connectionNetwork  `json:"network"`
	PrimaryNodeToNode connectionEndpoint `json:"primaryNodeToNode"`
	Files             map[string]string  `json:"files"`
}

type connectionNetwork struct {
	Name                 string `json:"name"`
	Namespace            string `json:"namespace"`
	Mode                 string `json:"mode"`
	Profile              string `json:"profile,omitempty"`
	NetworkMagic         *int64 `json:"networkMagic"`
	RequiresNetworkMagic *bool  `json:"requiresNetworkMagic,omitempty"`
	Era                  string `json:"era"`
	LocalnetFingerprint  string `json:"localnetFingerprint,omitempty"`
	NetworkFingerprint   string `json:"networkFingerprint,omitempty"`
}

type connectionEndpoint struct {
	Host string `json:"host"`
	Port int32  `json:"port"`
	URL  string `json:"url"`
}

// ConsumerConnection validates and parses connection.json from a network
// artifact ConfigMap after [ConsumerConfigMap] has accepted the bundle.
func ConsumerConnection(configMap *corev1.ConfigMap, network *yacdv1alpha1.CardanoNetwork) ConnectionResult {
	if configMap == nil {
		return ConnectionResult{Message: "Referenced CardanoNetwork artifact ConfigMap does not exist"}
	}
	if network == nil {
		return ConnectionResult{Message: "Referenced CardanoNetwork is required"}
	}

	raw := strings.TrimSpace(configMap.Data[cardanonetworkartifacts.ConnectionKey])
	if raw == "" {
		return ConnectionResult{Message: "Referenced CardanoNetwork connection artifact is missing"}
	}

	var doc connectionDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return ConnectionResult{Message: fmt.Sprintf("Referenced CardanoNetwork connection artifact is invalid JSON: %v", err)}
	}
	connection, err := validateConnectionDocument(doc, configMap, network)
	if err != nil {
		return ConnectionResult{Message: "Referenced CardanoNetwork connection artifact is invalid: " + err.Error()}
	}

	return ConnectionResult{
		Ready:      true,
		Connection: connection,
	}
}

func validateConnectionDocument(doc connectionDocument, configMap *corev1.ConfigMap, network *yacdv1alpha1.CardanoNetwork) (Connection, error) {
	if doc.SchemaVersion != cardanonetworkartifacts.SchemaVersion {
		return Connection{}, fmt.Errorf("schemaVersion %q is unsupported", doc.SchemaVersion)
	}
	if doc.Network.Name != network.Name {
		return Connection{}, fmt.Errorf("network.name %q does not match referenced CardanoNetwork %q", doc.Network.Name, network.Name)
	}
	if doc.Network.Namespace != network.Namespace {
		return Connection{}, fmt.Errorf("network.namespace %q does not match referenced namespace %q", doc.Network.Namespace, network.Namespace)
	}
	if network.Status.Network == nil {
		return Connection{}, fmt.Errorf("referenced CardanoNetwork has not published network identity")
	}
	mode := yacdv1alpha1.CardanoNetworkMode(doc.Network.Mode)
	if mode != network.Status.Network.Mode {
		return Connection{}, fmt.Errorf("network.mode %q does not match published mode %q", mode, network.Status.Network.Mode)
	}
	if mode != yacdv1alpha1.CardanoNetworkModeLocal && mode != yacdv1alpha1.CardanoNetworkModePublic {
		return Connection{}, fmt.Errorf("network.mode %q is unsupported", doc.Network.Mode)
	}
	if doc.Network.NetworkMagic == nil {
		return Connection{}, fmt.Errorf("network.networkMagic is required")
	}
	if network.Status.Network.NetworkMagic == nil || *doc.Network.NetworkMagic != *network.Status.Network.NetworkMagic {
		return Connection{}, fmt.Errorf("network.networkMagic %d does not match published network magic", *doc.Network.NetworkMagic)
	}

	fingerprint := connectionFingerprint(doc.Network)
	if fingerprint == "" {
		return Connection{}, fmt.Errorf("network fingerprint is required")
	}
	if network.Status.Network.NetworkFingerprint == "" || fingerprint != network.Status.Network.NetworkFingerprint {
		return Connection{}, fmt.Errorf("network fingerprint does not match published network identity")
	}
	if artifactNetworkFingerprint(configMap) != "" && artifactNetworkFingerprint(configMap) != network.Status.Network.NetworkFingerprint {
		return Connection{}, fmt.Errorf("artifact ConfigMap network fingerprint does not match published network identity")
	}

	expectedHost, expectedPort, expectedURL, err := expectedNodeToNodeEndpoint(network)
	if err != nil {
		return Connection{}, err
	}
	if doc.PrimaryNodeToNode.Host != expectedHost {
		return Connection{}, fmt.Errorf("primaryNodeToNode.host %q does not match published endpoint host %q", doc.PrimaryNodeToNode.Host, expectedHost)
	}
	if doc.PrimaryNodeToNode.Port != expectedPort {
		return Connection{}, fmt.Errorf("primaryNodeToNode.port %d does not match published endpoint port %d", doc.PrimaryNodeToNode.Port, expectedPort)
	}
	if doc.PrimaryNodeToNode.URL != expectedURL {
		return Connection{}, fmt.Errorf("primaryNodeToNode.url %q does not match published endpoint URL %q", doc.PrimaryNodeToNode.URL, expectedURL)
	}
	if err := validateConnectionFiles(doc.Files, configMap.Data, mode); err != nil {
		return Connection{}, err
	}

	connection := Connection{
		Mode:                 mode,
		NetworkMagic:         *doc.Network.NetworkMagic,
		NetworkFingerprint:   fingerprint,
		NodeToNodeHost:       doc.PrimaryNodeToNode.Host,
		NodeToNodePort:       doc.PrimaryNodeToNode.Port,
		NodeToNodeURL:        doc.PrimaryNodeToNode.URL,
		Files:                maps.Clone(doc.Files),
		RequiresNetworkMagic: true,
	}
	switch mode {
	case yacdv1alpha1.CardanoNetworkModeLocal:
		if doc.Network.Profile != "" {
			return Connection{}, fmt.Errorf("network.profile must be empty for local mode")
		}
		connection.NetworkName = network.Name
	case yacdv1alpha1.CardanoNetworkModePublic:
		profile := yacdv1alpha1.PublicNetworkProfile(doc.Network.Profile)
		if network.Status.Network.Profile == nil || profile != *network.Status.Network.Profile {
			return Connection{}, fmt.Errorf("network.profile %q does not match published profile", doc.Network.Profile)
		}
		if profile == "" {
			return Connection{}, fmt.Errorf("network.profile is required for public mode")
		}
		if doc.Network.RequiresNetworkMagic == nil {
			return Connection{}, fmt.Errorf("network.requiresNetworkMagic is required for public mode")
		}
		connection.Profile = profile
		connection.NetworkName = string(profile)
		connection.RequiresNetworkMagic = *doc.Network.RequiresNetworkMagic
	}

	return connection, nil
}

func connectionFingerprint(network connectionNetwork) string {
	if strings.TrimSpace(network.NetworkFingerprint) != "" {
		return strings.TrimSpace(network.NetworkFingerprint)
	}
	return strings.TrimSpace(network.LocalnetFingerprint)
}

func expectedNodeToNodeEndpoint(network *yacdv1alpha1.CardanoNetwork) (string, int32, string, error) {
	if network.Status.Endpoints == nil ||
		network.Status.Endpoints.NodeToNode == nil ||
		network.Status.Endpoints.NodeToNode.ServiceName == "" ||
		network.Status.Endpoints.NodeToNode.Port == 0 ||
		network.Status.Endpoints.NodeToNode.URL == "" {
		return "", 0, "", fmt.Errorf("referenced CardanoNetwork has not published a node-to-node endpoint")
	}
	endpoint := network.Status.Endpoints.NodeToNode
	host := fmt.Sprintf("%s.%s.svc.cluster.local", endpoint.ServiceName, network.Namespace)
	return host, endpoint.Port, endpoint.URL, nil
}

func validateConnectionFiles(files map[string]string, data map[string]string, mode yacdv1alpha1.CardanoNetworkMode) error {
	if len(files) == 0 {
		return fmt.Errorf("files map is required")
	}
	expected := map[string]string{
		"configuration":   cardanonetworkartifacts.ConfigurationKey,
		"byronGenesis":    cardanonetworkartifacts.ByronGenesisKey,
		"shelleyGenesis":  cardanonetworkartifacts.ShelleyGenesisKey,
		"alonzoGenesis":   cardanonetworkartifacts.AlonzoGenesisKey,
		"conwayGenesis":   cardanonetworkartifacts.ConwayGenesisKey,
		"primaryTopology": cardanonetworkartifacts.PrimaryTopologyKey,
		"connection":      cardanonetworkartifacts.ConnectionKey,
	}
	switch mode {
	case yacdv1alpha1.CardanoNetworkModeLocal:
		expected["localnetPlan"] = cardanonetworkartifacts.PlanManifestKey
	case yacdv1alpha1.CardanoNetworkModePublic:
		expected["publicProfile"] = cardanonetworkartifacts.PublicProfileManifestKey
	}
	for logicalKey, artifactKey := range expected {
		if files[logicalKey] != artifactKey {
			return fmt.Errorf("files.%s must reference %s", logicalKey, artifactKey)
		}
		if _, ok := data[artifactKey]; !ok {
			return fmt.Errorf("files.%s references missing artifact key %s", logicalKey, artifactKey)
		}
	}
	supportedDataKeys := supportedArtifactDataKeys()
	for logicalKey, artifactKey := range files {
		if artifactKey == "" {
			return fmt.Errorf("files.%s must not be empty", logicalKey)
		}
		if _, ok := supportedDataKeys[artifactKey]; !ok {
			return fmt.Errorf("files.%s references unsupported artifact key %s", logicalKey, artifactKey)
		}
		if _, ok := data[artifactKey]; !ok {
			return fmt.Errorf("files.%s references missing artifact key %s", logicalKey, artifactKey)
		}
	}

	return nil
}

func supportedArtifactDataKeys() map[string]struct{} {
	keys := make(map[string]struct{}, len(cardanonetworkartifacts.RequiredKeys())+len(cardanonetworkartifacts.OptionalKeys()))
	for _, key := range cardanonetworkartifacts.RequiredKeys() {
		keys[key] = struct{}{}
	}
	for _, key := range cardanonetworkartifacts.OptionalKeys() {
		keys[key] = struct{}{}
	}
	return keys
}
