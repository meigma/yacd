package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// envState describes how an existing output directory relates to the plan.
type envState int

const (
	// envAbsent means there is no existing environment to preserve.
	envAbsent envState = iota
	// envMatches means a prior run already generated this exact plan.
	envMatches
	// envConflicts means the directory holds a different (or partial) env.
	envConflicts
)

// Run builds the localnet plan from opts, and either prints it (DryRun) or
// generates the environment. Generation is idempotent: if the output directory
// already holds an environment matching this plan it re-runs only the
// (idempotent) genesis-hash enrichment and returns; if it holds a different or
// partial environment it refuses to overwrite, mirroring the init wrapper so a
// pod restart on a populated PVC cannot re-run create-env and wedge.
func Run(ctx context.Context, opts Options, out io.Writer) error {
	plan, err := localnet.BuildPlan(opts.Spec)
	if err != nil {
		return err
	}

	if opts.DryRun {
		return writeDryRun(out, plan)
	}

	switch state, err := inspectEnv(plan); {
	case err != nil:
		return err
	case state == envMatches:
		// Re-run enrichment idempotently in case a prior run died after
		// create-env but before enriching, then report and stop.
		if err := enrichConfigFile(ctx, plan, hasher(opts.CardanoCLI)); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "localnet environment at %s already matches the requested plan\n", plan.Layout.EnvDir)
		return err
	case state == envConflicts:
		return fmt.Errorf("existing localnet environment at %s does not match the requested plan; refusing to overwrite", plan.Layout.EnvDir)
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

// inspectEnv classifies the plan's output directory: absent (safe to
// generate), matches (already generated for this plan), or conflicts (holds a
// different or partial environment).
func inspectEnv(plan localnet.Plan) (envState, error) {
	existing, err := os.ReadFile(plan.Layout.ManifestFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// No plan manifest. If the env dir already holds other content we must
		// not run create-env over it.
		populated, perr := dirPopulated(plan.Layout.EnvDir)
		if perr != nil {
			return envAbsent, perr
		}
		if populated {
			return envConflicts, nil
		}
		return envAbsent, nil
	case err != nil:
		return envAbsent, fmt.Errorf("read existing plan manifest: %w", err)
	}

	// A manifest without its configuration is an inconsistent leftover.
	if _, err := os.Stat(plan.Layout.ConfigFile); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return envConflicts, nil
		}
		return envAbsent, fmt.Errorf("stat existing configuration: %w", err)
	}

	want, err := marshalManifest(plan)
	if err != nil {
		return envAbsent, err
	}
	if bytes.Equal(existing, want) {
		return envMatches, nil
	}
	return envConflicts, nil
}

// dirPopulated reports whether dir exists and contains at least one entry.
func dirPopulated(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
}

// hasher returns the genesis hasher for the configured cardano-cli binary,
// falling back to the environment default when binary is empty.
func hasher(binary string) GenesisHasher {
	if strings.TrimSpace(binary) == "" {
		return CardanoCLIHasherFromEnv()
	}
	return CardanoCLIHasher{Binary: binary}
}

// marshalManifest renders the plan manifest exactly as it is written to disk,
// so the bytes can be compared for the idempotency check.
func marshalManifest(plan localnet.Plan) ([]byte, error) {
	raw, err := json.MarshalIndent(plan.Manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal localnet plan manifest: %w", err)
	}
	return append(raw, '\n'), nil
}

// writeManifest serializes the plan manifest into the environment directory so
// downstream readers (the report verb, the node) can consume it.
func writeManifest(plan localnet.Plan) error {
	raw, err := marshalManifest(plan)
	if err != nil {
		return err
	}
	if err := os.WriteFile(plan.Layout.ManifestFile, raw, manifestFilePerm); err != nil {
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
