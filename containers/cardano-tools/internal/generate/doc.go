// Package generate produces a localnet artifact environment by shimming
// cardano-testnet create-env.
//
// Run builds the deterministic plan from internal/cardano/localnet, invokes
// cardano-testnet create-env to populate the environment directory, writes the
// plan manifest, and enriches the generated configuration.yaml with the genesis
// hashes cardano-node requires. Genesis hashing is delegated to cardano-cli
// through the GenesisHasher seam so the protocol-specific semantics stay in the
// upstream tool the image already ships.
//
// The package exports Options, Run, GenesisKind, GenesisHasher, CardanoCLIHasher,
// CardanoCLIHasherFromEnv, and EnrichGenesisHashes; everything else is
// unexported.
package generate
