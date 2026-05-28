package networkartifacts

import (
	"encoding/json"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	cardanonetworkartifacts "github.com/meigma/yacd/internal/cardano/networkartifacts"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConsumerConnectionParsesLocalConnection(t *testing.T) {
	network := connectionTestNetwork(yacdv1alpha1.CardanoNetworkModeLocal, "")
	configMap := connectionTestConfigMap(t, network, true)

	result := ConsumerConnection(configMap, network)

	require.True(t, result.Ready, result.Message)
	assert.Equal(t, yacdv1alpha1.CardanoNetworkModeLocal, result.Connection.Mode)
	assert.Equal(t, "devnet", result.Connection.NetworkName)
	assert.Equal(t, int64(42), result.Connection.NetworkMagic)
	assert.True(t, result.Connection.RequiresNetworkMagic)
	assert.Equal(t, "fingerprint", result.Connection.NetworkFingerprint)
	assert.Equal(t, "devnet-node.default.svc.cluster.local", result.Connection.NodeToNodeHost)
	assert.Equal(t, int32(3001), result.Connection.NodeToNodePort)
	assert.Equal(t, "tcp://devnet-node.default.svc.cluster.local:3001", result.Connection.NodeToNodeURL)
}

func TestConsumerConnectionParsesPublicConnection(t *testing.T) {
	network := connectionTestNetwork(yacdv1alpha1.CardanoNetworkModePublic, yacdv1alpha1.PublicNetworkProfilePreprod)
	configMap := connectionTestConfigMap(t, network, true)

	result := ConsumerConnection(configMap, network)

	require.True(t, result.Ready, result.Message)
	assert.Equal(t, yacdv1alpha1.CardanoNetworkModePublic, result.Connection.Mode)
	assert.Equal(t, yacdv1alpha1.PublicNetworkProfilePreprod, result.Connection.Profile)
	assert.Equal(t, "preprod", result.Connection.NetworkName)
	assert.Equal(t, int64(1), result.Connection.NetworkMagic)
	assert.True(t, result.Connection.RequiresNetworkMagic)
}

func TestConsumerConnectionParsesPublicMainnetIdentity(t *testing.T) {
	network := connectionTestNetwork(yacdv1alpha1.CardanoNetworkModePublic, yacdv1alpha1.PublicNetworkProfileMainnet)
	configMap := connectionTestConfigMap(t, network, false)

	result := ConsumerConnection(configMap, network)

	require.True(t, result.Ready, result.Message)
	assert.Equal(t, yacdv1alpha1.PublicNetworkProfileMainnet, result.Connection.Profile)
	assert.Equal(t, "mainnet", result.Connection.NetworkName)
	assert.Equal(t, int64(764824073), result.Connection.NetworkMagic)
	assert.False(t, result.Connection.RequiresNetworkMagic)
}

func TestConsumerConnectionRejectsInvalidConnection(t *testing.T) {
	network := connectionTestNetwork(yacdv1alpha1.CardanoNetworkModePublic, yacdv1alpha1.PublicNetworkProfilePreview)
	testCases := []struct {
		name    string
		mutate  func(*corev1.ConfigMap)
		message string
	}{
		{
			name: "missing connection",
			mutate: func(configMap *corev1.ConfigMap) {
				delete(configMap.Data, cardanonetworkartifacts.ConnectionKey)
			},
			message: "Referenced CardanoNetwork connection artifact is missing",
		},
		{
			name: "malformed connection",
			mutate: func(configMap *corev1.ConfigMap) {
				configMap.Data[cardanonetworkartifacts.ConnectionKey] = "{"
			},
			message: "Referenced CardanoNetwork connection artifact is invalid JSON",
		},
		{
			name: "magic mismatch",
			mutate: func(configMap *corev1.ConfigMap) {
				doc := connectionTestDocument(t, configMap)
				otherMagic := int64(9)
				doc.Network.NetworkMagic = &otherMagic
				configMap.Data[cardanonetworkartifacts.ConnectionKey] = marshalConnectionTestDocument(t, doc)
			},
			message: "network.networkMagic 9 does not match published network magic",
		},
		{
			name: "fingerprint mismatch",
			mutate: func(configMap *corev1.ConfigMap) {
				doc := connectionTestDocument(t, configMap)
				doc.Network.NetworkFingerprint = "other"
				configMap.Data[cardanonetworkartifacts.ConnectionKey] = marshalConnectionTestDocument(t, doc)
			},
			message: "network fingerprint does not match published network identity",
		},
		{
			name: "profile mismatch",
			mutate: func(configMap *corev1.ConfigMap) {
				doc := connectionTestDocument(t, configMap)
				doc.Network.Profile = string(yacdv1alpha1.PublicNetworkProfilePreprod)
				configMap.Data[cardanonetworkartifacts.ConnectionKey] = marshalConnectionTestDocument(t, doc)
			},
			message: `network.profile "preprod" does not match published profile`,
		},
		{
			name: "file reference missing",
			mutate: func(configMap *corev1.ConfigMap) {
				doc := connectionTestDocument(t, configMap)
				doc.Files["configuration"] = "missing.yaml"
				configMap.Data[cardanonetworkartifacts.ConnectionKey] = marshalConnectionTestDocument(t, doc)
			},
			message: "files.configuration must reference configuration.yaml",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			configMap := connectionTestConfigMap(t, network, true)
			testCase.mutate(configMap)

			result := ConsumerConnection(configMap, network)

			assert.False(t, result.Ready)
			assert.Contains(t, result.Message, testCase.message)
		})
	}
}

