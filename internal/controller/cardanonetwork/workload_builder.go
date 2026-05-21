package cardanonetwork

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	primaryNodeNameSuffix = "node"

	cardanoNodeContainerName = "cardano-node"
	cardanoNodeCommand       = "cardano-node"
	cardanoNodeSocketDir     = "/ipc"
	cardanoNodeSocketPath    = "/ipc/node.socket"
	cardanoNodeDatabaseDir   = "/state/db"
	cardanoNodeHostAddress   = "0.0.0.0"

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

// primaryWorkloadResources are the Kubernetes resources that run the initial
// singleton primary Cardano node.
type primaryWorkloadResources struct {
	PersistentVolumeClaim *corev1.PersistentVolumeClaim
	Deployment            *appsv1.Deployment
}

// primaryWorkloadBuilder converts a CardanoNetwork into the desired primary
// node workload resources. Reconciliation side effects stay in the controller.
type primaryWorkloadBuilder struct {
	scheme *runtime.Scheme
}

type unsupportedSpecError struct {
	message string
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

	deployment, err := b.deployment(network, plan, initContainer)
	if err != nil {
		return nil, err
	}
	persistentVolumeClaim, err := b.persistentVolumeClaim(network, plan)
	if err != nil {
		return nil, err
	}

	return &primaryWorkloadResources{
		PersistentVolumeClaim: persistentVolumeClaim,
		Deployment:            deployment,
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

func (b primaryWorkloadBuilder) deployment(network *yacdv1alpha1.CardanoNetwork, plan localnet.Plan, initContainer corev1.Container) (*appsv1.Deployment, error) {
	selectorLabels := primaryWorkloadSelectorLabels(network)
	labels := primaryWorkloadLabels(network)
	deploymentName := primaryWorkloadName(network)

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
					Containers: []corev1.Container{
						b.cardanoNodeContainer(network, plan),
					},
					Volumes: []corev1.Volume{
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
					},
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
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	if network.Spec.Node.Resources != nil {
		container.Resources = *network.Spec.Node.Resources.DeepCopy()
	}

	return container
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
