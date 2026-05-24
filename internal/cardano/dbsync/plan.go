package dbsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"
)

const (
	defaultDatabasePort        int32 = 5432
	defaultDatabaseName              = "cexplorer"
	defaultDatabaseUser              = "postgres"
	defaultDatabasePasswordKey       = "password"
	defaultDatabaseSSLMode           = "disable"
	defaultMetricsPort         int32 = 8080
	defaultNearTipEpoch        int64 = 580
	defaultStateStorageSize          = "10Gi"

	defaultConfigFile   = "/config/db-sync-config.yaml"
	defaultTopologyFile = "/config/follower-topology.json"
	defaultNodeConfig   = "/network-artifacts/configuration.yaml"
	defaultSocketPath   = "/ipc/node.socket"
	defaultStateDir     = "/state/db-sync-ledger"
	defaultSchemaDir    = "/opt/cardano-db-sync/schema"
	defaultPGPassFile   = "/secrets/postgres/.pgpass"

	defaultLedgerBackend = "lsm"
	defaultTxCBOR        = "disable"
	defaultTxOutMode     = "enable"
	defaultLedger        = "enable"
	defaultJSONType      = "text"

	insertOptionDisable = "disable"
	insertOptionEnable  = "enable"
)

// BuildPlan validates and normalizes spec into a deterministic db-sync plan.
func BuildPlan(spec Spec) (Plan, error) {
	normalized, err := normalizeSpec(spec)
	if err != nil {
		return Plan{}, err
	}

	configYAML, err := renderConfig(normalized)
	if err != nil {
		return Plan{}, err
	}
	topologyJSON, err := renderTopology(normalized.NodeToNode)
	if err != nil {
		return Plan{}, err
	}
	run := runtimeInvocation(normalized)

	fingerprint, err := computeFingerprint(normalized)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Spec:         normalized,
		ConfigYAML:   configYAML,
		TopologyJSON: topologyJSON,
		Run:          run,
		Fingerprint:  fingerprint,
	}, nil
}

