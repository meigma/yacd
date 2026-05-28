package cardanonetwork

import (
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	"github.com/meigma/yacd/internal/cardano/publicnet"
	ctrldbsync "github.com/meigma/yacd/internal/controller/cardanodbsync"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// primaryWorkloadResources is the desired-state bundle the builder produces
// for one CardanoNetwork. Every field is non-nil except localnet-only artifact
// publisher RBAC and optional chain API resources, which are nil when disabled
// or not applicable.
type primaryWorkloadResources struct {
	NetworkPlan                     primaryNetworkPlan
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
	DBSyncAttached                  bool
}

type localArtifactPublisherResources struct {
	ServiceAccount *corev1.ServiceAccount
	Role           *rbacv1.Role
	RoleBinding    *rbacv1.RoleBinding
	InitContainer  *corev1.Container
}

type chainAPISettings struct {
	Ogmios ogmiosSettings
	Kupo   kupoSettings
	Faucet faucetSettings
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

	dbSyncAttachment *ctrldbsync.PrimarySidecarAttachment

	publicCustomBundle *publicnet.CustomBundle
}

// Build composes the desired primary workload resources for the given
// CardanoNetwork.
//
// The order of operations is:
//  1. validate the spec into a runtime plan the planner can accept
//  2. compute the network plan (fingerprint, paths, invocation args)
//  3. build the artifact bundle (ConfigMap, and localnet-only RBAC)
//  4. build the localnet cardano-testnet init container fragment
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

	networkPlan, err := b.networkPlan(network)
	if err != nil {
		return nil, err
	}

	networkArtifactsConfigMap, err := b.networkArtifactsConfigMap(network, networkPlan)
	if err != nil {
		return nil, err
	}
	artifactPublisher, err := b.localArtifactPublisherResources(network, networkPlan)
	if err != nil {
		return nil, err
	}
	chainAPI, err := b.chainAPISettings(network, networkPlan)
	if err != nil {
		return nil, err
	}

	deployment, err := b.deployment(network, networkPlan, artifactPublisher.InitContainer, chainAPI.Ogmios, chainAPI.Kupo, chainAPI.Faucet)
	if err != nil {
		return nil, err
	}
	persistentVolumeClaim, err := b.persistentVolumeClaim(network, networkPlan)
	if err != nil {
		return nil, err
	}
	service, err := b.service(network)
	if err != nil {
		return nil, err
	}
	var ogmiosService *corev1.Service
	if chainAPI.Ogmios.enabled {
		ogmiosService, err = b.ogmiosService(network, chainAPI.Ogmios)
		if err != nil {
			return nil, err
		}
	}
	var kupoService *corev1.Service
	if chainAPI.Kupo.enabled {
		kupoService, err = b.kupoService(network, chainAPI.Kupo)
		if err != nil {
			return nil, err
		}
	}
	var faucetService *corev1.Service
	var faucetAuthSecret *corev1.Secret
	if chainAPI.Faucet.enabled {
		faucetService, err = b.faucetService(network, chainAPI.Faucet)
		if err != nil {
			return nil, err
		}
		faucetAuthSecret, err = b.faucetAuthSecret(network, chainAPI.Faucet)
		if err != nil {
			return nil, err
		}
	}

	return &primaryWorkloadResources{
		NetworkPlan:                     networkPlan,
		NetworkArtifactsConfigMap:       networkArtifactsConfigMap,
		ArtifactPublisherServiceAccount: artifactPublisher.ServiceAccount,
		ArtifactPublisherRole:           artifactPublisher.Role,
		ArtifactPublisherRoleBinding:    artifactPublisher.RoleBinding,
		PersistentVolumeClaim:           persistentVolumeClaim,
		Deployment:                      deployment,
		Service:                         service,
		OgmiosService:                   ogmiosService,
		KupoService:                     kupoService,
		FaucetService:                   faucetService,
		FaucetAuthSecret:                faucetAuthSecret,
		DBSyncAttached:                  b.dbSyncAttachment != nil,
	}, nil
}

func (b primaryWorkloadBuilder) localArtifactPublisherResources(network *yacdv1alpha1.CardanoNetwork, plan primaryNetworkPlan) (localArtifactPublisherResources, error) {
	if !plan.isLocal() {
		return localArtifactPublisherResources{}, nil
	}

	serviceAccount, err := b.artifactPublisherServiceAccount(network)
	if err != nil {
		return localArtifactPublisherResources{}, err
	}
	role, err := b.artifactPublisherRole(network)
	if err != nil {
		return localArtifactPublisherResources{}, err
	}
	roleBinding, err := b.artifactPublisherRoleBinding(network)
	if err != nil {
		return localArtifactPublisherResources{}, err
	}
	localnetPlan := *plan.Localnet
	initContainer, err := b.cardanoTestnetInitContainer(network, localnetPlan)
	if err != nil {
		return localArtifactPublisherResources{}, err
	}

	return localArtifactPublisherResources{
		ServiceAccount: serviceAccount,
		Role:           role,
		RoleBinding:    roleBinding,
		InitContainer:  &initContainer,
	}, nil
}

