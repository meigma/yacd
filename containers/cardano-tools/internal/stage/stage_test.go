package stage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/yacd/containers/cardano-tools/internal/artifactset"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

// writeCreateEnv lays out a representative cardano-testnet create-env state
// directory: the flat genesis/config files at the root, the primary topology
// nested under node-data/node1, and the localnet plan manifest. It returns the
// state directory path.
func writeCreateEnv(t *testing.T) string {
	t.Helper()
	stateDir := t.TempDir()

	files := map[string]string{
		"configuration.yaml":            "ConwayGenesisFile: conway-genesis.json\n",
		"byron-genesis.json":            `{"byron":true}` + "\n",
		"shelley-genesis.json":          `{"shelley":true}` + "\n",
		"alonzo-genesis.json":           `{"alonzo":true}` + "\n",
		"conway-genesis.json":           `{"conway":true}` + "\n",
		"node-data/node1/topology.json": `{"Producers":[]}` + "\n",
		"yacd-localnet-plan.json":       `{"schemaVersion":"test","inputs":{"networkMagic":42},"fingerprint":{"algorithm":"sha256","value":"abc123"}}` + "\n",
	}
	for rel, content := range files {
		path := filepath.Join(stateDir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return stateDir
}

// validNetwork returns the connection identity stage records in connection.json.
func validNetwork() artifactset.NetworkIdentity {
	return artifactset.NetworkIdentity{
		Name:           "demo",
		Namespace:      "dev",
		Mode:           "local",
		Era:            "conway",
		NodeToNodeHost: "demo-node.dev.svc.cluster.local",
		NodeToNodePort: 3001,
		NodeToNodeURL:  "tcp://demo-node.dev.svc.cluster.local:3001",
	}
}

func TestRunStagesFlatServedDirectory(t *testing.T) {
	t.Parallel()

	stateDir := writeCreateEnv(t)
	outDir := filepath.Join(t.TempDir(), "served")

	err := Run(context.Background(), Options{
		StateDir:  stateDir,
		OutputDir: outDir,
		Network:   validNetwork(),
	}, io.Discard)
	require.NoError(t, err)

	// The nested topology is flattened to its contract key at the served root.
	assert.FileExists(t, filepath.Join(outDir, networkartifacts.PrimaryTopologyKey))
	for _, key := range []string{
		networkartifacts.ConfigurationKey,
		networkartifacts.ByronGenesisKey,
		networkartifacts.ShelleyGenesisKey,
		networkartifacts.AlonzoGenesisKey,
		networkartifacts.ConwayGenesisKey,
		networkartifacts.ConnectionKey,
		networkartifacts.ManifestKey,
		networkartifacts.PlanManifestKey,
	} {
		assert.FileExists(t, filepath.Join(outDir, key), "served file %s", key)
	}
	// No nested directory leaks into the served directory.
	assert.NoDirExists(t, filepath.Join(outDir, "node-data"))

	// connection.json is the localnet shape: it carries the cluster identity and
	// node-to-node endpoint.
	connRaw, err := os.ReadFile(filepath.Join(outDir, networkartifacts.ConnectionKey))
	require.NoError(t, err)
	var conn struct {
		SchemaVersion string `json:"schemaVersion"`
		Network       struct {
			Name string `json:"name"`
			Mode string `json:"mode"`
		} `json:"network"`
		PrimaryNodeToNode struct {
			Host string `json:"host"`
		} `json:"primaryNodeToNode"`
		Files map[string]string `json:"files"`
	}
	require.NoError(t, json.Unmarshal(connRaw, &conn))
	assert.Equal(t, networkartifacts.SchemaVersion, conn.SchemaVersion)
	assert.Equal(t, "demo", conn.Network.Name)
	assert.Equal(t, "local", conn.Network.Mode)
	assert.Equal(t, "demo-node.dev.svc.cluster.local", conn.PrimaryNodeToNode.Host)
	assert.Equal(t, networkartifacts.PrimaryTopologyKey, conn.Files["primaryTopology"])

	// manifest.json verifies every served file and excludes itself.
	manifestRaw, err := os.ReadFile(filepath.Join(outDir, networkartifacts.ManifestKey))
	require.NoError(t, err)
	var manifest networkartifacts.Manifest
	require.NoError(t, json.Unmarshal(manifestRaw, &manifest))
	assert.NotContains(t, manifest.Files, networkartifacts.ManifestKey)
	for _, name := range manifest.SortedFileNames() {
		content, readErr := os.ReadFile(filepath.Join(outDir, name))
		require.NoErrorf(t, readErr, "manifest names an unreadable file %s", name)
		assert.NoErrorf(t, manifest.Verify(name, content), "manifest digest must match %s", name)
	}
	assert.NoError(t, manifest.Verify(networkartifacts.ConnectionKey, connRaw))
}

func TestRunDryRunWritesNothing(t *testing.T) {
	t.Parallel()

	stateDir := writeCreateEnv(t)
	outDir := filepath.Join(t.TempDir(), "served")

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		StateDir:  stateDir,
		OutputDir: outDir,
		Network:   validNetwork(),
		DryRun:    true,
	}, &out)
	require.NoError(t, err)

	assert.Contains(t, out.String(), "would write "+networkartifacts.ConnectionKey)
	assert.Contains(t, out.String(), "would write "+networkartifacts.ManifestKey)
	assert.NoDirExists(t, outDir, "dry-run creates no output directory")
}

func TestRunRejectsMissingState(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), Options{
		StateDir:  filepath.Join(t.TempDir(), "absent"),
		OutputDir: t.TempDir(),
		Network:   validNetwork(),
	}, io.Discard)
	require.Error(t, err)
}
