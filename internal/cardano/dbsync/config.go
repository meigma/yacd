package dbsync

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// dbSyncConfig mirrors the upstream cardano-db-sync configuration YAML
// schema. Field tags use the exact wire spellings; the surrounding Go names
// follow Go conventions.
type dbSyncConfig struct {
	NetworkName          string         `json:"NetworkName"`
	NodeConfigFile       string         `json:"NodeConfigFile"`
	RequiresNetworkMagic string         `json:"RequiresNetworkMagic"`
	EnableLogMetrics     bool           `json:"EnableLogMetrics"`
	EnableLogging        bool           `json:"EnableLogging"`
	PrometheusPort       int32          `json:"PrometheusPort"`
	MinSeverity          string         `json:"minSeverity"`
	LedgerBackend        string         `json:"ledger_backend"`
	SnapshotInterval     snapshotConfig `json:"snapshot_interval"`
	InsertOptions        insertConfig   `json:"insert_options"`
	IPFSGateway          []string       `json:"ipfs_gateway,omitempty"`
	Rotation             rotationConfig `json:"rotation"`
	SetupBackends        []string       `json:"setupBackends"`
	DefaultBackends      []string       `json:"defaultBackends"`
	SetupScribes         []scribeConfig `json:"setupScribes"`
	DefaultScribes       [][]string     `json:"defaultScribes"`
	Options              tracingOptions `json:"options"`
}

// snapshotConfig is the snapshot_interval wire fragment.
type snapshotConfig struct {
	NearTipEpoch int64 `json:"near_tip_epoch"`
}

// insertConfig is the insert_options wire fragment.
type insertConfig struct {
	TxCBOR                string        `json:"tx_cbor"`
	TxOut                 txOutConfig   `json:"tx_out"`
	Ledger                string        `json:"ledger"`
	Shelley               featureConfig `json:"shelley"`
	MultiAsset            featureConfig `json:"multi_asset"`
	Metadata              featureConfig `json:"metadata"`
	Plutus                featureConfig `json:"plutus"`
	Governance            string        `json:"governance"`
	OffchainPoolData      string        `json:"offchain_pool_data"`
	OffchainVoteData      string        `json:"offchain_vote_data"`
	PoolStats             string        `json:"pool_stat"`
	JSONType              string        `json:"json_type"`
	RemoveJSONBFromSchema string        `json:"remove_jsonb_from_schema"`
}

// txOutConfig is the tx_out wire fragment.
type txOutConfig struct {
	Value           string `json:"value"`
	ForceTxIn       bool   `json:"force_tx_in"`
	UseAddressTable bool   `json:"use_address_table"`
}

// featureConfig is the per-feature wire fragment shared by shelley,
// multi_asset, metadata, and plutus inserts.
type featureConfig struct {
	Enabled        bool     `json:"enable"`
	StakeAddresses []string `json:"stake_addresses,omitempty"`
	Policies       []string `json:"policies,omitempty"`
	Keys           []int64  `json:"keys,omitempty"`
	ScriptHashes   []string `json:"script_hashes,omitempty"`
}

// rotationConfig is the upstream log rotation wire fragment.
type rotationConfig struct {
	LogLimitBytes int64 `json:"rpLogLimitBytes"`
	KeepFilesNum  int   `json:"rpKeepFilesNum"`
	MaxAgeHours   int   `json:"rpMaxAgeHours"`
}

// scribeConfig is a single setupScribes entry.
type scribeConfig struct {
	Kind     string `json:"scKind"`
	Name     string `json:"scName"`
	Format   string `json:"scFormat"`
	Rotation *int   `json:"scRotation"`
}

// tracingOptions is the upstream "options" wire fragment.
type tracingOptions struct {
	MapBackends map[string]string            `json:"mapBackends"`
	MapSeverity map[string]string            `json:"mapSeverity"`
	MapSubtrace map[string]map[string]string `json:"mapSubtrace"`
}

