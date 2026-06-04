package cli

import _ "embed"

// defaultDevnetEnvYAML is the developer environment `yacd devnet` applies by
// default: a local network with Ogmios, Kupo, and a faucet, which makes the
// controller generate a genesis-funded wallet. It is a byte copy of
// examples/local/yacd.yaml (go:embed cannot reach outside the package
// directory); devnet_test.go guards the copy against drift.
//
//go:embed devnet.yaml
var defaultDevnetEnvYAML []byte

// defaultInitEnvYAML is the fully-commented developer environment template
// `yacd init` prints to stdout. Its active (uncommented) portion is a valid
// batteries-included local network; commented blocks document the rest of the
// API. init_test.go guards the active config against drift from the real schema.
//
//go:embed init.yaml
var defaultInitEnvYAML []byte
