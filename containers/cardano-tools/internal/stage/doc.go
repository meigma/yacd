// Package stage flattens a cardano-testnet create-env state directory into a
// complete flat served directory.
//
// A create-env state directory is nested (for example the primary topology is
// at node-data/node1/topology.json) and carries no connection.json. The stage
// verb reuses the report verb's flatten and connection assembly
// (internal/artifactset) to collect the contract-key artifact files and
// synthesize connection.json, writes them flat under the output directory, then
// writes an integrity manifest.json over every file. The result is the local
// counterpart of the fetch verb's public served directory.
package stage
