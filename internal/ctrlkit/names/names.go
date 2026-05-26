package names

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// MaxLabelValueLength is the Kubernetes DNS label and label-value length limit.
	MaxLabelValueLength = 63
	// shortHashLength is the number of hex characters used in derived names.
	shortHashLength = 10

	maxHashedSuffixLength = MaxLabelValueLength - len("x-") - shortHashLength - len("-")
)

// DNSLabelWithSuffix returns a DNS-label-safe name with suffix appended. A
// short hash is inserted whenever the input or suffix is sanitized or
// truncated.
func DNSLabelWithSuffix(value string, suffix string) string {
	base := sanitizeDNSLabel(value)
	needsHash := base != value
	if base == "" {
		base = "x"
		needsHash = true
	}

	safeSuffix := sanitizeDNSLabel(suffix)
	suffixNeedsHash := safeSuffix != suffix
	if safeSuffix == "" {
		safeSuffix = "x"
		suffixNeedsHash = true
	}

	hashInput := value
	if suffixNeedsHash {
		hashInput = value + "\x00" + suffix
	}
	hash := shortHash(hashInput)

	candidateSuffix := "-" + safeSuffix
	if needsHash || suffixNeedsHash {
		candidateSuffix = fmt.Sprintf("-%s-%s", hash, safeSuffix)
	}
	candidate := base + candidateSuffix
	if len(candidate) <= MaxLabelValueLength {
		return candidate
	}

	if len(safeSuffix) > maxHashedSuffixLength {
		hash = shortHash(value + "\x00" + suffix)
	}
	hashSuffix := fmt.Sprintf("-%s-%s", hash, truncateHashSuffix(safeSuffix))
	prefixLength := MaxLabelValueLength - len(hashSuffix)
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
		base = shortHash(value)
	}
	if len(base) <= MaxLabelValueLength {
		return base
	}

	hashSuffix := "-" + shortHash(value)
	prefixLength := MaxLabelValueLength - len(hashSuffix)
	prefix := strings.TrimRight(base[:prefixLength], "-_.")
	if prefix == "" {
		prefix = "x"
	}

	return prefix + hashSuffix
}

// shortHash returns the stable short hash used by controller-derived names.
func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:shortHashLength]
}

// truncateHashSuffix clips suffix to maxHashedSuffixLength while preserving a
// valid DNS-label trailing character.
func truncateHashSuffix(suffix string) string {
	if len(suffix) <= maxHashedSuffixLength {
		return suffix
	}

	truncated := strings.Trim(suffix[:maxHashedSuffixLength], "-")
	if truncated == "" {
		return "x"
	}

	return truncated
}

// sanitizeDNSLabel lowercases value and replaces characters outside the DNS
// label alphabet with '-', trimming leading and trailing dashes.
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

// sanitizeLabelValue replaces characters outside the Kubernetes label-value
// alphabet with '-', trimming leading and trailing punctuation.
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
