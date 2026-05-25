package names

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	maxLabelValueLength = 63
	shortHashLength     = 10
)

// DNSLabelWithSuffix returns a DNS-label-safe name with suffix appended. A
// short hash is inserted whenever the input is sanitized or truncated.
func DNSLabelWithSuffix(value string, suffix string) string {
	base := sanitizeDNSLabel(value)
	needsHash := base != value
	if base == "" {
		base = "x"
		needsHash = true
	}

	candidateSuffix := "-" + suffix
	if needsHash {
		candidateSuffix = fmt.Sprintf("-%s-%s", ShortHash(value), suffix)
	}
	candidate := base + candidateSuffix
	if len(candidate) <= maxLabelValueLength {
		return candidate
	}

	hashSuffix := fmt.Sprintf("-%s-%s", ShortHash(value), suffix)
	prefixLength := maxLabelValueLength - len(hashSuffix)
	prefix := strings.Trim(base[:prefixLength], "-")
	if prefix == "" {
		prefix = "x"
	}

	return prefix + hashSuffix
}

// LabelValue returns a Kubernetes label-value-safe representation of value. A
// short hash is appended when truncation is required.
func LabelValue(value string) string {
	base := sanitizeLabelValue(value)
	if base == "" {
		base = ShortHash(value)
	}
	if len(base) <= maxLabelValueLength {
		return base
	}

	hashSuffix := "-" + ShortHash(value)
	prefixLength := maxLabelValueLength - len(hashSuffix)
	prefix := strings.TrimRight(base[:prefixLength], "-_.")
	if prefix == "" {
		prefix = "x"
	}

	return prefix + hashSuffix
}

// ShortHash returns the stable short hash used by controller-derived names.
func ShortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:shortHashLength]
}

func sanitizeDNSLabel(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range strings.ToLower(value) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			builder.WriteRune(char)
			continue
		}
		builder.WriteByte('-')
	}

	return strings.Trim(builder.String(), "-")
}

func sanitizeLabelValue(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' ||
			char == '_' ||
			char == '.' {
			builder.WriteRune(char)
			continue
		}
		builder.WriteByte('-')
	}

	return strings.Trim(builder.String(), "-_.")
}
