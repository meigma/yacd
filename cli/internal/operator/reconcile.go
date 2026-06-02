package operator

import (
	"errors"
	"fmt"

	"golang.org/x/mod/semver"
)

// Action is the install action Decide selects from the embedded and installed
// versions. Install, Upgrade, and Noop all drive the same idempotent apply; the
// distinction exists for progress reporting and to gate the refuse path.
type Action int

const (
	// ActionInstall applies the operator into a cluster that has none.
	ActionInstall Action = iota

	// ActionUpgrade applies a newer same-major operator over an older install.
	ActionUpgrade

	// ActionNoop re-applies an equal version to heal any drift.
	ActionNoop

	// ActionRefuse declines to act; Decide returns a typed error alongside it.
	ActionRefuse
)

// ErrUnknownInstalledVersion is returned when the operator is installed but its
// version cannot be determined, so a safe upgrade decision is impossible.
var ErrUnknownInstalledVersion = errors.New("cannot determine installed operator version")

// ErrNewerOperator is returned when the in-cluster operator is newer than the
// version this CLI embeds.
var ErrNewerOperator = errors.New("installed operator is newer than this CLI")

// ErrMajorMismatch is returned when the in-cluster operator differs from the
// embedded version by major version, which requires a deliberate migration.
var ErrMajorMismatch = errors.New("installed operator has a different major version than this CLI")

// Decide chooses the install action by comparing the embedded (CLI) operator
// version against the installed state. It is pure so the policy is testable
// without a cluster. Both versions are expected in semver form with a leading
// "v" (for example "v0.1.1").
func Decide(embedded string, state State) (Action, error) {
	if !semver.IsValid(embedded) {
		// A malformed embedded version is a build defect, not a cluster state.
		return ActionRefuse, fmt.Errorf("embedded operator version %q is not valid semver", embedded)
	}

	if !state.Installed {
		return ActionInstall, nil
	}

	installed := state.Version
	if !semver.IsValid(installed) {
		return ActionRefuse, fmt.Errorf("%w: %q", ErrUnknownInstalledVersion, installed)
	}

	if semver.Major(installed) != semver.Major(embedded) {
		return ActionRefuse, fmt.Errorf(
			"%w: installed %s, this CLI embeds %s; a manual migration is required",
			ErrMajorMismatch, installed, embedded,
		)
	}

	switch semver.Compare(installed, embedded) {
	case 0:
		return ActionNoop, nil
	case -1:
		return ActionUpgrade, nil
	default:
		return ActionRefuse, fmt.Errorf(
			"%w: installed %s, this CLI embeds %s; upgrade the CLI",
			ErrNewerOperator, installed, embedded,
		)
	}
}
