package cardanonetwork

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/primarypod"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Container-construction internals shared by the four primary workload
// containers. Names appear in Deployment containers, Service port targets,
// readiness queries (status.go), and the faucet-revocation patch (apply.go);
// hence the package-private const block instead of inlined string literals.
const (
	// cardano-node container.
	cardanoNodeContainerName = primarypod.CardanoNodeContainerName
	cardanoNodeCommand       = "cardano-node"
	cardanoNodePortName      = primarypod.PortNameNodeToNode
	cardanoNodeSocketDir     = "/ipc"
	cardanoNodeSocketPath    = "/ipc/node.socket"
	cardanoNodeDatabaseDir   = "/state/db"
	cardanoNodeHostAddress   = "0.0.0.0"

	// ogmios sidecar.
	ogmiosContainerName = "ogmios"
	ogmiosCommand       = "/bin/ogmios"
	ogmiosPortName      = primarypod.PortNameOgmios
	ogmiosHostAddress   = "0.0.0.0"
	ogmiosHealthPath    = "/health"

	// kupo sidecar.
	kupoContainerName     = "kupo"
	kupoPortName          = primarypod.PortNameKupo
	kupoHostAddress       = "0.0.0.0"
	kupoOgmiosHostAddress = "127.0.0.1"
	kupoWorkDir           = "/kupo"
	kupoTmpDir            = "/tmp"

	// faucet sidecar.
	faucetContainerName     = "faucet"
	faucetPortName          = primarypod.PortNameFaucet
	faucetHostAddress       = "0.0.0.0"
	faucetChainHostAddress  = "127.0.0.1"
	faucetAuthTokenMountDir = "/var/run/yacd-faucet"
	faucetAuthTokenPath     = "/var/run/yacd-faucet/token"
	faucetUTXOKeysDir       = "/state/env/utxo-keys"
	faucetOgmiosURLScheme   = "ws"
	faucetKupoURLScheme     = "http"
	faucetHealthPath        = "/healthz"
	faucetReadinessPath     = "/readyz"
)

// cardanoNodeImage returns the resolved cardano-node container image
// reference. The spec override takes precedence; otherwise the
// cardano-testnet image carries cardano-node at the requested version
// (subject to the Reconciler-injected defaultCardanoTestnetImage override
// that lets the local dev stack substitute a freshly built tools image).
func (b primaryWorkloadBuilder) cardanoNodeImage(network *yacdv1alpha1.CardanoNetwork) string {
	if network.Spec.Node.Image != nil {
		return strings.TrimSpace(*network.Spec.Node.Image)
	}

	return b.cardanoTestnetImage(strings.TrimSpace(network.Spec.Node.Version))
}

