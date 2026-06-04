// Package genesisfund adds an initialFunds allocation for a Cardano address to
// a local Shelley genesis.
//
// A cardano-testnet create-env writes shelley-genesis.json with a populated
// initialFunds map of hex-encoded address bytes to lovelace, alongside a
// maxLovelaceSupply ceiling. This package decodes a bech32 address to its raw
// on-chain bytes (the exact hex map key the ledger expects) and inserts the
// allocation, replacing a fragile shell pipeline the operator previously used to
// fund a well-known faucet wallet at genesis.
//
// Run is idempotent (a present key is left untouched), refuses to exceed
// maxLovelaceSupply, and rewrites the genesis atomically while preserving every
// other top-level field. The downstream stage verb re-derives genesis hashes
// from the edited file, so byte-for-byte formatting need not be preserved.
package genesisfund