func (b primaryWorkloadBuilder) chainAPISettings(network *yacdv1alpha1.CardanoNetwork, plan primaryNetworkPlan) (chainAPISettings, error) {
	ogmios, err := resolveOgmiosSettings(network)
	if err != nil {
		return chainAPISettings{}, err
	}
	if plan.isPublic() {
		if kupoExplicitlyEnabled(network) {
			return chainAPISettings{}, unsupportedSpec("kupo is not supported for public networks")
		}
		if faucetExplicitlyEnabled(network) {
			return chainAPISettings{}, unsupportedSpec("faucet is not supported for public networks")
		}
	}
	kupo, kupoMentioned, err := resolveKupoSettings(network)
	if err != nil {
		return chainAPISettings{}, err
	}
	// Apply cross-component defaults (kupo follows ogmios when unmentioned)
	// before the hard invariant check: kupo cannot be explicitly enabled
	// without ogmios.
	kupo = applyDependentDefaults(ogmios, kupo, kupoMentioned)
	if plan.isPublic() && !kupoMentioned {
		kupo.enabled = false
	}
	if kupo.enabled && !ogmios.enabled {
		return chainAPISettings{}, unsupportedSpec("kupo requires ogmios to be enabled")
	}
	faucet, err := b.resolveFaucetSettings(network, ogmios, kupo)
	if err != nil {
		return chainAPISettings{}, err
	}
	// Skip the ogmios/cardano-node compatibility check when the CR is going
	// to be rejected as UnsupportedNetworkChange anyway; surface that specific
	// error instead.
	if !acceptedNetworkFingerprintChanged(network, plan.Fingerprint) {
		err = validateOgmiosCompatibility(network.Spec.Node.Version, ogmios)
	}
	if err != nil {
		return chainAPISettings{}, err
	}
	if err := validateKupoImage(kupo); err != nil {
		return chainAPISettings{}, err
	}
	if err := validatePrimaryWorkloadPorts(network.Spec.Node.Port, ogmios, kupo, faucet); err != nil {
		return chainAPISettings{}, err
	}

	return chainAPISettings{Ogmios: ogmios, Kupo: kupo, Faucet: faucet}, nil
}

func (b primaryWorkloadBuilder) networkPlan(network *yacdv1alpha1.CardanoNetwork) (primaryNetworkPlan, error) {
	if err := validateSharedNetworkSpec(network); err != nil {
		return primaryNetworkPlan{}, err
	}

	switch network.Spec.Mode {
	case yacdv1alpha1.CardanoNetworkModeLocal:
		spec, err := b.localnetSpec(network)
		if err != nil {
			return primaryNetworkPlan{}, err
		}
		plan, err := localnet.BuildPlan(spec)
		if err != nil {
			return primaryNetworkPlan{}, unsupportedSpec("build localnet plan: %v", err)
		}
		return localPrimaryNetworkPlan(plan, network.Spec.Local.Era), nil
	case yacdv1alpha1.CardanoNetworkModePublic:
		spec, err := b.publicNetworkSpec(network)
		if err != nil {
			return primaryNetworkPlan{}, err
		}
		plan, err := publicnet.BuildPlan(spec)
		if err != nil {
			return primaryNetworkPlan{}, unsupportedSpec("%v", err)
		}
		return publicPrimaryNetworkPlan(network, plan)
	default:
		return primaryNetworkPlan{}, unsupportedSpec("mode %q is not supported", network.Spec.Mode)
	}
}

func validateSharedNetworkSpec(network *yacdv1alpha1.CardanoNetwork) error {
	nodeVersion := strings.TrimSpace(network.Spec.Node.Version)
	if nodeVersion == "" {
		return unsupportedSpec("node version is required")
	}
	if network.Spec.Node.Image != nil && strings.TrimSpace(*network.Spec.Node.Image) == "" {
		return unsupportedSpec("node image override must not be blank")
	}
	if network.Spec.Node.Port < 1 || network.Spec.Node.Port > 65535 {
		return unsupportedSpec("node port must be between 1 and 65535")
	}
	if isPublicMainnet(network) && network.Spec.Node.Storage != nil {
		minimum := resourceQuantity(minimumMainnetNodeStorageSize)
		if network.Spec.Node.Storage.Size.Cmp(*minimum) < 0 {
			return unsupportedSpec("public mainnet node storage must be at least %s", minimumMainnetNodeStorageSize)
		}
	}

	return nil
}