func connectionTestNetwork(mode yacdv1alpha1.CardanoNetworkMode, profile yacdv1alpha1.PublicNetworkProfile) *yacdv1alpha1.CardanoNetwork {
	magic := int64(42)
	var statusProfile *yacdv1alpha1.PublicNetworkProfile
	if mode == yacdv1alpha1.CardanoNetworkModePublic {
		statusProfile = &profile
		switch profile {
		case yacdv1alpha1.PublicNetworkProfilePreprod:
			magic = 1
		case yacdv1alpha1.PublicNetworkProfilePreview:
			magic = 2
		case yacdv1alpha1.PublicNetworkProfileMainnet:
			magic = 764824073
		}
	}
	return &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "devnet", Namespace: "default"},
		Status: yacdv1alpha1.CardanoNetworkStatus{
			Network: &yacdv1alpha1.CardanoNetworkIdentityStatus{
				Mode:                mode,
				LocalnetFingerprint: "fingerprint",
				NetworkFingerprint:  "fingerprint",
				NetworkMagic:        &magic,
				Profile:             statusProfile,
			},
			Endpoints: &yacdv1alpha1.CardanoNetworkEndpointsStatus{
				NodeToNode: &yacdv1alpha1.ServiceEndpointStatus{
					ServiceName: "devnet-node",
					Port:        3001,
					URL:         "tcp://devnet-node.default.svc.cluster.local:3001",
				},
			},
		},
	}
}

func connectionTestConfigMap(t *testing.T, network *yacdv1alpha1.CardanoNetwork, requiresMagic bool) *corev1.ConfigMap {
	t.Helper()
	data := connectionTestData(t, network, requiresMagic)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devnet-network-artifacts",
			Namespace: "default",
			Annotations: map[string]string{
				ctrlannotations.NetworkFingerprint: "fingerprint",
			},
		},
		Data: data,
	}
}

func connectionTestData(t *testing.T, network *yacdv1alpha1.CardanoNetwork, requiresMagic bool) map[string]string {
	t.Helper()
	data := map[string]string{
		cardanonetworkartifacts.ConfigurationKey:   "configuration",
		cardanonetworkartifacts.ByronGenesisKey:    "byron",
		cardanonetworkartifacts.ShelleyGenesisKey:  "shelley",
		cardanonetworkartifacts.AlonzoGenesisKey:   "alonzo",
		cardanonetworkartifacts.ConwayGenesisKey:   "conway",
		cardanonetworkartifacts.PrimaryTopologyKey: "topology",
		cardanonetworkartifacts.ConnectionKey:      "",
	}
	if network.Status.Network.Mode == yacdv1alpha1.CardanoNetworkModeLocal {
		data[cardanonetworkartifacts.PlanManifestKey] = "localnet plan"
	} else {
		data[cardanonetworkartifacts.PublicProfileManifestKey] = "public profile"
	}
	doc := connectionDocument{
		SchemaVersion: cardanonetworkartifacts.SchemaVersion,
		Network: connectionNetwork{
			Name:               network.Name,
			Namespace:          network.Namespace,
			Mode:               string(network.Status.Network.Mode),
			NetworkMagic:       network.Status.Network.NetworkMagic,
			NetworkFingerprint: network.Status.Network.NetworkFingerprint,
		},
		PrimaryNodeToNode: connectionEndpoint{
			Host: "devnet-node.default.svc.cluster.local",
			Port: 3001,
			URL:  "tcp://devnet-node.default.svc.cluster.local:3001",
		},
		Files: map[string]string{
			"configuration":   cardanonetworkartifacts.ConfigurationKey,
			"byronGenesis":    cardanonetworkartifacts.ByronGenesisKey,
			"shelleyGenesis":  cardanonetworkartifacts.ShelleyGenesisKey,
			"alonzoGenesis":   cardanonetworkartifacts.AlonzoGenesisKey,
			"conwayGenesis":   cardanonetworkartifacts.ConwayGenesisKey,
			"primaryTopology": cardanonetworkartifacts.PrimaryTopologyKey,
			"connection":      cardanonetworkartifacts.ConnectionKey,
		},
	}
	if network.Status.Network.Mode == yacdv1alpha1.CardanoNetworkModeLocal {
		doc.Network.LocalnetFingerprint = network.Status.Network.LocalnetFingerprint
		doc.Network.NetworkFingerprint = ""
		doc.Files["localnetPlan"] = cardanonetworkartifacts.PlanManifestKey
	} else {
		doc.Network.Profile = string(*network.Status.Network.Profile)
		doc.Network.RequiresNetworkMagic = &requiresMagic
		doc.Files["publicProfile"] = cardanonetworkartifacts.PublicProfileManifestKey
	}
	data[cardanonetworkartifacts.ConnectionKey] = marshalConnectionTestDocument(t, doc)
	return data
}

func connectionTestDocument(t *testing.T, configMap *corev1.ConfigMap) connectionDocument {
	t.Helper()
	var doc connectionDocument
	require.NoError(t, json.Unmarshal([]byte(configMap.Data[cardanonetworkartifacts.ConnectionKey]), &doc))
	return doc
}

func marshalConnectionTestDocument(t *testing.T, doc connectionDocument) string {
	t.Helper()
	raw, err := json.MarshalIndent(doc, "", "  ")
	require.NoError(t, err)
	return string(raw) + "\n"
}
