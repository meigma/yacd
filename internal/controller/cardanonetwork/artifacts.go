package cardanonetwork

import (
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	ctrlnames "github.com/meigma/yacd/internal/ctrlkit/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
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

func artifactConfigMapNeedsRecovery(configMap *corev1.ConfigMap, expectedFingerprint string) bool {
	return networkartifacts.ProducerConfigMapNeedsRecovery(configMap, expectedFingerprint)
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
	return ctrlnames.DNSLabelWithSuffix(network.Name, "network-artifacts")
}

func artifactPublisherServiceAccountName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "artifact-publisher")
}

func artifactPublisherRoleName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "artifact-publisher")
}

func artifactPublisherRoleBindingName(network *yacdv1alpha1.CardanoNetwork) string {
	return ctrlnames.DNSLabelWithSuffix(network.Name, "artifact-publisher")
}
