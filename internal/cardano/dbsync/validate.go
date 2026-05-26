package dbsync

import "errors"

// validateSpec checks normalized Spec invariants and collects every violation
// so callers see the full picture instead of only the first failure.
func validateSpec(spec Spec) error {
	var errs []error

	if spec.NetworkName == "" {
		errs = append(errs, errors.New("network name is required"))
	}
	if spec.Image == "" {
		errs = append(errs, errors.New("db-sync image is required"))
	}
	if spec.NodeToNode.Host == "" {
		errs = append(errs, errors.New("node-to-node host is required"))
	}
	if spec.NodeToNode.Port < 1 || spec.NodeToNode.Port > 65535 {
		errs = append(errs, errors.New("node-to-node port must be between 1 and 65535"))
	}
	if spec.Database.Host == "" {
		errs = append(errs, errors.New("database host is required"))
	}
	if spec.Database.Port < 1 || spec.Database.Port > 65535 {
		errs = append(errs, errors.New("database port must be between 1 and 65535"))
	}
	if spec.Database.Name == "" {
		errs = append(errs, errors.New("database name is required"))
	}
	if spec.Database.User == "" {
		errs = append(errs, errors.New("database user is required"))
	}
	if spec.Database.PasswordSecretName == "" {
		errs = append(errs, errors.New("database password Secret name is required"))
	}
	if spec.Database.PasswordSecretKey == "" {
		errs = append(errs, errors.New("database password Secret key is required"))
	}
	if spec.Runtime.MetricsPort < 1 || spec.Runtime.MetricsPort > 65535 {
		errs = append(errs, errors.New("metrics port must be between 1 and 65535"))
	}
	// The LSM ledger backend cannot be initialized from the partial bootstrap
	// tx_out mode; combining them produces an unusable data directory.
	if spec.Storage.LedgerBackend == "lsm" && spec.Insert.TxOut.Mode == "bootstrap" {
		errs = append(errs, errors.New("tx_out bootstrap is not supported with lsm ledger_backend"))
	}

	return errors.Join(errs...)
}
