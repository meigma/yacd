// Package fetch downloads a public Cardano network's artifacts from trusted
// sources into an output directory.
//
// Run resolves a profile's download manifest from the reviewed pin table in
// pins.go, fetches each file through an injected httpDoer, verifies the pinned
// digests, and writes the files into the output directory. config.json is the
// single trust anchor — pinning it transitively covers the genesis and
// checkpoints files via cardano-node's startup verification — and the Mithril
// verification keys carry their own pins. Genesis, topology, and peer-snapshot
// files are fetched over TLS without their own pin: genesis is verified
// downstream by cardano-node, and topology/peer-snapshot are operational files.
//
// The package exports Options and Run; the source table and the HTTP seam are
// unexported.
package fetch
