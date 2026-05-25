package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
)

// computeDataHash returns a deterministic SHA-256 digest of data,
// prefixed with "sha256:". The digest is computed over a length-
// prefixed framing of sorted key/value pairs:
//
//	<len(key)>:<key>\n<len(value)>:<value>\n
//
// The framing prevents collision between distinct (key, value)
// boundary placements. The wire format is part of the YACD controller's
// artifact verification contract; downstream consumers recompute this
// hash to verify the published ConfigMap data, so the format must not
// change without coordinated controller updates.
func computeDataHash(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	digest := sha256.New()
	for _, key := range keys {
		value := data[key]
		fmt.Fprintf(digest, "%d:%s\n%d:", len(key), key, len(value))
		_, _ = io.WriteString(digest, value)
		_, _ = io.WriteString(digest, "\n")
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}
