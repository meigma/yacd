package cli

import (
	"bytes"
	"context"
	"testing"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/devconfig"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitTemplateLoadsAndValidates guards the embedded init template against
// drift from the real schema: its active (uncommented) portion must parse and
// validate through the same devconfig.Load `yacd up` uses, and must be the
// batteries-included local network `init` promises (a local network
// automatically gets the genesis-funded wallet).
func TestInitTemplateLoadsAndValidates(t *testing.T) {
	t.Parallel()

	env, err := devconfig.Load(bytes.NewReader(defaultInitEnvYAML))
	require.NoError(t, err)

	assert.Equal(t, yacdv1alpha1.CardanoNetworkModeLocal, env.Spec.Network.Mode)
	require.NotNil(t, env.Spec.Network.Local)
}

// TestInitCommandPrintsTemplate proves `yacd init` writes the embedded template
// verbatim to stdout (the redirect target for `yacd init > yacd.yaml`).
func TestInitCommandPrintsTemplate(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	root := NewRootCommand(Options{
		Out:   &stdout,
		Viper: viper.New(),
	})
	root.SetArgs([]string{"init"})

	require.NoError(t, root.ExecuteContext(context.Background()))
	require.NotEmpty(t, stdout.Bytes())
	assert.Equal(t, defaultInitEnvYAML, stdout.Bytes())
}
