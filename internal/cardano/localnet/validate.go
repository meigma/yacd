package localnet

import "errors"

// validateSpec checks normalized Spec invariants and collects every violation
// via errors.Join so callers see the full picture, not just the first failure.
func validateSpec(spec Spec) error {
	var errs []error

	if spec.NetworkMagic < 0 {
		errs = append(errs, errors.New("network magic must be greater than or equal to 0"))
	}
	if spec.PoolCount < 1 {
		errs = append(errs, errors.New("pool count must be greater than or equal to 1"))
	}
	if spec.Timing.EpochLength < 1 {
		errs = append(errs, errors.New("epoch length must be greater than or equal to 1"))
	}
	if spec.Timing.SlotLength <= 0 {
		errs = append(errs, errors.New("slot length must be greater than 0"))
	}
	if spec.Tool.Binary == "" {
		errs = append(errs, errors.New("tool binary must be set"))
	}

	return errors.Join(errs...)
}
