package cli

import _ "embed"

// defaultDevnetEnvYAML is the developer environment `yacd devnet` applies by
// default: a local network with Ogmios and Kupo. The controller automatically
// generates a genesis-funded faucet wallet for local networks. It started as a
// copy of examples/local/yacd.yaml but intentionally diverges: devnet owns the
// k3d cluster and maps host ports to it, so it exposes Ogmios/Kupo as NodePort
// Services with localhost externalURLs (the pinned nodePorts match
// cluster.DefaultPortMappings). examples/local stays ClusterIP so `yacd up -f`
// deploys correctly on any cluster. devnet_test.go validates structure (not a
// byte copy) and cross-checks the pinned ports against the cluster constants.
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
