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

	nodeIPCVolumeName       = "node-ipc"
	defaultNodeStorageSize  = "10Gi"
	localnetFingerprintAnno = "yacd.meigma.io/localnet-fingerprint"
	maxLabelValueLength     = 63
	safeNameHashLength      = 10

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

// primaryWorkloadBuilder converts a CardanoNetwork into the initial primary
// node workload resources. This slice only builds the StatefulSet; reconciliation
// side effects stay in the controller.
type primaryWorkloadBuilder struct {
	scheme *runtime.Scheme
}

func (b primaryWorkloadBuilder) Build(network *yacdv1alpha1.CardanoNetwork) (*appsv1.StatefulSet, error) {
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
		return nil, err
	}

	initContainer, err := b.cardanoTestnetInitContainer(plan)
	if err != nil {
		return nil, err
	}

	statefulSet, err := b.statefulSet(network, plan, initContainer)
	if err != nil {
		return nil, err
	}

	return statefulSet, nil
}

func (b primaryWorkloadBuilder) localnetSpec(network *yacdv1alpha1.CardanoNetwork) (localnet.Spec, error) {
	nodeVersion := strings.TrimSpace(network.Spec.Node.Version)
	if nodeVersion == "" {
		return localnet.Spec{}, fmt.Errorf("node version is required")
	}
	if network.Spec.Node.Image != nil && strings.TrimSpace(*network.Spec.Node.Image) == "" {
		return localnet.Spec{}, fmt.Errorf("node image override must not be blank")
	}
	if network.Spec.Node.Port < 1 || network.Spec.Node.Port > 65535 {
		return localnet.Spec{}, fmt.Errorf("node port must be between 1 and 65535")
	}
	if network.Spec.Mode != yacdv1alpha1.CardanoNetworkModeLocal {
		return localnet.Spec{}, fmt.Errorf("mode %q is not supported", network.Spec.Mode)
	}
	if network.Spec.Local == nil {
		return localnet.Spec{}, fmt.Errorf("local spec is required")
	}
	if network.Spec.Public != nil {
		return localnet.Spec{}, fmt.Errorf("public spec is not supported with local mode")
	}

	local := network.Spec.Local
	if local.Era == yacdv1alpha1.CardanoEraBabbage {
		return localnet.Spec{}, fmt.Errorf("local era %q is not supported", local.Era)
	}
	if local.Genesis != nil {
		return localnet.Spec{}, fmt.Errorf("local genesis tuning is not supported")
	}
	if local.Topology.Pools.Count != 1 {
		return localnet.Spec{}, fmt.Errorf("local pool count %d is not supported", local.Topology.Pools.Count)
	}
	if local.Topology.Pools.Defaults != nil {
		return localnet.Spec{}, fmt.Errorf("local pool defaults are not supported")
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

func (b primaryWorkloadBuilder) statefulSet(network *yacdv1alpha1.CardanoNetwork, plan localnet.Plan, initContainer corev1.Container) (*appsv1.StatefulSet, error) {
	selectorLabels := primaryWorkloadSelectorLabels(network)
	labels := primaryWorkloadLabels(network)
	statefulSetName := primaryStatefulSetName(network)

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      statefulSetName,
			Namespace: network.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    new(int32(1)),
			ServiceName: statefulSetName,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  appsv1.RetainPersistentVolumeClaimRetentionPolicyType,
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
							Name: nodeIPCVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: b.volumeClaimTemplates(network),
		},
	}

	if err := controllerutil.SetControllerReference(network, statefulSet, b.scheme); err != nil {
		return nil, fmt.Errorf("set primary StatefulSet owner reference: %w", err)
	}

	return statefulSet, nil
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

func (b primaryWorkloadBuilder) volumeClaimTemplates(network *yacdv1alpha1.CardanoNetwork) []corev1.PersistentVolumeClaim {
	storageSize := resource.MustParse(defaultNodeStorageSize)
	var storageClassName *string
	if network.Spec.Node.Storage != nil {
		storageSize = network.Spec.Node.Storage.Size
		storageClassName = network.Spec.Node.Storage.StorageClassName
	}

	return []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: localnetStateVolumeName,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: storageSize,
					},
				},
				StorageClassName: storageClassName,
			},
		},
	}
}

func primaryStatefulSetName(network *yacdv1alpha1.CardanoNetwork) string {
	return safeDNSLabelWithSuffix(network.Name, primaryNodeNameSuffix)
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
	if base == "" {
		base = shortNameHash(value)
	}

	candidate := fmt.Sprintf("%s-%s", base, suffix)
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
