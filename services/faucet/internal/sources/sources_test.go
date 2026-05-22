package sources

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreListDiscoversValidSources(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeSource(t, rootDir, "utxo3")
	writeSource(t, rootDir, "utxo1")
	writeSource(t, rootDir, "utxo2")
	writeSourceFile(t, filepath.Join(rootDir, "README.md"), "ignored")
	writeSourceFile(t, filepath.Join(rootDir, "loose-file"), "ignored")
	requireNoError(t, os.Mkdir(filepath.Join(rootDir, "incomplete"), 0o700))

	list, err := NewStore(rootDir, "utxo1").List()
	requireNoError(t, err)

	if got, want := list.DefaultSource, "utxo1"; got != want {
		t.Fatalf("DefaultSource = %q, want %q", got, want)
	}
	if got, want := sourceNames(list.Sources), []string{"utxo1", "utxo2", "utxo3"}; !equalStrings(got, want) {
		t.Fatalf("sources = %#v, want %#v", got, want)
	}
	if !list.Sources[0].Default {
		t.Fatal("utxo1 was not marked as default")
	}
	if list.Sources[0].VerificationKeyType != "GenesisUTxOVerificationKey_ed25519" {
		t.Fatalf("verification key type = %q", list.Sources[0].VerificationKeyType)
	}
	if list.Sources[0].SigningKeyType != "GenesisUTxOSigningKey_ed25519" {
		t.Fatalf("signing key type = %q", list.Sources[0].SigningKeyType)
	}
}

func TestStoreReadyRequiresDefaultSource(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeSource(t, rootDir, "utxo2")

	err := NewStore(rootDir, "utxo1").Ready()
	if err == nil {
		t.Fatal("Ready succeeded, want missing default source error")
	}
	if !IsCode(err, CodeSourceNotFound) {
		t.Fatalf("error = %v, want %s", err, CodeSourceNotFound)
	}
}

func TestStoreRejectsTraversalNames(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir(), "utxo1")
	for _, name := range []string{"../utxo1", "utxo/1", `utxo\1`, "..", "utxo..1"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := store.Get(name)
			if err == nil {
				t.Fatal("Get succeeded, want invalid source name")
			}
			if !IsCode(err, CodeInvalidSourceName) {
				t.Fatalf("error = %v, want %s", err, CodeInvalidSourceName)
			}
		})
	}
}

func TestSourceJSONDoesNotExposeCBORHex(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeSource(t, rootDir, "utxo1")
	source, err := NewStore(rootDir, "utxo1").Get("utxo1")
	requireNoError(t, err)

	encoded, err := json.Marshal(source)
	requireNoError(t, err)
	if strings.Contains(string(encoded), "cborHex") || strings.Contains(string(encoded), "secret-cbor") {
		t.Fatalf("source JSON exposed key material: %s", encoded)
	}
}

func TestStoreGetReturnsValidSource(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeSource(t, rootDir, "utxo1")

	source, err := NewStore(rootDir, "utxo1").Get("utxo1")
	requireNoError(t, err)

	if got, want := source.Name, "utxo1"; got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
	if got, want := source.SigningKeyPath, filepath.Join(rootDir, "utxo1", "utxo.skey"); got != want {
		t.Fatalf("SigningKeyPath = %q, want %q", got, want)
	}
}

func writeSource(t *testing.T, rootDir string, name string) {
	t.Helper()

	sourceDir := filepath.Join(rootDir, name)
	requireNoError(t, os.MkdirAll(sourceDir, 0o700))
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.vkey"), `{
  "type": "GenesisUTxOVerificationKey_ed25519",
  "description": "Genesis Initial UTxO Verification Key",
  "cborHex": "public-cbor"
}`)
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.skey"), `{
  "type": "GenesisUTxOSigningKey_ed25519",
  "description": "Genesis Initial UTxO Signing Key",
  "cborHex": "secret-cbor"
}`)
}

func writeSourceFile(t *testing.T, path string, contents string) {
	t.Helper()

	requireNoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func sourceNames(sources []Source) []string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, source.Name)
	}

	return names
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}

	return true
}

func requireNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