// defaultRotationConfig returns the log rotation defaults used by YACD.
func defaultRotationConfig() rotationConfig {
	return rotationConfig{
		LogLimitBytes: 5_000_000,
		KeepFilesNum:  10,
		MaxAgeHours:   24,
	}
}

// defaultScribes returns the stdout scribe configuration used by YACD.
func defaultScribes() []scribeConfig {
	return []scribeConfig{{
		Kind:   "StdoutSK",
		Name:   "stdout",
		Format: "ScText",
	}}
}

// defaultTracingOptions returns the iohk-monitoring tracing defaults used by
// YACD: empty backend remapping, conservative severity for the most
// noisy subsystems, and disabled message counter subtraces.
func defaultTracingOptions() tracingOptions {
	return tracingOptions{
		MapBackends: map[string]string{},
		MapSeverity: map[string]string{
			"db-sync-node":              "Info",
			"db-sync-node.Mux":          "Error",
			"db-sync-node.Subscription": "Error",
		},
		MapSubtrace: map[string]map[string]string{
			"#messagecounters.aggregation": {"subtrace": "NoTrace"},
			"#messagecounters.ekgview":     {"subtrace": "NoTrace"},
			"#messagecounters.katip":       {"subtrace": "NoTrace"},
			"#messagecounters.monitoring":  {"subtrace": "NoTrace"},
			"#messagecounters.switchboard": {"subtrace": "NoTrace"},
		},
	}
}

// renderConfig marshals the normalized Spec into the upstream db-sync
// configuration YAML format.
func renderConfig(spec Spec) (string, error) {
	requiresNetworkMagic := "RequiresNoMagic"
	if spec.RequiresNetworkMagic {
		requiresNetworkMagic = "RequiresMagic"
	}

	config := dbSyncConfig{
		NetworkName:          spec.NetworkName,
		NodeConfigFile:       spec.Paths.NodeConfig,
		RequiresNetworkMagic: requiresNetworkMagic,
		EnableLogMetrics:     true,
		EnableLogging:        true,
		PrometheusPort:       spec.Runtime.MetricsPort,
		MinSeverity:          "Info",
		LedgerBackend:        spec.Storage.LedgerBackend,
		SnapshotInterval:     snapshotConfig{NearTipEpoch: spec.Storage.NearTipEpoch},
		InsertOptions: insertConfig{
			TxCBOR: spec.Insert.TxCBOR,
			TxOut: txOutConfig{
				Value:           spec.Insert.TxOut.Mode,
				ForceTxIn:       spec.Insert.TxOut.ForceTxIn,
				UseAddressTable: spec.Insert.TxOut.UseAddressTable,
			},
			Ledger:                spec.Insert.Ledger,
			Shelley:               featureConfigFor(spec.Insert.Shelley),
			MultiAsset:            featureConfigFor(spec.Insert.MultiAsset),
			Metadata:              featureConfigFor(spec.Insert.Metadata),
			Plutus:                featureConfigFor(spec.Insert.Plutus),
			Governance:            spec.Insert.Governance,
			OffchainPoolData:      spec.Insert.OffchainPoolData,
			OffchainVoteData:      spec.Insert.OffchainVoteData,
			PoolStats:             spec.Insert.PoolStats,
			JSONType:              spec.Insert.JSONType,
			RemoveJSONBFromSchema: spec.Insert.RemoveJSONBFromSchema,
		},
		IPFSGateway:     spec.IPFSGateways,
		Rotation:        defaultRotationConfig(),
		SetupBackends:   []string{"KatipBK"},
		DefaultBackends: []string{"KatipBK"},
		SetupScribes:    defaultScribes(),
		DefaultScribes:  [][]string{{"StdoutSK", "stdout"}},
		Options:         defaultTracingOptions(),
	}

	out, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("marshal db-sync config: %w", err)
	}
	return string(out), nil
}

// featureConfigFor converts a FeatureSelection into the upstream wire form.
// The two types share a shape but carry different JSON tags (domain tags
// participate in fingerprints; wire tags participate in the rendered
// configuration), so a typed conversion is needed.
func featureConfigFor(feature FeatureSelection) featureConfig {
	return featureConfig(feature)
}
