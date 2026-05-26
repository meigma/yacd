package cardanonetwork

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	cardanoNodeContainerName = "cardano-node"
	cardanoNodeCommand       = "cardano-node"
	cardanoNodePortName      = "node-to-node"
	cardanoNodeSocketDir     = "/ipc"
	cardanoNodeSocketPath    = "/ipc/node.socket"
	cardanoNodeDatabaseDir   = "/state/db"
	cardanoNodeHostAddress   = "0.0.0.0"

	ogmiosContainerName = "ogmios"
	ogmiosCommand       = "/bin/ogmios"
	ogmiosPortName      = "ogmios"
	ogmiosHostAddress   = "0.0.0.0"
	ogmiosHealthPath    = "/health"

	kupoContainerName     = "kupo"
	kupoPortName          = "kupo"
	kupoHostAddress       = "0.0.0.0"
	kupoOgmiosHostAddress = "127.0.0.1"
	kupoWorkDir           = "/kupo"
	kupoDBVolumeName      = "kupo-db"
	kupoTmpDir            = "/tmp"
	kupoTmpVolumeName     = "kupo-tmp"

	faucetContainerName     = "faucet"
	faucetPortName          = "faucet"
	faucetHostAddress       = "0.0.0.0"
	faucetChainHostAddress  = "127.0.0.1"
	faucetAuthVolumeName    = "faucet-auth"
	faucetAuthTokenKey      = "token"
	faucetAuthTokenMountDir = "/var/run/yacd-faucet"
	faucetAuthTokenPath     = "/var/run/yacd-faucet/token"
	faucetUTXOKeysDir       = "/state/env/utxo-keys"
	faucetOgmiosURLScheme   = "ws"
	faucetKupoURLScheme     = "http"
	faucetHealthPath        = "/healthz"
	faucetReadinessPath     = "/readyz"

	nodeIPCVolumeName = "node-ipc"
)

// primaryWorkloadResources are the Kubernetes resources that run the initial
// singleton primary Cardano node.
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

// primaryWorkloadBuilder converts a CardanoNetwork into the desired primary
// node workload resources. Reconciliation side effects stay in the controller.
type primaryWorkloadBuilder struct {
	scheme             *runtime.Scheme
	defaultFaucetImage string
}

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
	kupo, err := resolveKupoSettings(network, ogmios)
	if err != nil {
		return nil, err
	}
	if kupo.enabled && !ogmios.enabled {
		return nil, unsupportedSpec("kupo requires ogmios to be enabled")
	}
	faucet, err := b.resolveFaucetSettings(network, ogmios, kupo)
	if err != nil {
		return nil, err
	}
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

func (b primaryWorkloadBuilder) deployment(network *yacdv1alpha1.CardanoNetwork, plan localnet.Plan, initContainer corev1.Container, ogmios ogmiosSettings, kupo kupoSettings, faucet faucetSettings) (*appsv1.Deployment, error) {
	selectorLabels := primaryWorkloadSelectorLabels(network)
	labels := primaryWorkloadLabels(network)
	deploymentName := primaryWorkloadName(network)
	containers := []corev1.Container{b.cardanoNodeContainer(network, plan)}
	if ogmios.enabled {
		containers = append(containers, b.ogmiosContainer(ogmios, plan))
	}
	if kupo.enabled {
		containers = append(containers, b.kupoContainer(kupo, ogmios))
	}
	if faucet.enabled {
		containers = append(containers, b.faucetContainer(faucet, ogmios, kupo))
	}
	initContainers := []corev1.Container{initContainer}
	if faucet.enabled {
		initContainers = append(initContainers, faucetSourceAddressInitContainer(plan))
	}
	volumes := []corev1.Volume{
		{
			Name: localnetStateVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: primaryNodeStatePVCName(network),
				},
			},
		},
		{
			Name: nodeIPCVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
	if kupo.enabled {
		volumes = append(volumes,
			corev1.Volume{
				Name: kupoDBVolumeName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						SizeLimit: resourceQuantity(defaultKupoDBSizeLimit),
					},
				},
			},
			corev1.Volume{
				Name: kupoTmpVolumeName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						SizeLimit: resourceQuantity(defaultKupoTmpSizeLimit),
					},
				},
			},
		)
	}
	if faucet.enabled {
		optional := false
		volumes = append(volumes, corev1.Volume{
			Name: faucetAuthVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: faucet.authSecretName,
					Items: []corev1.KeyToPath{
						{
							Key:  faucet.authSecretKey,
							Path: faucet.authSecretKey,
						},
					},
					Optional: &optional,
				},
			},
		})
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: network.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(1)),
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selectorLabels,
					Annotations: map[string]string{
						localnetFingerprintAnno: plan.Fingerprint.Value,
					},
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: new(false),
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:      new(localnetToolsRunAsID),
						RunAsGroup:   new(localnetToolsRunAsID),
						RunAsNonRoot: new(true),
						RunAsUser:    new(localnetToolsRunAsID),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					ServiceAccountName: artifactPublisherServiceAccountName(network),
					InitContainers:     initContainers,
					Containers:         containers,
					Volumes:            append(volumes, artifactPublisherProjectedVolume()),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(network, deployment, b.scheme); err != nil {
		return nil, fmt.Errorf("set primary Deployment owner reference: %w", err)
	}

	return deployment, nil
}

