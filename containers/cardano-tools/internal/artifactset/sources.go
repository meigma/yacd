package artifactset

import (
	"fmt"
	"path"
	"strings"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

// SourceFile declares one artifact file [Build] expects. The disk reader
// ([ReadArtifacts]) uses [Sources] to learn which files to read from the
// artifact directory before assembly.
type SourceFile struct {
	// Key is the ConfigMap data key the file is published under, e.g.
	// [networkartifacts.ByronGenesisKey].
	Key string
	// RelativePath is the file's path under the artifact directory. Always a
	// clean, relative path.
	RelativePath string
	// ConnectionKey is the key under which this artifact appears in
	// connection.json's files map, e.g. "byronGenesis". Empty means the file
	// is not referenced from connection.json.
	ConnectionKey string
	// Optional reports whether the file may be legitimately absent. Optional
	// sources missing from the artifact directory do not cause an error;
	// required sources do.
	Optional bool
}

// generatedSources is the registered set of localnet artifacts the tool
// assembles into the network artifact ConfigMap. Keys reference the shared
// contract in [networkartifacts] so the producer and the controller's verifier
// stay aligned by construction.
//
//nolint:gochecknoglobals // immutable declarative registry.
var generatedSources = []SourceFile{
	{Key: networkartifacts.ConfigurationKey, ConnectionKey: "configuration", RelativePath: "configuration.yaml"},
	{Key: networkartifacts.ByronGenesisKey, ConnectionKey: "byronGenesis", RelativePath: "byron-genesis.json"},
	{Key: networkartifacts.ShelleyGenesisKey, ConnectionKey: "shelleyGenesis", RelativePath: "shelley-genesis.json"},
	{Key: networkartifacts.AlonzoGenesisKey, ConnectionKey: "alonzoGenesis", RelativePath: "alonzo-genesis.json"},
	{Key: networkartifacts.ConwayGenesisKey, ConnectionKey: "conwayGenesis", RelativePath: "conway-genesis.json"},
	{Key: networkartifacts.DijkstraGenesisKey, ConnectionKey: "dijkstraGenesis", RelativePath: "dijkstra-genesis.json", Optional: true},
	{Key: networkartifacts.PrimaryTopologyKey, ConnectionKey: "primaryTopology", RelativePath: "node-data/node1/topology.json"},
}

// Sources returns the artifact files [Build] expects in [Input.Artifacts]. The
// returned slice is a copy; mutating it does not affect future calls.
func Sources() []SourceFile {
	return append([]SourceFile(nil), generatedSources...)
}

// validateSourcePath returns an error when s has an empty Key or RelativePath,
// when RelativePath escapes the artifact directory (absolute, ".", "..", or
// ".."-prefixed), or when RelativePath traverses a secret/key directory or ends
// in a key-material extension. It is an invariant guard on the registry and a
// reusable check for any path resolved relative to the artifact directory.
func validateSourcePath(key, relativePath string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("source has empty key")
	}
	if strings.TrimSpace(relativePath) == "" {
		return fmt.Errorf("source %s has empty relative path", key)
	}

	clean := path.Clean(relativePath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return fmt.Errorf("source %s must stay within the artifact directory", relativePath)
	}

	for part := range strings.SplitSeq(clean, "/") {
		if isSecretComponent(part) {
			return fmt.Errorf("source %s is under secret/key material", relativePath)
		}
	}

	if isSecretExtension(path.Ext(clean)) {
		return fmt.Errorf("source %s is key material", relativePath)
	}

	return nil
}

// isSecretComponent reports whether a path component names a Cardano secret or
// key directory that must never be published.
func isSecretComponent(name string) bool {
	switch strings.ToLower(name) {
	case "byron-gen-command", "delegate-keys", "drep-keys", "faucet-keys",
		"genesis-keys", "keys", "pools-keys", "secrets", "stake-delegators",
		"utxo-keys":
		return true
	default:
		return false
	}
}

// isSecretExtension reports whether a file extension marks Cardano key
// material that must never be published.
func isSecretExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".cert", ".counter", ".skey", ".vkey":
		return true
	default:
		return false
	}
}
