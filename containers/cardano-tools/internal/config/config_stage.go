package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// StageConfig is the validated stage runtime configuration produced by
// [LoadStage]. All fields are populated after derivation and validation, so
// consumers can trust the values without further normalization.
type StageConfig struct {
	// StateDir is the cardano-testnet create-env directory to flatten.
	StateDir string
	// PlanManifestFile is the absolute path of the localnet plan manifest. When
	// empty in input, it is derived as StateDir/yacd-localnet-plan.json.
	PlanManifestFile string
	// OutputDir is the flat served directory stage writes into.
	OutputDir string
	// CardanoNetworkName is the name of the owning CardanoNetwork resource.
	CardanoNetworkName string
	// CardanoNetworkNamespace is the namespace of the owning CardanoNetwork
	// resource.
	CardanoNetworkNamespace string
	// CardanoNetworkMode is the network mode (e.g. "local") recorded in the
	// connection metadata.
	CardanoNetworkMode string
	// CardanoNetworkEra is the Cardano era recorded in the connection metadata.
	CardanoNetworkEra string
	// CardanoNodeToNodeHost is the hostname clients use to reach the primary
	// node's node-to-node Service.
	CardanoNodeToNodeHost string
	// CardanoNodeToNodePort is the TCP port for the node-to-node Service. Valid
	// range is 1-65535.
	CardanoNodeToNodePort int
	// CardanoNodeToNodeURL is the full URL for the node-to-node Service. When
	// empty in input and both host and port are set, it is synthesized as
	// "tcp://<host>:<port>".
	CardanoNodeToNodeURL string
	// DryRun reports whether stage should print the files it would write instead
	// of writing them.
	DryRun bool
}

// LoadStage reads, derives, and validates the stage configuration from vp. The
// returned StageConfig is safe to use; on error the zero value is returned with
// a message naming the offending flag.
//
// PlanManifestFile is passed through verbatim: an empty value lets the stage
// package derive StateDir/yacd-localnet-plan.json against the resolved absolute
// state directory, so a relative --state-dir still satisfies the manifest
// reader's absolute-path invariant.
//
// Derivation rules applied before validation:
//   - An empty CardanoNodeToNodeURL is synthesized as "tcp://<host>:<port>"
//     when host and port are both supplied.
func LoadStage(vp *viper.Viper) (StageConfig, error) {
	cfg := StageConfig{
		StateDir:                strings.TrimSpace(vp.GetString("state-dir")),
		PlanManifestFile:        strings.TrimSpace(vp.GetString("plan-manifest-file")),
		OutputDir:               strings.TrimSpace(vp.GetString("output-dir")),
		CardanoNetworkName:      strings.TrimSpace(vp.GetString("cardano-network-name")),
		CardanoNetworkNamespace: strings.TrimSpace(vp.GetString("cardano-network-namespace")),
		CardanoNetworkMode:      strings.TrimSpace(vp.GetString("cardano-network-mode")),
		CardanoNetworkEra:       strings.TrimSpace(vp.GetString("cardano-network-era")),
		CardanoNodeToNodeHost:   strings.TrimSpace(vp.GetString("cardano-node-to-node-host")),
		CardanoNodeToNodePort:   vp.GetInt("cardano-node-to-node-port"),
		CardanoNodeToNodeURL:    strings.TrimSpace(vp.GetString("cardano-node-to-node-url")),
		DryRun:                  vp.GetBool("dry-run"),
	}

	if cfg.CardanoNodeToNodeURL == "" && cfg.CardanoNodeToNodeHost != "" && cfg.CardanoNodeToNodePort != 0 {
		cfg.CardanoNodeToNodeURL = fmt.Sprintf("tcp://%s:%d", cfg.CardanoNodeToNodeHost, cfg.CardanoNodeToNodePort)
	}

	if err := cfg.validate(); err != nil {
		return StageConfig{}, err
	}
	return cfg, nil
}

// validate returns an error when c is missing a required field or carries an
// out-of-range port. Messages reference the user-facing flag name.
func (c StageConfig) validate() error {
	required := []struct {
		flag  string
		value string
	}{
		{"--state-dir", c.StateDir},
		{"--output-dir", c.OutputDir},
		{"--cardano-network-name", c.CardanoNetworkName},
		{"--cardano-network-namespace", c.CardanoNetworkNamespace},
		{"--cardano-network-mode", c.CardanoNetworkMode},
		{"--cardano-network-era", c.CardanoNetworkEra},
		{"--cardano-node-to-node-host", c.CardanoNodeToNodeHost},
	}
	for _, r := range required {
		if r.value == "" {
			return fmt.Errorf("%s is required", r.flag)
		}
	}

	if c.CardanoNodeToNodePort < 1 || c.CardanoNodeToNodePort > 65535 {
		return fmt.Errorf("--cardano-node-to-node-port must be a TCP port between 1 and 65535")
	}
	if c.CardanoNodeToNodeURL == "" {
		return fmt.Errorf("--cardano-node-to-node-url is required")
	}
	return nil
}
