package builder

import (
	"fmt"
	"path"
	"strings"
)

// SourceFile declares one localnet artifact file the builder expects.
// The caller (filesystem reader) uses [Sources] to learn which files
// to read from the localnet env dir before invoking [Build].
type SourceFile struct {
	// Key is the ConfigMap data key the file is published under
	// (e.g. "byron-genesis.json").
	Key string
	// RelativePath is the path of the source file under the localnet
	// env dir. Always a clean, relative path.
	RelativePath string
	// ConnectionKey is the key under which this artifact appears in
	// connection.json's files map, e.g. "byronGenesis". Empty means
	// the file is not referenced from connection.json.
	ConnectionKey string
	// Optional reports whether the file may be legitimately absent.
	// Optional sources missing from [Input.Artifacts] do not cause an
	// error; required sources do.
	Optional bool
}

// generatedSources is the registered set of localnet artifacts the
// publisher publishes to the network artifact ConfigMap.
//
//nolint:gochecknoglobals // immutable declarative registry.
var generatedSources = []SourceFile{
	{Key: "configuration.yaml", ConnectionKey: "configuration", RelativePath: "configuration.yaml"},
	{Key: "byron-genesis.json", ConnectionKey: "byronGenesis", RelativePath: "byron-genesis.json"},
	{Key: "shelley-genesis.json", ConnectionKey: "shelleyGenesis", RelativePath: "shelley-genesis.json"},
	{Key: "alonzo-genesis.json", ConnectionKey: "alonzoGenesis", RelativePath: "alonzo-genesis.json"},
	{Key: "conway-genesis.json", ConnectionKey: "conwayGenesis", RelativePath: "conway-genesis.json"},
	{Key: "dijkstra-genesis.json", ConnectionKey: "dijkstraGenesis", RelativePath: "dijkstra-genesis.json", Optional: true},
	{Key: "primary-topology.json", ConnectionKey: "primaryTopology", RelativePath: "node-data/node1/topology.json"},
}

// Sources returns the set of localnet artifact files the builder
// expects to receive in [Input.Artifacts]. The returned slice is a
// copy; mutating it does not affect future calls.
func Sources() []SourceFile {
	return append([]SourceFile(nil), generatedSources...)
}

// validatePublicArtifactSource returns an error when s has an empty
// Key or RelativePath, when RelativePath escapes the localnet env dir
// (absolute, "..", or ".."-prefixed), or when RelativePath traverses a
// denylisted secret/key directory or ends in a denylisted key
// extension (.cert, .counter, .skey, .vkey).
func validatePublicArtifactSource(s SourceFile) error {
	if strings.TrimSpace(s.Key) == "" {
		return fmt.Errorf("source has empty Key")
	}
	if strings.TrimSpace(s.RelativePath) == "" {
		return fmt.Errorf("source %s has empty RelativePath", s.Key)
	}

	clean := path.Clean(s.RelativePath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return fmt.Errorf("source %s must stay within the localnet environment", s.RelativePath)
	}

	deniedComponents := map[string]struct{}{
		"byron-gen-command": {},
		"delegate-keys":     {},
		"drep-keys":         {},
		"faucet-keys":       {},
		"genesis-keys":      {},
		"keys":              {},
		"pools-keys":        {},
		"secrets":           {},
		"stake-delegators":  {},
		"utxo-keys":         {},
	}
	for _, part := range strings.Split(clean, "/") {
		if _, denied := deniedComponents[strings.ToLower(part)]; denied {
			return fmt.Errorf("source %s is under secret/key material", s.RelativePath)
		}
	}

	deniedExtensions := map[string]struct{}{
		".cert":    {},
		".counter": {},
		".skey":    {},
		".vkey":    {},
	}
	if _, denied := deniedExtensions[strings.ToLower(path.Ext(clean))]; denied {
		return fmt.Errorf("source %s is key material", s.RelativePath)
	}

	return nil
}
