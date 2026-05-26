package dbsync

// BuildPlan normalizes spec and renders the deterministic db-sync runtime
// plan. The returned Plan carries the rendered configuration, follower
// topology, command invocation, libpq environment inputs, and the two
// fingerprints reconcilers use to detect change.
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
	run := buildInvocation(normalized)

	fingerprint, err := computeFingerprint(normalized)
	if err != nil {
		return Plan{}, err
	}
	databaseIdentityFingerprint, err := computeDatabaseIdentityFingerprint(normalized)
	if err != nil {
		return Plan{}, err
	}

	return Plan{
		Spec:                        normalized,
		ConfigYAML:                  configYAML,
		TopologyJSON:                topologyJSON,
		Run:                         run,
		Fingerprint:                 fingerprint,
		DatabaseIdentityFingerprint: databaseIdentityFingerprint,
	}, nil
}

// buildInvocation maps the normalized Spec into the cardano-db-sync command
// arguments. The DisableCache and DisableEpochTable booleans map directly to
// db-sync's --disable-* flags, so the false zero value leaves the feature
// active.
func buildInvocation(spec Spec) Invocation {
	args := []string{
		"--config", spec.Paths.ConfigFile,
		"--socket-path", spec.Paths.SocketPath,
		"--pg-pass-env", "PGPASSFILE",
	}
	if spec.Runtime.DisableCache {
		args = append(args, "--disable-cache")
	}
	if spec.Runtime.DisableEpochTable {
		args = append(args, "--disable-epoch")
	}
	if spec.Runtime.ForceIndexes {
		args = append(args, "--force-indexes")
	}

	return Invocation{Args: args}
}