// localnetSpec validates the CardanoNetwork spec for the local-mode runtime
// and converts it into a localnet.Spec the planner accepts. It rejects every
// API shape the current builder slice does not implement (public mode,
// genesis tuning, multi-pool topologies, the babbage era, etc.) so the
// builder fails fast with an actionable UnsupportedSpec message.
func (b primaryWorkloadBuilder) localnetSpec(network *yacdv1alpha1.CardanoNetwork) (localnet.Spec, error) {
	nodeVersion := strings.TrimSpace(network.Spec.Node.Version)
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

func (b primaryWorkloadBuilder) publicNetworkSpec(network *yacdv1alpha1.CardanoNetwork) (publicnet.Spec, error) {
	if network.Spec.Public == nil {
		return publicnet.Spec{}, unsupportedSpec("public spec is required")
	}
	if network.Spec.Local != nil {
		return publicnet.Spec{}, unsupportedSpec("local spec is not supported with public mode")
	}

	public := network.Spec.Public
	switch public.Profile {
	case yacdv1alpha1.PublicNetworkProfileCustom:
		if public.Bootstrap != nil {
			return publicnet.Spec{}, unsupportedSpec("public bootstrap is supported only for mainnet")
		}
		if public.ConfigSource == nil {
			return publicnet.Spec{}, unsupportedSpec("public custom profile configSource is required")
		}
		if b.publicCustomBundle == nil {
			return publicnet.Spec{}, unsupportedSpec("public custom profile source has not been resolved")
		}
	case yacdv1alpha1.PublicNetworkProfilePreview, yacdv1alpha1.PublicNetworkProfilePreprod:
		if public.Bootstrap != nil {
			return publicnet.Spec{}, unsupportedSpec("public bootstrap is supported only for mainnet")
		}
		if public.ConfigSource != nil {
			return publicnet.Spec{}, unsupportedSpec("public configSource is supported only for custom profiles")
		}
	case yacdv1alpha1.PublicNetworkProfileMainnet:
		if public.ConfigSource != nil {
			return publicnet.Spec{}, unsupportedSpec("public configSource is supported only for custom profiles")
		}
		if public.Bootstrap == nil || public.Bootstrap.Mithril == nil {
			return publicnet.Spec{}, unsupportedSpec("public mainnet profile requires spec.public.bootstrap.mithril")
		}
	default:
		if public.Profile == "" {
			return publicnet.Spec{}, unsupportedSpec("public profile is required")
		}
	}

	bootstrap := publicBootstrapSpec(public)
	return publicnet.Spec{
		Profile:   string(public.Profile),
		Custom:    b.publicCustomBundle,
		Bootstrap: bootstrap,
		Paths: publicnet.Paths{
			ProfileDir: publicProfileMountDir,
		},
	}, nil
}

func publicBootstrapSpec(public *yacdv1alpha1.PublicNetworkSpec) *publicnet.BootstrapSpec {
	if public == nil || public.Bootstrap == nil || public.Bootstrap.Mithril == nil {
		return nil
	}
	return &publicnet.BootstrapSpec{
		Mithril: &publicnet.MithrilBootstrapSpec{
			Image:    public.Bootstrap.Mithril.Image,
			Snapshot: public.Bootstrap.Mithril.Snapshot,
		},
	}
}

func isPublicMainnet(network *yacdv1alpha1.CardanoNetwork) bool {
	return network != nil &&
		network.Spec.Mode == yacdv1alpha1.CardanoNetworkModePublic &&
		network.Spec.Public != nil &&
		network.Spec.Public.Profile == yacdv1alpha1.PublicNetworkProfileMainnet
}

func kupoExplicitlyEnabled(network *yacdv1alpha1.CardanoNetwork) bool {
	return network.Spec.ChainAPI != nil &&
		network.Spec.ChainAPI.Kupo != nil &&
		network.Spec.ChainAPI.Kupo.Enabled
}

func faucetExplicitlyEnabled(network *yacdv1alpha1.CardanoNetwork) bool {
	return network.Spec.ChainAPI != nil &&
		network.Spec.ChainAPI.Faucet != nil &&
		network.Spec.ChainAPI.Faucet.Enabled
}
