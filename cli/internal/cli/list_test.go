package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// listTestNetwork builds a CardanoNetwork list item with the given identity,
// mode, and readiness. A ready network carries a fresh Ready condition and a
// published Ogmios endpoint; a not-ready network carries neither.
func listTestNetwork(namespace string, name string, mode yacdv1alpha1.CardanoNetworkMode, ready bool) yacdv1alpha1.CardanoNetwork {
	network := yacdv1alpha1.CardanoNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
		},
		Spec: yacdv1alpha1.CardanoNetworkSpec{
			Mode: mode,
		},
	}
	if ready {
		network.Status = yacdv1alpha1.CardanoNetworkStatus{
			ObservedGeneration: 1,
			Endpoints: &yacdv1alpha1.CardanoNetworkEndpointsStatus{
				Ogmios: &yacdv1alpha1.ServiceEndpointStatus{
					ServiceName: name + "-ogmios",
					Port:        1337,
					URL:         "ws://" + name + "-ogmios." + namespace + ".svc.cluster.local:1337",
				},
			},
			Conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionTrue,
					Reason:             "Ready",
					Message:            "ready",
					ObservedGeneration: 1,
					LastTransitionTime: metav1.Now(),
				},
			},
		}
	}

	return network
}

func TestListRendersTable(t *testing.T) {
	t.Setenv("YACD_NAMESPACE", "team-a")

	networks := []yacdv1alpha1.CardanoNetwork{
		listTestNetwork("team-a", "devnet", yacdv1alpha1.CardanoNetworkModeLocal, true),
		listTestNetwork("team-a", "preview", yacdv1alpha1.CardanoNetworkModePublic, false),
	}

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("default-ns").Maybe()
	client.EXPECT().ListCardanoNetworks(mock.Anything, "team-a").Return(networks, nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"list"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	output := stdout.String()
	assert.Contains(t, output, "NAME")
	assert.Contains(t, output, "NAMESPACE")
	assert.Contains(t, output, "MODE")
	assert.Contains(t, output, "READY")
	assert.Contains(t, output, "ENDPOINTS")
	assert.Contains(t, output, "devnet")
	assert.Contains(t, output, "local")
	assert.Contains(t, output, "true")
	assert.Contains(t, output, "ogmios")
	assert.Contains(t, output, "preview")
	assert.Contains(t, output, "public")
	assert.Contains(t, output, "false")
}

func TestListUsesDefaultNamespaceWhenUnset(t *testing.T) {
	t.Setenv("YACD_NAMESPACE", "")

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("default-ns").Once()
	client.EXPECT().ListCardanoNetworks(mock.Anything, "default-ns").Return(nil, nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"list"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Contains(t, stdout.String(), `No CardanoNetworks found in namespace "default-ns".`)
}

func TestListAllNamespacesPassesEmptyNamespace(t *testing.T) {
	t.Parallel()

	networks := []yacdv1alpha1.CardanoNetwork{
		listTestNetwork("team-a", "devnet", yacdv1alpha1.CardanoNetworkModeLocal, true),
		listTestNetwork("team-b", "preview", yacdv1alpha1.CardanoNetworkModePublic, false),
	}

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("default-ns").Maybe()
	client.EXPECT().ListCardanoNetworks(mock.Anything, "").Return(networks, nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"list", "-A"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	output := stdout.String()
	assert.Contains(t, output, "team-a")
	assert.Contains(t, output, "team-b")
}

func TestListEmptyResultReportsNone(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("default-ns").Maybe()
	client.EXPECT().ListCardanoNetworks(mock.Anything, mock.Anything).Return([]yacdv1alpha1.CardanoNetwork{}, nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"list", "-n", "team-a"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	assert.Equal(t, "No CardanoNetworks found in namespace \"team-a\".\n", stdout.String())
}

func TestListJSONOutputShape(t *testing.T) {
	t.Parallel()

	networks := []yacdv1alpha1.CardanoNetwork{
		listTestNetwork("team-a", "devnet", yacdv1alpha1.CardanoNetworkModeLocal, true),
		listTestNetwork("team-a", "preview", yacdv1alpha1.CardanoNetworkModePublic, false),
	}

	client := newKubeMock(t)
	client.EXPECT().DefaultNamespace().Return("default-ns").Maybe()
	client.EXPECT().ListCardanoNetworks(mock.Anything, "team-a").Return(networks, nil)

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:               &stdout,
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"list", "-n", "team-a", "--json"})

	require.NoError(t, root.ExecuteContext(context.Background()))

	var items []listItem
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &items))
	require.Len(t, items, 2)

	assert.Equal(t, "devnet", items[0].Name)
	assert.Equal(t, "team-a", items[0].Namespace)
	assert.Equal(t, "local", items[0].Mode)
	assert.True(t, items[0].Ready)
	assert.Equal(t, "ws://devnet-ogmios.team-a.svc.cluster.local:1337", items[0].Endpoints.Ogmios)

	assert.Equal(t, "preview", items[1].Name)
	assert.Equal(t, "public", items[1].Mode)
	assert.False(t, items[1].Ready)
	assert.Empty(t, items[1].Endpoints.Ogmios)
}

func TestListRejectsArgs(t *testing.T) {
	t.Parallel()

	client := newKubeMock(t)

	root := NewRootCommand(Options{
		Viper:             viper.New(),
		KubeClientFactory: kubeClientFactory(client),
	})
	root.SetArgs([]string{"list", "unexpected"})

	err := root.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}
