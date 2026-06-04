package genesisfund

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// existingFundKey and existingFundLovelace describe the single allocation the
// realistic fixture genesis already carries, so idempotency and preservation
// cases have a concrete prior entry to assert against.
const (
	existingFundKey      = "60dd1f87ea2b6f5857c928fd7ce53ed5176c3c100b2779ca04d4e478f5"
	existingFundLovelace = 15000003000000
	fixtureMaxSupply     = 100000020000000
)

// fixtureGenesis returns a representative shelley-genesis.json: a populated
// initialFunds map, a maxLovelaceSupply ceiling, and unrelated top-level fields
// (including a nested object and an array) that must survive a rewrite.
func fixtureGenesis(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"systemStart":         "2024-01-01T00:00:00Z",
		"networkMagic":        42,
		"networkId":           "Testnet",
		"maxLovelaceSupply":   fixtureMaxSupply,
		"epochLength":         500,
		"securityParam":       2160,
		"slotsPerKESPeriod":   129600,
		"updateQuorum":        1,
		"protocolParams":      map[string]any{"decentralisationParam": 0, "minFeeA": 44, "minFeeB": 155381},
		"genDelegs":           map[string]any{},
		"staking":             map[string]any{"pools": map[string]any{}, "stake": map[string]any{}},
		"initialFunds":        map[string]any{existingFundKey: existingFundLovelace},
		"unrelatedArrayField": []any{"a", "b", "c"},
	})
	require.NoError(t, err)
	return raw
}

// writeGenesis writes content as shelley-genesis.json into a fresh env
// directory and returns the env directory path.
func writeGenesis(t *testing.T, content []byte) string {
	t.Helper()
	envDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(envDir, genesisFileName), content, 0o644))
	return envDir
}

