package builder

import (
	"encoding/json"
	"strings"
	"testing"
)

func validInput(t *testing.T) Input {
	t.Helper()
	return Input{
		Network: NetworkIdentity{
			Name:           "demo",
			Namespace:      "dev",
			Mode:           "local",
			Era:            "conway",
			NodeToNodeHost: "demo-node.dev.svc.cluster.local",
			NodeToNodePort: 3001,
			NodeToNodeURL:  "tcp://demo-node.dev.svc.cluster.local:3001",
		},
		Manifest: Manifest{
			NetworkMagic: 42,
			Fingerprint:  "abc123",
			Raw:          `{"schemaVersion":"test","inputs":{"networkMagic":42},"fingerprint":{"algorithm":"sha256","value":"abc123"}}`,
		},
		Artifacts: map[string]string{
			"configuration.yaml":    "ConwayGenesisFile: conway-genesis.json\n",
			"byron-genesis.json":    `{"byron":true}`,
			"shelley-genesis.json":  `{"shelley":true}`,
			"alonzo-genesis.json":   `{"alonzo":true}`,
			"conway-genesis.json":   `{"conway":true}`,
			"dijkstra-genesis.json": `{"dijkstra":true}`,
			"primary-topology.json": `{"Producers":[]}`,
		},
	}
}

func TestBuild_Success(t *testing.T) {
	patch, err := Build(validInput(t))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	wantKeys := []string{
		"configuration.yaml",
		"byron-genesis.json",
		"shelley-genesis.json",
		"alonzo-genesis.json",
		"conway-genesis.json",
		"dijkstra-genesis.json",
		"primary-topology.json",
		PlanManifestKey,
		ConnectionKey,
	}
	for _, k := range wantKeys {
		if patch.Data[k] == "" {
			t.Errorf("Data[%q] is empty", k)
		}
	}
	if len(patch.Data) != len(wantKeys) {
		t.Errorf("len(Data) = %d, want %d", len(patch.Data), len(wantKeys))
	}

	if patch.Annotations.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %q", patch.Annotations.SchemaVersion)
	}
	if patch.Annotations.LocalnetFingerprint != "abc123" {
		t.Errorf("LocalnetFingerprint = %q", patch.Annotations.LocalnetFingerprint)
	}
	if !strings.HasPrefix(patch.Annotations.DataHash, "sha256:") {
		t.Errorf("DataHash = %q, want sha256: prefix", patch.Annotations.DataHash)
	}

	if len(patch.KnownKeys) != len(generatedSources)+2 {
		t.Errorf("len(KnownKeys) = %d, want %d", len(patch.KnownKeys), len(generatedSources)+2)
	}
	for i := 1; i < len(patch.KnownKeys); i++ {
		if patch.KnownKeys[i-1] >= patch.KnownKeys[i] {
			t.Errorf("KnownKeys not sorted at %d: %q >= %q", i, patch.KnownKeys[i-1], patch.KnownKeys[i])
		}
	}

	var doc connectionDocument
	if err := json.Unmarshal([]byte(patch.Data[ConnectionKey]), &doc); err != nil {
		t.Fatalf("parse connection.json: %v", err)
	}
	if doc.SchemaVersion != SchemaVersion {
		t.Errorf("connection schema = %q", doc.SchemaVersion)
	}
	if doc.Network.Name != "demo" || doc.Network.Namespace != "dev" {
		t.Errorf("connection network = %+v", doc.Network)
	}
	if doc.Network.NetworkMagic != 42 {
		t.Errorf("connection magic = %d", doc.Network.NetworkMagic)
	}
	if doc.Network.LocalnetFingerprint != "abc123" {
		t.Errorf("connection fingerprint = %q", doc.Network.LocalnetFingerprint)
	}
	if doc.PrimaryNodeToNode.URL != "tcp://demo-node.dev.svc.cluster.local:3001" {
		t.Errorf("connection URL = %q", doc.PrimaryNodeToNode.URL)
	}
	if doc.Files["primaryTopology"] != "primary-topology.json" {
		t.Errorf("connection primaryTopology = %q", doc.Files["primaryTopology"])
	}
	if doc.Files["localnetPlan"] != PlanManifestKey {
		t.Errorf("connection localnetPlan = %q", doc.Files["localnetPlan"])
	}
}

