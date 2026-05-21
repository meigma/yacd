package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CardanoNetworkMode selects whether YACD creates a fresh local network or
// joins a public/external profile.
// +kubebuilder:validation:Enum=local;public
type CardanoNetworkMode string

const (
	// CardanoNetworkModeLocal tells YACD to generate and own a local devnet.
	CardanoNetworkModeLocal CardanoNetworkMode = "local"
	// CardanoNetworkModePublic tells YACD to join a public or supplied network profile.
	CardanoNetworkModePublic CardanoNetworkMode = "public"
)

// CardanoEra selects the newest ledger era used by a generated local devnet.
// +kubebuilder:validation:Enum=babbage;conway
type CardanoEra string

const (
	// CardanoEraBabbage starts the local network in Babbage.
	CardanoEraBabbage CardanoEra = "babbage"
	// CardanoEraConway starts the local network in Conway.
	CardanoEraConway CardanoEra = "conway"
)

// PublicNetworkProfile names a known public Cardano network profile or a
// caller-supplied profile bundle.
// +kubebuilder:validation:Enum=preprod;preview;mainnet;custom
type PublicNetworkProfile string

const (
	// PublicNetworkProfilePreprod joins the Cardano preprod testnet.
	PublicNetworkProfilePreprod PublicNetworkProfile = "preprod"
	// PublicNetworkProfilePreview joins the Cardano preview testnet.
	PublicNetworkProfilePreview PublicNetworkProfile = "preview"
	// PublicNetworkProfileMainnet joins Cardano mainnet.
	PublicNetworkProfileMainnet PublicNetworkProfile = "mainnet"
	// PublicNetworkProfileCustom joins a supplied network profile bundle.
	PublicNetworkProfileCustom PublicNetworkProfile = "custom"
)

// GenesisProfile chooses a curated local genesis preset. Custom genesis tuning
// should grow here after the first controller slice proves the generated path.
// +kubebuilder:validation:Enum=default;zero-fee;zero-min-utxo;zero-fee-and-min-utxo
type GenesisProfile string

const (
	// GenesisProfileDefault uses YACD's normal local development defaults.
	GenesisProfileDefault GenesisProfile = "default"
	// GenesisProfileZeroFee removes transaction fees for fast local tests.
	GenesisProfileZeroFee GenesisProfile = "zero-fee"
	// GenesisProfileZeroMinUTxOValue removes the minimum UTxO value.
	GenesisProfileZeroMinUTxOValue GenesisProfile = "zero-min-utxo"
	// GenesisProfileZeroFeeAndMinUTxOValue removes fees and minimum UTxO value.
	GenesisProfileZeroFeeAndMinUTxOValue GenesisProfile = "zero-fee-and-min-utxo"
)

// CardanoNetworkSpec defines the desired state of a Cardano development network.
// +kubebuilder:validation:XValidation:rule="self.mode == 'local' ? has(self.local) && !has(self.public) : self.mode == 'public' ? has(self.public) && !has(self.local) : false",message="mode must match exactly one of spec.local or spec.public"
type CardanoNetworkSpec struct {
	// mode selects whether this network is generated locally or joins a public
	// profile.
	// +required
	Mode CardanoNetworkMode `json:"mode"`

	// node controls the primary Cardano node workload shared by local and
	// public modes.
	// +required
	Node CardanoNodeSpec `json:"node"`

	// local configures a generated local devnet. It is required when mode is
	// local and forbidden otherwise.
	// +optional
	Local *LocalNetworkSpec `json:"local,omitempty"`

	// public configures a node that joins a known or supplied public/external
	// network profile. It is required when mode is public and forbidden
	// otherwise.
	// +optional
	Public *PublicNetworkSpec `json:"public,omitempty"`

	// chainAPI configures network-facing APIs exposed next to the primary node.
	// Ogmios is enabled by default because it is the first supported chain API.
	// +optional
	ChainAPI *ChainAPISpec `json:"chainAPI,omitempty"`
}

