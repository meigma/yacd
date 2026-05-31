package cardanonetwork

import (
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	"github.com/meigma/yacd/internal/cardano/publicnet"
	"github.com/meigma/yacd/internal/cardano/toolsimage"
	corev1 "k8s.io/api/core/v1"
)

const (
	cardanoTestnetImageRepository = "ghcr.io/meigma/yacd/cardano-testnet"
	cardanoTestnetImageRevision   = "yacd.4"

	localnetCreateEnvInitContainerName   = "cardano-testnet-create-env"
	mithrilBootstrapInitContainerName    = "mithril-bootstrap"
	faucetSourceAddressInitContainerName = "faucet-source-addresses"
	servedArtifactsInitContainerName     = "served-artifacts"
	localnetStateVolumeName              = "localnet-state"
	mithrilTmpVolumeName                 = "mithril-tmp"
	localnetCreateEnvCommand             = "/opt/yacd/bin/yacd-cardano-testnet-init"
	cardanoToolsCommand                  = "/opt/yacd/bin/yacd-cardano-tools"
	mithrilBootstrapCommand              = "/bin/sh"
	faucetSourceAddressCommand           = "/bin/sh"
	faucetVerificationKeyFileName        = "utxo.vkey"
	faucetAddressFileName                = "utxo.addr"

	localnetToolsRunAsID int64 = 10001

	localnetEnvDirEnvName       = "YACD_LOCALNET_ENV_DIR"
	localnetConfigFileEnvName   = "YACD_LOCALNET_CONFIG_FILE"
	localnetManifestFileEnvName = "YACD_LOCALNET_PLAN_MANIFEST_FILE"
	localnetManifestEnvName     = "YACD_LOCALNET_PLAN_MANIFEST"

	mithrilAggregatorEndpointEnvName       = "AGGREGATOR_ENDPOINT"
	mithrilSnapshotEnvName                 = "MITHRIL_SNAPSHOT"
	mithrilGenesisVerificationKeyEnvName   = "GENESIS_VERIFICATION_KEY"
	mithrilAncillaryVerificationKeyEnvName = "ANCILLARY_VERIFICATION_KEY"
)

// cardanoTestnetInitContainer converts a localnet plan into the init
// container fragment that generates the cardano-testnet environment.
func (b primaryWorkloadBuilder) cardanoTestnetInitContainer(network *yacdv1alpha1.CardanoNetwork, plan localnet.Plan) (corev1.Container, error) {
	if network == nil {
		return corev1.Container{}, fmt.Errorf("cardanonetwork is required")
	}
	if network.Spec.Local == nil {
		return corev1.Container{}, fmt.Errorf("local spec is required")
	}
	if err := validateLocalnetInitContainerPlan(plan); err != nil {
		return corev1.Container{}, err
	}

	manifest, err := json.Marshal(plan.Manifest)
	if err != nil {
		return corev1.Container{}, fmt.Errorf("marshal localnet plan manifest: %w", err)
	}

	args := make([]string, 0, len(plan.CreateEnv.Args))
	args = append(args, plan.CreateEnv.Args...)
	toolVersion := strings.TrimSpace(plan.Spec.Tool.Version)

	return corev1.Container{
		Name:            localnetCreateEnvInitContainerName,
		Image:           b.cardanoTestnetImage(toolVersion),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{localnetCreateEnvCommand},
		Args:            args,
		Env: []corev1.EnvVar{
			{Name: localnetEnvDirEnvName, Value: plan.Layout.EnvDir},
			{Name: localnetConfigFileEnvName, Value: plan.Layout.ConfigFile},
			{Name: localnetManifestFileEnvName, Value: plan.Layout.ManifestFile},
			{Name: localnetManifestEnvName, Value: string(manifest)},
			{Name: artifactConfigMapNameEnv, Value: networkArtifactsConfigMapName(network)},
			{Name: artifactNetworkNameEnv, Value: network.Name},
			{Name: artifactNetworkNamespaceEnv, Value: network.Namespace},
			{Name: artifactNetworkModeEnv, Value: string(network.Spec.Mode)},
			{Name: artifactNetworkEraEnv, Value: string(network.Spec.Local.Era)},
			{Name: artifactNodeToNodeHostEnv, Value: nodeToNodeHost(network)},
			{Name: artifactNodeToNodePortEnv, Value: strconv.Itoa(int(network.Spec.Node.Port))},
			{Name: artifactNodeToNodeURLEnv, Value: nodeToNodeURL(network)},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      localnetStateVolumeName,
				MountPath: plan.Layout.StateDir,
			},
			artifactPublisherVolumeMount(),
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			ReadOnlyRootFilesystem: new(true),
			RunAsGroup:             new(localnetToolsRunAsID),
			RunAsNonRoot:           new(true),
			RunAsUser:              new(localnetToolsRunAsID),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}, nil
}

