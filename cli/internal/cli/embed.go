package cli

import _ "embed"

// defaultDevnetEnvYAML is the developer environment `yacd devnet` applies by
// default: a local network with Ogmios, Kupo, a faucet, and a pre-funded
// developer wallet. It is a byte copy of examples/local/yacd.yaml (go:embed
// cannot reach outside the package directory); devnet_test.go guards the copy
// against drift.
//
//go:embed devnet.yaml
var defaultDevnetEnvYAML []byte
