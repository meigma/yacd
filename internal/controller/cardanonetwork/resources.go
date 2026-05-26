package cardanonetwork

import (
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Resource-construction internals shared by the Deployment and the faucet
// auth Secret.
const (
	// nodeIPCVolumeName is the EmptyDir volume cardano-node and ogmios share
	// for IPC socket communication.
	nodeIPCVolumeName = "node-ipc"

	// kupoDBVolumeName is the EmptyDir volume backing kupo's working database.
	kupoDBVolumeName = "kupo-db"

	// kupoTmpVolumeName is the EmptyDir scratch volume kupo writes /tmp into
	// so kupo can run with a read-only root filesystem.
	kupoTmpVolumeName = "kupo-tmp"

	// faucetAuthVolumeName is the Secret-backed volume that mounts the
	// faucet's auth token into its container.
	faucetAuthVolumeName = "faucet-auth"

	// faucetAuthTokenKey is the data key inside the faucet auth Secret that
	// carries the token. The Secret data map is shaped {faucetAuthTokenKey:
	// []byte(token)}.
	faucetAuthTokenKey = "token"
)

// deployment builds the primary workload Deployment. It composes the
// cardano-node container with the enabled optional sidecars (ogmios, kupo,
// faucet), wires the init container that prepares the localnet environment,
// and adds the artifact publisher's projected ServiceAccount token volume.
// The RecreateDeploymentStrategyType prevents two cardano-node instances
// from running at once (they cannot share the underlying state PVC).
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
		// The faucet auth token Secret is always present at apply time
		// because the apply orchestrator creates it before the Deployment
		// rolls; Optional=false fails the Pod fast if the token disappears.
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
					// Pod-level token automount is off; only the artifact
					// publisher's init container projects a scoped
					// ServiceAccount token via its own volume.
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

// persistentVolumeClaim builds the primary node state PVC. Annotations carry
// the accepted localnet fingerprint and the requested storage class so PVC
// apply can detect and reject incompatible drift before mutating the live
// object.
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

// service builds the primary node-to-node Service. It shares its name with
// the Deployment so node-to-node DNS resolution is predictable.
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

// ogmiosService builds the optional ogmios ClusterIP Service.
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

// kupoService builds the optional kupo ClusterIP Service.
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

// faucetService builds the optional faucet ClusterIP Service.
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

// faucetAuthSecret builds the opaque Secret that carries the faucet's auth
// token. The data map is populated by the apply phase (the builder cannot
// generate random material since it must stay pure).
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

// persistentVolumeClaimAnnotations carries the accepted localnet fingerprint
// and (optionally) the requested storage class on the primary PVC. The PVC
// apply path validates these against the live object before allowing the
// patch.
func persistentVolumeClaimAnnotations(network *yacdv1alpha1.CardanoNetwork, plan localnet.Plan) map[string]string {
	annotations := map[string]string{
		localnetFingerprintAnno: plan.Fingerprint.Value,
	}
	if network.Spec.Node.Storage != nil && network.Spec.Node.Storage.StorageClassName != nil {
		annotations[ctrlannotations.RequestedStorageClass] = *network.Spec.Node.Storage.StorageClassName
	}

	return annotations
}

// persistentVolumeClaimSpec builds the PVC spec from the CardanoNetwork
// storage configuration, defaulting size and leaving the storage class
// unspecified when the spec is silent.
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
