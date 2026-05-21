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

	localnetCreateEnvInitContainerName = "cardano-testnet-create-env"
	localnetStateVolumeName            = "localnet-state"

	localnetToolsRunAsID int64 = 10001

	localnetEnvDirEnvName       = "YACD_LOCALNET_ENV_DIR"
	localnetConfigFileEnvName   = "YACD_LOCALNET_CONFIG_FILE"
	localnetManifestFileEnvName = "YACD_LOCALNET_PLAN_MANIFEST_FILE"
	localnetManifestEnvName     = "YACD_LOCALNET_PLAN_MANIFEST"
)

const localnetCreateEnvWrapperScript = `manifest_file="${YACD_LOCALNET_PLAN_MANIFEST_FILE:?YACD_LOCALNET_PLAN_MANIFEST_FILE is required}"
env_dir="${YACD_LOCALNET_ENV_DIR:?YACD_LOCALNET_ENV_DIR is required}"
config_file="${YACD_LOCALNET_CONFIG_FILE:?YACD_LOCALNET_CONFIG_FILE is required}"
requested_manifest="${YACD_LOCALNET_PLAN_MANIFEST:?YACD_LOCALNET_PLAN_MANIFEST is required}"

if [ -f "$manifest_file" ] && [ -f "$config_file" ] && printf "%s" "$requested_manifest" | cmp -s - "$manifest_file"; then
  echo "localnet env already matches requested plan"
  exit 0
fi

if [ -e "$env_dir" ]; then
  echo "existing localnet env does not match requested plan; refusing to overwrite $env_dir" >&2
  exit 1
fi

"$0" "$@"
printf "%s" "$requested_manifest" > "$manifest_file"`

// localnetCreateEnvInitContainer converts a localnet plan into the init
// container fragment that generates the cardano-testnet environment.
func localnetCreateEnvInitContainer(plan localnet.Plan) (corev1.Container, error) {
	if err := validateLocalnetInitContainerPlan(plan); err != nil {
		return corev1.Container{}, err
	}

	manifest, err := json.Marshal(plan.Manifest)
	if err != nil {
		return corev1.Container{}, fmt.Errorf("marshal localnet plan manifest: %w", err)
	}

	args := make([]string, 0, 2+len(plan.CreateEnv.Args))
	args = append(args, localnetCreateEnvWrapperScript, plan.CreateEnv.Command)
	args = append(args, plan.CreateEnv.Args...)

	return corev1.Container{
		Name:            localnetCreateEnvInitContainerName,
		Image:           fmt.Sprintf("%s:%s", cardanoTestnetImageRepository, strings.TrimSpace(plan.Spec.Tool.Version)),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-ec"},
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
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}, nil
}

func validateLocalnetInitContainerPlan(plan localnet.Plan) error {
	if strings.TrimSpace(plan.Spec.Tool.Version) == "" {
		return fmt.Errorf("localnet tool version is required")
	}
	if strings.TrimSpace(plan.CreateEnv.Command) == "" {
		return fmt.Errorf("localnet create-env command is required")
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
