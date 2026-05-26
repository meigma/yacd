package cardanonetwork

import (
	"k8s.io/apimachinery/pkg/api/resource"

	corev1 "k8s.io/api/core/v1"
)

const (
	// defaultNodeStorageSize is the requested PVC size for the primary node
	// state volume when the CardanoNetwork spec does not override it.
	defaultNodeStorageSize = "10Gi"

	// localnetStateDir is the durable state mount root inside the primary
	// workload Pod. cardano-testnet, cardano-node, ogmios, and the faucet share
	// this prefix.
	localnetStateDir = "/state"

	// localnetEnvDir is the cardano-testnet create-env output directory and the
	// effective working directory for the localnet chain bootstrap fragment.
	localnetEnvDir = "/state/env"

	// defaultOgmiosImage is the ogmios sidecar image used when the
	// CardanoNetwork spec does not specify one.
	defaultOgmiosImage = "cardanosolutions/ogmios:v6.14.0"

	// defaultOgmiosPort is the ogmios container port used when the
	// CardanoNetwork spec does not specify one.
	defaultOgmiosPort = 1337

	// ogmiosServiceURLType is the scheme published on the ogmios endpoint
	// status. ogmios speaks WebSocket.
	ogmiosServiceURLType = "ws"

	// defaultKupoImage is the only supported kupo image at this time. Other
	// images are rejected by validateKupoImage.
	defaultKupoImage = "cardanosolutions/kupo:v2.11.0"

	// defaultKupoPort is the kupo container port used when the CardanoNetwork
	// spec does not specify one.
	defaultKupoPort = 1442

	// defaultKupoSince is the chain checkpoint kupo starts indexing from.
	defaultKupoSince = "origin"

	// defaultKupoMatchPattern is the kupo --match value that captures every
	// address/asset pair on the chain.
	defaultKupoMatchPattern = "*/*"

	// defaultKupoDBSizeLimit caps the in-Pod kupo database volume.
	defaultKupoDBSizeLimit = "1Gi"

	// defaultKupoTmpSizeLimit caps kupo's scratch tmp volume.
	defaultKupoTmpSizeLimit = "256Mi"

	// defaultKupoStorageLimit caps kupo's container-level ephemeral storage.
	defaultKupoStorageLimit = "1536Mi"

	// kupoServiceURLType is the scheme published on the kupo endpoint status.
	kupoServiceURLType = "http"

	// defaultFaucetImage is the faucet sidecar image used when neither the
	// CardanoNetwork spec nor the Reconciler-injected default specifies one.
	// The Reconciler-injected DefaultFaucetImage is the legitimate primary
	// injection point for the local dev stack's ko-built image; this constant
	// is the last-resort fallback.
	defaultFaucetImage = "ghcr.io/meigma/yacd/faucet:dev"

	// defaultFaucetPort is the faucet HTTP port used when the CardanoNetwork
	// spec does not specify one.
	defaultFaucetPort = 8080

	// defaultFaucetSource is the default UTXO source name for faucet top-ups.
	defaultFaucetSource = "utxo1"

	// defaultFaucetMinLovelace is the minimum top-up amount in lovelace when
	// the CardanoNetwork spec does not specify one.
	defaultFaucetMinLovelace = 1_000_000

	// defaultFaucetMaxLovelace is the maximum top-up amount in lovelace when
	// the CardanoNetwork spec does not specify one.
	defaultFaucetMaxLovelace = 10_000_000_000

	// faucetServiceURLType is the scheme published on the faucet endpoint
	// status.
	faucetServiceURLType = "http"
)

// supportedOgmiosNodeVersions maps the recognized ogmios major.minor tag to
// the cardano-node versions it has been validated against. Pairs outside this
// table are rejected by validateOgmiosCompatibility.
var supportedOgmiosNodeVersions = map[string][]string{
	"v6.14": {"10.5.1", "10.5.3", "11.0.1"},
	"v6.13": {"10.1.2", "10.1.3", "10.1.4"},
	"v6.12": {"10.1.2", "10.1.3", "10.1.4"},
	"v6.11": {"10.1.2", "10.1.3", "10.1.4"},
	"v6.10": {"10.1.2", "10.1.3", "10.1.4"},
	"v6.9":  {"10.1.2", "10.1.3"},
	"v6.8":  {"9.1.1", "9.2.0"},
	"v6.7":  {"9.1.1", "9.2.0"},
}

// defaultKupoResources is the kupo container resource floor. The Limits cap
// container-level ephemeral storage so a runaway kupo state cannot fill the
// node disk; Requests track the tmp volume so the Pod schedules on a node with
// enough ephemeral storage for both.
func defaultKupoResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceEphemeralStorage: resource.MustParse(defaultKupoTmpSizeLimit),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceEphemeralStorage: resource.MustParse(defaultKupoStorageLimit),
		},
	}
}

// resourceQuantity parses value as a Kubernetes resource quantity and returns
// a heap pointer. Use this only at builder construction time; the input is
// trusted to be a compile-time string literal.
func resourceQuantity(value string) *resource.Quantity {
	quantity := resource.MustParse(value)
	return &quantity
}
