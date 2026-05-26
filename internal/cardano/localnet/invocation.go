package localnet

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// buildCreateEnvInvocation returns the cardano-testnet create-env command and
// arguments derived from the normalized Spec. The pre-rendered slotLength is
// passed in so the same string flows into both the Args slice and the
// fingerprint inputs without re-formatting.
func buildCreateEnvInvocation(spec Spec, slotLength string) Invocation {
	return Invocation{
		Command: spec.Tool.Binary,
		Args: []string{
			"create-env",
			"--num-pool-nodes", strconv.Itoa(spec.PoolCount),
			"--testnet-magic", strconv.FormatInt(spec.NetworkMagic, 10),
			"--epoch-length", strconv.Itoa(spec.Timing.EpochLength),
			"--slot-length", slotLength,
			"--output", spec.Paths.EnvDir,
		},
	}
}

// formatSlotLength converts a duration to the seconds value expected by
// cardano-testnet create-env. The --slot-length flag expects seconds, optionally
// with a fractional component up to nanosecond precision.
func formatSlotLength(duration time.Duration) string {
	nanos := duration.Nanoseconds()
	whole := nanos / int64(time.Second)
	remainder := nanos % int64(time.Second)
	if remainder == 0 {
		return strconv.FormatInt(whole, 10)
	}

	fractional := strings.TrimRight(fmt.Sprintf("%09d", remainder), "0")

	return strconv.FormatInt(whole, 10) + "." + fractional
}
