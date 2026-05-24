package dbsync

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestBuildPlanDefaultsAndRendersRuntimeFiles(t *testing.T) {
	plan, err := BuildPlan(minimalSpec())

	require.NoError(t, err)
	assert.Equal(t, defaultConfigFile, plan.Spec.Paths.ConfigFile)
	assert.Equal(t, defaultTopologyFile, plan.Spec.Paths.TopologyFile)
	assert.Equal(t, defaultNodeConfig, plan.Spec.Paths.NodeConfig)
	assert.Equal(t, defaultSocketPath, plan.Spec.Paths.SocketPath)
	assert.Equal(t, defaultStateDir, plan.Spec.Paths.StateDir)
	assert.Equal(t, defaultPGPassFile, plan.Spec.Paths.PGPassFile)
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
	assert.Contains(t, plan.ConfigYAML, "pool_stat: enable")
	assert.Contains(t, plan.ConfigYAML, "remove_jsonb_from_schema: disable")
	assert.Contains(t, plan.TopologyJSON, `"address": "devnet-node.default.svc.cluster.local"`)
	assert.Contains(t, plan.TopologyJSON, `"port": 3001`)
	assert.Empty(t, plan.Run.Command)
	assert.Equal(t, []string{
		"--config", "/config/db-sync-config.yaml",
		"--socket-path", "/ipc/node.socket",
		"--pg-pass-env", "PGPASSFILE",
	}, plan.Run.Args)
}

func TestBuildPlanRendersRemoveJSONBFromSchemaAsStringEnum(t *testing.T) {
	spec := minimalSpec()
	spec.Insert.RemoveJSONBFromSchema = insertOptionEnable

	plan, err := BuildPlan(spec)

	require.NoError(t, err)
	var config map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(plan.ConfigYAML), &config))
	insertOptions, ok := config["insert_options"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, insertOptionEnable, insertOptions["remove_jsonb_from_schema"])
}

func TestBuildPlanRendersIPFSGateways(t *testing.T) {
	spec := minimalSpec()
	spec.IPFSGateways = []string{" https://ipfs.example.test ", "", "https://backup-ipfs.example.test"}

	plan, err := BuildPlan(spec)

	require.NoError(t, err)
	assert.Equal(t, []string{"https://ipfs.example.test", "https://backup-ipfs.example.test"}, plan.Spec.IPFSGateways)
	assert.Contains(t, plan.ConfigYAML, "ipfs_gateway:")
	assert.Contains(t, plan.ConfigYAML, "- https://ipfs.example.test")
	assert.Contains(t, plan.ConfigYAML, "- https://backup-ipfs.example.test")
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
	assert.Equal(t, "sha256", base.DatabaseIdentityFingerprint.Algorithm)
	assert.Len(t, base.DatabaseIdentityFingerprint.Value, 64)
	assert.Equal(t, base.Fingerprint, again.Fingerprint)
	assert.Equal(t, base.DatabaseIdentityFingerprint, again.DatabaseIdentityFingerprint)
	assert.NotEqual(t, base.Fingerprint, changed.Fingerprint)
	assert.NotEqual(t, base.DatabaseIdentityFingerprint, changed.DatabaseIdentityFingerprint)
}

func TestBuildPlanDatabaseIdentitySeparatesSafeRuntimeChanges(t *testing.T) {
	base, err := BuildPlan(minimalSpec())
	require.NoError(t, err)

	runtimeChangedSpec := minimalSpec()
	runtimeChangedSpec.Runtime.ForceIndexes = true
	runtimeChanged, err := BuildPlan(runtimeChangedSpec)
	require.NoError(t, err)

	insertChangedSpec := minimalSpec()
	insertChangedSpec.Insert = defaultInsertOptions()
	insertChangedSpec.Insert.TxOut.Mode = "consumed"
	insertChanged, err := BuildPlan(insertChangedSpec)
	require.NoError(t, err)

	passwordRefChangedSpec := minimalSpec()
	passwordRefChangedSpec.Database.PasswordSecretName = "rotated-secret"
	passwordRefChanged, err := BuildPlan(passwordRefChangedSpec)
	require.NoError(t, err)

	imageChangedSpec := minimalSpec()
	imageChangedSpec.Image = "ghcr.io/intersectmbo/cardano-db-sync:13.8.0.0"
	imageChanged, err := BuildPlan(imageChangedSpec)
	require.NoError(t, err)

	assert.NotEqual(t, base.Fingerprint, runtimeChanged.Fingerprint)
	assert.Equal(t, base.DatabaseIdentityFingerprint, runtimeChanged.DatabaseIdentityFingerprint)
	assert.NotEqual(t, base.DatabaseIdentityFingerprint, insertChanged.DatabaseIdentityFingerprint)
	assert.Equal(t, base.DatabaseIdentityFingerprint, passwordRefChanged.DatabaseIdentityFingerprint)
	assert.NotEqual(t, base.DatabaseIdentityFingerprint, imageChanged.DatabaseIdentityFingerprint)
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
			name: "missing db-sync image",
			mutate: func(spec *Spec) {
				spec.Image = ""
			},
			wantErr: "db-sync image is required",
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
		{
			name: "bootstrap tx out with lsm",
			mutate: func(spec *Spec) {
				spec.Storage.LedgerBackend = "lsm"
				spec.Insert = defaultInsertOptions()
				spec.Insert.TxOut.Mode = "bootstrap"
			},
			wantErr: "tx_out bootstrap is not supported with lsm ledger_backend",
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
	assert.Equal(t, "/configuration/pgpass", got["PGPASSFILE"])
	assert.NotContains(t, strings.Join(mapKeys(got), ","), "PGPASSWORD")
}

func minimalSpec() Spec {
	return Spec{
		NetworkName:          "devnet",
		RequiresNetworkMagic: true,
		NetworkArtifactHash:  "sha256:network-artifacts",
		Image:                "ghcr.io/intersectmbo/cardano-db-sync:13.7.1.0",
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
