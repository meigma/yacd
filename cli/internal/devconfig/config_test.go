package devconfig

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validConfig = `
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: local
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 2Gi
    local:
      networkMagic: 42
      era: conway
      timing:
        slotLength: 100ms
        epochLength: 500
      topology:
        pools:
          count: 1
`

const validPublicPreviewConfig = `
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: public
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 20Gi
    public:
      profile: preview
`

const validPublicCustomConfig = `
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: public
    node:
      version: "11.0.1"
      port: 3001
      storage:
        size: 20Gi
    public:
      profile: custom
      configSource:
        configMapRef:
          name: custom-profile
`

const validPublicMainnetConfig = `
apiVersion: yacd.meigma.io/devconfig/v1alpha1
kind: Environment
spec:
  network:
    mode: public
    node:
      version: "11.0.1"
      port: 3001
    public:
      profile: mainnet
      bootstrap:
        mithril: {}
`

func TestLoadReadsEnvironmentConfig(t *testing.T) {
	t.Parallel()

	environment, err := Load(strings.NewReader(validConfig))
	require.NoError(t, err)
	assert.Equal(t, APIVersion, environment.APIVersion)
	assert.Equal(t, Kind, environment.Kind)
	assert.Equal(t, int64(42), environment.Spec.Network.Local.NetworkMagic)
}

func TestLoadReadsPublicEnvironmentConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      string
		wantProfile string
	}{
		{name: "preview", config: validPublicPreviewConfig, wantProfile: "preview"},
		{
			name:        "preprod",
			config:      validPublicPreviewConfig,
			wantProfile: "preprod",
		},
		{
			name:        "mainnet",
			config:      validPublicMainnetConfig,
			wantProfile: "mainnet",
		},
		{name: "custom", config: validPublicCustomConfig, wantProfile: "custom"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			environment, err := Load(strings.NewReader(strings.Replace(tc.config, "profile: preview", "profile: "+tc.wantProfile, 1)))
			require.NoError(t, err)
			require.NotNil(t, environment.Spec.Network.Public)
			assert.Equal(t, tc.wantProfile, string(environment.Spec.Network.Public.Profile))
		})
	}
}

func TestLoadRejectsUnknownTopLevelFields(t *testing.T) {
	t.Parallel()

	_, err := Load(strings.NewReader(validConfig + "\nunknown: true\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}

func TestLoadRejectsOmittedConcreteCRDDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "node version",
			config:  strings.Replace(validConfig, "      version: \"11.0.1\"\n", "", 1),
			wantErr: "spec.network.node.version",
		},
		{
			name:    "node port",
			config:  strings.Replace(validConfig, "      port: 3001\n", "", 1),
			wantErr: "spec.network.node.port",
		},
		{
			name:    "local network magic",
			config:  strings.Replace(validConfig, "      networkMagic: 42\n", "", 1),
			wantErr: "spec.network.local.networkMagic",
		},
		{
			name: "kupo image",
			config: validConfig + `    chainAPI:
      kupo:
        enabled: true
        port: 1442
`,
			wantErr: "spec.network.chainAPI.kupo.image",
		},
		{
			name: "faucet port",
			config: validConfig + `    chainAPI:
      faucet:
        enabled: true
        defaultSource: utxo1
        minTopUpLovelace: 1000000
        maxTopUpLovelace: 10000000000
`,
			wantErr: "spec.network.chainAPI.faucet.port",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(tc.config))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestLoadRejectsUnsupportedPublicConfigs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "missing profile",
			config:  strings.Replace(validPublicPreviewConfig, "      profile: preview\n", "", 1),
			wantErr: "spec.network.public",
		},
		{
			name:    "custom without config source",
			config:  strings.Replace(validPublicPreviewConfig, "profile: preview", "profile: custom", 1),
			wantErr: "spec.network.public.configSource",
		},
		{
			name:    "mainnet without bootstrap",
			config:  strings.Replace(validPublicPreviewConfig, "profile: preview", "profile: mainnet", 1),
			wantErr: "spec.network.public.bootstrap.mithril",
		},
		{
			name: "preview with bootstrap",
			config: strings.Replace(validPublicPreviewConfig, "      profile: preview\n", `      profile: preview
      bootstrap:
        mithril: {}
`, 1),
			wantErr: "spec.network.public.bootstrap",
		},
		{
			name: "curated config source",
			config: strings.Replace(validPublicPreviewConfig, "      profile: preview\n", `      profile: preview
      configSource:
        configMapRef:
          name: custom-profile
`, 1),
			wantErr: "spec.network.public.configSource",
		},
		{
			name: "custom with both sources",
			config: strings.Replace(validPublicCustomConfig, `        configMapRef:
          name: custom-profile
`, `        configMapRef:
          name: custom-profile
        secretRef:
          name: custom-profile
`, 1),
			wantErr: "spec.network.public.configSource",
		},
		{
			name:    "unknown profile",
			config:  strings.Replace(validPublicPreviewConfig, "profile: preview", "profile: unknown", 1),
			wantErr: "spec.network.public.profile",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(tc.config))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestLoadAllowsFaucetWithoutImageOverride(t *testing.T) {
	t.Parallel()

	environment, err := Load(strings.NewReader(validConfig + `    chainAPI:
      faucet:
        enabled: true
        port: 8080
        defaultSource: utxo1
        minTopUpLovelace: 1000000
        maxTopUpLovelace: 10000000000
`))
	require.NoError(t, err)
	require.NotNil(t, environment.Spec.Network.ChainAPI)
	require.NotNil(t, environment.Spec.Network.ChainAPI.Faucet)
	assert.Nil(t, environment.Spec.Network.ChainAPI.Faucet.Image)
}

func TestValidateRequiresEnvelope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "api version",
			config:  strings.Replace(validConfig, APIVersion, "example.com/v1", 1),
			wantErr: "apiVersion",
		},
		{
			name:    "kind",
			config:  strings.Replace(validConfig, Kind, "Other", 1),
			wantErr: "kind",
		},
		{
			name:    "blank node version",
			config:  strings.Replace(validConfig, "version: \"11.0.1\"", "version: \"\"", 1),
			wantErr: "spec.network.node.version is required",
		},
		{
			name:    "non-positive node port",
			config:  strings.Replace(validConfig, "port: 3001", "port: 0", 1),
			wantErr: "spec.network.node.port must be greater than 0",
		},
		{
			name:    "unsupported mode",
			config:  strings.Replace(validConfig, "mode: local", "mode: hybrid", 1),
			wantErr: "spec.network.mode",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(strings.NewReader(tc.config))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
