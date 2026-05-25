package artifacts

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

type fakeGenesisHasher func(context.Context, GenesisKind, string) (string, error)

func (f fakeGenesisHasher) HashGenesis(ctx context.Context, kind GenesisKind, genesisPath string) (string, error) {
	return f(ctx, kind, genesisPath)
}

func TestEnrichGenesisHashesAddsMissingHashes(t *testing.T) {
	artifactData := map[string]string{
		configurationKey: strings.Join([]string{
			"ByronGenesisFile: byron-genesis.json",
			"ShelleyGenesisFile: shelley-genesis.json",
			"AlonzoGenesisFile: alonzo-genesis.json",
			"ConwayGenesisFile: conway-genesis.json",
			"ConwayGenesisHash: existing-conway",
			"",
		}, "\n"),
	}

	enriched, err := EnrichGenesisHashes(context.Background(), "/state/env", artifactData, fakeGenesisHasher(func(_ context.Context, _ GenesisKind, genesisPath string) (string, error) {
		return "hash-" + strings.TrimSuffix(path.Base(genesisPath), ".json"), nil
	}))
	if err != nil {
		t.Fatalf("EnrichGenesisHashes() error = %v", err)
	}
	if artifactData[configurationKey] == enriched[configurationKey] {
		t.Fatal("EnrichGenesisHashes() did not enrich configuration.yaml")
	}

	var configuration map[string]string
	if err := json.Unmarshal([]byte(enriched[configurationKey]), &configuration); err != nil {
		t.Fatalf("parse enriched configuration: %v", err)
	}

	expected := map[string]string{
		"ByronGenesisHash":   "hash-byron-genesis",
		"ShelleyGenesisHash": "hash-shelley-genesis",
		"AlonzoGenesisHash":  "hash-alonzo-genesis",
		"ConwayGenesisHash":  "existing-conway",
	}
	for key, value := range expected {
		if configuration[key] != value {
			t.Errorf("%s = %q, want %q", key, configuration[key], value)
		}
	}
}

func TestEnrichGenesisHashesRejectsEscapingPath(t *testing.T) {
	artifactData := map[string]string{
		configurationKey: "ConwayGenesisFile: ../conway-genesis.json\n",
	}

	_, err := EnrichGenesisHashes(context.Background(), "/state/env", artifactData, fakeGenesisHasher(func(_ context.Context, _ GenesisKind, _ string) (string, error) {
		return "hash", nil
	}))
	if err == nil {
		t.Fatal("EnrichGenesisHashes() error = nil, want escaping-path failure")
	}
	if !strings.Contains(err.Error(), "must stay within the localnet environment") {
		t.Errorf("error = %v, want path boundary failure", err)
	}
}

func TestCardanoCLIHasherUsesExpectedCommands(t *testing.T) {
	tempDir := t.TempDir()
	argsPath := filepath.Join(tempDir, "args")
	cliPath := filepath.Join(tempDir, "cardano-cli")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsPath + "\necho test-hash\n"
	if err := os.WriteFile(cliPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cardano-cli: %v", err)
	}

	hasher := CardanoCLIHasher{Binary: cliPath}
	if hash, err := hasher.HashGenesis(context.Background(), GenesisKindByron, "/state/env/byron-genesis.json"); err != nil {
		t.Fatalf("HashGenesis(byron) error = %v", err)
	} else if hash != "test-hash" {
		t.Fatalf("HashGenesis(byron) = %q", hash)
	}
	assertRecordedArgs(t, argsPath, []string{"byron", "genesis", "print-genesis-hash", "--genesis-json", "/state/env/byron-genesis.json"})

	if _, err := hasher.HashGenesis(context.Background(), GenesisKindConway, "/state/env/conway-genesis.json"); err != nil {
		t.Fatalf("HashGenesis(conway) error = %v", err)
	}
	assertRecordedArgs(t, argsPath, []string{"latest", "genesis", "hash", "--genesis", "/state/env/conway-genesis.json"})
}

func assertRecordedArgs(t *testing.T, argsPath string, expected []string) {
	t.Helper()

	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read recorded args: %v", err)
	}
	actual := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
		t.Fatalf("args = %#v, want %#v", actual, expected)
	}
}
