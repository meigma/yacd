package names

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDNSLabelWithSuffix(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		suffix string
		want   string
	}{
		{
			name:   "unchanged value",
			value:  "network",
			suffix: "node",
			want:   "network-node",
		},
		{
			name:   "sanitized value includes hash",
			value:  "Network One",
			suffix: "node",
			want:   "network-one-" + ShortHash("Network One") + "-node",
		},
		{
			name:   "empty value",
			value:  "",
			suffix: "node",
			want:   "x-" + ShortHash("") + "-node",
		},
		{
			name:   "trimmed punctuation",
			value:  "___",
			suffix: "node",
			want:   "x-" + ShortHash("___") + "-node",
		},
		{
			name:   "sanitized suffix includes hash",
			value:  "network",
			suffix: "Node One",
			want:   "network-" + ShortHash("network\x00Node One") + "-node-one",
		},
		{
			name:   "empty suffix",
			value:  "network",
			suffix: "",
			want:   "network-" + ShortHash("network\x00") + "-x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, DNSLabelWithSuffix(tt.value, tt.suffix))
		})
	}
}

func TestDNSLabelWithSuffixTruncatesWithHash(t *testing.T) {
	value := strings.Repeat("a", 80)

	got := DNSLabelWithSuffix(value, "network-artifacts")

	require.LessOrEqual(t, len(got), MaxLabelValueLength)
	assert.True(t, strings.HasSuffix(got, "-"+ShortHash(value)+"-network-artifacts"))
}

func TestDNSLabelWithSuffixTruncatesLongSuffix(t *testing.T) {
	suffix := strings.Repeat("suffix", 20)

	got := DNSLabelWithSuffix("network", suffix)

	require.LessOrEqual(t, len(got), MaxLabelValueLength)
	assert.True(t, strings.HasPrefix(got, "n-"+ShortHash("network\x00"+suffix)+"-"))
}

func TestDNSLabelWithSuffixHashesTruncatedSuffix(t *testing.T) {
	prefix := strings.Repeat("suffix", 20)

	first := DNSLabelWithSuffix("network", prefix+"first")
	second := DNSLabelWithSuffix("network", prefix+"second")

	require.LessOrEqual(t, len(first), MaxLabelValueLength)
	require.LessOrEqual(t, len(second), MaxLabelValueLength)
	assert.NotEqual(t, first, second)
}

func TestLabelValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "keeps valid label value characters",
			value: "Network_One.1",
			want:  "Network_One.1",
		},
		{
			name:  "sanitizes invalid characters",
			value: "Network One!",
			want:  "Network-One",
		},
		{
			name:  "empty value becomes hash",
			value: "",
			want:  ShortHash(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, LabelValue(tt.value))
		})
	}
}

func TestLabelValueTruncatesWithHash(t *testing.T) {
	value := strings.Repeat("a", 80)

	got := LabelValue(value)

	require.LessOrEqual(t, len(got), MaxLabelValueLength)
	assert.True(t, strings.HasSuffix(got, "-"+ShortHash(value)))
}

func TestShortHashIsStable(t *testing.T) {
	assert.Equal(t, "955af62a37", ShortHash("Network One"))
	assert.Len(t, ShortHash("Network One"), ShortHashLength)
}
