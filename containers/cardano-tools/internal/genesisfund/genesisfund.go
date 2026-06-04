package genesisfund

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"

	apolloAddress "github.com/Salvionied/apollo/serialization/Address"
)

// genesisFileName is the Shelley genesis file Run reads and rewrites under the
// environment directory.
const genesisFileName = "shelley-genesis.json"

// genesisFilePerm is the mode Run writes the rewritten genesis with, matching
// the create-env output.
const genesisFilePerm = 0o644

// Options bundles the inputs [Run] consumes.
type Options struct {
	// EnvDir is the directory holding shelley-genesis.json, typically the
	// cardano-testnet create-env environment directory.
	EnvDir string
	// Address is the bech32 Cardano testnet address (addr_test1...) to fund. Its
	// raw decoded bytes form the initialFunds map key.
	Address string
	// Lovelace is the positive allocation added to initialFunds for Address.
	Lovelace int64
}

// Run adds an initialFunds allocation for Options.Address to the Shelley genesis
// under Options.EnvDir.
//
// Run decodes the bech32 address to its raw on-chain bytes and uses their hex
// encoding as the initialFunds map key. It is idempotent: when the key is
// already present the genesis is left untouched and a skip line is written. It
// fails when the new total initialFunds would exceed maxLovelaceSupply. On a
// real change it preserves every other top-level field and rewrites the genesis
// atomically (temp file plus rename).
//
// ctx is accepted for symmetry with the other verbs; funding is local
// filesystem work and does not block on it.
func Run(_ context.Context, opts Options, out io.Writer) error {
	if opts.Lovelace <= 0 {
		return fmt.Errorf("lovelace must be positive, got %d", opts.Lovelace)
	}

	key, err := addressKey(opts.Address)
	if err != nil {
		return err
	}

	envDir, err := filepath.Abs(opts.EnvDir)
	if err != nil {
		return fmt.Errorf("resolve env directory: %w", err)
	}
	genesisPath := filepath.Join(envDir, genesisFileName)

	raw, err := os.ReadFile(genesisPath)
	if err != nil {
		return fmt.Errorf("read shelley genesis: %w", err)
	}

	doc, err := parseGenesis(raw)
	if err != nil {
		return err
	}

	if _, present := doc.initialFunds[key]; present {
		_, err = fmt.Fprintf(out, "address %s (key %s) already funded in %s; leaving genesis unchanged\n",
			opts.Address, key, genesisPath)
		return err
	}

	if err := doc.checkHeadroom(opts.Lovelace); err != nil {
		return err
	}

	rewritten, err := doc.withFund(key, opts.Lovelace)
	if err != nil {
		return err
	}

	if err := writeAtomic(genesisPath, rewritten); err != nil {
		return err
	}

	_, err = fmt.Fprintf(out, "funded %s (key %s) with %d lovelace in %s\n",
		opts.Address, key, opts.Lovelace, genesisPath)
	return err
}

// addressKey decodes a bech32 Cardano address to the hex-encoded raw bytes used
// as the initialFunds map key. For an enterprise testnet address those bytes are
// the 0x60 header followed by the 28-byte payment-key hash, which is exactly
// what the ledger stores as the genesis key.
func addressKey(address string) (string, error) {
	if address == "" {
		return "", fmt.Errorf("address is required")
	}
	decoded, err := apolloAddress.DecodeAddress(address)
	if err != nil {
		return "", fmt.Errorf("decode address %q: %w", address, err)
	}
	return hex.EncodeToString(decoded.Bytes()), nil
}

// genesis is the subset of shelley-genesis.json this package reads and mutates,
// with every other top-level field retained verbatim for round-tripping.
type genesis struct {
	// fields holds every top-level genesis member as raw JSON so non-funding
	// fields survive a rewrite untouched.
	fields map[string]json.RawMessage
	// initialFunds is the decoded genesis allocation map of hex address key to
	// lovelace.
	initialFunds map[string]*big.Int
	// maxLovelaceSupply is the genesis supply ceiling the funded total must not
	// exceed.
	maxLovelaceSupply *big.Int
}

