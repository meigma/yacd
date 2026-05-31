package primarypod

import (
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSelectorLabels(t *testing.T) {
	network := localNetwork("devnet")

	assert.Equal(t, map[string]string{
		LabelAppName:        LabelPrimaryNodeName,
		LabelAppInstance:    "devnet",
		LabelAppComponent:   LabelPrimaryRole,
		LabelCardanoNetwork: "devnet",
		LabelCardanoRole:    LabelPrimaryRole,
	}, SelectorLabels(network))
}

func TestPortOwners(t *testing.T) {
	tests := []struct {
		name    string
		network *yacdv1alpha1.CardanoNetwork
		want    map[int32]string
	}{
		{
			name:    "defaults",
			network: localNetwork("devnet"),
			want: map[int32]string{
				DefaultNodePort:   PortNameNodeToNode,
				DefaultOgmiosPort: PortNameOgmios,
				DefaultKupoPort:   PortNameKupo,
			},
		},
		{
			name: "ogmios disabled disables implicit kupo",
			network: chainAPINetwork(&yacdv1alpha1.ChainAPISpec{
				Ogmios: &yacdv1alpha1.OgmiosSpec{Enabled: false},
			}),
			want: map[int32]string{
				DefaultNodePort: PortNameNodeToNode,
			},
		},
		{
			name: "explicit kupo survives disabled ogmios",
			network: chainAPINetwork(&yacdv1alpha1.ChainAPISpec{
				Ogmios: &yacdv1alpha1.OgmiosSpec{Enabled: false},
				Kupo:   &yacdv1alpha1.KupoSpec{Enabled: true, Port: 1443},
			}),
			want: map[int32]string{
				DefaultNodePort: PortNameNodeToNode,
				1443:            PortNameKupo,
			},
		},
		{
			name: "custom sidecar ports",
			network: chainAPINetwork(&yacdv1alpha1.ChainAPISpec{
				Ogmios: &yacdv1alpha1.OgmiosSpec{Enabled: true, Port: 1338},
				Kupo:   &yacdv1alpha1.KupoSpec{Enabled: true, Port: 1443},
				Faucet: &yacdv1alpha1.FaucetSpec{Enabled: true, Port: 8081},
			}),
			want: map[int32]string{
				DefaultNodePort: PortNameNodeToNode,
				1338:            PortNameOgmios,
				1443:            PortNameKupo,
				8081:            PortNameFaucet,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, PortOwners(tt.network))
		})
	}
}

// TestServePortIsNotOwnedYet documents that the cardano-tools serve port is
// defined for primary Pod rendering but intentionally excluded from PortOwners
// so it does not yet participate in the CardanoDBSync placement collision
// check (deferred to the A3 follow-up).
func TestServePortIsNotOwnedYet(t *testing.T) {
	assert.Equal(t, int32(8090), DefaultServePort)
	assert.Equal(t, "serve", PortNameServe)

	owners := PortOwners(localNetwork("serveport"))
	_, ok := owners[DefaultServePort]
	assert.False(t, ok, "serve port must not be registered in PortOwners yet")
}

func localNetwork(name string) *yacdv1alpha1.CardanoNetwork {
	return &yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: yacdv1alpha1.CardanoNetworkSpec{
			Node: yacdv1alpha1.CardanoNodeSpec{
				Port: DefaultNodePort,
			},
		},
	}
}

func chainAPINetwork(chainAPI *yacdv1alpha1.ChainAPISpec) *yacdv1alpha1.CardanoNetwork {
	network := localNetwork("devnet")
	network.Spec.ChainAPI = chainAPI

	return network
}
