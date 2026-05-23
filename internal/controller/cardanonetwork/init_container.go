package cardanonetwork

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/meigma/yacd/internal/cardano/localnet"
	corev1 "k8s.io/api/core/v1"
)

const (
	cardanoTestnetImageRepository = "ghcr.io/meigma/yacd/cardano-testnet"
	cardanoTestnetImageRevision   = "yacd.1"

	localnetCreateEnvInitContainerName   = "cardano-testnet-create-env"
	faucetSourceAddressInitContainerName = "faucet-source-addresses"
	localnetStateVolumeName              = "localnet-state"
	localnetCreateEnvCommand             = "/opt/yacd/bin/yacd-cardano-testnet-init"
	faucetSourceAddressCommand           = "/bin/sh"
	faucetVerificationKeyFileName        = "utxo.vkey"
	faucetAddressFileName                = "utxo.addr"

	localnetToolsRunAsID int64 = 10001

	localnetEnvDirEnvName       = "YACD_LOCALNET_ENV_DIR"
	localnetConfigFileEnvName   = "YACD_LOCALNET_CONFIG_FILE"
	localnetManifestFileEnvName = "YACD_LOCALNET_PLAN_MANIFEST_FILE"
	localnetManifestEnvName     = "YACD_LOCALNET_PLAN_MANIFEST"
)

// cardanoTestnetInitContainer converts a localnet plan into the init
// container fragment that generates the cardano-testnet environment.
func (b primaryWorkloadBuilder) cardanoTestnetInitContainer(plan localnet.Plan) (corev1.Container, error) {
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
		Image:           cardanoTestnetImage(toolVersion),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{localnetCreateEnvCommand},
		Args:            args,
		Env: []corev1.EnvVar{
			{Name: localnetEnvDirEnvName, Value: plan.Layout.EnvDir},
			{Name: localnetConfigFileEnvName, Value: plan.Layout.ConfigFile},
			{Name: localnetManifestFileEnvName, Value: plan.Layout.ManifestFile},
			{Name: localnetManifestEnvName, Value: string(manifest)},
		},
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
	}, nil
}

func faucetSourceAddressInitContainer(plan localnet.Plan) corev1.Container {
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
		Image:           cardanoTestnetImage(toolVersion),
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

func cardanoTestnetImage(toolVersion string) string {
	return fmt.Sprintf("%s:%s-%s", cardanoTestnetImageRepository, toolVersion, cardanoTestnetImageRevision)
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
