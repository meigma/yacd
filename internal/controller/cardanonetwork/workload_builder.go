package cardanonetwork

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	primaryNodeNameSuffix = "node"

	cardanoNodeContainerName = "cardano-node"
	cardanoNodeCommand       = "cardano-node"
	cardanoNodePortName      = "node-to-node"
	cardanoNodeSocketDir     = "/ipc"
	cardanoNodeSocketPath    = "/ipc/node.socket"
	cardanoNodeDatabaseDir   = "/state/db"
	cardanoNodeHostAddress   = "0.0.0.0"

	ogmiosContainerName  = "ogmios"
	ogmiosCommand        = "/bin/ogmios"
	ogmiosPortName       = "ogmios"
	ogmiosHostAddress    = "0.0.0.0"
	defaultOgmiosImage   = "cardanosolutions/ogmios:v6.14.0"
	defaultOgmiosPort    = 1337
	ogmiosHealthPath     = "/health"
	ogmiosServiceURLType = "ws"

	kupoContainerName       = "kupo"
	kupoPortName            = "kupo"
	kupoHostAddress         = "0.0.0.0"
	kupoOgmiosHostAddress   = "127.0.0.1"
	kupoWorkDir             = "/kupo"
	kupoDBVolumeName        = "kupo-db"
	kupoTmpDir              = "/tmp"
	kupoTmpVolumeName       = "kupo-tmp"
	defaultKupoImage        = "cardanosolutions/kupo:v2.11.0"
	defaultKupoPort         = 1442
	defaultKupoSince        = "origin"
	defaultKupoMatchPattern = "*/*"
	defaultKupoDBSizeLimit  = "1Gi"
	defaultKupoTmpSizeLimit = "256Mi"
	defaultKupoStorageLimit = "1536Mi"
	kupoServiceURLType      = "http"

	nodeIPCVolumeName         = "node-ipc"
	defaultNodeStorageSize    = "10Gi"
	localnetFingerprintAnno   = "yacd.meigma.io/localnet-fingerprint"
	requestedStorageClassAnno = "yacd.meigma.io/requested-storage-class"
	maxLabelValueLength       = 63
	safeNameHashLength        = 10

	labelAppName         = "app.kubernetes.io/name"
	labelAppInstance     = "app.kubernetes.io/instance"
	labelAppComponent    = "app.kubernetes.io/component"
	labelAppManagedBy    = "app.kubernetes.io/managed-by"
	labelCardanoNetwork  = "yacd.meigma.io/cardanonetwork"
	labelCardanoRole     = "yacd.meigma.io/role"
	labelPrimaryNodeName = "cardano-node"
	labelPrimaryRole     = "primary-node"

	// localnetStateDir is the durable state mount root used by the first
	// CardanoNetwork workload shape.
	localnetStateDir = "/state"

	// localnetEnvDir is the cardano-testnet create-env output directory used by
	// the first CardanoNetwork workload shape.
	localnetEnvDir = "/state/env"
)

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

// primaryWorkloadResources are the Kubernetes resources that run the initial
// singleton primary Cardano node.
type primaryWorkloadResources struct {
	PersistentVolumeClaim *corev1.PersistentVolumeClaim
	Deployment            *appsv1.Deployment
	Service               *corev1.Service
	OgmiosService         *corev1.Service
	KupoService           *corev1.Service
}

// primaryWorkloadBuilder converts a CardanoNetwork into the desired primary
// node workload resources. Reconciliation side effects stay in the controller.
type primaryWorkloadBuilder struct {
	scheme *runtime.Scheme
}

type unsupportedSpecError struct {
	message string
}

type ogmiosSettings struct {
	enabled   bool
	image     string
	port      int32
	resources *corev1.ResourceRequirements
}

type kupoSettings struct {
	enabled   bool
	image     string
	port      int32
	resources *corev1.ResourceRequirements
}

func (e unsupportedSpecError) Error() string {
	return e.message
}

