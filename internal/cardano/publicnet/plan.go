package publicnet

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

const (
	previewProfileName = "preview"
	defaultProfileDir  = "/profile"

	manifestSchemaVersion = "yacd.meigma.io/public-network-profile/v1alpha1"
	previewSourceURL      = "https://book.play.dev.cardano.org/environments/preview/"
)

//go:embed profiles/preview/*
var profileAssets embed.FS

var previewFiles = []profileFile{
	{artifactKey: networkartifacts.ConfigurationKey, assetPath: "config.json", connectionKey: "configuration"},
	{artifactKey: networkartifacts.ByronGenesisKey, assetPath: "byron-genesis.json", connectionKey: "byronGenesis"},
	{artifactKey: networkartifacts.ShelleyGenesisKey, assetPath: "shelley-genesis.json", connectionKey: "shelleyGenesis"},
	{artifactKey: networkartifacts.AlonzoGenesisKey, assetPath: "alonzo-genesis.json", connectionKey: "alonzoGenesis"},
	{artifactKey: networkartifacts.ConwayGenesisKey, assetPath: "conway-genesis.json", connectionKey: "conwayGenesis"},
	{artifactKey: networkartifacts.PrimaryTopologyKey, assetPath: "topology.json", connectionKey: "primaryTopology"},
	{artifactKey: networkartifacts.CheckpointsKey, assetPath: "checkpoints.json", connectionKey: "checkpoints"},
	{artifactKey: networkartifacts.PeerSnapshotKey, assetPath: "peer-snapshot.json", connectionKey: "peerSnapshot"},
}

type previewConfig struct {
	RequiresNetworkMagic string `json:"RequiresNetworkMagic"`
}

type previewShelleyGenesis struct {
	NetworkMagic int64 `json:"networkMagic"`
}

// BuildPlan normalizes spec and assembles the deterministic public profile
// plan. Slice 1 intentionally supports only the checked-in preview bundle.
func BuildPlan(spec Spec) (Plan, error) {
	spec.Profile = strings.TrimSpace(spec.Profile)
	if spec.Profile != previewProfileName {
		if spec.Profile == "" {
			return Plan{}, fmt.Errorf("public profile is required")
		}
		return Plan{}, fmt.Errorf("public profile %q is not supported", spec.Profile)
	}
	if strings.TrimSpace(spec.Paths.ProfileDir) == "" {
		spec.Paths.ProfileDir = defaultProfileDir
	}
	spec.Paths.ProfileDir = path.Clean(spec.Paths.ProfileDir)
	if spec.Paths.ProfileDir == "." || !strings.HasPrefix(spec.Paths.ProfileDir, "/") {
		return Plan{}, fmt.Errorf("public profile mount dir must be an absolute path")
	}

	artifacts, err := loadPreviewArtifacts()
	if err != nil {
		return Plan{}, err
	}

	networkMagic, err := previewNetworkMagic(artifacts[networkartifacts.ShelleyGenesisKey])
	if err != nil {
		return Plan{}, err
	}
	requiresNetworkMagic, err := previewRequiresNetworkMagic(artifacts[networkartifacts.ConfigurationKey])
	if err != nil {
		return Plan{}, err
	}

	input := fingerprintInput{
		SchemaVersion:        manifestSchemaVersion,
		Profile:              previewProfileName,
		NetworkMagic:         networkMagic,
		RequiresNetworkMagic: requiresNetworkMagic,
		Files:                make([]fingerprintInputFile, 0, len(artifacts)),
	}
	for _, file := range previewFiles {
		input.Files = append(input.Files, fingerprintInputFile{
			Key:    file.artifactKey,
			SHA256: contentHash(artifacts[file.artifactKey]),
		})
	}
	fingerprint, err := computeFingerprint(input)
	if err != nil {
		return Plan{}, fmt.Errorf("compute public profile fingerprint: %w", err)
	}

	manifest := Manifest{
		SchemaVersion:        manifestSchemaVersion,
		Profile:              previewProfileName,
		NetworkMagic:         networkMagic,
		RequiresNetworkMagic: requiresNetworkMagic,
		Files:                previewConnectionFiles(),
		Source:               previewSourceURL,
		Fingerprint:          fingerprint,
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Plan{}, fmt.Errorf("marshal public profile manifest: %w", err)
	}
	artifacts[networkartifacts.PublicProfileManifestKey] = string(manifestJSON) + "\n"

	return Plan{
		Spec: spec,
		Layout: Layout{
			ProfileDir:   spec.Paths.ProfileDir,
			ConfigFile:   path.Join(spec.Paths.ProfileDir, networkartifacts.ConfigurationKey),
			TopologyFile: path.Join(spec.Paths.ProfileDir, networkartifacts.PrimaryTopologyKey),
		},
		Profile:              previewProfileName,
		NetworkMagic:         networkMagic,
		RequiresNetworkMagic: requiresNetworkMagic,
		Fingerprint:          fingerprint,
		Manifest:             manifest,
		Artifacts:            artifacts,
	}, nil
}

func loadPreviewArtifacts() (map[string]string, error) {
	artifacts := make(map[string]string, len(previewFiles))
	for _, file := range previewFiles {
		raw, err := profileAssets.ReadFile(path.Join("profiles", previewProfileName, file.assetPath))
		if err != nil {
			return nil, fmt.Errorf("read preview profile asset %s: %w", file.assetPath, err)
		}
		artifacts[file.artifactKey] = string(raw)
	}
	return artifacts, nil
}

func previewNetworkMagic(shelleyGenesis string) (int64, error) {
	var genesis previewShelleyGenesis
	if err := json.Unmarshal([]byte(shelleyGenesis), &genesis); err != nil {
		return 0, fmt.Errorf("parse preview shelley genesis: %w", err)
	}
	if genesis.NetworkMagic != 2 {
		return 0, fmt.Errorf("preview shelley genesis network magic %d is not supported", genesis.NetworkMagic)
	}
	return genesis.NetworkMagic, nil
}

func previewRequiresNetworkMagic(config string) (bool, error) {
	var nodeConfig previewConfig
	if err := json.Unmarshal([]byte(config), &nodeConfig); err != nil {
		return false, fmt.Errorf("parse preview node config: %w", err)
	}
	switch nodeConfig.RequiresNetworkMagic {
	case "RequiresMagic":
		return true, nil
	case "RequiresNoMagic":
		return false, nil
	default:
		return false, fmt.Errorf("preview RequiresNetworkMagic value %q is not supported", nodeConfig.RequiresNetworkMagic)
	}
}

func previewConnectionFiles() map[string]string {
	files := make(map[string]string, len(previewFiles)+2)
	for _, file := range previewFiles {
		files[file.connectionKey] = file.artifactKey
	}
	files["publicProfile"] = networkartifacts.PublicProfileManifestKey
	files["connection"] = networkartifacts.ConnectionKey
	return files
}
