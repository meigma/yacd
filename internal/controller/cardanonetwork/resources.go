package cardanonetwork

import (
	"fmt"
	"maps"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlannotations "github.com/meigma/yacd/internal/controller/annotations"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Resource-construction internals shared by the Deployment and the owned
// Secrets.
const (
	// nodeIPCVolumeName is the EmptyDir volume cardano-node and ogmios share
	// for IPC socket communication.
	nodeIPCVolumeName = "node-ipc"

	// kupoDBVolumeName is the EmptyDir volume backing kupo's working database.
	kupoDBVolumeName = "kupo-db"

	// kupoTmpVolumeName is the EmptyDir scratch volume kupo writes /tmp into
	// so kupo can run with a read-only root filesystem.
	kupoTmpVolumeName = "kupo-tmp"

	// walletNameLabel marks an owned wallet Secret with its well-known name so
	// consumers (the CLI, dashboards) can select a specific wallet without
	// parsing the Secret name. The genesis-funded faucet wallet and CLI-managed
	// wallets share the same Secret shape; this label distinguishes them.
	walletNameLabel = "yacd.meigma.io/wallet-name"

	// walletSourceLabel records how a wallet Secret is funded. The faucet wallet
	// is allocated directly at genesis; CLI-managed wallets are funded by a
	// host-built transaction from another wallet.
	walletSourceLabel = "yacd.meigma.io/wallet-source"

	// faucetWalletName is the well-known wallet name for the genesis-funded
	// faucet wallet.
	faucetWalletName = "faucet"

	// walletSourceGenesisFunded is the walletSourceLabel value for a wallet
	// allocated at genesis.
	walletSourceGenesisFunded = "genesis-funded"
)

// deployment builds the primary workload Deployment. It composes the
// cardano-node container with the enabled optional sidecars (ogmios, kupo) and
// wires the init container that prepares the localnet environment.
// The RecreateDeploymentStrategyType prevents two cardano-node instances
// from running at once (they cannot share the underlying state PVC).
func (b primaryWorkloadBuilder) deployment(network *yacdv1alpha1.CardanoNetwork, plan primaryNetworkPlan, initContainer *corev1.Container, ogmios ogmiosSettings, kupo kupoSettings, faucetWallet faucetWalletSettings) (*appsv1.Deployment, error) {
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
	if b.dbSyncAttachment != nil {
		containers = append(containers, b.dbSyncAttachment.Container)
	}
	// The served-artifact producer (stage/fetch init) and the always-on serve
	// sidecar are wired for LOCAL and CURATED PUBLIC networks only;
	// custom-public is deferred out of this additive PR.
	serveArtifacts := plan.isLocal() || isCuratedPublicProfile(plan)
	if serveArtifacts {
		containers = append(containers, b.serveContainer(network, plan))
	}
	initContainers := make([]corev1.Container, 0, 5)
	if initContainer != nil {
		initContainers = append(initContainers, *initContainer)
	}
	// The genesis-funding init must run after create-env (which writes the
	// genesis) and before the LOCAL stage init below (which flattens the env
	// dir onto the served-artifact subdirectory), so the staged copy and the
	// node both see the funded genesis on the first reconcile.
	if faucetWallet.enabled {
		genesisFundingInit, err := b.faucetWalletGenesisFundingInitContainer(*plan.Localnet, faucetWallet, b.faucetWalletAddress)
		if err != nil {
			return nil, err
		}
		initContainers = append(initContainers, genesisFundingInit)
	}
	// Order matters: the LOCAL stage init reads the create-env output appended
	// above, and the CURATED PUBLIC fetch init must run before any Mithril
	// bootstrap appended below.
	if serveArtifacts {
		servedArtifactsInit, err := b.servedArtifactsInitContainer(network, plan)
		if err != nil {
			return nil, err
		}
		initContainers = append(initContainers, servedArtifactsInit)
	}
	if mithril := plan.mithrilBootstrap(); mithril != nil {
		initContainers = append(initContainers, b.mithrilBootstrapInitContainer(*mithril))
	}
	if b.dbSyncAttachment != nil {
		initContainers = append(initContainers, b.dbSyncAttachment.InitContainer)
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
	if b.dbSyncAttachment != nil {
		volumes = append(volumes, b.dbSyncAttachment.Volumes...)
	}
	if plan.mithrilBootstrap() != nil {
		volumes = append(volumes, corev1.Volume{
			Name: mithrilTmpVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}
	templateLabels := primaryWorkloadSelectorLabels(network)
	templateAnnotations := map[string]string{
		networkFingerprintAnno: plan.Fingerprint,
	}
	if localnetFingerprint := plan.localnetFingerprint(); localnetFingerprint != "" {
		templateAnnotations[localnetFingerprintAnno] = localnetFingerprint
	}
	if b.dbSyncAttachment != nil {
		maps.Copy(templateLabels, b.dbSyncAttachment.PodLabels)
		maps.Copy(templateAnnotations, b.dbSyncAttachment.PodAnnotations)
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
					Labels:      templateLabels,
					Annotations: templateAnnotations,
				},
				Spec: corev1.PodSpec{
					// The primary Pod needs no ServiceAccount token; disabling
					// automount keeps the API-access footprint minimal.
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
					InitContainers: initContainers,
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

// persistentVolumeClaim builds the primary node state PVC. Annotations carry
// the accepted localnet fingerprint and the requested storage class so PVC
// apply can detect and reject incompatible drift before mutating the live
// object.
func (b primaryWorkloadBuilder) persistentVolumeClaim(network *yacdv1alpha1.CardanoNetwork, plan primaryNetworkPlan) (*corev1.PersistentVolumeClaim, error) {
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

// ogmiosService builds the optional ogmios Service. It is ClusterIP by default
// and NodePort when the spec requests it.
func (b primaryWorkloadBuilder) ogmiosService(network *yacdv1alpha1.CardanoNetwork, settings ogmiosSettings) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryOgmiosServiceName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		Spec: corev1.ServiceSpec{
			Type:     settings.serviceType,
			Selector: primaryWorkloadSelectorLabels(network),
			Ports: []corev1.ServicePort{
				{
					Name:       ogmiosPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       settings.port,
					TargetPort: intstr.FromString(ogmiosPortName),
					NodePort:   pinnedNodePort(settings.serviceType, settings.nodePort),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(network, service, b.scheme); err != nil {
		return nil, fmt.Errorf("set Ogmios Service owner reference: %w", err)
	}

	return service, nil
}

// kupoService builds the optional kupo Service. It is ClusterIP by default and
// NodePort when the spec requests it.
func (b primaryWorkloadBuilder) kupoService(network *yacdv1alpha1.CardanoNetwork, settings kupoSettings) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryKupoServiceName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		Spec: corev1.ServiceSpec{
			Type:     settings.serviceType,
			Selector: primaryWorkloadSelectorLabels(network),
			Ports: []corev1.ServicePort{
				{
					Name:       kupoPortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       settings.port,
					TargetPort: intstr.FromString(kupoPortName),
					NodePort:   pinnedNodePort(settings.serviceType, settings.nodePort),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(network, service, b.scheme); err != nil {
		return nil, fmt.Errorf("set Kupo Service owner reference: %w", err)
	}

	return service, nil
}

// pinnedNodePort returns the node port to render on a chain API Service port.
// It returns the configured value only when the Service is NodePort: a non-zero
// value pins the node port, while 0 lets Kubernetes auto-assign (MutateService
// then preserves the assignment across reconciles). ClusterIP Services never
// carry a node port.
func pinnedNodePort(serviceType corev1.ServiceType, nodePort int32) int32 {
	if serviceType == corev1.ServiceTypeNodePort {
		return nodePort
	}

	return 0
}

// artifactsService builds the artifacts ClusterIP Service that exposes the
// always-on cardano-tools serve sidecar. It mirrors the chain API Services:
// the selector targets the primary node Pod labels and the single port maps to
// the serve container port.
func (b primaryWorkloadBuilder) artifactsService(network *yacdv1alpha1.CardanoNetwork) (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      primaryArtifactsServiceName(network),
			Namespace: network.Namespace,
			Labels:    primaryWorkloadLabels(network),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: primaryWorkloadSelectorLabels(network),
			Ports: []corev1.ServicePort{
				{
					Name:       servePortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       defaultServePort,
					TargetPort: intstr.FromString(servePortName),
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(network, service, b.scheme); err != nil {
		return nil, fmt.Errorf("set artifacts Service owner reference: %w", err)
	}

	return service, nil
}

// faucetWalletSecret builds the opaque Secret that carries the well-known
// faucet wallet's payment key envelopes and address. It shares the developer
// wallet's data shape and create-once contract; the marker labels identify it
// as the genesis-funded faucet wallet so consumers can select it directly. Like
// the other key-bearing Secrets, the data map is populated by the apply phase
// (the builder stays pure and cannot generate key material).
func (b primaryWorkloadBuilder) faucetWalletSecret(network *yacdv1alpha1.CardanoNetwork, settings faucetWalletSettings) (*corev1.Secret, error) {
	labels := primaryWorkloadLabels(network)
	labels[walletNameLabel] = faucetWalletName
	labels[walletSourceLabel] = walletSourceGenesisFunded

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      settings.secretName,
			Namespace: network.Namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
	}

	if err := controllerutil.SetControllerReference(network, secret, b.scheme); err != nil {
		return nil, fmt.Errorf("set faucet wallet Secret owner reference: %w", err)
	}

	return secret, nil
}

// persistentVolumeClaimAnnotations carries the accepted localnet fingerprint
// and (optionally) the requested storage class on the primary PVC. The PVC
// apply path validates these against the live object before allowing the
// patch.
func persistentVolumeClaimAnnotations(network *yacdv1alpha1.CardanoNetwork, plan primaryNetworkPlan) map[string]string {
	annotations := map[string]string{
		networkFingerprintAnno: plan.Fingerprint,
	}
	if localnetFingerprint := plan.localnetFingerprint(); localnetFingerprint != "" {
		annotations[localnetFingerprintAnno] = localnetFingerprint
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
	if isPublicMainnet(network) {
		storageSize = resource.MustParse(defaultMainnetNodeStorageSize)
	}
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
