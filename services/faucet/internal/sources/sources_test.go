package sources

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testDefaultSource     = "utxo1"
	testAddress           = "addr_test1vqy2n0vz5rlpykf6dcqn55xdcpey7mejyexlgj6370leayst4k6ta"
	testKeyCBORHex        = "58200101010101010101010101010101010101010101010101010101010101010101"
	testRawKeyHex         = "0101010101010101010101010101010101010101010101010101010101010101"
	testSecretMaterial    = "secret-cbor-material"
	oversizedFileContents = 5 * 1024
)

func TestStoreListDiscoversValidSources(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeSource(t, rootDir, "utxo3")
	writeSource(t, rootDir, testDefaultSource)
	writeSource(t, rootDir, "utxo2")
	writeSourceFile(t, filepath.Join(rootDir, "README.md"), "ignored")
	writeSourceFile(t, filepath.Join(rootDir, "loose-file"), "ignored")
	requireNoError(t, os.Mkdir(filepath.Join(rootDir, "incomplete"), 0o700))

	list, err := NewStore(rootDir, testDefaultSource).List()
	requireNoError(t, err)

	if got, want := list.DefaultSource, testDefaultSource; got != want {
		t.Fatalf("DefaultSource = %q, want %q", got, want)
	}
	if got, want := sourceNames(list.Sources), []string{testDefaultSource, "utxo2", "utxo3"}; !equalStrings(got, want) {
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

	err := NewStore(rootDir, testDefaultSource).Ready()
	if err == nil {
		t.Fatal("Ready succeeded, want missing default source error")
	}
	if !IsCode(err, CodeSourceNotFound) {
		t.Fatalf("error = %v, want %s", err, CodeSourceNotFound)
	}
}

func TestStoreRejectsTraversalNames(t *testing.T) {
	t.Parallel()

	store := NewStore(t.TempDir(), testDefaultSource)
	for _, name := range []string{"../utxo1", "utxo/1", `utxo\1`, "..", "utxo..1", "wallet1", "utxo0", "utxo01", "utxo1\x00"} {
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
	writeSource(t, rootDir, testDefaultSource)
	source, err := NewStore(rootDir, testDefaultSource).Get(testDefaultSource)
	requireNoError(t, err)

	encoded, err := json.Marshal(source)
	requireNoError(t, err)
	if strings.Contains(string(encoded), "cborHex") ||
		strings.Contains(string(encoded), testSecretMaterial) ||
		strings.Contains(string(encoded), testRawKeyHex) {
		t.Fatalf("source JSON exposed key material: %s", encoded)
	}
	for _, secretPathDetail := range []string{"verificationKeyPath", "signingKeyPath", rootDir, "utxo.skey"} {
		if strings.Contains(string(encoded), secretPathDetail) {
			t.Fatalf("source JSON exposed path detail %q: %s", secretPathDetail, encoded)
		}
	}
}

func TestStoreGetReturnsValidSource(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeSource(t, rootDir, testDefaultSource)

	source, err := NewStore(rootDir, testDefaultSource).Get(testDefaultSource)
	requireNoError(t, err)

	if got, want := source.Name, testDefaultSource; got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
	if got, want := source.SigningKeyType, signingKeyType; got != want {
		t.Fatalf("SigningKeyType = %q, want %q", got, want)
	}
	if got, want := source.Address, testAddress; got != want {
		t.Fatalf("Address = %q, want %q", got, want)
	}
}

func TestStoreReadFundingSourceReturnsRawKeyHex(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	writeSource(t, rootDir, testDefaultSource)

	source, err := NewStore(rootDir, testDefaultSource).ReadFundingSource(t.Context(), testDefaultSource)
	requireNoError(t, err)

	if got, want := source.Name, testDefaultSource; got != want {
		t.Fatalf("Name = %q, want %q", got, want)
	}
	if got, want := source.Address, testAddress; got != want {
		t.Fatalf("Address = %q, want %q", got, want)
	}
	if got, want := source.VerificationKeyHex, testRawKeyHex; got != want {
		t.Fatalf("VerificationKeyHex = %q, want %q", got, want)
	}
	if got, want := source.SigningKeyHex, testRawKeyHex; got != want {
		t.Fatalf("SigningKeyHex = %q, want %q", got, want)
	}
}

func TestStoreRejectsSymlinkedSource(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	outsideDir := t.TempDir()
	writeSource(t, outsideDir, "external")
	requireNoError(t, os.Symlink(filepath.Join(outsideDir, "external"), filepath.Join(rootDir, testDefaultSource)))

	err := NewStore(rootDir, testDefaultSource).Ready()
	if err == nil {
		t.Fatal("Ready succeeded, want symlinked source rejection")
	}
	if !IsCode(err, CodeSourceNotFound) {
		t.Fatalf("error = %v, want %s", err, CodeSourceNotFound)
	}
}

func TestStoreRejectsSymlinkedKeyFiles(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, testDefaultSource)
	requireNoError(t, os.MkdirAll(sourceDir, 0o700))
	outsideKey := filepath.Join(t.TempDir(), "utxo.vkey")
	writeKey(t, outsideKey, verificationKeyType)
	requireNoError(t, os.Symlink(outsideKey, filepath.Join(sourceDir, "utxo.vkey")))
	writeAddress(t, filepath.Join(sourceDir, "utxo.addr"), testAddress)
	writeKey(t, filepath.Join(sourceDir, "utxo.skey"), signingKeyType)

	_, err := NewStore(rootDir, testDefaultSource).Get(testDefaultSource)
	if err == nil {
		t.Fatal("Get succeeded, want symlinked key rejection")
	}
	if !IsCode(err, CodeSourceInvalidKey) {
		t.Fatalf("error = %v, want %s", err, CodeSourceInvalidKey)
	}
}

func TestStoreRejectsUnexpectedKeyType(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, testDefaultSource)
	requireNoError(t, os.MkdirAll(sourceDir, 0o700))
	writeAddress(t, filepath.Join(sourceDir, "utxo.addr"), testAddress)
	writeKey(t, filepath.Join(sourceDir, "utxo.vkey"), "PaymentVerificationKeyShelley_ed25519")
	writeKey(t, filepath.Join(sourceDir, "utxo.skey"), signingKeyType)

	err := NewStore(rootDir, testDefaultSource).Ready()
	if err == nil {
		t.Fatal("Ready succeeded, want unexpected key type error")
	}
	if !IsCode(err, CodeSourceInvalidKey) {
		t.Fatalf("error = %v, want %s", err, CodeSourceInvalidKey)
	}
}

func TestStoreRequiresUsableAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		address     string
		expectCode  string
		omitAddress bool
	}{
		{
			name:        "missing address",
			expectCode:  CodeSourceIncomplete,
			omitAddress: true,
		},
		{
			name:       "wrong address prefix",
			address:    "addr1qx2fxv2umyhttkxyxp8x0dlpdt3k6cwng5pxj3l62x5n0x",
			expectCode: CodeSourceInvalidKey,
		},
		{
			name:       "bad address checksum",
			address:    "addr_test1vqy2n0vz5rlpykf6dcqn55xdcpey7mejyexlgj6370leayst4k6tx",
			expectCode: CodeSourceInvalidKey,
		},
		{
			name:       "address contains whitespace",
			address:    testAddress + "\nextra",
			expectCode: CodeSourceInvalidKey,
		},
		{
			name:       "address file is oversized",
			address:    "addr_test1" + strings.Repeat("a", maxAddressFileSize),
			expectCode: CodeSourceInvalidKey,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rootDir := t.TempDir()
			sourceDir := filepath.Join(rootDir, testDefaultSource)
			requireNoError(t, os.MkdirAll(sourceDir, 0o700))
			if !tt.omitAddress {
				writeAddress(t, filepath.Join(sourceDir, "utxo.addr"), tt.address)
			}
			writeKey(t, filepath.Join(sourceDir, "utxo.vkey"), verificationKeyType)
			writeKey(t, filepath.Join(sourceDir, "utxo.skey"), signingKeyType)

			err := NewStore(rootDir, testDefaultSource).Ready()
			if err == nil {
				t.Fatal("Ready succeeded, want unusable address error")
			}
			if !IsCode(err, tt.expectCode) {
				t.Fatalf("error = %v, want %s", err, tt.expectCode)
			}
		})
	}
}