func unsupportedSpec(format string, args ...any) unsupportedSpecError {
	return unsupportedSpecError{message: fmt.Sprintf(format, args...)}
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

	initContainer, err := b.cardanoTestnetInitContainer(plan)
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
	if !acceptedLocalnetFingerprintChanged(network, plan.Fingerprint.Value) {
		err = validateOgmiosCompatibility(network.Spec.Node.Version, ogmios)
	}
	if err != nil {
		return nil, err
	}
	if err := validateKupoImage(kupo); err != nil {
		return nil, err
	}
	if err := validatePrimaryWorkloadPorts(network.Spec.Node.Port, ogmios, kupo); err != nil {
		return nil, err
	}

	deployment, err := b.deployment(network, plan, initContainer, ogmios, kupo)
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

	return &primaryWorkloadResources{
		PersistentVolumeClaim: persistentVolumeClaim,
		Deployment:            deployment,
		Service:               service,
		OgmiosService:         ogmiosService,
		KupoService:           kupoService,
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

func resolveOgmiosSettings(network *yacdv1alpha1.CardanoNetwork) (ogmiosSettings, error) {
	settings := ogmiosSettings{
		enabled: true,
		image:   defaultOgmiosImage,
		port:    defaultOgmiosPort,
	}
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Ogmios == nil {
		return settings, nil
	}

	spec := network.Spec.ChainAPI.Ogmios
	if !spec.Enabled {
		settings.enabled = false
		return settings, nil
	}

	settings.image = strings.TrimSpace(spec.Image)
	if settings.image == "" {
		return ogmiosSettings{}, unsupportedSpec("ogmios image is required")
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return ogmiosSettings{}, unsupportedSpec("ogmios port must be between 1 and 65535")
	}
	settings.port = spec.Port
	if spec.Resources != nil {
		settings.resources = spec.Resources.DeepCopy()
	}

	return settings, nil
}

func resolveKupoSettings(network *yacdv1alpha1.CardanoNetwork, ogmios ogmiosSettings) (kupoSettings, error) {
	settings := kupoSettings{
		enabled: true,
		image:   defaultKupoImage,
		port:    defaultKupoPort,
	}
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Kupo == nil {
		if !ogmios.enabled {
			settings.enabled = false
		}
		return settings, nil
	}

	spec := network.Spec.ChainAPI.Kupo
	if !spec.Enabled {
		settings.enabled = false
		return settings, nil
	}

	settings.image = strings.TrimSpace(spec.Image)
	if settings.image == "" {
		return kupoSettings{}, unsupportedSpec("kupo image is required")
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return kupoSettings{}, unsupportedSpec("kupo port must be between 1 and 65535")
	}
	settings.port = spec.Port
	if spec.Resources != nil {
		settings.resources = spec.Resources.DeepCopy()
	}

	return settings, nil
}

func validateKupoImage(settings kupoSettings) error {
	if !settings.enabled {
		return nil
	}
	if settings.image == defaultKupoImage {
		return nil
	}

	return unsupportedSpec("kupo image %q is not supported; supported image: %s", settings.image, defaultKupoImage)
}

func validatePrimaryWorkloadPorts(nodePort int32, ogmios ogmiosSettings, kupo kupoSettings) error {
	seen := map[int32]string{
		nodePort: cardanoNodePortName,
	}
	if ogmios.enabled {
		if owner, ok := seen[ogmios.port]; ok {
			return unsupportedSpec("ogmios port %d conflicts with %s port", ogmios.port, owner)
		}
		seen[ogmios.port] = ogmiosPortName
	}
	if kupo.enabled {
		if owner, ok := seen[kupo.port]; ok {
			return unsupportedSpec("kupo port %d conflicts with %s port", kupo.port, owner)
		}
	}

	return nil
}

func validateOgmiosCompatibility(nodeVersion string, settings ogmiosSettings) error {
	if !settings.enabled {
		return nil
	}

	compatibilityKey, err := ogmiosCompatibilityKey(settings.image)
	if err != nil {
		return err
	}

	supportedNodeVersions, ok := supportedOgmiosNodeVersions[compatibilityKey]
	if !ok {
		return unsupportedSpec("ogmios image tag %q is not supported", mustContainerImageTag(settings.image))
	}

	nodeVersion = strings.TrimSpace(nodeVersion)
	if slices.Contains(supportedNodeVersions, nodeVersion) {
		return nil
	}

	return unsupportedSpec(
		"ogmios %s.* is not supported with cardano-node %s; supported cardano-node versions: %s",
		compatibilityKey,
		nodeVersion,
		strings.Join(supportedNodeVersions, ", "),
	)
}

func acceptedLocalnetFingerprintChanged(network *yacdv1alpha1.CardanoNetwork, localnetFingerprint string) bool {
	return network.Status.Network != nil &&
		network.Status.Network.LocalnetFingerprint != "" &&
		network.Status.Network.LocalnetFingerprint != localnetFingerprint
}

func ogmiosCompatibilityKey(image string) (string, error) {
	tag, ok := containerImageTag(image)
	if !ok {
		return "", unsupportedSpec("ogmios image %q must include a supported release tag", image)
	}
	if !strings.HasPrefix(tag, "v") {
		return "", unsupportedSpec("ogmios image tag %q is not a supported release tag", tag)
	}

	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(parts) != 3 {
		return "", unsupportedSpec("ogmios image tag %q is not a supported release tag", tag)
	}
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return "", unsupportedSpec("ogmios image tag %q is not a supported release tag", tag)
		}
	}

	return "v" + parts[0] + "." + parts[1], nil
}

