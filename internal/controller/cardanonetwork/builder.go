package cardanonetwork

import (
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// primaryWorkloadResources is the desired-state bundle the builder produces
// for one CardanoNetwork. Every field is non-nil except OgmiosService,
// KupoService, FaucetService, and FaucetAuthSecret, which are nil when the
// corresponding chain API sidecar is disabled.
type primaryWorkloadResources struct {
	NetworkArtifactsConfigMap       *corev1.ConfigMap
	ArtifactPublisherServiceAccount *corev1.ServiceAccount
	ArtifactPublisherRole           *rbacv1.Role
	ArtifactPublisherRoleBinding    *rbacv1.RoleBinding
	PersistentVolumeClaim           *corev1.PersistentVolumeClaim
	Deployment                      *appsv1.Deployment
	Service                         *corev1.Service
	OgmiosService                   *corev1.Service
	KupoService                     *corev1.Service
	FaucetService                   *corev1.Service
	FaucetAuthSecret                *corev1.Secret
}

// primaryWorkloadBuilder converts a CardanoNetwork spec into the desired
// primary node workload resources. The builder is pure: it produces
// in-memory Kubernetes objects and never touches the API server, the file
// system, time, or randomness. All reconciliation side effects (apply,
// delete, status patch) live on CardanoNetworkReconciler.
type primaryWorkloadBuilder struct {
	// scheme is required to set controller references on owned children.
	scheme *runtime.Scheme

	// defaultFaucetImage is the Reconciler-injected faucet image used when
	// the CardanoNetwork spec does not override it. The local dev stack's
	// ko-built image flows in through here; see defaults.go for the final
	// fallback constant.
	defaultFaucetImage string

	// defaultCardanoTestnetImage is the Reconciler-injected override for
	// the cardano-testnet container image. When non-empty it replaces the
	// computed "<repo>:<toolVersion>-<revision>" reference used by the
	// create-env init container, the faucet source-address init
	// container, and the default cardano-node container. The local dev
	// stack's docker-built image flows in through here so manual testing
	// picks up post-release publisher changes that the published
	// cardano-testnet tag does not yet contain.
	defaultCardanoTestnetImage string
}

// Build composes the desired primary workload resources for the given
// CardanoNetwork.
//
// The order of operations is:
//  1. validate the spec into a localnet.Spec the planner can accept
//  2. compute the localnet plan (fingerprint, paths, invocation args)
//  3. build the artifact publisher bundle (ConfigMap, RBAC)
//  4. build the cardano-testnet init container fragment
//  5. resolve effective sidecar settings (ogmios/kupo/faucet) and run the
//     cross-component validations
//  6. assemble the Deployment, PVC, and Services
//
// Build returns an unsupportedSpecError when the spec is not satisfiable;
// the reconciler surfaces that as a Degraded condition rather than retrying.
func (b primaryWorkloadBuilder) Build(network *yacdv1alpha1.CardanoNetwork) (*primaryWorkloadResources, error) {
	if network == nil {
		return nil, fmt.Errorf("cardanonetwork is required")
	}
	if b.scheme == nil {
		return nil, fmt.Errorf("scheme is required")
	}

	spec, err := b.localnetSpec(network)
	if err != nil {
		return nil, err
	}

	plan, err := localnet.BuildPlan(spec)
	if err != nil {
		return nil, unsupportedSpec("build localnet plan: %v", err)
	}

	networkArtifactsConfigMap, err := b.networkArtifactsConfigMap(network, plan.Fingerprint.Value)
	if err != nil {
		return nil, err
	}
	artifactPublisherServiceAccount, err := b.artifactPublisherServiceAccount(network)
	if err != nil {
		return nil, err
	}
	artifactPublisherRole, err := b.artifactPublisherRole(network)
	if err != nil {
		return nil, err
	}
	artifactPublisherRoleBinding, err := b.artifactPublisherRoleBinding(network)
	if err != nil {
		return nil, err
	}

	initContainer, err := b.cardanoTestnetInitContainer(network, plan)
	if err != nil {
		return nil, err
	}

	ogmios, err := resolveOgmiosSettings(network)
	if err != nil {
		return nil, err
	}
	kupo, kupoMentioned, err := resolveKupoSettings(network)
	if err != nil {
		return nil, err
	}
	// Apply cross-component defaults (kupo follows ogmios when unmentioned)
	// before the hard invariant check: kupo cannot be explicitly enabled
	// without ogmios.
	kupo = applyDependentDefaults(ogmios, kupo, kupoMentioned)
	if kupo.enabled && !ogmios.enabled {
		return nil, unsupportedSpec("kupo requires ogmios to be enabled")
	}
	faucet, err := b.resolveFaucetSettings(network, ogmios, kupo)
	if err != nil {
		return nil, err
	}
	// Skip the ogmios/cardano-node compatibility check when the CR is going
	// to be rejected as UnsupportedLocalnetChange anyway; surface that
	// specific error instead.
	if !acceptedLocalnetFingerprintChanged(network, plan.Fingerprint.Value) {
		err = validateOgmiosCompatibility(network.Spec.Node.Version, ogmios)
	}
	if err != nil {
		return nil, err
	}
	if err := validateKupoImage(kupo); err != nil {
		return nil, err
	}
	if err := validatePrimaryWorkloadPorts(network.Spec.Node.Port, ogmios, kupo, faucet); err != nil {
		return nil, err
	}

	deployment, err := b.deployment(network, plan, initContainer, ogmios, kupo, faucet)
	if err != nil {
		return nil, err
	}
	persistentVolumeClaim, err := b.persistentVolumeClaim(network, plan)
	if err != nil {
		return nil, err
	}
	service, err := b.service(network)
	if err != nil {
		return nil, err
	}
	var ogmiosService *corev1.Service
	if ogmios.enabled {
		ogmiosService, err = b.ogmiosService(network, ogmios)
		if err != nil {
			return nil, err
		}
	}
	var kupoService *corev1.Service
	if kupo.enabled {
		kupoService, err = b.kupoService(network, kupo)
		if err != nil {
			return nil, err
		}
	}
	var faucetService *corev1.Service
	var faucetAuthSecret *corev1.Secret
	if faucet.enabled {
		faucetService, err = b.faucetService(network, faucet)
		if err != nil {
			return nil, err
		}
		faucetAuthSecret, err = b.faucetAuthSecret(network, faucet)
		if err != nil {
			return nil, err
		}
	}

	return &primaryWorkloadResources{
		NetworkArtifactsConfigMap:       networkArtifactsConfigMap,
		ArtifactPublisherServiceAccount: artifactPublisherServiceAccount,
		ArtifactPublisherRole:           artifactPublisherRole,
		ArtifactPublisherRoleBinding:    artifactPublisherRoleBinding,
		PersistentVolumeClaim:           persistentVolumeClaim,
		Deployment:                      deployment,
		Service:                         service,
		OgmiosService:                   ogmiosService,
		KupoService:                     kupoService,
		FaucetService:                   faucetService,
		FaucetAuthSecret:                faucetAuthSecret,
	}, nil
}

// localnetSpec validates the CardanoNetwork spec for the local-mode runtime
// and converts it into a localnet.Spec the planner accepts. It rejects every
// API shape the current builder slice does not implement (public mode,
// genesis tuning, multi-pool topologies, the babbage era, etc.) so the
// builder fails fast with an actionable UnsupportedSpec message.
func (b primaryWorkloadBuilder) localnetSpec(network *yacdv1alpha1.CardanoNetwork) (localnet.Spec, error) {
	nodeVersion := strings.TrimSpace(network.Spec.Node.Version)
	if nodeVersion == "" {
		return localnet.Spec{}, unsupportedSpec("node version is required")
	}
	if network.Spec.Node.Image != nil && strings.TrimSpace(*network.Spec.Node.Image) == "" {
		return localnet.Spec{}, unsupportedSpec("node image override must not be blank")
	}
	if network.Spec.Node.Port < 1 || network.Spec.Node.Port > 65535 {
		return localnet.Spec{}, unsupportedSpec("node port must be between 1 and 65535")
	}
	if network.Spec.Mode != yacdv1alpha1.CardanoNetworkModeLocal {
		return localnet.Spec{}, unsupportedSpec("mode %q is not supported", network.Spec.Mode)
	}
	if network.Spec.Local == nil {
		return localnet.Spec{}, unsupportedSpec("local spec is required")
	}
	if network.Spec.Public != nil {
		return localnet.Spec{}, unsupportedSpec("public spec is not supported with local mode")
	}

	local := network.Spec.Local
	if local.Era == yacdv1alpha1.CardanoEraBabbage {
		return localnet.Spec{}, unsupportedSpec("local era %q is not supported", local.Era)
	}
	if local.Genesis != nil {
		return localnet.Spec{}, unsupportedSpec("local genesis tuning is not supported")
	}
	if local.Topology.Pools.Count != 1 {
		return localnet.Spec{}, unsupportedSpec("local pool count %d is not supported", local.Topology.Pools.Count)
	}
	if local.Topology.Pools.Defaults != nil {
		return localnet.Spec{}, unsupportedSpec("local pool defaults are not supported")
	}

	return localnet.Spec{
		NetworkMagic: local.NetworkMagic,
		PoolCount:    int(local.Topology.Pools.Count),
		Timing: localnet.Timing{
			SlotLength:  local.Timing.SlotLength.Duration,
			EpochLength: int(local.Timing.EpochLength),
		},
		Paths: localnet.Paths{
			StateDir: localnetStateDir,
			EnvDir:   localnetEnvDir,
		},
		Tool: localnet.Tool{
			Version: nodeVersion,
		},
	}, nil
}
