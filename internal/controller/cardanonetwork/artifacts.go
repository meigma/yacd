package cardanonetwork

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	networkArtifactSchemaVersion = "yacd.meigma.io/cardano-network-artifacts/v1alpha1"

	networkArtifactSchemaVersionAnno = "yacd.meigma.io/artifact-schema-version"
	networkArtifactDataHashAnno      = "yacd.meigma.io/artifact-data-hash"
	networkArtifactsConfigMapUIDAnno = "yacd.meigma.io/network-artifacts-configmap-uid"
	artifactConfigMapNameEnv         = "YACD_ARTIFACT_CONFIGMAP_NAME"

	artifactPublisherServiceAccountMountDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	artifactPublisherTokenVolumeName        = "artifact-publisher-token"
	artifactPublisherTokenPath              = "token"
	artifactPublisherCAPath                 = "ca.crt"
	artifactPublisherNamespacePath          = "namespace"

	artifactNetworkNameEnv            = "YACD_CARDANO_NETWORK_NAME"
	artifactNetworkNamespaceEnv       = "YACD_CARDANO_NETWORK_NAMESPACE"
	artifactNetworkModeEnv            = "YACD_CARDANO_NETWORK_MODE"
	artifactNetworkEraEnv             = "YACD_CARDANO_NETWORK_ERA"
	artifactNodeToNodeHostEnv         = "YACD_CARDANO_NODE_TO_NODE_HOST"
	artifactNodeToNodePortEnv         = "YACD_CARDANO_NODE_TO_NODE_PORT"
	artifactNodeToNodeURLEnv          = "YACD_CARDANO_NODE_TO_NODE_URL"
	artifactPublisherTokenTTL   int64 = 3600
)

var requiredNetworkArtifactKeys = []string{
	"configuration.yaml",
	"byron-genesis.json",
	"shelley-genesis.json",
	"alonzo-genesis.json",
	"conway-genesis.json",
	"primary-topology.json",
	"yacd-localnet-plan.json",
	"connection.json",
}

var optionalNetworkArtifactKeys = []string{
	"dijkstra-genesis.json",
}

type networkArtifactsStatusResult struct {
	status yacdv1alpha1.CardanoNetworkArtifactsStatus
	ready  bool
	reason string
}

func (b primaryWorkloadBuilder) networkArtifactsConfigMap(network *yacdv1alpha1.CardanoNetwork, localnetFingerprint string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      networkArtifactsConfigMapName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
			Annotations: map[string]string{
				localnetFingerprintAnno: localnetFingerprint,
			},
		},
	}
	if err := controllerutil.SetControllerReference(network, configMap, b.scheme); err != nil {
		return nil, fmt.Errorf("set network artifacts ConfigMap owner reference: %w", err)
	}

	return configMap, nil
}

func (b primaryWorkloadBuilder) artifactPublisherServiceAccount(network *yacdv1alpha1.CardanoNetwork) (*corev1.ServiceAccount, error) {
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      artifactPublisherServiceAccountName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		AutomountServiceAccountToken: new(false),
	}
	if err := controllerutil.SetControllerReference(network, serviceAccount, b.scheme); err != nil {
		return nil, fmt.Errorf("set artifact publisher ServiceAccount owner reference: %w", err)
	}

	return serviceAccount, nil
}

func (b primaryWorkloadBuilder) artifactPublisherRole(network *yacdv1alpha1.CardanoNetwork) (*rbacv1.Role, error) {
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      artifactPublisherRoleName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{""},
				Resources:     []string{"configmaps"},
				ResourceNames: []string{networkArtifactsConfigMapName(network)},
				Verbs:         []string{"get", "patch"},
			},
		},
	}
	if err := controllerutil.SetControllerReference(network, role, b.scheme); err != nil {
		return nil, fmt.Errorf("set artifact publisher Role owner reference: %w", err)
	}

	return role, nil
}

func (b primaryWorkloadBuilder) artifactPublisherRoleBinding(network *yacdv1alpha1.CardanoNetwork) (*rbacv1.RoleBinding, error) {
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      artifactPublisherRoleBindingName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     artifactPublisherRoleName(network),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      artifactPublisherServiceAccountName(network),
				Namespace: network.Namespace,
			},
		},
	}
	if err := controllerutil.SetControllerReference(network, roleBinding, b.scheme); err != nil {
		return nil, fmt.Errorf("set artifact publisher RoleBinding owner reference: %w", err)
	}

	return roleBinding, nil
}