func normalizeSpec(spec Spec) (Spec, error) {
	spec.NetworkName = strings.TrimSpace(spec.NetworkName)
	spec.NodeToNode.Host = strings.TrimSpace(spec.NodeToNode.Host)
	spec.Database.Host = strings.TrimSpace(spec.Database.Host)
	spec.Database.Name = strings.TrimSpace(spec.Database.Name)
	spec.Database.User = strings.TrimSpace(spec.Database.User)
	spec.Database.PasswordSecretName = strings.TrimSpace(spec.Database.PasswordSecretName)
	spec.Database.PasswordSecretKey = strings.TrimSpace(spec.Database.PasswordSecretKey)
	spec.Database.SSLMode = strings.TrimSpace(spec.Database.SSLMode)
	spec.Storage.LedgerBackend = strings.TrimSpace(spec.Storage.LedgerBackend)
	spec.Insert.TxCBOR = strings.TrimSpace(spec.Insert.TxCBOR)
	spec.Insert.TxOut.Mode = strings.TrimSpace(spec.Insert.TxOut.Mode)
	spec.Insert.Ledger = strings.TrimSpace(spec.Insert.Ledger)
	spec.Insert.Governance = strings.TrimSpace(spec.Insert.Governance)
	spec.Insert.OffchainPoolData = strings.TrimSpace(spec.Insert.OffchainPoolData)
	spec.Insert.OffchainVoteData = strings.TrimSpace(spec.Insert.OffchainVoteData)
	spec.Insert.PoolStats = strings.TrimSpace(spec.Insert.PoolStats)
	spec.Insert.JSONType = strings.TrimSpace(spec.Insert.JSONType)
	spec.IPFSGateways = trimStrings(spec.IPFSGateways)
	if insertOptionsZero(spec.Insert) {
		spec.Insert = defaultInsertOptions()
	}

	if spec.Database.Port == 0 {
		spec.Database.Port = defaultDatabasePort
	}
	if spec.Database.Name == "" {
		spec.Database.Name = defaultDatabaseName
	}
	if spec.Database.User == "" {
		spec.Database.User = defaultDatabaseUser
	}
	if spec.Database.PasswordSecretKey == "" {
		spec.Database.PasswordSecretKey = defaultDatabasePasswordKey
	}
	if spec.Database.SSLMode == "" {
		spec.Database.SSLMode = defaultDatabaseSSLMode
	}
	if spec.Runtime.MetricsPort == 0 {
		spec.Runtime.MetricsPort = defaultMetricsPort
	}
	if spec.Storage.LedgerBackend == "" {
		spec.Storage.LedgerBackend = defaultLedgerBackend
	}
	if spec.Storage.NearTipEpoch == 0 {
		spec.Storage.NearTipEpoch = defaultNearTipEpoch
	}
	if strings.TrimSpace(spec.Storage.StateStorageSize) == "" {
		spec.Storage.StateStorageSize = defaultStateStorageSize
	}
	if spec.Insert.TxCBOR == "" {
		spec.Insert.TxCBOR = defaultTxCBOR
	}
	if spec.Insert.TxOut.Mode == "" {
		spec.Insert.TxOut.Mode = defaultTxOutMode
	}
	if spec.Insert.Ledger == "" {
		spec.Insert.Ledger = defaultLedger
	}
	if spec.Insert.Governance == "" {
		spec.Insert.Governance = insertOptionEnable
	}
	if spec.Insert.OffchainPoolData == "" {
		spec.Insert.OffchainPoolData = insertOptionDisable
	}
	if spec.Insert.OffchainVoteData == "" {
		spec.Insert.OffchainVoteData = insertOptionDisable
	}
	if spec.Insert.PoolStats == "" {
		spec.Insert.PoolStats = insertOptionDisable
	}
	if spec.Insert.JSONType == "" {
		spec.Insert.JSONType = defaultJSONType
	}

	spec.Paths.ConfigFile = defaultPath(spec.Paths.ConfigFile, defaultConfigFile)
	spec.Paths.TopologyFile = defaultPath(spec.Paths.TopologyFile, defaultTopologyFile)
	spec.Paths.NodeConfig = defaultPath(spec.Paths.NodeConfig, defaultNodeConfig)
	spec.Paths.SocketPath = defaultPath(spec.Paths.SocketPath, defaultSocketPath)
	spec.Paths.StateDir = defaultPath(spec.Paths.StateDir, defaultStateDir)
	spec.Paths.SchemaDir = defaultPath(spec.Paths.SchemaDir, defaultSchemaDir)
	spec.Paths.PGPassFile = defaultPath(spec.Paths.PGPassFile, defaultPGPassFile)

	if err := validateSpec(spec); err != nil {
		return Spec{}, err
	}

	return spec, nil
}

func trimStrings(values []string) []string {
	if values == nil {
		return nil
	}
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		trimmed = append(trimmed, value)
	}
	if len(trimmed) == 0 {
		return nil
	}

	return trimmed
}

func insertOptionsZero(options InsertOptions) bool {
	return options.TxCBOR == "" &&
		options.TxOut == (TxOutOption{}) &&
		options.Ledger == "" &&
		featureSelectionZero(options.Shelley) &&
		featureSelectionZero(options.MultiAsset) &&
		featureSelectionZero(options.Metadata) &&
		featureSelectionZero(options.Plutus) &&
		options.Governance == "" &&
		options.OffchainPoolData == "" &&
		options.OffchainVoteData == "" &&
		options.PoolStats == "" &&
		options.JSONType == "" &&
		!options.RemoveJSONBFromSchema
}

func featureSelectionZero(feature FeatureSelection) bool {
	return !feature.Enabled &&
		len(feature.StakeAddresses) == 0 &&
		len(feature.Policies) == 0 &&
		len(feature.Keys) == 0 &&
		len(feature.ScriptHashes) == 0
}

