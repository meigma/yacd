package localnet

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPlanUsesDefaultCreateEnvInvocation(t *testing.T) {
	plan, err := BuildPlan(Spec{})
	require.NoError(t, err)

	assert.Equal(t, DefaultSpec(), plan.Spec)
	assert.Equal(t, "cardano-testnet", plan.CreateEnv.Command)
	assert.Equal(t, []string{
		"create-env",
		"--num-pool-nodes", "1",
		"--testnet-magic", "42",
		"--epoch-length", "500",
		"--slot-length", "0.1",
		"--output", "/state/env",
	}, plan.CreateEnv.Args)
	assert.Equal(t, Layout{
		StateDir:     "/state",
		EnvDir:       "/state/env",
		ConfigFile:   "/state/env/configuration.yaml",
		ManifestFile: "/state/env/yacd-localnet-plan.json",
	}, plan.Layout)
}

func TestBuildPlanFormatsSlotLengthInSeconds(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{
			name:     "fractional second",
			duration: 100 * time.Millisecond,
			want:     "0.1",
		},
		{
			name:     "whole second",
			duration: time.Second,
			want:     "1",
		},
		{
			name:     "mixed whole and fractional seconds",
			duration: 1500 * time.Millisecond,
			want:     "1.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := DefaultSpec()
			spec.Timing.SlotLength = tt.duration

			plan, err := BuildPlan(spec)
			require.NoError(t, err)

			assert.Equal(t, tt.want, plan.Manifest.Inputs.SlotLength)
			assert.Equal(t, tt.want, plan.CreateEnv.Args[8])
		})
	}
}

func TestBuildPlanNormalizesPaths(t *testing.T) {
	spec := DefaultSpec()
	spec.Paths.StateDir = "/data//state/"
	spec.Paths.EnvDir = ""

	plan, err := BuildPlan(spec)
	require.NoError(t, err)

	assert.Equal(t, "/data/state", plan.Spec.Paths.StateDir)
	assert.Equal(t, "/data/state/env", plan.Spec.Paths.EnvDir)
	assert.Equal(t, "/data/state/env", plan.Manifest.Inputs.EnvDir)
	assert.Equal(t, "/data/state/env/configuration.yaml", plan.Layout.ConfigFile)
	assert.Equal(t, "/data/state/env/yacd-localnet-plan.json", plan.Layout.ManifestFile)
}

func TestBuildPlanRejectsInvalidSpec(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Spec)
		wantErr string
	}{
		{
			name: "negative network magic",
			mutate: func(spec *Spec) {
				spec.NetworkMagic = -1
			},
			wantErr: "network magic must be greater than or equal to 0",
		},
		{
			name: "negative pool count",
			mutate: func(spec *Spec) {
				spec.PoolCount = -1
			},
			wantErr: "pool count must be greater than or equal to 1",
		},
		{
			name: "negative epoch length",
			mutate: func(spec *Spec) {
				spec.Timing.EpochLength = -1
			},
			wantErr: "epoch length must be greater than or equal to 1",
		},
		{
			name: "negative slot length",
			mutate: func(spec *Spec) {
				spec.Timing.SlotLength = -time.Second
			},
			wantErr: "slot length must be greater than 0",
		},
		{
			name: "relative state dir",
			mutate: func(spec *Spec) {
				spec.Paths.StateDir = "state"
			},
			wantErr: "state dir must be an absolute container path",
		},
		{
			name: "relative env dir",
			mutate: func(spec *Spec) {
				spec.Paths.EnvDir = "env"
			},
			wantErr: "env dir must be an absolute container path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := DefaultSpec()
			tt.mutate(&spec)

			_, err := BuildPlan(spec)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestBuildPlanFingerprintIsStableAndInputSensitive(t *testing.T) {
	base := DefaultSpec()
	plan, err := BuildPlan(base)
	require.NoError(t, err)

	assert.Equal(t, "sha256", plan.Fingerprint.Algorithm)
	assert.Equal(t, "8523eefd2aa6a1e4050f85bf96503c30c1a9d9fbe1886dcf2296c9abad26aa80", plan.Fingerprint.Value)

	tests := []struct {
		name   string
		mutate func(*Spec)
	}{
		{
			name: "network magic",
			mutate: func(spec *Spec) {
				spec.NetworkMagic = 7
			},
		},
		{
			name: "pool count",
			mutate: func(spec *Spec) {
				spec.PoolCount = 2
			},
		},
		{
			name: "epoch length",
			mutate: func(spec *Spec) {
				spec.Timing.EpochLength = 1000
			},
		},
		{
			name: "slot length",
			mutate: func(spec *Spec) {
				spec.Timing.SlotLength = time.Second
			},
		},
		{
			name: "env dir",
			mutate: func(spec *Spec) {
				spec.Paths.EnvDir = "/data/env"
			},
		},
		{
			name: "tool version",
			mutate: func(spec *Spec) {
				spec.Tool.Version = "11.0.1"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := base
			tt.mutate(&spec)

			changed, err := BuildPlan(spec)
			require.NoError(t, err)
			assert.NotEqual(t, plan.Fingerprint, changed.Fingerprint)
		})
	}
}

func TestBuildPlanFingerprintExcludesToolBinary(t *testing.T) {
	base, err := BuildPlan(DefaultSpec())
	require.NoError(t, err)

	spec := DefaultSpec()
	spec.Tool.Binary = "/usr/local/bin/cardano-testnet"

	changed, err := BuildPlan(spec)
	require.NoError(t, err)

	assert.Equal(t, "/usr/local/bin/cardano-testnet", changed.CreateEnv.Command)
	assert.Equal(t, base.Fingerprint, changed.Fingerprint)
}

func TestBuildPlanManifestMarshalsDeterministically(t *testing.T) {
	spec := DefaultSpec()
	spec.Tool.Version = "11.0.1"

	plan, err := BuildPlan(spec)
	require.NoError(t, err)

	raw, err := json.MarshalIndent(plan.Manifest, "", "  ")
	require.NoError(t, err)

	assert.Equal(t, `{
  "schemaVersion": "yacd.meigma.io/localnet-plan/v1alpha1",
  "inputs": {
    "networkMagic": 42,
    "poolCount": 1,
    "epochLength": 500,
    "slotLength": "0.1",
    "envDir": "/state/env",
    "toolVersion": "11.0.1"
  },
  "fingerprint": {
    "algorithm": "sha256",
    "value": "063971a6a56498017a9fe831df5e25968b8fd06b9f4730e0ecec218503ae28ac"
  }
}`, string(raw))
}