func artifactPublisherProjectedVolume() corev1.Volume {
	defaultMode := int32(0444)
	expirationSeconds := artifactPublisherTokenTTL
	return corev1.Volume{
		Name: artifactPublisherTokenVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				DefaultMode: &defaultMode,
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							ExpirationSeconds: &expirationSeconds,
							Path:              artifactPublisherTokenPath,
						},
					},
					{
						ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"},
							Items: []corev1.KeyToPath{
								{Key: "ca.crt", Path: artifactPublisherCAPath},
							},
						},
					},
					{
						DownwardAPI: &corev1.DownwardAPIProjection{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									Path: artifactPublisherNamespacePath,
									FieldRef: &corev1.ObjectFieldSelector{
										APIVersion: "v1",
										FieldPath:  "metadata.namespace",
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func artifactPublisherVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      artifactPublisherTokenVolumeName,
		MountPath: artifactPublisherServiceAccountMountDir,
		ReadOnly:  true,
	}
}

func artifactConfigMapStatus(configMap *corev1.ConfigMap, expectedFingerprint string) networkArtifactsStatusResult {
	if configMap == nil {
		return networkArtifactsStatusResult{
			reason: "artifact ConfigMap is missing",
		}
	}

	status := yacdv1alpha1.CardanoNetworkArtifactsStatus{
		NetworkConfigMapName: configMap.Name,
	}

	if !configMap.DeletionTimestamp.IsZero() {
		return networkArtifactsStatusResult{
			status: status,
			reason: "artifact ConfigMap is deleting",
		}
	}

	if configMap.Annotations[networkArtifactSchemaVersionAnno] != networkArtifactSchemaVersion {
		return networkArtifactsStatusResult{
			status: status,
			reason: "artifact ConfigMap schema version is not published",
		}
	}
	status.SchemaVersion = networkArtifactSchemaVersion

	if configMap.Annotations[localnetFingerprintAnno] != expectedFingerprint {
		return networkArtifactsStatusResult{
			status: status,
			reason: "artifact ConfigMap localnet fingerprint does not match the accepted localnet",
		}
	}

	dataHash := strings.TrimSpace(configMap.Annotations[networkArtifactDataHashAnno])
	if !validNetworkArtifactDataHash(dataHash) {
		return networkArtifactsStatusResult{
			status: status,
			reason: "artifact ConfigMap data hash is not published",
		}
	}

	if len(configMap.BinaryData) > 0 {
		return networkArtifactsStatusResult{
			status: status,
			reason: "artifact ConfigMap contains binary data",
		}
	}
	if key, ok := unsupportedNetworkArtifactDataKey(configMap.Data); ok {
		return networkArtifactsStatusResult{
			status: status,
			reason: fmt.Sprintf("artifact ConfigMap contains unsupported key %s", key),
		}
	}

	for _, key := range requiredNetworkArtifactKeys {
		if _, ok := configMap.Data[key]; !ok {
			return networkArtifactsStatusResult{
				status: status,
				reason: fmt.Sprintf("artifact ConfigMap is missing %s", key),
			}
		}
	}

	actualHash := computeNetworkArtifactDataHash(configMap.Data)
	if dataHash != actualHash {
		return networkArtifactsStatusResult{
			status: status,
			reason: "artifact ConfigMap data hash does not match data",
		}
	}
	status.DataHash = dataHash

	return networkArtifactsStatusResult{
		status: status,
		ready:  true,
	}
}

func artifactConfigMapNeedsRecovery(configMap *corev1.ConfigMap, expectedFingerprint string) bool {
	if configMap == nil || !artifactConfigMapHasPublishedData(configMap) {
		return false
	}

	return !artifactConfigMapStatus(configMap, expectedFingerprint).ready
}

func artifactConfigMapHasPublishedData(configMap *corev1.ConfigMap) bool {
	if configMap.Annotations[networkArtifactSchemaVersionAnno] != "" ||
		configMap.Annotations[networkArtifactDataHashAnno] != "" {
		return true
	}

	return len(configMap.Data) > 0
}

func unsupportedNetworkArtifactDataKey(data map[string]string) (string, bool) {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if !networkArtifactDataKeyAllowed(key) {
			return key, true
		}
	}

	return "", false
}

func networkArtifactDataKeyAllowed(key string) bool {
	return slices.Contains(requiredNetworkArtifactKeys, key) ||
		slices.Contains(optionalNetworkArtifactKeys, key)
}

func validNetworkArtifactDataHash(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return false
	}
	for _, char := range strings.TrimPrefix(value, "sha256:") {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}

	return true
}

func computeNetworkArtifactDataHash(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	digest := sha256.New()
	for _, key := range keys {
		value := data[key]
		_, _ = fmt.Fprintf(digest, "%d:%s\n%d:", len(key), key, len(value))
		_, _ = io.WriteString(digest, value)
		_, _ = io.WriteString(digest, "\n")
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func setDeploymentArtifactConfigMapUID(deployment *appsv1.Deployment, configMap *corev1.ConfigMap) {
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno] = string(configMap.UID)
}

func nodeToNodeHost(network *yacdv1alpha1.CardanoNetwork) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", primaryWorkloadName(network), network.Namespace)
}

func nodeToNodeURL(network *yacdv1alpha1.CardanoNetwork) string {
	return fmt.Sprintf("tcp://%s:%d", nodeToNodeHost(network), network.Spec.Node.Port)
}

func networkArtifactsConfigMapName(network *yacdv1alpha1.CardanoNetwork) string {
	return safeDNSLabelWithSuffix(network.Name, "network-artifacts")
}

func artifactPublisherServiceAccountName(network *yacdv1alpha1.CardanoNetwork) string {
	return safeDNSLabelWithSuffix(network.Name, "artifact-publisher")
}

func artifactPublisherRoleName(network *yacdv1alpha1.CardanoNetwork) string {
	return safeDNSLabelWithSuffix(network.Name, "artifact-publisher")
}

func artifactPublisherRoleBindingName(network *yacdv1alpha1.CardanoNetwork) string {
	return safeDNSLabelWithSuffix(network.Name, "artifact-publisher")
}
