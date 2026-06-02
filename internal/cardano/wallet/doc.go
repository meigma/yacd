// Package wallet is the pure-domain generator for YACD developer payment
// wallets on local Cardano development networks.
//
// It produces an ed25519 payment key pair as cardano-cli-compatible text
// envelopes (PaymentSigningKeyShelley_ed25519 / PaymentVerificationKeyShelley_
// ed25519) plus the derived enterprise testnet address (addr_test...), so a
// developer can export the signing key and use it directly with cardano-cli.
//
// Address derivation is the single source of truth shared with the faucet's
// source-key handling: both go through DeriveTestnetAddress so the operator and
// the faucet can never disagree about an address. The package has no Kubernetes
// dependencies and no network or filesystem I/O; New reads crypto/rand for key
// material and FromSeed is fully deterministic for golden tests.
package wallet
