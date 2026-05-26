package dbsync

import "strings"

// normalizeSpec trims input strings, applies defaults, cleans paths, and
// validates the resulting Spec. The returned Spec is the authoritative form
// used to render config, topology, invocation, and fingerprints.
func normalizeSpec(spec Spec) (Spec, error) {
	// Trim string inputs so semantically identical specs produce identical
	// fingerprints regardless of incidental whitespace.
	spec.NetworkName = strings.TrimSpace(spec.NetworkName)
	spec.NetworkArtifactHash = strings.TrimSpace(spec.NetworkArtifactHash)
	spec.Image = strings.TrimSpace(spec.Image)
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
	spec.Insert.RemoveJSONBFromSchema = strings.TrimSpace(spec.Insert.RemoveJSONBFromSchema)
	spec.IPFSGateways = trimStrings(spec.IPFSGateways)

	// A fully-zero InsertOptions is treated as "use the YACD-recommended
	// defaults" so callers that omit insert configuration get a sensible
	// baseline. Mixing zero and non-zero fields is supported only when the
	// caller starts from DefaultInsertOptions().
	if insertOptionsZero(spec.Insert) {
		spec.Insert = DefaultInsertOptions()
	}

	// Apply scalar defaults to database, runtime, storage, and remaining
	// insert fields.
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
		spec.Insert.PoolStats = insertOptionEnable
	}
	if spec.Insert.JSONType == "" {
		spec.Insert.JSONType = defaultJSONType
	}
	if spec.Insert.RemoveJSONBFromSchema == "" {
		spec.Insert.RemoveJSONBFromSchema = insertOptionDisable
	}

	// Apply container-path defaults last so caller overrides win.
	spec.Paths.ConfigFile = defaultPath(spec.Paths.ConfigFile, defaultConfigFile)
	spec.Paths.TopologyFile = defaultPath(spec.Paths.TopologyFile, defaultTopologyFile)
	spec.Paths.NodeConfig = defaultPath(spec.Paths.NodeConfig, defaultNodeConfig)
	spec.Paths.SocketPath = defaultPath(spec.Paths.SocketPath, defaultSocketPath)
	spec.Paths.StateDir = defaultPath(spec.Paths.StateDir, defaultStateDir)
	spec.Paths.PGPassFile = defaultPath(spec.Paths.PGPassFile, defaultPGPassFile)

	if err := validateSpec(spec); err != nil {
		return Spec{}, err
	}

	return spec, nil
}

// trimStrings drops blanks and returns nil for an empty result so the
// normalized Spec hides incidental whitespace from the fingerprint.
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

// defaultPath returns value with whitespace trimmed, or fallback when value is
// blank.
func defaultPath(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

// insertOptionsZero reports whether every InsertOptions field is the zero
// value.
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
		options.RemoveJSONBFromSchema == ""
}

// featureSelectionZero reports whether every FeatureSelection field is the
// zero value.
func featureSelectionZero(feature FeatureSelection) bool {
	return !feature.Enabled &&
		len(feature.StakeAddresses) == 0 &&
		len(feature.Policies) == 0 &&
		len(feature.Keys) == 0 &&
		len(feature.ScriptHashes) == 0
}
