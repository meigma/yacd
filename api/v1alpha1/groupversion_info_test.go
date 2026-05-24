package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToSchemeRegistersAPIObjects(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))

	for _, object := range []runtime.Object{
		&CardanoNetwork{},
		&CardanoNetworkList{},
		&CardanoDBSync{},
		&CardanoDBSyncList{},
	} {
		gvks, _, err := scheme.ObjectKinds(object)
		require.NoError(t, err)
		require.NotEmpty(t, gvks)
		require.Equal(t, SchemeGroupVersion, gvks[0].GroupVersion())
	}
}
