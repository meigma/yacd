package builder

import (
	"testing"
)

func TestSources_NoSecretMaterial(t *testing.T) {
	for _, src := range Sources() {
		t.Run(src.Key, func(t *testing.T) {
			if err := validatePublicArtifactSource(src); err != nil {
				t.Errorf("source %q rejected by allowlist: %v", src.Key, err)
			}
		})
	}
}

func TestSources_KeysUnique(t *testing.T) {
	seenKey := map[string]struct{}{}
	seenConnKey := map[string]struct{}{}
	for _, src := range Sources() {
		if _, dup := seenKey[src.Key]; dup {
			t.Errorf("duplicate Key %q", src.Key)
		}
		seenKey[src.Key] = struct{}{}
		if src.ConnectionKey == "" {
			continue
		}
		if _, dup := seenConnKey[src.ConnectionKey]; dup {
			t.Errorf("duplicate ConnectionKey %q", src.ConnectionKey)
		}
		seenConnKey[src.ConnectionKey] = struct{}{}
	}
}

func TestSources_ReturnsCopy(t *testing.T) {
	first := Sources()
	if len(first) == 0 {
		t.Fatal("Sources() returned empty slice")
	}
	originalKey := first[0].Key
	first[0].Key = "MUTATED"

	second := Sources()
	if second[0].Key != originalKey {
		t.Errorf("Sources() shares state: %q != %q", second[0].Key, originalKey)
	}
}

func TestValidatePublicArtifactSource_Rejects(t *testing.T) {
	cases := []string{
		"pools-keys/pool1/kes.skey",
		"delegate-keys/delegate1/key",
		"stake-delegators/delegator1/payment.vkey",
		"utxo-keys/utxo1/utxo.addr",
		"genesis-keys/genesis1.vkey",
		"../outside.json",
		"/absolute.json",
		"node/cert.cert",
		"node/seed.skey",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			err := validatePublicArtifactSource(SourceFile{Key: "bad", RelativePath: p})
			if err == nil {
				t.Fatal("validatePublicArtifactSource() error = nil, want rejection")
			}
		})
	}
}