func TestBuild_OptionalDijkstraAbsent(t *testing.T) {
	in := validInput(t)
	delete(in.Artifacts, "dijkstra-genesis.json")

	patch, err := Build(in)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, present := patch.Data["dijkstra-genesis.json"]; present {
		t.Error("dijkstra-genesis.json present in Data despite being absent in input")
	}

	var sawDijkstra bool
	for _, k := range patch.KnownKeys {
		if k == "dijkstra-genesis.json" {
			sawDijkstra = true
			break
		}
	}
	if !sawDijkstra {
		t.Error("KnownKeys missing dijkstra-genesis.json — adapter cannot prune it")
	}

	var doc connectionDocument
	if err := json.Unmarshal([]byte(patch.Data[ConnectionKey]), &doc); err != nil {
		t.Fatalf("parse connection.json: %v", err)
	}
	if _, present := doc.Files["dijkstraGenesis"]; present {
		t.Error("connection.json files map references absent dijkstraGenesis")
	}
}

func TestBuild_MissingRequiredArtifact(t *testing.T) {
	in := validInput(t)
	delete(in.Artifacts, "alonzo-genesis.json")

	_, err := Build(in)
	if err == nil {
		t.Fatal("Build() error = nil, want missing-artifact failure")
	}
	if !strings.Contains(err.Error(), "alonzo-genesis.json") {
		t.Errorf("error = %v, want alonzo-genesis.json named", err)
	}
}

func TestBuild_UnknownArtifactKey(t *testing.T) {
	in := validInput(t)
	in.Artifacts["surprise.json"] = "{}"

	_, err := Build(in)
	if err == nil {
		t.Fatal("Build() error = nil, want unknown-key failure")
	}
	if !strings.Contains(err.Error(), "surprise.json") {
		t.Errorf("error = %v, want surprise.json named", err)
	}
}

func TestBuild_ManifestValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Manifest)
		wantMsg string
	}{
		{"zero network magic", func(m *Manifest) { m.NetworkMagic = 0 }, "network magic"},
		{"empty fingerprint", func(m *Manifest) { m.Fingerprint = "" }, "fingerprint"},
		{"empty raw", func(m *Manifest) { m.Raw = "" }, "raw content"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput(t)
			tc.mutate(&in.Manifest)
			_, err := Build(in)
			if err == nil {
				t.Fatalf("Build() error = nil, want %q", tc.wantMsg)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %v, want %q", err, tc.wantMsg)
			}
		})
	}
}

func TestBuild_NetworkValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*NetworkIdentity)
		wantMsg string
	}{
		{"empty name", func(n *NetworkIdentity) { n.Name = "" }, "name"},
		{"empty namespace", func(n *NetworkIdentity) { n.Namespace = "" }, "namespace"},
		{"empty mode", func(n *NetworkIdentity) { n.Mode = "" }, "mode"},
		{"empty era", func(n *NetworkIdentity) { n.Era = "" }, "era"},
		{"empty host", func(n *NetworkIdentity) { n.NodeToNodeHost = "" }, "host"},
		{"empty url", func(n *NetworkIdentity) { n.NodeToNodeURL = "" }, "url"},
		{"port zero", func(n *NetworkIdentity) { n.NodeToNodePort = 0 }, "1-65535"},
		{"port too high", func(n *NetworkIdentity) { n.NodeToNodePort = 70000 }, "1-65535"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput(t)
			tc.mutate(&in.Network)
			_, err := Build(in)
			if err == nil {
				t.Fatalf("Build() error = nil, want %q", tc.wantMsg)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %v, want %q", err, tc.wantMsg)
			}
		})
	}
}

func TestBuild_HashIsDeterministic(t *testing.T) {
	first, err := Build(validInput(t))
	if err != nil {
		t.Fatalf("first Build() error = %v", err)
	}
	second, err := Build(validInput(t))
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	if first.Annotations.DataHash != second.Annotations.DataHash {
		t.Errorf("hash changed: %q != %q", first.Annotations.DataHash, second.Annotations.DataHash)
	}
}

func TestBuild_HashChangesWithContent(t *testing.T) {
	base, err := Build(validInput(t))
	if err != nil {
		t.Fatalf("base Build() error = %v", err)
	}

	mutated := validInput(t)
	mutated.Artifacts["byron-genesis.json"] = `{"byron":false}`
	tweaked, err := Build(mutated)
	if err != nil {
		t.Fatalf("tweaked Build() error = %v", err)
	}
	if base.Annotations.DataHash == tweaked.Annotations.DataHash {
		t.Errorf("hash unchanged after content tweak: %q", base.Annotations.DataHash)
	}
}
