package dbsync

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPlanDefaultsAndRendersRuntimeFiles(t *testing.T) {
	plan, err := BuildPlan(minimalSpec())

	require.NoError(t, err)
	assert.Equal(t, defaultConfigFile, plan.Spec.Paths.ConfigFile)
	assert.Equal(t, defaultTopologyFile, plan.Spec.Paths.TopologyFile)
	assert.Equal(t, defaultNodeConfig, plan.Spec.Paths.NodeConfig)
	assert.Equal(t, defaultSocketPath, plan.Spec.Paths.SocketPath)
	assert.Equal(t, defaultStateDir, plan.Spec.Paths.StateDir)
	assert.Equal(t, defaultSchemaDir, plan.Spec.Paths.SchemaDir)
	assert.Equal(t, defaultMetricsPort, plan.Spec.Runtime.MetricsPort)
	assert.Equal(t, defaultLedgerBackend, plan.Spec.Storage.LedgerBackend)
	assert.Equal(t, defaultNearTipEpoch, plan.Spec.Storage.NearTipEpoch)
	assert.Equal(t, defaultStateStorageSize, plan.Spec.Storage.StateStorageSize)

	assert.Contains(t, plan.ConfigYAML, "NetworkName: devnet")
	assert.Contains(t, plan.ConfigYAML, "NodeConfigFile: /network-artifacts/configuration.yaml")
	assert.Contains(t, plan.ConfigYAML, "RequiresNetworkMagic: RequiresMagic")
	assert.Contains(t, plan.ConfigYAML, "PrometheusPort: 8080")
	assert.Contains(t, plan.ConfigYAML, "ledger_backend: lsm")
	assert.Contains(t, plan.ConfigYAML, "near_tip_epoch: 580")
	assert.Contains(t, plan.ConfigYAML, "tx_out:")
	assert.Contains(t, plan.ConfigYAML, "value: enable")
	assert.Contains(t, plan.TopologyJSON, `"address": "devnet-node.default.svc.cluster.local"`)
	assert.Contains(t, plan.TopologyJSON, `"port": 3001`)
	assert.Equal(t, "cardano-db-sync", plan.Run.Command)
	assert.Equal(t, []string{
		"--config", "/config/db-sync-config.yaml",
		"--socket-path", "/ipc/node.socket",
		"--state-dir", "/state/db-sync-ledger",
		"--schema-dir", "/opt/cardano-db-sync/schema",
	}, plan.Run.Args)
}

func TestBuildPlanUsesRuntimeFlags(t *testing.T) {
	spec := minimalSpec()
	spec.Runtime.Cache = false
	spec.Runtime.EpochTable = false
	spec.Runtime.ForceIndexes = true

	plan, err := BuildPlan(spec)

	require.NoError(t, err)
	assert.Contains(t, plan.Run.Args, "--disable-cache")
	assert.Contains(t, plan.Run.Args, "--disable-epoch")
	assert.Contains(t, plan.Run.Args, "--force-indexes")
}

func TestBuildPlanFingerprintIsStableAndInputSensitive(t *testing.T) {
	base, err := BuildPlan(minimalSpec())
	require.NoError(t, err)
	again, err := BuildPlan(minimalSpec())
	require.NoError(t, err)
	changedSpec := minimalSpec()
	changedSpec.Database.Name = "other"
	changed, err := BuildPlan(changedSpec)
	require.NoError(t, err)

	assert.Equal(t, "sha256", base.Fingerprint.Algorithm)
	assert.Len(t, base.Fingerprint.Value, 64)
	assert.Equal(t, base.Fingerprint, again.Fingerprint)
	assert.NotEqual(t, base.Fingerprint, changed.Fingerprint)
}

func TestBuildPlanRejectsInvalidSpec(t *testing.T) {
	testCases := []struct {
		name    string
		mutate  func(*Spec)
		wantErr string
	}{
		{
			name: "missing network name",
			mutate: func(spec *Spec) {
				spec.NetworkName = ""
			},
			wantErr: "network name is required",
		},
		{
			name: "missing node endpoint",
			mutate: func(spec *Spec) {
				spec.NodeToNode.Host = ""
			},
			wantErr: "node-to-node host is required",
		},
		{
			name: "invalid database port",
			mutate: func(spec *Spec) {
				spec.Database.Port = 70000
			},
			wantErr: "database port must be between 1 and 65535",
		},
		{
			name: "missing password secret",
			mutate: func(spec *Spec) {
				spec.Database.PasswordSecretName = ""
			},
			wantErr: "database password Secret name is required",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			spec := minimalSpec()
			testCase.mutate(&spec)

			_, err := BuildPlan(spec)

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

func TestPlanEnvironmentExcludesPassword(t *testing.T) {
	plan, err := BuildPlan(minimalSpec())
	require.NoError(t, err)

	got := map[string]string{}
	for _, env := range plan.Environment() {
		got[env.Name] = env.Value
	}

	assert.Equal(t, "postgres.default.svc.cluster.local", got["PGHOST"])
	assert.Equal(t, "5432", got["PGPORT"])
	assert.Equal(t, "cexplorer", got["PGDATABASE"])
	assert.Equal(t, "postgres", got["PGUSER"])
	assert.Equal(t, "disable", got["PGSSLMODE"])
	assert.NotContains(t, strings.Join(mapKeys(got), ","), "PGPASSWORD")
}

func minimalSpec() Spec {
	return Spec{
		NetworkName:          "devnet",
		RequiresNetworkMagic: true,
		NodeToNode: NodeToNode{
			Host: "devnet-node.default.svc.cluster.local",
			Port: 3001,
		},
		Database: Database{
			Host:               "postgres.default.svc.cluster.local",
			PasswordSecretName: "dbsync-postgres",
			PasswordSecretKey:  "password",
		},
		Runtime: Runtime{
			Cache:      true,
			EpochTable: true,
		},
	}
}

func mapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
