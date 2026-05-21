package localnet

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// cleanAbsolutePath trims and normalizes a container path, rejecting relative
// locations.
func cleanAbsolutePath(value string, field string) (string, error) {
	cleaned := path.Clean(strings.TrimSpace(value))
	if cleaned == "." || !strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("%s must be an absolute container path", field)
	}

	return cleaned, nil
}

// validateSpec checks the normalized localnet spec invariants.
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