// cardanoNodeContainer builds the primary cardano-node container. The args
// thread the localnet plan's generated paths through cardano-node's CLI;
// volume mounts attach the persistent state directory and the node IPC
// EmptyDir shared with the ogmios sidecar.
func (b primaryWorkloadBuilder) cardanoNodeContainer(network *yacdv1alpha1.CardanoNetwork, plan primaryNetworkPlan) corev1.Container {
	args := []string{
		"run",
		"--config", plan.ConfigFile,
		"--topology", plan.TopologyFile,
		"--database-path", cardanoNodeDatabaseDir,
		"--socket-path", cardanoNodeSocketPath,
		"--host-addr", cardanoNodeHostAddress,
		"--port", strconv.Itoa(int(network.Spec.Node.Port)),
	}
	if plan.isLocal() {
		args = append(args,
			"--shelley-kes-key", path.Join(plan.Localnet.Layout.EnvDir, "pools-keys", "pool1", "kes.skey"),
			"--shelley-vrf-key", path.Join(plan.Localnet.Layout.EnvDir, "pools-keys", "pool1", "vrf.skey"),
			"--shelley-operational-certificate", path.Join(plan.Localnet.Layout.EnvDir, "pools-keys", "pool1", "opcert.cert"),
		)
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      localnetStateVolumeName,
			MountPath: plan.StateDir,
		},
		{
			Name:      nodeIPCVolumeName,
			MountPath: cardanoNodeSocketDir,
		},
	}
	if plan.isPublic() {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      publicProfileVolumeName,
			MountPath: plan.ProfileDir,
			ReadOnly:  true,
		})
	}

	container := corev1.Container{
		Name:            cardanoNodeContainerName,
		Image:           b.cardanoNodeImage(network),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{cardanoNodeCommand},
		Args:            args,
		Ports: []corev1.ContainerPort{
			{
				Name:          cardanoNodePortName,
				ContainerPort: network.Spec.Node.Port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: volumeMounts,
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

	if network.Spec.Node.Resources != nil {
		container.Resources = *network.Spec.Node.Resources.DeepCopy()
	} else if isPublicMainnet(network) {
		container.Resources = defaultMainnetNodeResources()
	}
	if plan.isPublic() {
		container.WorkingDir = plan.ProfileDir
	}

	return container
}

// ogmiosContainer builds the optional ogmios sidecar. It speaks to
// cardano-node through the shared IPC socket and reads node config through
// the shared state mount (read-only).
func (b primaryWorkloadBuilder) ogmiosContainer(settings ogmiosSettings, plan primaryNetworkPlan) corev1.Container {
	volumeMounts := make([]corev1.VolumeMount, 0, 3)
	if plan.isLocal() {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      localnetStateVolumeName,
			MountPath: plan.StateDir,
			ReadOnly:  true,
		})
	}
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      nodeIPCVolumeName,
		MountPath: cardanoNodeSocketDir,
	})
	if plan.isPublic() {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      publicProfileVolumeName,
			MountPath: plan.ProfileDir,
			ReadOnly:  true,
		})
	}

	container := corev1.Container{
		Name:            ogmiosContainerName,
		Image:           settings.image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{ogmiosCommand},
		Args: []string{
			"--node-socket", cardanoNodeSocketPath,
			"--node-config", plan.ConfigFile,
			"--host", ogmiosHostAddress,
			"--port", strconv.Itoa(int(settings.port)),
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          ogmiosPortName,
				ContainerPort: settings.port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		StartupProbe:   ogmiosHealthProbe(settings.port, 5, 2, 60),
		LivenessProbe:  ogmiosHealthProbe(settings.port, 10, 5, 12),
		ReadinessProbe: ogmiosHealthProbe(settings.port, 5, 2, 3),
		VolumeMounts:   volumeMounts,
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
	if settings.resources != nil {
		container.Resources = *settings.resources.DeepCopy()
	}
	if plan.isPublic() {
		container.WorkingDir = plan.ProfileDir
	}

	return container
}

// kupoContainer builds the optional kupo sidecar. It talks to ogmios through
// the Pod's loopback interface; resource limits and the EmptyDir tmp/db
// mounts are derived from the package defaults so a stuck index cannot
// exhaust the node's ephemeral storage.
func (b primaryWorkloadBuilder) kupoContainer(settings kupoSettings, ogmios ogmiosSettings) corev1.Container {
	container := corev1.Container{
		Name:            kupoContainerName,
		Image:           settings.image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args: []string{
			"--ogmios-host", kupoOgmiosHostAddress,
			"--ogmios-port", strconv.Itoa(int(ogmios.port)),
			"--since", defaultKupoSince,
			"--match", defaultKupoMatchPattern,
			"--prune-utxo",
			"--workdir", kupoWorkDir,
			"--host", kupoHostAddress,
			"--port", strconv.Itoa(int(settings.port)),
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          kupoPortName,
				ContainerPort: settings.port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		StartupProbe:   kupoHealthProbe(settings.port, 5, 2, 60),
		LivenessProbe:  kupoHealthProbe(settings.port, 10, 5, 12),
		ReadinessProbe: kupoHealthProbe(settings.port, 5, 2, 3),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      kupoDBVolumeName,
				MountPath: kupoWorkDir,
			},
			{
				Name:      kupoTmpVolumeName,
				MountPath: kupoTmpDir,
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
		Resources:                defaultKupoResources(),
	}
	if settings.resources != nil {
		container.Resources = *settings.resources.DeepCopy()
	}

	return container
}

// faucetContainer builds the optional faucet sidecar. It calls into ogmios
// and kupo through the Pod's loopback interface, reads its auth token from
// a Secret-backed projection, and reads UTXO source keys (read-only) from a
// subpath of the localnet state mount populated by the
// faucetSourceAddressInitContainer.
func (b primaryWorkloadBuilder) faucetContainer(settings faucetSettings, ogmios ogmiosSettings, kupo kupoSettings) corev1.Container {
	container := corev1.Container{
		Name:            faucetContainerName,
		Image:           settings.image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Args: []string{
			"--listen-address", fmt.Sprintf("%s:%d", faucetHostAddress, settings.port),
			"--utxo-keys-dir", faucetUTXOKeysDir,
			"--default-source", settings.defaultSource,
			"--ogmios-url", fmt.Sprintf("%s://%s:%d", faucetOgmiosURLScheme, faucetChainHostAddress, ogmios.port),
			"--kupo-url", fmt.Sprintf("%s://%s:%d", faucetKupoURLScheme, faucetChainHostAddress, kupo.port),
			"--auth-token-file", settings.authTokenFilePath,
			"--allow-remote-listen",
			"--min-topup-lovelace", strconv.FormatInt(settings.minTopUpLovelace, 10),
			"--max-topup-lovelace", strconv.FormatInt(settings.maxTopUpLovelace, 10),
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          faucetPortName,
				ContainerPort: settings.port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		StartupProbe:   faucetHTTPProbe(faucetHealthPath, settings.port, 5, 2, 60),
		LivenessProbe:  faucetHTTPProbe(faucetHealthPath, settings.port, 10, 5, 12),
		ReadinessProbe: faucetHTTPProbe(faucetReadinessPath, settings.port, 5, 2, 3),
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      localnetStateVolumeName,
				MountPath: faucetUTXOKeysDir,
				SubPath:   "env/utxo-keys",
				ReadOnly:  true,
			},
			{
				Name:      faucetAuthVolumeName,
				MountPath: faucetAuthTokenMountDir,
				ReadOnly:  true,
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
	if settings.resources != nil {
		container.Resources = *settings.resources.DeepCopy()
	}

	return container
}

// faucetHTTPProbe builds an HTTP GET probe against the faucet's health or
// readiness endpoint. periodSeconds, timeoutSeconds, and failureThreshold are
// tuned per probe phase (startup vs. liveness vs. readiness).
func faucetHTTPProbe(probePath string, port int32, periodSeconds int32, timeoutSeconds int32, failureThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: probePath,
				Port: intstr.FromInt(int(port)),
			},
		},
		PeriodSeconds:    periodSeconds,
		TimeoutSeconds:   timeoutSeconds,
		FailureThreshold: failureThreshold,
	}
}

// ogmiosHealthProbe builds an exec-based probe that runs ogmios's own
// health-check subcommand. ogmios does not expose a usable HTTP health
// endpoint, so an exec probe is the canonical option.
func ogmiosHealthProbe(port int32, periodSeconds int32, timeoutSeconds int32, failureThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					ogmiosCommand,
					"health-check",
					"--port",
					strconv.Itoa(int(port)),
				},
			},
		},
		PeriodSeconds:    periodSeconds,
		TimeoutSeconds:   timeoutSeconds,
		FailureThreshold: failureThreshold,
	}
}

// kupoHealthProbe builds an exec-based probe using kupo's health-check
// subcommand.
func kupoHealthProbe(port int32, periodSeconds int32, timeoutSeconds int32, failureThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					kupoContainerName,
					"health-check",
					"--port",
					strconv.Itoa(int(port)),
				},
			},
		},
		PeriodSeconds:    periodSeconds,
		TimeoutSeconds:   timeoutSeconds,
		FailureThreshold: failureThreshold,
	}
}
