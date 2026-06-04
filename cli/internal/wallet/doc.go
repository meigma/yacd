// Package wallet is the managed-wallet store and selector for the YACD CLI.
//
// Store is a repository over the CLI's Kubernetes Secret port that owns the
// wallet Secret naming, labelling, and ownership conventions, mirroring the
// controller's primary wallet Secret shape (payment.skey / payment.vkey /
// address data keys and the yacd.meigma.io/wallet-name and -source labels). It
// lists managed wallets (excluding the reserved genesis-funded faucet wallet),
// resolves a wallet selector — a managed name, a 32-byte ed25519 public key as
// hex, or a raw bech32 testnet address — to a fundable address, gates funding on
// the faucet wallet, and creates and deletes owned wallet Secrets. The faucet
// name is reserved: it cannot be created or removed through the CLI.
//
// The name generator draws an adjective-noun name from embedded DNS-1123-safe
// wordlists and re-rolls on collision up to the per-network ceiling, so managed
// wallets get stable, human-friendly, cluster-valid names without the caller
// inventing one.
package wallet