func defaultInsertOptions() InsertOptions {
	return InsertOptions{
		TxCBOR: defaultTxCBOR,
		TxOut: TxOutOption{
			Mode: defaultTxOutMode,
		},
		Ledger:           defaultLedger,
		Shelley:          FeatureSelection{Enabled: true},
		MultiAsset:       FeatureSelection{Enabled: true},
		Metadata:         FeatureSelection{Enabled: true},
		Plutus:           FeatureSelection{Enabled: true},
		Governance:       insertOptionEnable,
		OffchainPoolData: insertOptionDisable,
		OffchainVoteData: insertOptionDisable,
		PoolStats:        insertOptionDisable,
		JSONType:         defaultJSONType,
	}
}

func defaultPath(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func validateSpec(spec Spec) error {
	switch {
	case spec.NetworkName == "":
		return fmt.Errorf("network name is required")
	case spec.NodeToNode.Host == "":
		return fmt.Errorf("node-to-node host is required")
	case spec.NodeToNode.Port < 1 || spec.NodeToNode.Port > 65535:
		return fmt.Errorf("node-to-node port must be between 1 and 65535")
	case spec.Database.Host == "":
		return fmt.Errorf("database host is required")
	case spec.Database.Port < 1 || spec.Database.Port > 65535:
		return fmt.Errorf("database port must be between 1 and 65535")
	case spec.Database.Name == "":
		return fmt.Errorf("database name is required")
	case spec.Database.User == "":
		return fmt.Errorf("database user is required")
	case spec.Database.PasswordSecretName == "":
		return fmt.Errorf("database password Secret name is required")
	case spec.Database.PasswordSecretKey == "":
		return fmt.Errorf("database password Secret key is required")
	case spec.Runtime.MetricsPort < 1 || spec.Runtime.MetricsPort > 65535:
		return fmt.Errorf("metrics port must be between 1 and 65535")
	}

	return nil
}

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

type snapshotConfig struct {
	NearTipEpoch int64 `json:"near_tip_epoch"`
}

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
	RemoveJSONBFromSchema bool          `json:"remove_jsonb_from_schema"`
}

type txOutConfig struct {
	Value           string `json:"value"`
	ForceTxIn       bool   `json:"force_tx_in"`
	UseAddressTable bool   `json:"use_address_table"`
}

type featureConfig struct {
	Enabled        bool     `json:"enable"`
	StakeAddresses []string `json:"stake_addresses,omitempty"`
	Policies       []string `json:"policies,omitempty"`
	Keys           []int64  `json:"keys,omitempty"`
	ScriptHashes   []string `json:"script_hashes,omitempty"`
}

type rotationConfig struct {
	LogLimitBytes int64 `json:"rpLogLimitBytes"`
	KeepFilesNum  int   `json:"rpKeepFilesNum"`
	MaxAgeHours   int   `json:"rpMaxAgeHours"`
}

type scribeConfig struct {
	Kind     string `json:"scKind"`
	Name     string `json:"scName"`
	Format   string `json:"scFormat"`
	Rotation *int   `json:"scRotation"`
}

