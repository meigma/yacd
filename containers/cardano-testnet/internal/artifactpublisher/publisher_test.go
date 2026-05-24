package artifactpublisher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPatchesConfigMapWithArtifacts(t *testing.T) {
	envDir := writeLocalnetArtifacts(t)
	tokenPath := writeFile(t, t.TempDir(), "token", "test-token\n")
	namespacePath := writeFile(t, t.TempDir(), "namespace", "dev\n")

	var gotPatch configMapPatch
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/api/v1/namespaces/dev/configmaps/demo-network-artifacts" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("authorization = %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != contentTypeMergePatch {
			t.Errorf("content type = %s", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPatch); err != nil {
			t.Errorf("decode patch: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	var stdout strings.Builder
	err := Run(context.Background(), nil, publisherEnv(envDir, server.URL, tokenPath, namespacePath), &stdout)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if gotPatch.Metadata.Annotations[AnnotationSchemaVersion] != SchemaVersion {
		t.Fatalf("schema annotation = %q", gotPatch.Metadata.Annotations[AnnotationSchemaVersion])
	}
	if gotPatch.Metadata.Annotations[AnnotationLocalnetFingerprint] != "abc123" {
		t.Fatalf("fingerprint annotation = %q", gotPatch.Metadata.Annotations[AnnotationLocalnetFingerprint])
	}
	if !strings.HasPrefix(gotPatch.Metadata.Annotations[AnnotationDataHash], "sha256:") {
		t.Fatalf("data hash annotation = %q", gotPatch.Metadata.Annotations[AnnotationDataHash])
	}

	requiredKeys := []string{
		"configuration.yaml",
		"byron-genesis.json",
		"shelley-genesis.json",
		"alonzo-genesis.json",
		"conway-genesis.json",
		"dijkstra-genesis.json",
		"primary-topology.json",
		"yacd-localnet-plan.json",
		"connection.json",
	}
	for _, key := range requiredKeys {
		if gotPatch.Data[key] == nil || *gotPatch.Data[key] == "" {
			t.Fatalf("patch data missing %s", key)
		}
	}

	var connection connectionDocument
	if err := json.Unmarshal([]byte(*gotPatch.Data["connection.json"]), &connection); err != nil {
		t.Fatalf("parse connection.json: %v", err)
	}
	if connection.SchemaVersion != SchemaVersion {
		t.Errorf("connection schema = %q", connection.SchemaVersion)
	}
	if connection.Network.Name != "demo" {
		t.Errorf("connection network name = %q", connection.Network.Name)
	}
	if connection.Network.Namespace != "dev" {
		t.Errorf("connection namespace = %q", connection.Network.Namespace)
	}
	if connection.Network.NetworkMagic != 42 {
		t.Errorf("connection network magic = %d", connection.Network.NetworkMagic)
	}
	if connection.Network.LocalnetFingerprint != "abc123" {
		t.Errorf("connection fingerprint = %q", connection.Network.LocalnetFingerprint)
	}
	if connection.PrimaryNodeToNode.URL != "tcp://demo-node.dev.svc.cluster.local:3001" {
		t.Errorf("connection node-to-node URL = %q", connection.PrimaryNodeToNode.URL)
	}
	if connection.Files["primaryTopology"] != "primary-topology.json" {
		t.Errorf("primary topology file key = %q", connection.Files["primaryTopology"])
	}
	if !strings.Contains(stdout.String(), gotPatch.Metadata.Annotations[AnnotationDataHash]) {
		t.Errorf("stdout did not include data hash: %s", stdout.String())
	}
}

func TestBuildPatchDataHashIsIdempotent(t *testing.T) {
	envDir := writeLocalnetArtifacts(t)
	opts := testOptions(envDir)

	_, firstAnnotations, err := buildPatchData(opts)
	if err != nil {
		t.Fatalf("build first patch data: %v", err)
	}
	_, secondAnnotations, err := buildPatchData(opts)
	if err != nil {
		t.Fatalf("build second patch data: %v", err)
	}

	if firstAnnotations[AnnotationDataHash] != secondAnnotations[AnnotationDataHash] {
		t.Fatalf("data hash changed: %s != %s", firstAnnotations[AnnotationDataHash], secondAnnotations[AnnotationDataHash])
	}
}

func TestRunRequiresGeneratedArtifactFiles(t *testing.T) {
	envDir := writeLocalnetArtifacts(t)
	if err := os.Remove(filepath.Join(envDir, "alonzo-genesis.json")); err != nil {
		t.Fatal(err)
	}

	_, _, err := buildPatchData(testOptions(envDir))
	if err == nil {
		t.Fatal("buildPatchData() error = nil, want missing artifact error")
	}
	if !strings.Contains(err.Error(), "alonzo-genesis.json") {
		t.Fatalf("error = %v, want alonzo-genesis.json", err)
	}
}

func TestValidatePublicArtifactSourceRejectsSecretMaterial(t *testing.T) {
	for _, sourcePath := range []string{
		"pools-keys/pool1/kes.skey",
		"delegate-keys/delegate1/key",
		"stake-delegators/delegator1/payment.vkey",
		"utxo-keys/utxo1/utxo.addr",
		"genesis-keys/genesis1.vkey",
		"../outside.json",
	} {
		t.Run(sourcePath, func(t *testing.T) {
			err := validatePublicArtifactSource(artifactSource{
				key:  "bad",
				path: sourcePath,
			})
			if err == nil {
				t.Fatal("validatePublicArtifactSource() error = nil, want rejection")
			}
		})
	}
}

func TestRunOmitsAbsentOptionalDijkstraGenesis(t *testing.T) {
	envDir := writeLocalnetArtifacts(t)
	if err := os.Remove(filepath.Join(envDir, "dijkstra-genesis.json")); err != nil {
		t.Fatal(err)
	}

	data, _, err := buildPatchData(testOptions(envDir))
	if err != nil {
		t.Fatalf("buildPatchData() error = %v", err)
	}
	if _, exists := data["dijkstra-genesis.json"]; exists {
		t.Fatal("dijkstra-genesis.json was published when source file was absent")
	}
}

func TestRunPrunesAbsentOptionalDijkstraGenesis(t *testing.T) {
	envDir := writeLocalnetArtifacts(t)
	if err := os.Remove(filepath.Join(envDir, "dijkstra-genesis.json")); err != nil {
		t.Fatal(err)
	}

	data, _, err := buildPatchData(testOptions(envDir))
	if err != nil {
		t.Fatalf("buildPatchData() error = %v", err)
	}
	patch := buildConfigMapDataPatch(data)
	if _, exists := patch["dijkstra-genesis.json"]; !exists {
		t.Fatal("dijkstra-genesis.json patch key missing")
	}
	if patch["dijkstra-genesis.json"] != nil {
		t.Fatalf("dijkstra-genesis.json patch value = %q, want null", *patch["dijkstra-genesis.json"])
	}
}

func publisherEnv(envDir, apiURL, tokenPath, namespacePath string) map[string]string {
	env := map[string]string{
		envArtifactConfigMapName: "demo-network-artifacts",
		envArtifactNamespacePath: namespacePath,
		envArtifactTokenPath:     tokenPath,
		envKubernetesAPIURL:      apiURL,
		envLocalnetEnvDir:        envDir,
		envLocalnetManifestFile:  filepath.ToSlash(filepath.Join(envDir, "yacd-localnet-plan.json")),
		envNetworkName:           "demo",
		envNetworkMode:           "local",
		envNetworkEra:            "conway",
		envNodeToNodeHost:        "demo-node.dev.svc.cluster.local",
		envNodeToNodePort:        "3001",
	}
	env[envNodeToNodeURL] = "tcp://demo-node.dev.svc.cluster.local:3001"
	return env
}

func testOptions(envDir string) options {
	return options{
		configMapName:      "demo-network-artifacts",
		configMapNamespace: "dev",
		apiURL:             "http://127.0.0.1",
		tokenPath:          "/tmp/token",
		envDir:             filepath.ToSlash(envDir),
		manifestFile:       filepath.ToSlash(filepath.Join(envDir, "yacd-localnet-plan.json")),
		networkName:        "demo",
		networkNamespace:   "dev",
		networkMode:        "local",
		networkEra:         "conway",
		nodeToNodeHost:     "demo-node.dev.svc.cluster.local",
		nodeToNodePort:     3001,
		nodeToNodeURL:      "tcp://demo-node.dev.svc.cluster.local:3001",
	}
}

func writeLocalnetArtifacts(t *testing.T) string {
	t.Helper()

	envDir := t.TempDir()
	writeFile(t, envDir, "configuration.yaml", "ConwayGenesisFile: conway-genesis.json\n")
	writeFile(t, envDir, "byron-genesis.json", `{"byron":true}`)
	writeFile(t, envDir, "shelley-genesis.json", `{"shelley":true}`)
	writeFile(t, envDir, "alonzo-genesis.json", `{"alonzo":true}`)
	writeFile(t, envDir, "conway-genesis.json", `{"conway":true}`)
	writeFile(t, envDir, "dijkstra-genesis.json", `{"dijkstra":true}`)
	writeFile(t, envDir, "node-data/node1/topology.json", `{"Producers":[]}`)
	writeFile(t, envDir, "yacd-localnet-plan.json", `{"schemaVersion":"test","inputs":{"networkMagic":42},"fingerprint":{"algorithm":"sha256","value":"abc123"}}`)

	return filepath.ToSlash(envDir)
}

func writeFile(t *testing.T, root, name, content string) string {
	t.Helper()

	filePath := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(filePath)
}
