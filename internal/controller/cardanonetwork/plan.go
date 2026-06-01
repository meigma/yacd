package cardanonetwork

import (
	"path"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/localnet"
	"github.com/meigma/yacd/internal/cardano/publicnet"
)

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
	Localnet             *localnet.Plan
	Public               *publicnet.Plan
}

func (p primaryNetworkPlan) isLocal() bool {
	return p.Mode == yacdv1alpha1.CardanoNetworkModeLocal
}

func (p primaryNetworkPlan) isPublic() bool {
	return p.Mode == yacdv1alpha1.CardanoNetworkModePublic
}

func (p primaryNetworkPlan) mithrilBootstrap() *publicnet.MithrilPlan {
	if p.Public == nil {
		return nil
	}
	return p.Public.Mithril
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

func publicPrimaryNetworkPlan(plan publicnet.Plan) primaryNetworkPlan {
	profile := yacdv1alpha1.PublicNetworkProfile(plan.Profile)
	era := yacdv1alpha1.CardanoEraConway

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
		Public:               &plan,
	}
}
