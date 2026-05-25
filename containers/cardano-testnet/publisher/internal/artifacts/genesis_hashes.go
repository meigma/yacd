// Package artifacts prepares generated localnet artifacts before the
// publisher turns them into a ConfigMap patch.
package artifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path"
	"strings"

	"sigs.k8s.io/yaml"
)

const configurationKey = "configuration.yaml"

// GenesisKind identifies which Cardano genesis hash command should be
// used for a genesis file referenced by configuration.yaml.
type GenesisKind string

const (
	GenesisKindByron   GenesisKind = "byron"
	GenesisKindShelley GenesisKind = "shelley"
	GenesisKindAlonzo  GenesisKind = "alonzo"
	GenesisKindConway  GenesisKind = "conway"
)

// GenesisHasher computes a Cardano genesis hash for a local genesis
// file. Implementations may call cardano-cli or provide test doubles.
type GenesisHasher interface {
	HashGenesis(context.Context, GenesisKind, string) (string, error)
}

// CardanoCLIHasher shells out to cardano-cli for genesis hash
// computation. YACD's cardano-testnet tools image already includes
// cardano-cli, which keeps protocol-specific hashing semantics in the
// upstream tool instead of reimplementing them in the publisher.
type CardanoCLIHasher struct {
	Binary string
}

// CardanoCLIHasherFromEnv returns a hasher using CARDANO_CLI when set,
// otherwise "cardano-cli".
func CardanoCLIHasherFromEnv() CardanoCLIHasher {
	binary := strings.TrimSpace(os.Getenv("CARDANO_CLI"))
	if binary == "" {
		binary = "cardano-cli"
	}
	return CardanoCLIHasher{Binary: binary}
}

// HashGenesis computes the hash for genesisPath using cardano-cli.
func (h CardanoCLIHasher) HashGenesis(ctx context.Context, kind GenesisKind, genesisPath string) (string, error) {
	binary := strings.TrimSpace(h.Binary)
	if binary == "" {
		binary = "cardano-cli"
	}

	args := []string{"latest", "genesis", "hash", "--genesis", genesisPath}
	if kind == GenesisKindByron {
		args = []string{"byron", "genesis", "print-genesis-hash", "--genesis-json", genesisPath}
	}

	out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", binary, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// EnrichGenesisHashes returns a copy of artifactData whose
// configuration.yaml includes missing genesis hash fields for any
// referenced genesis files.
func EnrichGenesisHashes(ctx context.Context, envDir string, artifactData map[string]string, hasher GenesisHasher) (map[string]string, error) {
	out := make(map[string]string, len(artifactData))
	maps.Copy(out, artifactData)

	content, ok := out[configurationKey]
	if !ok {
		return out, nil
	}

	configuration, err := enrichNodeConfiguration(ctx, envDir, content, hasher)
	if err != nil {
		return nil, err
	}
	out[configurationKey] = configuration
	return out, nil
}

type genesisHashField struct {
	fileKey string
	hashKey string
	kind    GenesisKind
}

var genesisHashFields = []genesisHashField{
	{fileKey: "ByronGenesisFile", hashKey: "ByronGenesisHash", kind: GenesisKindByron},
	{fileKey: "ShelleyGenesisFile", hashKey: "ShelleyGenesisHash", kind: GenesisKindShelley},
	{fileKey: "AlonzoGenesisFile", hashKey: "AlonzoGenesisHash", kind: GenesisKindAlonzo},
	{fileKey: "ConwayGenesisFile", hashKey: "ConwayGenesisHash", kind: GenesisKindConway},
}

func enrichNodeConfiguration(ctx context.Context, envDir, content string, hasher GenesisHasher) (string, error) {
	var nodeConfig map[string]any
	if err := yaml.Unmarshal([]byte(content), &nodeConfig); err != nil {
		return "", fmt.Errorf("parse configuration.yaml: %w", err)
	}

	changed := false
	for _, field := range genesisHashFields {
		if value, ok := nodeConfig[field.hashKey].(string); ok && strings.TrimSpace(value) != "" {
			continue
		}

		genesisFile, ok := nodeConfig[field.fileKey].(string)
		if !ok || strings.TrimSpace(genesisFile) == "" {
			continue
		}

		genesisPath, err := artifactPathUnderEnv(envDir, genesisFile)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", field.fileKey, err)
		}
		hash, err := hasher.HashGenesis(ctx, field.kind, genesisPath)
		if err != nil {
			return "", fmt.Errorf("compute %s: %w", field.hashKey, err)
		}
		hash = strings.TrimSpace(hash)
		if hash == "" {
			return "", fmt.Errorf("compute %s: hash is empty", field.hashKey)
		}

		nodeConfig[field.hashKey] = hash
		changed = true
	}
	if !changed {
		return content, nil
	}

	out, err := json.MarshalIndent(nodeConfig, "", "    ")
	if err != nil {
		return "", fmt.Errorf("marshal enriched configuration.yaml: %w", err)
	}
	return string(out) + "\n", nil
}

func artifactPathUnderEnv(envDir, filePath string) (string, error) {
	envDir = path.Clean(envDir)
	clean := path.Clean(filePath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("artifact path %s must stay within the localnet environment", filePath)
	}
	return path.Join(envDir, clean), nil
}
