package stage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/meigma/yacd/containers/cardano-tools/internal/artifactset"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

// outputDirPerm is the mode for the created served directory.
const outputDirPerm = 0o755

// outputFilePerm is the mode for files written into the served directory.
const outputFilePerm = 0o644

// Options bundles the inputs [Run] consumes.
type Options struct {
	// StateDir is the cardano-testnet create-env directory to flatten. It holds
	// the nested node state and the localnet plan manifest.
	StateDir string
	// PlanManifestFile is the absolute path to the localnet plan manifest. When
	// empty it defaults to StateDir/yacd-localnet-plan.json.
	PlanManifestFile string
	// OutputDir is the flat served directory the contract-key files,
	// connection.json, and manifest.json are written into.
	OutputDir string
	// Network is the connection identity recorded in connection.json.
	Network artifactset.NetworkIdentity
	// DryRun reports whether Run should print the files it would write instead
	// of writing them.
	DryRun bool
}

// Run flattens a cardano-testnet create-env state directory into a complete
// flat served directory: the contract-key artifact files, the synthesized
// connection.json, and an integrity manifest.json over all of them.
//
// It reuses the same flatten and connection assembly as the report verb
// ([artifactset.ReadManifest], [artifactset.ReadArtifacts], and
// [artifactset.Build]) so the staged directory and the ConfigMap report path
// stay byte-for-byte aligned. manifest.json is written last so it covers
// connection.json.
//
// ctx is accepted for symmetry with the other verbs; staging is local
// filesystem work and does not block on it.
func Run(_ context.Context, opts Options, out io.Writer) error {
	data, err := assemble(opts)
	if err != nil {
		return err
	}

	if opts.DryRun {
		return writeDryRun(out, opts.OutputDir, data)
	}

	if err := os.MkdirAll(opts.OutputDir, outputDirPerm); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	for _, name := range sortedKeys(data) {
		if err := writeFile(opts.OutputDir, name, []byte(data[name])); err != nil {
			return err
		}
	}

	if err := writeManifest(opts.OutputDir, data); err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "staged %s artifacts to %s\n", opts.Network.Mode, opts.OutputDir)
	return err
}

// assemble reads the create-env state directory and returns the flat
// contract-key data map (including connection.json) the served directory holds.
func assemble(opts Options) (map[string]string, error) {
	stateDir, err := filepath.Abs(opts.StateDir)
	if err != nil {
		return nil, fmt.Errorf("resolve state directory: %w", err)
	}

	// ReadManifest enforces manifestFile == stateDir/yacd-localnet-plan.json, so
	// an explicit PlanManifestFile must already point there; otherwise derive it
	// from the resolved state directory.
	manifestFile := opts.PlanManifestFile
	if manifestFile == "" {
		manifestFile = filepath.Join(stateDir, networkartifacts.PlanManifestKey)
	}

	manifest, err := artifactset.ReadManifest(stateDir, manifestFile)
	if err != nil {
		return nil, err
	}

	artifacts, err := artifactset.ReadArtifacts(stateDir)
	if err != nil {
		return nil, err
	}

	set, err := artifactset.Build(artifactset.Input{
		Network:   opts.Network,
		Manifest:  manifest,
		Artifacts: artifacts,
	})
	if err != nil {
		return nil, err
	}
	return set.Data, nil
}

// writeManifest builds the integrity manifest over the served files and writes
// it to OutputDir/manifest.json. [networkartifacts.BuildManifest] skips
// manifest.json itself, so the caller must invoke this after every other file
// is written.
func writeManifest(dir string, data map[string]string) error {
	files := make(map[string][]byte, len(data))
	for name, content := range data {
		files[name] = []byte(content)
	}
	manifest := networkartifacts.BuildManifest(files)
	raw, err := manifest.JSON()
	if err != nil {
		return fmt.Errorf("render manifest.json: %w", err)
	}
	return writeFile(dir, networkartifacts.ManifestKey, raw)
}

// writeFile writes body to name under dir, refusing names that escape dir. The
// staged directory is flat, so any path separator in name is rejected.
func writeFile(dir, name string, body []byte) error {
	if name == "" || strings.ContainsRune(name, '/') || strings.ContainsRune(name, os.PathSeparator) || name == "." || name == ".." {
		return fmt.Errorf("refusing to write artifact with unsafe name %q", name)
	}
	target := filepath.Join(dir, name)
	if err := os.WriteFile(target, body, outputFilePerm); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// writeDryRun prints the served filenames that would be written, in
// deterministic order, plus manifest.json (which a real run also writes).
func writeDryRun(out io.Writer, dir string, data map[string]string) error {
	if _, err := fmt.Fprintf(out, "output directory: %s\n", dir); err != nil {
		return err
	}
	names := sortedKeys(data)
	names = append(names, networkartifacts.ManifestKey)
	sort.Strings(names)
	for _, name := range names {
		if _, err := fmt.Fprintf(out, "would write %s\n", name); err != nil {
			return err
		}
	}
	return nil
}

// sortedKeys returns the keys of data in deterministic order.
func sortedKeys(data map[string]string) []string {
	names := make([]string, 0, len(data))
	for name := range data {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
