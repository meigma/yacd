package localnet

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// formatSlotLength converts a duration to the seconds value expected by
// cardano-testnet create-env.
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
