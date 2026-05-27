package cardanonetwork

import (
	"encoding/json"
	"fmt"
	"maps"
	"path"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/meigma/yacd/internal/cardano/publicnet"
	ctrlartifacts "github.com/meigma/yacd/internal/ctrlkit/artifacts"
)

const publicProfileMountDir = "/profile"

type primaryNetworkPlan struct {
	Mode                 yacdv1alpha1.CardanoNetworkMode
	Profile              *yacdv1alpha1.PublicNetworkProfile
	NetworkMagic         int64
	RequiresNetworkMagic bool
	Era                  *yacdv1alpha1.CardanoEra
	Fingerprint          string
	ConfigFile           string
	TopologyFile         string
	StateDir             string
	ProfileDir           string
	ArtifactData         map[string]string
	ArtifactDataHash     string
	Localnet             *localnet.Plan
	Public               *publicnet.Plan
}

func (p primaryNetworkPlan) isLocal() bool {
	return p.Mode == yacdv1alpha1.CardanoNetworkModeLocal
}

func (p primaryNetworkPlan) isPublic() bool {
	return p.Mode == yacdv1alpha1.CardanoNetworkModePublic
}

func (p primaryNetworkPlan) localnetFingerprint() string {
	if p.Localnet == nil {
		return ""
	}
	return p.Localnet.Fingerprint.Value
}

func localPrimaryNetworkPlan(plan localnet.Plan, era yacdv1alpha1.CardanoEra) primaryNetworkPlan {
	return primaryNetworkPlan{
		Mode:         yacdv1alpha1.CardanoNetworkModeLocal,
		NetworkMagic: plan.Spec.NetworkMagic,
		Era:          &era,
		Fingerprint:  plan.Fingerprint.Value,
		ConfigFile:   plan.Layout.ConfigFile,
		TopologyFile: path.Join(plan.Layout.EnvDir, "node-data", "node1", "topology.json"),
		StateDir:     plan.Layout.StateDir,
		Localnet:     &plan,
	}
}

func publicPrimaryNetworkPlan(network *yacdv1alpha1.CardanoNetwork, plan publicnet.Plan) (primaryNetworkPlan, error) {
	profile := yacdv1alpha1.PublicNetworkProfile(plan.Profile)
	era := yacdv1alpha1.CardanoEraConway
	data := make(map[string]string, len(plan.Artifacts)+1)
	maps.Copy(data, plan.Artifacts)

	connectionJSON, err := publicConnectionJSON(network, plan)
	if err != nil {
		return primaryNetworkPlan{}, err
	}
	data[networkartifacts.ConnectionKey] = connectionJSON

	return primaryNetworkPlan{
		Mode:                 yacdv1alpha1.CardanoNetworkModePublic,
		Profile:              &profile,
		NetworkMagic:         plan.NetworkMagic,
		RequiresNetworkMagic: plan.RequiresNetworkMagic,
		Era:                  &era,
		Fingerprint:          plan.Fingerprint.Value,
		ConfigFile:           plan.Layout.ConfigFile,
		TopologyFile:         plan.Layout.TopologyFile,
		StateDir:             localnetStateDir,
		ProfileDir:           plan.Layout.ProfileDir,
		ArtifactData:         data,
		ArtifactDataHash:     ctrlartifacts.ComputeDataHash(data),
		Public:               &plan,
	}, nil
}

type publicConnectionDocument struct {
	SchemaVersion     string                   `json:"schemaVersion"`
	Network           publicConnectionNetwork  `json:"network"`
	PrimaryNodeToNode publicConnectionEndpoint `json:"primaryNodeToNode"`
	Files             map[string]string        `json:"files"`
}

type publicConnectionNetwork struct {
	Name                 string `json:"name"`
	Namespace            string `json:"namespace"`
	Mode                 string `json:"mode"`
	Profile              string `json:"profile"`
	NetworkMagic         int64  `json:"networkMagic"`
	RequiresNetworkMagic bool   `json:"requiresNetworkMagic"`
	Era                  string `json:"era"`
	NetworkFingerprint   string `json:"networkFingerprint"`
}

type publicConnectionEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	URL  string `json:"url"`
}

func publicConnectionJSON(network *yacdv1alpha1.CardanoNetwork, plan publicnet.Plan) (string, error) {
	doc := publicConnectionDocument{
		SchemaVersion: networkartifacts.SchemaVersion,
		Network: publicConnectionNetwork{
			Name:                 network.Name,
			Namespace:            network.Namespace,
			Mode:                 string(yacdv1alpha1.CardanoNetworkModePublic),
			Profile:              plan.Profile,
			NetworkMagic:         plan.NetworkMagic,
			RequiresNetworkMagic: plan.RequiresNetworkMagic,
			Era:                  string(yacdv1alpha1.CardanoEraConway),
			NetworkFingerprint:   plan.Fingerprint.Value,
		},
		PrimaryNodeToNode: publicConnectionEndpoint{
			Host: nodeToNodeHost(network),
			Port: int(network.Spec.Node.Port),
			URL:  nodeToNodeURL(network),
		},
		Files: plan.Manifest.Files,
	}

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal public connection.json: %w", err)
	}
	return string(raw) + "\n", nil
}