type tracingOptions struct {
	MapBackends map[string]string            `json:"mapBackends"`
	MapSeverity map[string]string            `json:"mapSeverity"`
	MapSubtrace map[string]map[string]string `json:"mapSubtrace"`
}

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
		SnapshotInterval: snapshotConfig{
			NearTipEpoch: spec.Storage.NearTipEpoch,
		},
		InsertOptions: insertConfig{
			TxCBOR: spec.Insert.TxCBOR,
			TxOut: txOutConfig{
				Value:           spec.Insert.TxOut.Mode,
				ForceTxIn:       spec.Insert.TxOut.ForceTxIn,
				UseAddressTable: spec.Insert.TxOut.UseAddressTable,
			},
			Ledger:                spec.Insert.Ledger,
			Shelley:               featureConfigFrom(spec.Insert.Shelley),
			MultiAsset:            featureConfigFrom(spec.Insert.MultiAsset),
			Metadata:              featureConfigFrom(spec.Insert.Metadata),
			Plutus:                featureConfigFrom(spec.Insert.Plutus),
			Governance:            spec.Insert.Governance,
			OffchainPoolData:      spec.Insert.OffchainPoolData,
			OffchainVoteData:      spec.Insert.OffchainVoteData,
			PoolStats:             spec.Insert.PoolStats,
			JSONType:              spec.Insert.JSONType,
			RemoveJSONBFromSchema: spec.Insert.RemoveJSONBFromSchema,
		},
		IPFSGateway: spec.IPFSGateways,
		Rotation: rotationConfig{
			LogLimitBytes: 5000000,
			KeepFilesNum:  10,
			MaxAgeHours:   24,
		},
		SetupBackends:   []string{"KatipBK"},
		DefaultBackends: []string{"KatipBK"},
		SetupScribes: []scribeConfig{{
			Kind:   "StdoutSK",
			Name:   "stdout",
			Format: "ScText",
		}},
		DefaultScribes: [][]string{{"StdoutSK", "stdout"}},
		Options: tracingOptions{
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
		},
	}

	out, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("marshal db-sync config: %w", err)
	}
	return string(out), nil
}

func featureConfigFrom(feature FeatureSelection) featureConfig {
	return featureConfig(feature)
}

type topology struct {
	LocalRoots         []localRoot `json:"localRoots"`
	PublicRoots        []any       `json:"publicRoots"`
	UseLedgerAfterSlot int         `json:"useLedgerAfterSlot"`
}

type localRoot struct {
	AccessPoints []accessPoint `json:"accessPoints"`
	Advertise    bool          `json:"advertise"`
	Valency      int           `json:"valency"`
}

type accessPoint struct {
	Address string `json:"address"`
	Port    int32  `json:"port"`
}

func renderTopology(nodeToNode NodeToNode) (string, error) {
	out, err := json.MarshalIndent(topology{
		LocalRoots: []localRoot{{
			AccessPoints: []accessPoint{{
				Address: nodeToNode.Host,
				Port:    nodeToNode.Port,
			}},
			Advertise: false,
			Valency:   1,
		}},
		PublicRoots:        []any{},
		UseLedgerAfterSlot: -1,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal follower topology: %w", err)
	}
	return string(out) + "\n", nil
}

func runtimeInvocation(spec Spec) Invocation {
	args := []string{
		"--config", spec.Paths.ConfigFile,
		"--socket-path", spec.Paths.SocketPath,
		"--state-dir", spec.Paths.StateDir,
		"--schema-dir", spec.Paths.SchemaDir,
		"--pg-pass-env", "PGPASSFILE",
	}
	if !spec.Runtime.Cache {
		args = append(args, "--disable-cache")
	}
	if !spec.Runtime.EpochTable {
		args = append(args, "--disable-epoch")
	}
	if spec.Runtime.ForceIndexes {
		args = append(args, "--force-indexes")
	}

	return Invocation{
		Command: "cardano-db-sync",
		Args:    args,
	}
}

func computeFingerprint(spec Spec) (Fingerprint, error) {
	input, err := json.Marshal(spec)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("marshal fingerprint input: %w", err)
	}
	sum := sha256.Sum256(input)
	return Fingerprint{
		Algorithm: "sha256",
		Value:     hex.EncodeToString(sum[:]),
	}, nil
}

// Environment returns non-secret libpq environment variables for the plan.
func (p Plan) Environment() []EnvVar {
	return []EnvVar{
		{Name: "PGHOST", Value: p.Spec.Database.Host},
		{Name: "PGPORT", Value: strconv.Itoa(int(p.Spec.Database.Port))},
		{Name: "PGDATABASE", Value: p.Spec.Database.Name},
		{Name: "PGUSER", Value: p.Spec.Database.User},
		{Name: "PGSSLMODE", Value: p.Spec.Database.SSLMode},
		{Name: "PGPASSFILE", Value: p.Spec.Paths.PGPassFile},
	}
}

// EnvVar describes a non-secret process environment variable.
type EnvVar struct {
	Name  string
	Value string
}
