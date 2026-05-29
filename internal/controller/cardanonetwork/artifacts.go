package cardanonetwork

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	cardanonetworkartifacts "github.com/meigma/yacd/internal/cardano/networkartifacts"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	ctrlnetworkartifacts "github.com/meigma/yacd/internal/controller/networkartifacts"
	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Artifact publisher constants. Env vars are read by the
// /opt/yacd/bin/yacd-cardano-testnet-init wrapper inside the tools image;
// projected volume paths match what client-go reads off the standard
// in-cluster ServiceAccount mount.
const (
	// artifactConfigMapNameEnv carries the artifact ConfigMap name into the
	// init container.
	artifactConfigMapNameEnv = "YACD_ARTIFACT_CONFIGMAP_NAME"

	// artifactPublisherServiceAccountMountDir is where the in-Pod projected
	// volume mounts the per-network token, root CA, and namespace files.
	// Client-go discovers these by convention.
	artifactPublisherServiceAccountMountDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	// artifactPublisherTokenVolumeName names the projected volume.
	artifactPublisherTokenVolumeName = "artifact-publisher-token"
	// artifactPublisherTokenPath / CAPath / NamespacePath are the file names
	// projected inside artifactPublisherServiceAccountMountDir.
	artifactPublisherTokenPath     = "token"
	artifactPublisherCAPath        = "ca.crt"
	artifactPublisherNamespacePath = "namespace"

	// Env vars consumed by the artifact publisher to populate the artifact
	// ConfigMap content. The init container fills them from spec/identity at
	// pod startup.
	artifactNetworkNameEnv      = "YACD_CARDANO_NETWORK_NAME"
	artifactNetworkNamespaceEnv = "YACD_CARDANO_NETWORK_NAMESPACE"
	artifactNetworkModeEnv      = "YACD_CARDANO_NETWORK_MODE"
	artifactNetworkEraEnv       = "YACD_CARDANO_NETWORK_ERA"
	artifactNodeToNodeHostEnv   = "YACD_CARDANO_NODE_TO_NODE_HOST"
	artifactNodeToNodePortEnv   = "YACD_CARDANO_NODE_TO_NODE_PORT"
	artifactNodeToNodeURLEnv    = "YACD_CARDANO_NODE_TO_NODE_URL"

	// artifactPublisherTokenTTL bounds the lifetime of the projected
	// ServiceAccount token. The token only needs to live long enough for
	// the init container to patch the artifact ConfigMap once, so a short
	// TTL keeps the security footprint small.
	artifactPublisherTokenTTL int64 = 3600
)

// networkArtifactsConfigMap builds the empty artifact ConfigMap the init
// container later patches with the generated network artifacts. The
// fingerprint annotation lets downstream verification reject a ConfigMap
// produced for a different localnet shape.
func (b primaryWorkloadBuilder) networkArtifactsConfigMap(network *yacdv1alpha1.CardanoNetwork, plan primaryNetworkPlan) (*corev1.ConfigMap, error) {
	annotations := map[string]string{
		networkFingerprintAnno: plan.Fingerprint,
	}
	if localnetFingerprint := plan.localnetFingerprint(); localnetFingerprint != "" {
		annotations[localnetFingerprintAnno] = localnetFingerprint
	}
	data := map[string]string(nil)
	if len(plan.ArtifactData) > 0 {
		data = make(map[string]string, len(plan.ArtifactData))
		maps.Copy(data, plan.ArtifactData)
		annotations[ctrlannotations.ArtifactSchemaVersion] = cardanonetworkartifacts.SchemaVersion
		annotations[ctrlannotations.ArtifactDataHash] = plan.ArtifactDataHash
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        networkArtifactsConfigMapName(network),
			Namespace:   network.Namespace,
			Labels:      primaryWorkloadLabels(network),
			Annotations: annotations,
		},
		Data: data,
	}
	if err := controllerutil.SetControllerReference(network, configMap, b.scheme); err != nil {
		return nil, fmt.Errorf("set network artifacts ConfigMap owner reference: %w", err)
	}

	return configMap, nil
}

// artifactPublisherServiceAccount builds the dedicated ServiceAccount the
// init container uses to patch the artifact ConfigMap. Pod-level token
// automount is disabled because only the init container needs a token, and
// it receives a short-lived projected token through artifactPublisherProjectedVolume.
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

// artifactPublisherRole builds the namespaced Role granting get/patch on
// exactly the network artifact ConfigMap. resourceNames is the security
// boundary: the init container cannot read or mutate any other ConfigMap
// in the namespace.
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

// artifactPublisherRoleBinding binds the artifact publisher Role to the
// matching ServiceAccount.
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