// readInitialFunds parses the initialFunds map of the on-disk genesis under
// envDir as decimal strings, so amount assertions are exact regardless of JSON
// number formatting.
func readInitialFunds(t *testing.T, envDir string) map[string]json.Number {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(envDir, genesisFileName))
	require.NoError(t, err)

	var doc struct {
		InitialFunds map[string]json.Number `json:"initialFunds"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&doc))
	return doc.InitialFunds
}

// TestAddressKeyGoldenVectors locks the bech32-to-hex decode that produces the
// initialFunds map key. These are real enterprise testnet addresses
// (addr_test1v..., a 0x60 header byte followed by a 28-byte payment-key hash);
// each wantKey is that exact 29-byte on-chain payload hex-encoded, which is what
// the ledger stores as the genesis allocation key. The vectors were derived by
// decoding each address with the same Apollo library addressKey uses, so the
// test pins the decode against drift rather than re-deriving the expectation.
func TestAddressKeyGoldenVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		address string
		wantKey string
	}{
		{
			name:    "faucet vector one",
			address: "addr_test1vrw3lpl29dh4s47f9r7heef765tkc0qspvnhnjsy6nj83ag8ge63v",
			wantKey: "60dd1f87ea2b6f5857c928fd7ce53ed5176c3c100b2779ca04d4e478f5",
		},
		{
			name:    "faucet vector two",
			address: "addr_test1vrhgd9mzh6t4jdq2sxp3mnhlcuju2wz5pn8kf2268hzj48ggh3qnm",
			wantKey: "60ee869762be9759340a81831dceffc725c538540ccf64a95a3dc52a9d",
		},
		{
			name:    "faucet vector three",
			address: "addr_test1vqxk54m7j3q6mrkevcunryrwf4p7e68c93cjk8gzxkhlkpsffv7s0",
			wantKey: "600d6a577e9441ad8ed9663931906e4d43ece8f82c712b1d0235affb06",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key, err := addressKey(tt.address)
			require.NoError(t, err, "golden address must decode")
			assert.Equal(t, tt.wantKey, key, "decoded key must match the golden raw bytes")
		})
	}
}

// TestRun exercises the funding behavior end to end against the realistic
// fixture: adding a new allocation, idempotency, supply headroom, and the
// input/genesis error paths.
func TestRun(t *testing.T) {
	t.Parallel()

	// newAddress and newKey are a second golden vector not present in the
	// fixture, used to assert a real insertion.
	const (
		newAddress = "addr_test1vrhgd9mzh6t4jdq2sxp3mnhlcuju2wz5pn8kf2268hzj48ggh3qnm"
		newKey     = "60ee869762be9759340a81831dceffc725c538540ccf64a95a3dc52a9d"
	)

	tests := []struct {
		name        string
		genesis     func(t *testing.T) []byte
		address     string
		lovelace    int64
		wantErr     string
		wantOut     string
		assertState func(t *testing.T, envDir string, before, after map[string]json.Number)
	}{
		{
			name:     "adds a new allocation",
			genesis:  fixtureGenesis,
			address:  newAddress,
			lovelace: 2_500_000,
			wantOut:  "funded",
			assertState: func(t *testing.T, _ string, before, after map[string]json.Number) {
				assert.Len(t, before, 1, "fixture starts with one allocation")
				require.Contains(t, after, newKey, "new key must be present after funding")
				assert.Equal(t, "2500000", after[newKey].String(), "new amount must match the request")
				assert.Contains(t, after, existingFundKey, "existing allocation must be retained")
				assert.Equal(t, "15000003000000", after[existingFundKey].String(), "existing amount must be untouched")
			},
		},
		{
			name:     "idempotent when key already present",
			genesis:  fixtureGenesis,
			address:  "addr_test1vrw3lpl29dh4s47f9r7heef765tkc0qspvnhnjsy6nj83ag8ge63v",
			lovelace: 9_000_000,
			wantOut:  "already funded",
			assertState: func(t *testing.T, _ string, before, after map[string]json.Number) {
				assert.Equal(t, before, after, "a present key must leave initialFunds unchanged")
			},
		},
		{
			name:     "fails when supply headroom exceeded",
			genesis:  fixtureGenesis,
			address:  newAddress,
			lovelace: fixtureMaxSupply,
			wantErr:  "exceeding maxLovelaceSupply",
		},
		{
			name:     "rejects an invalid address",
			genesis:  fixtureGenesis,
			address:  "not-a-bech32-address",
			lovelace: 1_000_000,
			wantErr:  "decode address",
		},
		{
			name:     "rejects an empty address",
			genesis:  fixtureGenesis,
			address:  "",
			lovelace: 1_000_000,
			wantErr:  "address is required",
		},
		{
			name:     "rejects a zero lovelace amount",
			genesis:  fixtureGenesis,
			address:  newAddress,
			lovelace: 0,
			wantErr:  "lovelace must be positive",
		},
		{
			name:     "rejects a negative lovelace amount",
			genesis:  fixtureGenesis,
			address:  newAddress,
			lovelace: -1,
			wantErr:  "lovelace must be positive",
		},
		{
			name:     "rejects a genesis missing maxLovelaceSupply",
			genesis:  func(t *testing.T) []byte { return []byte(`{"initialFunds":{}}`) },
			address:  newAddress,
			lovelace: 1_000_000,
			wantErr:  "missing maxLovelaceSupply",
		},
		{
			name:     "rejects malformed genesis json",
			genesis:  func(t *testing.T) []byte { return []byte("{not json") },
			address:  newAddress,
			lovelace: 1_000_000,
			wantErr:  "parse shelley genesis",
		},
		{
			name: "rejects a genesis whose initialFunds is not an object",
			genesis: func(t *testing.T) []byte {
				return []byte(`{"maxLovelaceSupply":100000020000000,"initialFunds":"oops"}`)
			},
			address:  newAddress,
			lovelace: 1_000_000,
			wantErr:  "parse initialFunds",
		},
		{
			name: "rejects a genesis whose initialFunds amount is fractional",
			genesis: func(t *testing.T) []byte {
				return []byte(`{"maxLovelaceSupply":100000020000000,"initialFunds":{"` + existingFundKey + `":1.5}}`)
			},
			address:  newAddress,
			lovelace: 1_000_000,
			wantErr:  "parse initialFunds[",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			envDir := writeGenesis(t, tt.genesis(t))

			var out bytes.Buffer
			run := func() error {
				return Run(context.Background(), Options{
					EnvDir:   envDir,
					Address:  tt.address,
					Lovelace: tt.lovelace,
				}, &out)
			}

			if tt.wantErr != "" {
				err := run()
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			before := readInitialFunds(t, envDir)
			require.NoError(t, run())
			assert.Contains(t, out.String(), tt.wantOut, "success line must describe the outcome")
			if tt.assertState != nil {
				tt.assertState(t, envDir, before, readInitialFunds(t, envDir))
			}
		})
	}
}

// TestRunPreservesOtherTopLevelFields proves a successful rewrite keeps every
// non-funding top-level field byte-for-byte, including nested objects and
// arrays, and still produces valid JSON.
func TestRunPreservesOtherTopLevelFields(t *testing.T) {
	t.Parallel()

	envDir := writeGenesis(t, fixtureGenesis(t))

	original := map[string]json.RawMessage{}
	originalRaw, err := os.ReadFile(filepath.Join(envDir, genesisFileName))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(originalRaw, &original))

	err = Run(context.Background(), Options{
		EnvDir:   envDir,
		Address:  "addr_test1vqxk54m7j3q6mrkevcunryrwf4p7e68c93cjk8gzxkhlkpsffv7s0",
		Lovelace: 1_000_000,
	}, &bytes.Buffer{})
	require.NoError(t, err)

	rewritten := map[string]json.RawMessage{}
	rewrittenRaw, err := os.ReadFile(filepath.Join(envDir, genesisFileName))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rewrittenRaw, &rewritten), "rewritten genesis must be valid JSON")

	for field, value := range original {
		if field == "initialFunds" {
			continue
		}
		assert.JSONEq(t, string(value), string(rewritten[field]), "field %s must be preserved", field)
	}
	assert.Len(t, rewritten, len(original), "no top-level field may be added or dropped")
}

// TestRunRejectsMissingGenesisFile covers the env directory with no
// shelley-genesis.json at all.
func TestRunRejectsMissingGenesisFile(t *testing.T) {
	t.Parallel()

	err := Run(context.Background(), Options{
		EnvDir:   t.TempDir(),
		Address:  "addr_test1vqxk54m7j3q6mrkevcunryrwf4p7e68c93cjk8gzxkhlkpsffv7s0",
		Lovelace: 1_000_000,
	}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read shelley genesis")
}

// TestRunPreservesLargeLovelacePrecision proves the big.Int funding path keeps
// values that exceed JSON double precision exact through the read-rewrite-read
// round trip. 2^53 (9007199254740992) is the largest integer every float64 can
// represent, so a value above it would be silently corrupted by a float-backed
// decoder; funding it and reading it back unchanged demonstrates that does not
// happen here.
func TestRunPreservesLargeLovelacePrecision(t *testing.T) {
	t.Parallel()

	// newKey is the decoded key for the funded address below; preDangerKey holds
	// an existing allocation just under 2^53 so both the retained and the newly
	// written amount straddle the float64 integer boundary.
	const (
		fundAddress  = "addr_test1vrhgd9mzh6t4jdq2sxp3mnhlcuju2wz5pn8kf2268hzj48ggh3qnm"
		newKey       = "60ee869762be9759340a81831dceffc725c538540ccf64a95a3dc52a9d"
		preDangerKey = "60dd1f87ea2b6f5857c928fd7ce53ed5176c3c100b2779ca04d4e478f5"
		// twoPow53 is 2^53, the float64 exact-integer ceiling.
		twoPow53 = 9007199254740992
		// aboveTwoPow53 is 2^53 + 1, the smallest positive integer float64 cannot
		// represent, so it must survive only because funding uses big.Int.
		aboveTwoPow53 = 9007199254740993
		// preDanger is 2^53 - 1, the largest integer float64 still represents
		// exactly, used as a retained allocation to prove existing amounts are
		// re-encoded without drift.
		preDanger = 9007199254740991
		// largeMaxSupply is Cardano's mainnet maxLovelaceSupply, far above the
		// summed allocations so headroom never masks the precision assertion.
		largeMaxSupply = "45000000000000000"
	)

	genesisJSON := []byte(`{` +
		`"maxLovelaceSupply":` + largeMaxSupply + `,` +
		`"initialFunds":{"` + preDangerKey + `":` + itoa(preDanger) + `}}`)
	envDir := writeGenesis(t, genesisJSON)

	require.NoError(t, Run(context.Background(), Options{
		EnvDir:   envDir,
		Address:  fundAddress,
		Lovelace: aboveTwoPow53,
	}, &bytes.Buffer{}))

	funds := readInitialFunds(t, envDir)
	assert.Equal(t, itoa(aboveTwoPow53), funds[newKey].String(),
		"funded amount above 2^53 must be stored exactly")
	assert.Equal(t, itoa(preDanger), funds[preDangerKey].String(),
		"retained amount at 2^53-1 must be re-encoded without drift")
	// Sanity: the funded value is genuinely outside the float64 exact-integer
	// range, so the assertions above could not have passed through a float path.
	assert.Greater(t, int64(aboveTwoPow53), int64(twoPow53))
}

// itoa renders n as a base-10 string for inline JSON construction.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

// TestDecodeInteger locks the lovelace decoder's accept/reject contract: it must
// take exact integers of any magnitude and reject anything fractional or
// non-numeric so amounts never silently lose precision.
func TestDecodeInteger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{
			name: "small integer",
			raw:  "42",
			want: "42",
		},
		{
			name: "integer above float64 precision",
			raw:  "9007199254740993",
			want: "9007199254740993",
		},
		{
			// json.Number accepts a quoted decimal integer, so the decoder treats
			// it as the underlying number rather than rejecting it.
			name: "quoted integer string",
			raw:  `"123"`,
			want: "123",
		},
		{
			name:    "json object is not a number",
			raw:     "{}",
			wantErr: "not a JSON number",
		},
		{
			name:    "json array is not a number",
			raw:     "[]",
			wantErr: "not a JSON number",
		},
		{
			name:    "json bool is not a number",
			raw:     "true",
			wantErr: "not a JSON number",
		},
		{
			// json null unmarshals into an empty json.Number, which is not a valid
			// base-10 integer.
			name:    "json null is not an integer",
			raw:     "null",
			wantErr: "not an integer",
		},
		{
			name:    "fractional value is not an integer",
			raw:     "1.5",
			wantErr: "not an integer",
		},
		{
			name:    "exponent value is not an integer",
			raw:     "1e3",
			wantErr: "not an integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeInteger(json.RawMessage(tt.raw))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}

// TestWriteAtomicCreateTempError proves the atomic writer reports a wrapped
// error when it cannot create its sibling temp file, the first failure point in
// the temp-then-rename sequence. A read-only parent directory makes
// os.CreateTemp fail without leaving any partial output behind.
func TestWriteAtomicCreateTempError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "genesis.json")

	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() {
		// Restore write permission so t.TempDir cleanup can remove the directory.
		_ = os.Chmod(dir, 0o700)
	})

	err := writeAtomic(target, []byte("payload"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create temp genesis")
	assert.NoFileExists(t, target, "a failed create must not leave the target behind")
}

// TestWriteAtomicRenameError proves the writer wraps a rename failure and
// removes its temp file so a failed swap never leaves an orphaned sibling. The
// rename fails because the target path is a directory rather than a file.
func TestWriteAtomicRenameError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// target is a directory, so renaming a regular temp file onto it fails.
	target := filepath.Join(dir, "genesis.json")
	require.NoError(t, os.Mkdir(target, 0o755))

	err := writeAtomic(target, []byte("payload"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace genesis")

	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	for _, entry := range entries {
		assert.Equal(t, target, filepath.Join(dir, entry.Name()),
			"only the target directory may remain; the temp file must be cleaned up")
	}
}

// TestWriteAtomicSucceeds proves the happy path writes the payload to the target
// with the genesis file mode, complementing the error-path coverage above.
func TestWriteAtomicSucceeds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "genesis.json")

	require.NoError(t, writeAtomic(target, []byte("payload")))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(got))

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(genesisFilePerm), info.Mode().Perm(),
		"the written genesis must carry the create-env file mode")
}