func containerImageTag(image string) (string, bool) {
	withoutDigest, _, _ := strings.Cut(strings.TrimSpace(image), "@")
	lastSlash := strings.LastIndex(withoutDigest, "/")
	lastColon := strings.LastIndex(withoutDigest, ":")
	if lastColon <= lastSlash || lastColon == len(withoutDigest)-1 {
		return "", false
	}

	return withoutDigest[lastColon+1:], true
}

func mustContainerImageTag(image string) string {
	tag, ok := containerImageTag(image)
	if !ok {
		return ""
	}

	return tag
}

func (b primaryWorkloadBuilder) deployment(network *yacdv1alpha1.CardanoNetwork, plan localnet.Plan, initContainer corev1.Container, ogmios ogmiosSettings, kupo kupoSettings) (*appsv1.Deployment, error) {
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
					InitContainers: []corev1.Container{initContainer},
					Containers:     containers,
					Volumes:        volumes,
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

func resourceQuantity(value string) *resource.Quantity {
	quantity := resource.MustParse(value)
	return &quantity
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

func persistentVolumeClaimAnnotations(network *yacdv1alpha1.CardanoNetwork, plan localnet.Plan) map[string]string {
	annotations := map[string]string{
		localnetFingerprintAnno: plan.Fingerprint.Value,
	}
	if network.Spec.Node.Storage != nil && network.Spec.Node.Storage.StorageClassName != nil {
		annotations[requestedStorageClassAnno] = *network.Spec.Node.Storage.StorageClassName
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

func primaryWorkloadName(network *yacdv1alpha1.CardanoNetwork) string {
	return safeDNSLabelWithSuffix(network.Name, primaryNodeNameSuffix)
}

func primaryNodeStatePVCName(network *yacdv1alpha1.CardanoNetwork) string {
	return safeDNSLabelWithSuffix(network.Name, "node-state")
}

func primaryOgmiosServiceName(network *yacdv1alpha1.CardanoNetwork) string {
	return safeDNSLabelWithSuffix(network.Name, "ogmios")
}

func primaryKupoServiceName(network *yacdv1alpha1.CardanoNetwork) string {
	return safeDNSLabelWithSuffix(network.Name, "kupo")
}

func primaryWorkloadSelectorLabels(network *yacdv1alpha1.CardanoNetwork) map[string]string {
	instance := safeLabelValue(network.Name)

	return map[string]string{
		labelAppName:        labelPrimaryNodeName,
		labelAppInstance:    instance,
		labelAppComponent:   labelPrimaryRole,
		labelCardanoNetwork: instance,
		labelCardanoRole:    labelPrimaryRole,
	}
}

func primaryWorkloadLabels(network *yacdv1alpha1.CardanoNetwork) map[string]string {
	labels := primaryWorkloadSelectorLabels(network)
	labels[labelAppManagedBy] = "yacd"

	return labels
}

func safeDNSLabelWithSuffix(value string, suffix string) string {
	base := sanitizeDNSLabel(value)
	needsHash := base != value
	if base == "" {
		base = "x"
		needsHash = true
	}

	candidateSuffix := "-" + suffix
	if needsHash {
		candidateSuffix = fmt.Sprintf("-%s-%s", shortNameHash(value), suffix)
	}
	candidate := base + candidateSuffix
	if len(candidate) <= maxLabelValueLength {
		return candidate
	}

	hash := shortNameHash(value)
	hashSuffix := fmt.Sprintf("-%s-%s", hash, suffix)
	prefixLength := maxLabelValueLength - len(hashSuffix)
	prefix := strings.Trim(base[:prefixLength], "-")
	if prefix == "" {
		prefix = "x"
	}

	return prefix + hashSuffix
}

func safeLabelValue(value string) string {
	base := sanitizeLabelValue(value)
	if base == "" {
		base = shortNameHash(value)
	}
	if len(base) <= maxLabelValueLength {
		return base
	}

	hash := shortNameHash(value)
	hashSuffix := "-" + hash
	prefixLength := maxLabelValueLength - len(hashSuffix)
	prefix := strings.TrimRight(base[:prefixLength], "-_.")
	if prefix == "" {
		prefix = "x"
	}

	return prefix + hashSuffix
}

func sanitizeDNSLabel(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range strings.ToLower(value) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' {
			builder.WriteRune(char)
			continue
		}
		builder.WriteByte('-')
	}

	return strings.Trim(builder.String(), "-")
}

func sanitizeLabelValue(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' ||
			char == '_' ||
			char == '.' {
			builder.WriteRune(char)
			continue
		}
		builder.WriteByte('-')
	}

	return strings.Trim(builder.String(), "-_.")
}

func shortNameHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:safeNameHashLength]
}