func (b primaryWorkloadBuilder) cardanoNodeContainer(network *yacdv1alpha1.CardanoNetwork, plan localnet.Plan) corev1.Container {
	container := corev1.Container{
		Name:            cardanoNodeContainerName,
		Image:           b.cardanoNodeImage(network),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{cardanoNodeCommand},
		Args: []string{
			"run",
			"--config", plan.Layout.ConfigFile,
			"--topology", path.Join(plan.Layout.EnvDir, "node-data", "node1", "topology.json"),
			"--database-path", cardanoNodeDatabaseDir,
			"--socket-path", cardanoNodeSocketPath,
			"--host-addr", cardanoNodeHostAddress,
			"--port", strconv.Itoa(int(network.Spec.Node.Port)),
			"--shelley-kes-key", path.Join(plan.Layout.EnvDir, "pools-keys", "pool1", "kes.skey"),
			"--shelley-vrf-key", path.Join(plan.Layout.EnvDir, "pools-keys", "pool1", "vrf.skey"),
			"--shelley-operational-certificate", path.Join(plan.Layout.EnvDir, "pools-keys", "pool1", "opcert.cert"),
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          cardanoNodePortName,
				ContainerPort: network.Spec.Node.Port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      localnetStateVolumeName,
				MountPath: plan.Layout.StateDir,
			},
			{
				Name:      nodeIPCVolumeName,
				MountPath: cardanoNodeSocketDir,
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

	if network.Spec.Node.Resources != nil {
		container.Resources = *network.Spec.Node.Resources.DeepCopy()
	}

	return container
}

func (b primaryWorkloadBuilder) ogmiosContainer(settings ogmiosSettings, plan localnet.Plan) corev1.Container {
	container := corev1.Container{
		Name:            ogmiosContainerName,
		Image:           settings.image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{ogmiosCommand},
		Args: []string{
			"--node-socket", cardanoNodeSocketPath,
			"--node-config", plan.Layout.ConfigFile,
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
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      localnetStateVolumeName,
				MountPath: plan.Layout.StateDir,
				ReadOnly:  true,
			},
			{
				Name:      nodeIPCVolumeName,
				MountPath: cardanoNodeSocketDir,
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

func (b primaryWorkloadBuilder) cardanoNodeImage(network *yacdv1alpha1.CardanoNetwork) string {
	if network.Spec.Node.Image != nil {
		return strings.TrimSpace(*network.Spec.Node.Image)
	}

	return fmt.Sprintf("%s:%s-%s", cardanoTestnetImageRepository, strings.TrimSpace(network.Spec.Node.Version), cardanoTestnetImageRevision)
}

func (b primaryWorkloadBuilder) persistentVolumeClaim(network *yacdv1alpha1.CardanoNetwork, plan localnet.Plan) (*corev1.PersistentVolumeClaim, error) {
	persistentVolumeClaim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        primaryNodeStatePVCName(network),
			Namespace:   network.Namespace,
			Labels:      primaryWorkloadLabels(network),
			Annotations: persistentVolumeClaimAnnotations(network, plan),
		},
		Spec: b.persistentVolumeClaimSpec(network),
	}

	if err := controllerutil.SetControllerReference(network, persistentVolumeClaim, b.scheme); err != nil {
		return nil, fmt.Errorf("set primary PVC owner reference: %w", err)
	}

	return persistentVolumeClaim, nil
}

func (b primaryWorkloadBuilder) service(network *yacdv1alpha1.CardanoNetwork) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryWorkloadName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: primaryWorkloadSelectorLabels(network),
			Ports: []corev1.ServicePort{
				{
					Name:       cardanoNodePortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       network.Spec.Node.Port,
					TargetPort: intstr.FromString(cardanoNodePortName),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(network, service, b.scheme); err != nil {
		return nil, fmt.Errorf("set primary Service owner reference: %w", err)
	}

	return service, nil
}

func (b primaryWorkloadBuilder) ogmiosService(network *yacdv1alpha1.CardanoNetwork, settings ogmiosSettings) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryOgmiosServiceName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: primaryWorkloadSelectorLabels(network),
			Ports: []corev1.ServicePort{
				{
					Name:       ogmiosPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       settings.port,
					TargetPort: intstr.FromString(ogmiosPortName),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(network, service, b.scheme); err != nil {
		return nil, fmt.Errorf("set Ogmios Service owner reference: %w", err)
	}

	return service, nil
}

func (b primaryWorkloadBuilder) kupoService(network *yacdv1alpha1.CardanoNetwork, settings kupoSettings) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryKupoServiceName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: primaryWorkloadSelectorLabels(network),
			Ports: []corev1.ServicePort{
				{
					Name:       kupoPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       settings.port,
					TargetPort: intstr.FromString(kupoPortName),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(network, service, b.scheme); err != nil {
		return nil, fmt.Errorf("set Kupo Service owner reference: %w", err)
	}

	return service, nil
}

func (b primaryWorkloadBuilder) faucetService(network *yacdv1alpha1.CardanoNetwork, settings faucetSettings) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryFaucetServiceName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: primaryWorkloadSelectorLabels(network),
			Ports: []corev1.ServicePort{
				{
					Name:       faucetPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       settings.port,
					TargetPort: intstr.FromString(faucetPortName),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(network, service, b.scheme); err != nil {
		return nil, fmt.Errorf("set faucet Service owner reference: %w", err)
	}

	return service, nil
}

func (b primaryWorkloadBuilder) faucetAuthSecret(network *yacdv1alpha1.CardanoNetwork, settings faucetSettings) (*corev1.Secret, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      settings.authSecretName,
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		Type: corev1.SecretTypeOpaque,
	}

	if err := controllerutil.SetControllerReference(network, secret, b.scheme); err != nil {
		return nil, fmt.Errorf("set faucet auth Secret owner reference: %w", err)
	}

	return secret, nil
}

func persistentVolumeClaimAnnotations(network *yacdv1alpha1.CardanoNetwork, plan localnet.Plan) map[string]string {
	annotations := map[string]string{
		localnetFingerprintAnno: plan.Fingerprint.Value,
	}
	if network.Spec.Node.Storage != nil && network.Spec.Node.Storage.StorageClassName != nil {
		annotations[ctrlannotations.RequestedStorageClass] = *network.Spec.Node.Storage.StorageClassName
	}

	return annotations
}

func (b primaryWorkloadBuilder) persistentVolumeClaimSpec(network *yacdv1alpha1.CardanoNetwork) corev1.PersistentVolumeClaimSpec {
	storageSize := resource.MustParse(defaultNodeStorageSize)
	var storageClassName *string
	if network.Spec.Node.Storage != nil {
		storageSize = network.Spec.Node.Storage.Size
		storageClassName = network.Spec.Node.Storage.StorageClassName
	}

	return corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: storageSize,
			},
		},
		StorageClassName: storageClassName,
	}
}

