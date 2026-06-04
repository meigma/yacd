// Package tx is the stateless engine for building, validating, signing, and
// submitting a single funding transaction on a Cardano testnet.
//
// Given primitive inputs — a source payment key pair (raw hex), a source
// address, a destination address, an exact lovelace amount, and Ogmios/Kupo
// endpoints — Submit produces one transaction that spends only source UTxOs,
// pays the exact amount to the destination, returns change to the source, and
// is submitted through Ogmios. The engine validates the completed transaction
// before signing so a misbehaving builder can never overpay, add assets, or
// spend foreign inputs.
//
// The package is domain-pure: it has no Kubernetes, HTTP-server, or
// filesystem dependencies and no knowledge of how callers store keys. The
// Submitter port isolates the Apollo/Ogmios/Kupo adapter so callers can mock
// chain submission, and the request/result types carry only chain primitives.
// Callers that hold keys behind a store or Secret adapt their own material
// into a Request.
package tx