func TestStoreRejectsInvalidKeyCBORHex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cborHex string
	}{
		{
			name: "missing cborHex",
		},
		{
			name:    "not hex",
			cborHex: "not-hex",
		},
		{
			name:    "valid cbor wrong length",
			cborHex: "41ff",
		},
		{
			name:    "valid cbor wrong envelope",
			cborHex: "01",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rootDir := t.TempDir()
			sourceDir := filepath.Join(rootDir, testDefaultSource)
			requireNoError(t, os.MkdirAll(sourceDir, 0o700))
			writeAddress(t, filepath.Join(sourceDir, "utxo.addr"), testAddress)
			writeKeyWithCBORHex(t, filepath.Join(sourceDir, "utxo.vkey"), verificationKeyType, tt.cborHex)
			writeKey(t, filepath.Join(sourceDir, "utxo.skey"), signingKeyType)

			err := NewStore(rootDir, testDefaultSource).Ready()
			if err == nil {
				t.Fatal("Ready succeeded, want invalid cborHex error")
			}
			if !IsCode(err, CodeSourceInvalidKey) {
				t.Fatalf("error = %v, want %s", err, CodeSourceInvalidKey)
			}
		})
	}
}

func TestStoreRejectsOversizedKeyFile(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	sourceDir := filepath.Join(rootDir, testDefaultSource)
	requireNoError(t, os.MkdirAll(sourceDir, 0o700))
	writeAddress(t, filepath.Join(sourceDir, "utxo.addr"), testAddress)
	writeSourceFile(t, filepath.Join(sourceDir, "utxo.vkey"), strings.Repeat("x", oversizedFileContents))
	writeKey(t, filepath.Join(sourceDir, "utxo.skey"), signingKeyType)

	err := NewStore(rootDir, testDefaultSource).Ready()
	if err == nil {
		t.Fatal("Ready succeeded, want oversized key error")
	}
	if !IsCode(err, CodeSourceInvalidKey) {
		t.Fatalf("error = %v, want %s", err, CodeSourceInvalidKey)
	}
}

func TestStoreRejectsTooManySourceEntries(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	for i := 1; i <= maxSourceEntries+1; i++ {
		writeSource(t, rootDir, fmt.Sprintf("utxo%d", i))
	}

	_, err := NewStore(rootDir, testDefaultSource).List()
	if err == nil {
		t.Fatal("List succeeded, want source count cap error")
	}
	if !IsCode(err, CodeSourceReadFailed) {
		t.Fatalf("error = %v, want %s", err, CodeSourceReadFailed)
	}
}

func writeSource(t *testing.T, rootDir string, name string) {
	t.Helper()

	sourceDir := filepath.Join(rootDir, name)
	requireNoError(t, os.MkdirAll(sourceDir, 0o700))
	writeAddress(t, filepath.Join(sourceDir, "utxo.addr"), testAddress)
	writeKey(t, filepath.Join(sourceDir, "utxo.vkey"), verificationKeyType)
	writeKey(t, filepath.Join(sourceDir, "utxo.skey"), signingKeyType)
}

func writeAddress(t *testing.T, path string, address string) {
	t.Helper()

	writeSourceFile(t, path, address)
}

func writeKey(t *testing.T, path string, keyType string) {
	t.Helper()

	writeKeyWithCBORHex(t, path, keyType, testKeyCBORHex)
}

func writeKeyWithCBORHex(t *testing.T, path string, keyType string, cborHex string) {
	t.Helper()

	writeSourceFile(t, path, `{
  "type": "`+keyType+`",
  "description": "Genesis Initial UTxO Key",
  "cborHex": "`+cborHex+`",
  "testSecretMaterial": "`+testSecretMaterial+`"
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