// CardanoNodeSpec configures the primary cardano-node workload.
type CardanoNodeSpec struct {
	// version selects the cardano-node release used by the managed node image.
	// +kubebuilder:default="11.0.1"
	// +required
	Version string `json:"version"`

	// image optionally overrides the full cardano-node image reference. When
	// omitted, the controller derives an image from version.
	// +optional
	Image *string `json:"image,omitempty"`

	// port is the node-to-node TCP port exposed by the node Service.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=3001
	// +required
	Port int32 `json:"port"`

	// storage configures persistent cardano-node database storage.
	// +optional
	Storage *NodeStorageSpec `json:"storage,omitempty"`

	// resources configures the primary cardano-node container resources.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// NodeStorageSpec configures persistent node database storage.
type NodeStorageSpec struct {
	// size is the requested persistent volume size for the node database.
	// +kubebuilder:default="10Gi"
	// +required
	Size resource.Quantity `json:"size"`

	// storageClassName optionally selects the Kubernetes StorageClass used for
	// the node database PVC.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// LocalNetworkSpec configures a YACD-generated local Cardano devnet.
type LocalNetworkSpec struct {
	// networkMagic is the testnet magic used by local node and client commands.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=42
	// +required
	NetworkMagic int64 `json:"networkMagic"`

	// era selects the newest ledger era for the generated network.
	// +kubebuilder:default=conway
	// +required
	Era CardanoEra `json:"era"`

	// timing controls slot and epoch duration for the generated network.
	// +required
	Timing LocalNetworkTimingSpec `json:"timing"`

	// topology describes the initial local network topology. The first
	// controller slice only reconciles one primary node, but this keeps the CRD
	// environment-shaped instead of node-shaped.
	// +required
	Topology LocalNetworkTopologySpec `json:"topology"`

	// genesis configures local genesis material generated by YACD.
	// +optional
	Genesis *LocalGenesisSpec `json:"genesis,omitempty"`
}

// LocalNetworkTimingSpec controls local devnet timing.
type LocalNetworkTimingSpec struct {
	// slotLength is the local network slot duration.
	// +kubebuilder:default="100ms"
	// +required
	SlotLength metav1.Duration `json:"slotLength"`

	// epochLength is the number of slots in an epoch.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=500
	// +required
	EpochLength int64 `json:"epochLength"`
}

// LocalNetworkTopologySpec describes generated local topology.
type LocalNetworkTopologySpec struct {
	// pools configures generated stake pool nodes.
	// +required
	Pools LocalPoolTopologySpec `json:"pools"`
}

// LocalPoolTopologySpec configures generated stake pool topology.
type LocalPoolTopologySpec struct {
	// count is the number of generated pool nodes. The initial controller may
	// reject values above one until multi-node reconciliation is implemented.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +required
	Count int32 `json:"count"`

	// defaults configures shared pool economics for generated pools.
	// +optional
	Defaults *LocalPoolDefaultsSpec `json:"defaults,omitempty"`
}

// LocalPoolDefaultsSpec configures shared generated pool economics.
type LocalPoolDefaultsSpec struct {
	// costLovelace is the fixed pool cost used for generated pools.
	// +kubebuilder:validation:Minimum=0
	// +optional
	CostLovelace *int64 `json:"costLovelace,omitempty"`

	// pledgeLovelace is the pool pledge used for generated pools.
	// +kubebuilder:validation:Minimum=0
	// +optional
	PledgeLovelace *int64 `json:"pledgeLovelace,omitempty"`

	// margin is the pool margin as a decimal string in the inclusive range
	// [0, 1]. It is a string to avoid exposing floating point behavior in the
	// Kubernetes API.
	// +kubebuilder:validation:Pattern=`^(0(\.[0-9]+)?|1(\.0+)?)$`
	// +optional
	Margin *string `json:"margin,omitempty"`
}

// LocalGenesisSpec configures local genesis generation.
type LocalGenesisSpec struct {
	// profile selects a curated genesis preset.
	// +kubebuilder:default=default
	// +required
	Profile GenesisProfile `json:"profile"`

	// securityParameter sets the generated Shelley security parameter.
	// +kubebuilder:validation:Minimum=1
	// +optional
	SecurityParameter *int32 `json:"securityParameter,omitempty"`

	// maxLovelaceSupply sets the generated Shelley max lovelace supply.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxLovelaceSupply *int64 `json:"maxLovelaceSupply,omitempty"`

	// delegatedSupply is the lovelace supply initially delegated to generated
	// pools.
	// +kubebuilder:validation:Minimum=0
	// +optional
	DelegatedSupply *int64 `json:"delegatedSupply,omitempty"`

	// protocolVersion sets the initial protocol version in generated genesis
	// material.
	// +optional
	ProtocolVersion *ProtocolVersionSpec `json:"protocolVersion,omitempty"`
}

// ProtocolVersionSpec describes a Cardano protocol version.
type ProtocolVersionSpec struct {
	// major is the protocol major version.
	// +kubebuilder:validation:Minimum=0
	// +required
	Major int32 `json:"major"`

	// minor is the protocol minor version.
	// +kubebuilder:validation:Minimum=0
	// +required
	Minor int32 `json:"minor"`
}

// PublicNetworkSpec configures a node that joins a public or supplied network profile.
// +kubebuilder:validation:XValidation:rule="self.profile == 'custom' ? has(self.configSource) : !has(self.configSource)",message="configSource is required only when public.profile is custom"
type PublicNetworkSpec struct {
	// profile selects the public network profile.
	// +required
	Profile PublicNetworkProfile `json:"profile"`

	// configSource supplies network config and genesis files for custom public
	// profiles.
	// +optional
	ConfigSource *NetworkConfigSource `json:"configSource,omitempty"`
}

// NetworkConfigSource identifies the in-cluster object that supplies custom
// profile files. The expected bundle keys are config.json, topology.json,
// byron-genesis.json, shelley-genesis.json, alonzo-genesis.json, and
// conway-genesis.json.
// +kubebuilder:validation:XValidation:rule="(has(self.configMapRef) && !has(self.secretRef) && size(self.configMapRef.name) > 0) || (has(self.secretRef) && !has(self.configMapRef) && size(self.secretRef.name) > 0)",message="exactly one of configMapRef or secretRef must be set with a non-empty name"
type NetworkConfigSource struct {
	// configMapRef loads profile files from a ConfigMap in the same namespace.
	// +optional
	ConfigMapRef *corev1.LocalObjectReference `json:"configMapRef,omitempty"`

	// secretRef loads profile files from a Secret in the same namespace.
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

// ChainAPISpec configures APIs exposed for clients and supporting services.
type ChainAPISpec struct {
	// ogmios configures the Ogmios sidecar and Service.
	// +optional
	Ogmios *OgmiosSpec `json:"ogmios,omitempty"`
}

// OgmiosSpec configures the default Ogmios chain API.
type OgmiosSpec struct {
	// enabled controls whether the Ogmios sidecar is deployed.
	// +kubebuilder:default=true
	// +required
	Enabled bool `json:"enabled"`

	// image is the Ogmios image reference.
	// +kubebuilder:default="cardanosolutions/ogmios:v6.14.0"
	// +required
	Image string `json:"image"`

	// port is the Ogmios service port.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=1337
	// +required
	Port int32 `json:"port"`

	// resources configures the Ogmios container resources.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// CardanoNetworkStatus defines the observed state of CardanoNetwork.
type CardanoNetworkStatus struct {
	// observedGeneration is the most recent generation observed by the
	// controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// network captures resolved network identity once the controller has
	// generated or loaded chain material.
	// +optional
	Network *CardanoNetworkIdentityStatus `json:"network,omitempty"`

	// endpoints publishes cluster-local connection details for clients and
	// supporting controllers.
	// +optional
	Endpoints *CardanoNetworkEndpointsStatus `json:"endpoints,omitempty"`

	// conditions represent the current state of the CardanoNetwork resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Expected condition types include:
	// - "Ready": the network is usable through its published endpoints
	// - "NodeReady": the primary node is running and queryable
	// - "OgmiosReady": Ogmios is enabled and serving health checks
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// CardanoNetworkIdentityStatus reports resolved network identity.
type CardanoNetworkIdentityStatus struct {
	// mode is the resolved network mode.
	// +optional
	Mode CardanoNetworkMode `json:"mode,omitempty"`

	// localnetFingerprint is the accepted fingerprint for generated localnet
	// inputs. Changing those inputs requires deleting and recreating the
	// CardanoNetwork.
	// +optional
	LocalnetFingerprint string `json:"localnetFingerprint,omitempty"`

	// networkMagic is the resolved Cardano network magic.
	// +optional
	NetworkMagic *int64 `json:"networkMagic,omitempty"`

	// profile is the resolved public network profile, when mode is public.
	// +optional
	Profile *PublicNetworkProfile `json:"profile,omitempty"`

	// era is the newest resolved ledger era known to the controller.
	// +optional
	Era *CardanoEra `json:"era,omitempty"`
}

// CardanoNetworkEndpointsStatus reports discovered Service endpoints.
type CardanoNetworkEndpointsStatus struct {
	// nodeToNode is the primary node-to-node endpoint.
	// +optional
	NodeToNode *ServiceEndpointStatus `json:"nodeToNode,omitempty"`

	// ogmios is the Ogmios JSON/RPC endpoint.
	// +optional
	Ogmios *ServiceEndpointStatus `json:"ogmios,omitempty"`
}

// ServiceEndpointStatus reports a cluster-local Service endpoint.
type ServiceEndpointStatus struct {
	// serviceName is the Kubernetes Service name.
	// +optional
	ServiceName string `json:"serviceName,omitempty"`

	// port is the Service port.
	// +optional
	Port int32 `json:"port,omitempty"`

	// url is a convenience URL for protocols with a stable URL shape.
	// +optional
	URL string `json:"url,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CardanoNetwork is the Schema for the cardanonetworks API.
type CardanoNetwork struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of CardanoNetwork
	// +required
	Spec CardanoNetworkSpec `json:"spec"`

	// status defines the observed state of CardanoNetwork
	// +optional
	Status CardanoNetworkStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// CardanoNetworkList contains a list of CardanoNetwork.
type CardanoNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []CardanoNetwork `json:"items"`
}