// artifactPublisherProjectedVolume builds the projected volume that mounts
// a scoped, short-lived ServiceAccount token, the cluster root CA, and the
// namespace file into the init container. Using a projected volume (instead
// of leaning on automounted Pod tokens) lets us bypass pod-level automount
// while still giving the init container the API client materials it needs.
func artifactPublisherProjectedVolume() corev1.Volume {
	// 0444 read-only file mode; the init container only reads the files.
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

// artifactPublisherVolumeMount returns the read-only init-container mount
// for the projected artifact publisher volume. Pairs with
// artifactPublisherProjectedVolume.
func artifactPublisherVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      artifactPublisherTokenVolumeName,
		MountPath: artifactPublisherServiceAccountMountDir,
		ReadOnly:  true,
	}
}

// artifactConfigMapNeedsRecovery reports whether the live artifact
// ConfigMap fails the producer-side verification and should be deleted and
// recreated. Thin wrapper around the shared networkartifacts predicate so
// the apply path reads naturally.
func artifactConfigMapNeedsRecovery(configMap *corev1.ConfigMap, expectedFingerprint string) bool {
	return ctrlnetworkartifacts.ProducerConfigMapNeedsRecovery(configMap, expectedFingerprint)
}

// setDeploymentArtifactConfigMapUID stamps the live artifact ConfigMap UID
// onto the Deployment pod-template annotations. The UID changes after a bounded
// recovery delete+create, which rolls the primary Pod through the standard
// Deployment hash-change path and forces the init container to re-publish.
func setDeploymentArtifactConfigMapUID(deployment *appsv1.Deployment, configMap *corev1.ConfigMap) {
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno] = string(configMap.UID)
}

// setDeploymentFaucetAuthTokenHash stamps the live faucet auth token hash
// onto the Deployment pod template so token creation or rotation rolls the
// primary Pod through Kubernetes' normal ReplicaSet machinery.
func setDeploymentFaucetAuthTokenHash(deployment *appsv1.Deployment, secret *corev1.Secret) {
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations[faucetAuthTokenHashAnno] = faucetAuthTokenHash(secret)
}

func setDeploymentNetworkArtifactsRecoveryRolloutAt(deployment *appsv1.Deployment, at time.Time) {
	if deployment.Annotations == nil {
		deployment.Annotations = map[string]string{}
	}
	deployment.Annotations[networkArtifactsRecoveryRolloutAtAnno] = at.UTC().Format(time.RFC3339)
}

func (r *CardanoNetworkReconciler) networkArtifactsRecoveryCooldownRemaining(
	ctx context.Context,
	desiredDeployment *appsv1.Deployment,
	now time.Time,
) (time.Duration, error) {
	current, err := r.currentPrimaryDeployment(ctx, desiredDeployment)
	if err != nil || current == nil {
		return 0, err
	}
	last, ok := networkArtifactsRecoveryRolloutAt(current.Annotations)
	if !ok {
		return 0, nil
	}
	elapsed := now.Sub(last)
	if elapsed < 0 {
		return networkArtifactsRecoveryCooldown, nil
	}
	if elapsed >= networkArtifactsRecoveryCooldown {
		return 0, nil
	}
	return networkArtifactsRecoveryCooldown - elapsed, nil
}

func (r *CardanoNetworkReconciler) preserveDeploymentArtifactConfigMapUID(
	ctx context.Context,
	desiredDeployment *appsv1.Deployment,
) error {
	current, err := r.currentPrimaryDeployment(ctx, desiredDeployment)
	if err != nil || current == nil {
		return err
	}
	uid := strings.TrimSpace(current.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno])
	if uid == "" {
		return nil
	}
	if desiredDeployment.Spec.Template.Annotations == nil {
		desiredDeployment.Spec.Template.Annotations = map[string]string{}
	}
	desiredDeployment.Spec.Template.Annotations[networkArtifactsConfigMapUIDAnno] = uid

	return nil
}

func (r *CardanoNetworkReconciler) currentPrimaryDeployment(
	ctx context.Context,
	desiredDeployment *appsv1.Deployment,
) (*appsv1.Deployment, error) {
	current := &appsv1.Deployment{}
	if err := r.Get(ctx, ctrlmetadata.ObjectKey(desiredDeployment), current); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if err := validateControllerOwner(current, desiredDeployment); err != nil {
		return nil, err
	}

	return current, nil
}

func networkArtifactsRecoveryRolloutAt(annotations map[string]string) (time.Time, bool) {
	value := strings.TrimSpace(annotations[networkArtifactsRecoveryRolloutAtAnno])
	if value == "" {
		return time.Time{}, false
	}
	rolloutAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}

	return rolloutAt, true
}