func (b primaryWorkloadBuilder) faucetSourceAddressInitContainer(plan localnet.Plan) corev1.Container {
	toolVersion := strings.TrimSpace(plan.Spec.Tool.Version)
	script := fmt.Sprintf(`for dir in %s/utxo[1-9]*; do
  [ -d "$dir" ] || continue
  [ -f "$dir/%s" ] || continue
  cardano-cli address build --testnet-magic %d --payment-verification-key-file "$dir/%s" --out-file "$dir/%s"
done`,
		faucetUTXOKeysDir,
		faucetVerificationKeyFileName,
		plan.Spec.NetworkMagic,
		faucetVerificationKeyFileName,
		faucetAddressFileName,
	)

	return corev1.Container{
		Name:            faucetSourceAddressInitContainerName,
		Image:           b.cardanoTestnetImage(toolVersion),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{faucetSourceAddressCommand},
		Args:            []string{"-eu", "-c", script},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      localnetStateVolumeName,
				MountPath: plan.Layout.StateDir,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			ReadOnlyRootFilesystem: new(true),
			RunAsGroup:             new(localnetToolsRunAsID),
			RunAsNonRoot:           new(true),
			RunAsUser:              new(localnetToolsRunAsID),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
}

func (b primaryWorkloadBuilder) mithrilBootstrapInitContainer(plan publicnet.MithrilPlan) corev1.Container {
	script := fmt.Sprintf(`target_db=%q
staging_root=%q
download_dir="${staging_root}/download"

if [ -d "${target_db}" ] && [ "$(find "${target_db}" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
  echo "Existing cardano-node database found at ${target_db}; skipping Mithril bootstrap."
  exit 0
fi

rm -rf "${staging_root}"
mkdir -p "${download_dir}"

mithril-client cardano-db download \
  --include-ancillary \
  --download-dir "${download_dir}" \
  --genesis-verification-key "${GENESIS_VERIFICATION_KEY}" \
  --ancillary-verification-key "${ANCILLARY_VERIFICATION_KEY}" \
  "${MITHRIL_SNAPSHOT}"

if [ -d "${target_db}" ] && [ "$(find "${target_db}" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
  echo "Cardano-node database was populated while Mithril bootstrap was running; refusing to overwrite ${target_db}." >&2
  exit 1
fi

candidate_db="$(find "${download_dir}" -mindepth 1 -type d -name db -print -quit)"
if [ -z "${candidate_db}" ]; then
  echo "Mithril download did not produce a cardano-node db directory under ${download_dir}." >&2
  exit 1
fi

rm -rf "${target_db}"
mkdir -p "$(dirname "${target_db}")"
mv "${candidate_db}" "${target_db}"
rm -rf "${staging_root}"
`,
		cardanoNodeDatabaseDir,
		path.Join(localnetStateDir, "bootstrap", "mithril"),
	)

	return corev1.Container{
		Name:            mithrilBootstrapInitContainerName,
		Image:           plan.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		WorkingDir:      localnetStateDir,
		Command:         []string{mithrilBootstrapCommand},
		Args:            []string{"-eu", "-c", script},
		Env: []corev1.EnvVar{
			{Name: mithrilAggregatorEndpointEnvName, Value: plan.AggregatorEndpoint},
			{Name: mithrilSnapshotEnvName, Value: plan.Snapshot},
			{Name: mithrilGenesisVerificationKeyEnvName, Value: plan.GenesisVerificationKey},
			{Name: mithrilAncillaryVerificationKeyEnvName, Value: plan.AncillaryVerificationKey},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      localnetStateVolumeName,
				MountPath: localnetStateDir,
			},
			{
				Name:      mithrilTmpVolumeName,
				MountPath: "/tmp",
			},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			ReadOnlyRootFilesystem: new(true),
			RunAsGroup:             new(localnetToolsRunAsID),
			RunAsNonRoot:           new(true),
			RunAsUser:              new(localnetToolsRunAsID),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
}

// servedArtifactsInitContainer builds the init container that populates the
// flat served-artifact directory (servedArtifactsDir) on the node-state PVC
// before the always-on serve sidecar starts.
//
// For LOCAL networks it runs cardano-tools "stage", flattening the
// cardano-testnet create-env directory (localnetEnvDir) into the served
// directory; it therefore must be ordered after the create-env init container.
// For CURATED PUBLIC networks (public profile other than "custom") it runs
// cardano-tools "fetch", downloading the profile's pinned artifacts into the
// served directory; it must be ordered before any Mithril bootstrap init
// container. Custom-public networks are out of scope for this additive PR and
// must not reach this builder.
//
// The container reuses the hardened uid/gid 10001 security context shared by
// the other tools init containers and mounts the node-state PVC read-write at
// localnetStateDir so it can write servedArtifactsDir.
func (b primaryWorkloadBuilder) servedArtifactsInitContainer(network *yacdv1alpha1.CardanoNetwork, plan primaryNetworkPlan) (corev1.Container, error) {
	args, toolVersion, err := b.servedArtifactsInitArgs(network, plan)
	if err != nil {
		return corev1.Container{}, err
	}

	return corev1.Container{
		Name:            servedArtifactsInitContainerName,
		Image:           b.cardanoToolsImage(toolVersion),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{cardanoToolsCommand},
		Args:            args,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      localnetStateVolumeName,
				MountPath: localnetStateDir,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			ReadOnlyRootFilesystem: new(true),
			RunAsGroup:             new(localnetToolsRunAsID),
			RunAsNonRoot:           new(true),
			RunAsUser:              new(localnetToolsRunAsID),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
		},
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}, nil
}

// servedArtifactsInitArgs returns the cardano-tools subcommand args and the
// tool version used to resolve the cardano-tools image for the served-artifact
// init container. LOCAL produces "stage" args carrying the network identity the
// stage config requires; CURATED PUBLIC produces "fetch" args for the resolved
// profile.
func (b primaryWorkloadBuilder) servedArtifactsInitArgs(network *yacdv1alpha1.CardanoNetwork, plan primaryNetworkPlan) ([]string, string, error) {
	switch {
	case plan.isLocal():
		if plan.Localnet == nil {
			return nil, "", fmt.Errorf("localnet plan is required for served-artifact staging")
		}
		era := ""
		if plan.Era != nil {
			era = string(*plan.Era)
		}
		args := []string{
			"stage",
			"--state-dir", plan.Localnet.Layout.EnvDir,
			"--output-dir", servedArtifactsDir,
			"--cardano-network-name", network.Name,
			"--cardano-network-namespace", network.Namespace,
			"--cardano-network-mode", string(network.Spec.Mode),
			"--cardano-network-era", era,
			"--cardano-node-to-node-host", nodeToNodeHost(network),
			"--cardano-node-to-node-port", strconv.Itoa(int(network.Spec.Node.Port)),
		}
		return args, strings.TrimSpace(plan.Localnet.Spec.Tool.Version), nil
	case plan.isPublic() && isCuratedPublicProfile(plan):
		args := []string{
			"fetch",
			"--profile", string(*plan.Profile),
			"--output-dir", servedArtifactsDir,
		}
		return args, strings.TrimSpace(network.Spec.Node.Version), nil
	default:
		return nil, "", fmt.Errorf("served-artifact staging is not supported for this network plan")
	}
}

// isCuratedPublicProfile reports whether the plan targets a curated public
// profile (preview, preprod, mainnet) rather than the custom profile. Curated
// profiles have pinned, fetchable artifacts; the custom profile is supplied by
// the user and is intentionally out of scope for served-artifact staging in
// this additive PR.
func isCuratedPublicProfile(plan primaryNetworkPlan) bool {
	return plan.isPublic() &&
		plan.Profile != nil &&
		*plan.Profile != yacdv1alpha1.PublicNetworkProfileCustom
}

// cardanoTestnetImage returns the cardano-testnet container image reference
// used for the create-env init container, the faucet source-address init
// container, and the default cardano-node container. The
// Reconciler-injected defaultCardanoTestnetImage takes precedence so the
// local dev stack can substitute a freshly built tools image when the
// published cardano-testnet tag is behind the publisher code that depends
// on it (e.g. db-sync genesis hash enrichment). With no injected override,
// the built-in formula reproduces the legacy "<repo>:<toolVersion>-<revision>"
// reference.
func (b primaryWorkloadBuilder) cardanoTestnetImage(toolVersion string) string {
	if override := strings.TrimSpace(b.defaultCardanoTestnetImage); override != "" {
		return override
	}
	return fmt.Sprintf("%s:%s-%s", cardanoTestnetImageRepository, toolVersion, cardanoTestnetImageRevision)
}

// cardanoToolsImage returns the cardano-tools utility image reference used by
// the served-artifact stage/fetch init container and the always-on serve
// sidecar. The Reconciler-injected defaultCardanoToolsImage takes precedence
// so the local dev stack can substitute a freshly built tools image; the
// built-in formula is owned by the shared toolsimage package so both the
// CardanoNetwork and CardanoDBSync controllers agree on the same reference.
func (b primaryWorkloadBuilder) cardanoToolsImage(toolVersion string) string {
	return toolsimage.Reference(b.defaultCardanoToolsImage, toolVersion)
}

func validateLocalnetInitContainerPlan(plan localnet.Plan) error {
	if strings.TrimSpace(plan.Spec.Tool.Version) == "" {
		return fmt.Errorf("localnet tool version is required")
	}
	if len(plan.CreateEnv.Args) == 0 {
		return fmt.Errorf("localnet create-env args are required")
	}
	if strings.TrimSpace(plan.Layout.StateDir) == "" {
		return fmt.Errorf("localnet state dir is required")
	}
	if strings.TrimSpace(plan.Layout.EnvDir) == "" {
		return fmt.Errorf("localnet env dir is required")
	}
	if strings.TrimSpace(plan.Layout.ConfigFile) == "" {
		return fmt.Errorf("localnet config file is required")
	}
	if strings.TrimSpace(plan.Layout.ManifestFile) == "" {
		return fmt.Errorf("localnet manifest file is required")
	}

	return nil
}