// parseGenesis decodes the genesis bytes into a round-trippable genesis,
// reading initialFunds and maxLovelaceSupply while keeping all other top-level
// fields as raw JSON.
//
// Lovelace amounts are decoded with json.Number into big.Int so values near
// maxLovelaceSupply are summed exactly rather than through float64.
func parseGenesis(raw []byte) (*genesis, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("parse shelley genesis: %w", err)
	}

	maxField, ok := fields["maxLovelaceSupply"]
	if !ok {
		return nil, fmt.Errorf("shelley genesis is missing maxLovelaceSupply")
	}
	maxSupply, err := decodeInteger(maxField)
	if err != nil {
		return nil, fmt.Errorf("parse maxLovelaceSupply: %w", err)
	}

	initialFunds := map[string]*big.Int{}
	if fundsField, ok := fields["initialFunds"]; ok {
		rawFunds := map[string]json.RawMessage{}
		if err := json.Unmarshal(fundsField, &rawFunds); err != nil {
			return nil, fmt.Errorf("parse initialFunds: %w", err)
		}
		for key, amount := range rawFunds {
			value, err := decodeInteger(amount)
			if err != nil {
				return nil, fmt.Errorf("parse initialFunds[%s]: %w", key, err)
			}
			initialFunds[key] = value
		}
	}

	return &genesis{
		fields:            fields,
		initialFunds:      initialFunds,
		maxLovelaceSupply: maxSupply,
	}, nil
}

// checkHeadroom returns an error when adding lovelace to the existing
// initialFunds total would exceed maxLovelaceSupply.
func (g *genesis) checkHeadroom(lovelace int64) error {
	total := big.NewInt(0)
	for _, amount := range g.initialFunds {
		total.Add(total, amount)
	}
	total.Add(total, big.NewInt(lovelace))

	if total.Cmp(g.maxLovelaceSupply) > 0 {
		return fmt.Errorf("funding %d lovelace would raise initialFunds to %s, exceeding maxLovelaceSupply %s",
			lovelace, total, g.maxLovelaceSupply)
	}
	return nil
}

// withFund returns the genesis bytes with key set to lovelace in initialFunds
// and every other top-level field preserved. The funded map is re-encoded
// rather than text-patched so the output is always valid JSON.
func (g *genesis) withFund(key string, lovelace int64) ([]byte, error) {
	funds := make(map[string]json.RawMessage, len(g.initialFunds)+1)
	for existing, amount := range g.initialFunds {
		funds[existing] = json.RawMessage(amount.String())
	}
	funds[key] = json.RawMessage(big.NewInt(lovelace).String())

	encodedFunds, err := json.Marshal(funds)
	if err != nil {
		return nil, fmt.Errorf("encode initialFunds: %w", err)
	}
	g.fields["initialFunds"] = encodedFunds

	out, err := json.MarshalIndent(g.fields, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode shelley genesis: %w", err)
	}
	return append(out, '\n'), nil
}

// decodeInteger decodes a JSON number as an exact big.Int, rejecting fractional
// or non-numeric values so lovelace amounts never lose precision.
func decodeInteger(raw json.RawMessage) (*big.Int, error) {
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return nil, fmt.Errorf("not a JSON number: %w", err)
	}
	value, ok := new(big.Int).SetString(number.String(), 10)
	if !ok {
		return nil, fmt.Errorf("not an integer: %s", number.String())
	}
	return value, nil
}

// writeAtomic writes data to path through a sibling temp file and a rename so a
// failed write never leaves a partially rewritten genesis.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp genesis: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp genesis: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp genesis: %w", err)
	}
	if err := os.Chmod(tmpName, genesisFilePerm); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod temp genesis: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace genesis: %w", err)
	}
	return nil
}
