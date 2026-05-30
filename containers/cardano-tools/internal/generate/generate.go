package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/meigma/yacd/internal/cardano/localnet"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

// configFilePerm is the mode for the enriched configuration.yaml written back
// into the environment directory.
const configFilePerm = 0o644

// manifestFilePerm is the mode for the plan manifest written into the
// environment directory.
const manifestFilePerm = 0o644

// Options bundles the inputs [Run] consumes.
type Options struct {
	// Spec is the localnet plan spec built from the command flags. Zero-valued
	// fields are defaulted by [localnet.BuildPlan].
	Spec localnet.Spec
	// CardanoCLI overrides the cardano-cli binary used for genesis hashing.
	// Empty selects CARDANO_CLI or "cardano-cli".
	CardanoCLI string
	// DryRun reports whether Run should print the resolved plan instead of
	// generating the environment.
	DryRun bool
}

// Run builds the localnet plan from opts, and either prints it (DryRun) or
// invokes cardano-testnet create-env, writes the plan manifest, and enriches
// configuration.yaml with the genesis hashes cardano-node requires.
func Run(ctx context.Context, opts Options, out io.Writer) error {
	plan, err := localnet.BuildPlan(opts.Spec)
	if err != nil {
		return err
	}

	if opts.DryRun {
		return writeDryRun(out, plan)
	}

	create := exec.CommandContext(ctx, plan.CreateEnv.Command, plan.CreateEnv.Args...)
	create.Stdout = out
	create.Stderr = out
	if err := create.Run(); err != nil {
		return fmt.Errorf("run cardano-testnet create-env: %w", err)
	}

	if err := writeManifest(plan); err != nil {
		return err
	}

	if err := enrichConfigFile(ctx, plan, hasher(opts.CardanoCLI)); err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "generated localnet environment at %s (fingerprint %s)\n",
		plan.Layout.EnvDir, plan.Fingerprint.Value)
	return err
}

// hasher returns the genesis hasher for the configured cardano-cli binary,
// falling back to the environment default when binary is empty.
func hasher(binary string) GenesisHasher {
	if strings.TrimSpace(binary) == "" {
		return CardanoCLIHasherFromEnv()
	}
	return CardanoCLIHasher{Binary: binary}
}

// writeManifest serializes the plan manifest into the environment directory so
// downstream readers (the report verb, the node) can consume it.
func writeManifest(plan localnet.Plan) error {
	raw, err := json.MarshalIndent(plan.Manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal localnet plan manifest: %w", err)
	}
	if err := os.WriteFile(plan.Layout.ManifestFile, append(raw, '\n'), manifestFilePerm); err != nil {
		return fmt.Errorf("write localnet plan manifest: %w", err)
	}
	return nil
}

// enrichConfigFile reads the generated configuration.yaml, fills in any missing
// genesis hash fields, and writes the result back in place.
func enrichConfigFile(ctx context.Context, plan localnet.Plan, h GenesisHasher) error {
	content, err := os.ReadFile(plan.Layout.ConfigFile)
	if err != nil {
		return fmt.Errorf("read generated configuration: %w", err)
	}

	enriched, err := EnrichGenesisHashes(ctx, plan.Layout.EnvDir,
		map[string]string{networkartifacts.ConfigurationKey: string(content)}, h)
	if err != nil {
		return err
	}

	if err := os.WriteFile(plan.Layout.ConfigFile, []byte(enriched[networkartifacts.ConfigurationKey]), configFilePerm); err != nil {
		return fmt.Errorf("write enriched configuration: %w", err)
	}
	return nil
}

// writeDryRun prints the create-env invocation and resolved layout that Run
// would execute, without generating anything.
func writeDryRun(out io.Writer, plan localnet.Plan) error {
	lines := []string{
		"would run: " + plan.CreateEnv.Command + " " + strings.Join(plan.CreateEnv.Args, " "),
		"state-dir: " + plan.Layout.StateDir,
		"env-dir: " + plan.Layout.EnvDir,
		"config-file: " + plan.Layout.ConfigFile,
		"manifest-file: " + plan.Layout.ManifestFile,
		"fingerprint: " + plan.Fingerprint.Value,
	}
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}
