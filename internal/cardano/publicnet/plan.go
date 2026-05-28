package publicnet

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

const (
	previewProfileName = "preview"
	preprodProfileName = "preprod"
	mainnetProfileName = "mainnet"
	customProfileName  = "custom"

	defaultProfileDir = "/profile"

	manifestSchemaVersion = "yacd.meigma.io/public-network-profile/v1alpha1"

	operationsBookNodeRelease = "11.0.1"
	customProfileSource       = "custom"

	defaultMithrilClientImage       = "ghcr.io/input-output-hk/mithril-client:main-2478748"
	defaultMithrilSnapshot          = "latest"
	releaseMainnetMithrilAggregator = "https://aggregator.release-mainnet.api.mithril.network/aggregator"
)

//go:embed profiles/preview/* profiles/preprod/* profiles/mainnet/*
var profileAssets embed.FS

var requiredProfileFiles = []profileFile{
	{artifactKey: networkartifacts.ConfigurationKey, assetPath: "config.json", connectionKey: "configuration"},
	{artifactKey: networkartifacts.ByronGenesisKey, assetPath: "byron-genesis.json", connectionKey: "byronGenesis"},
	{artifactKey: networkartifacts.ShelleyGenesisKey, assetPath: "shelley-genesis.json", connectionKey: "shelleyGenesis"},
	{artifactKey: networkartifacts.AlonzoGenesisKey, assetPath: "alonzo-genesis.json", connectionKey: "alonzoGenesis"},
	{artifactKey: networkartifacts.ConwayGenesisKey, assetPath: "conway-genesis.json", connectionKey: "conwayGenesis"},
	{artifactKey: networkartifacts.PrimaryTopologyKey, assetPath: "topology.json", connectionKey: "primaryTopology"},
}

var optionalProfileFiles = []profileFile{
	{artifactKey: networkartifacts.CheckpointsKey, assetPath: "checkpoints.json", connectionKey: "checkpoints"},
	{artifactKey: networkartifacts.PeerSnapshotKey, assetPath: "peer-snapshot.json", connectionKey: "peerSnapshot"},
}

var mithrilProfileFiles = []profileFile{
	{artifactKey: networkartifacts.MithrilGenesisKey, assetPath: "mithril-genesis.vkey", connectionKey: "mithrilGenesisVerificationKey"},
	{artifactKey: networkartifacts.MithrilAncillaryKey, assetPath: "mithril-ancillary.vkey", connectionKey: "mithrilAncillaryVerificationKey"},
}

var curatedProfiles = map[string]profileDefinition{
	previewProfileName: {
		name:                  previewProfileName,
		assetDir:              previewProfileName,
		source:                "https://book.play.dev.cardano.org/environments/preview/",
		compatibleNodeRelease: operationsBookNodeRelease,
		optionalFiles:         optionalProfileFiles,
	},
	preprodProfileName: {
		name:                  preprodProfileName,
		assetDir:              preprodProfileName,
		source:                "https://book.play.dev.cardano.org/environments/preprod/",
		compatibleNodeRelease: operationsBookNodeRelease,
		optionalFiles: []profileFile{
			{artifactKey: networkartifacts.PeerSnapshotKey, assetPath: "peer-snapshot.json", connectionKey: "peerSnapshot"},
		},
	},
	mainnetProfileName: {
		name:                  mainnetProfileName,
		assetDir:              mainnetProfileName,
		source:                "https://book.play.dev.cardano.org/environments/mainnet/",
		compatibleNodeRelease: operationsBookNodeRelease,
		optionalFiles:         append(optionalProfileFiles, mithrilProfileFiles...),
		mithril: &mithrilDefinition{
			aggregatorEndpoint:               releaseMainnetMithrilAggregator,
			genesisVerificationKeyArtifact:   networkartifacts.MithrilGenesisKey,
			ancillaryVerificationKeyArtifact: networkartifacts.MithrilAncillaryKey,
		},
	},
}

type profileDefinition struct {
	name                  string
	assetDir              string
	source                string
	compatibleNodeRelease string
	optionalFiles         []profileFile
	mithril               *mithrilDefinition
}

type mithrilDefinition struct {
	aggregatorEndpoint               string
	genesisVerificationKeyArtifact   string
	ancillaryVerificationKeyArtifact string
}

type nodeConfig struct {
	RequiresNetworkMagic string `json:"RequiresNetworkMagic"`
}

type shelleyGenesis struct {
	NetworkMagic int64 `json:"networkMagic"`
}

// BuildPlan normalizes spec and assembles the deterministic public profile
// plan.
func BuildPlan(spec Spec) (Plan, error) {
	profile := strings.TrimSpace(spec.Profile)
	if profile == "" {
		return Plan{}, fmt.Errorf("public profile is required")
	}

	profileDir, err := normalizeProfileDir(spec.Paths.ProfileDir)
	if err != nil {
		return Plan{}, err
	}
	spec.Profile = profile
	spec.Paths.ProfileDir = profileDir

	if err := validateBootstrapProfile(profile, spec.Bootstrap); err != nil {
		return Plan{}, err
	}

	var artifacts map[string]string
	var files []profileFile
	var definition profileDefinition
	source := customProfileSource
	compatibleNodeRelease := ""
	if profile == customProfileName {
		if spec.Custom == nil {
			return Plan{}, fmt.Errorf("public custom profile requires configSource files")
		}
		artifacts, files, err = customArtifacts(*spec.Custom)
		if err != nil {
			return Plan{}, err
		}
	} else {
		if spec.Custom != nil {
			return Plan{}, fmt.Errorf("public configSource is supported only for custom profiles")
		}
		var ok bool
		definition, ok = curatedProfiles[profile]
		if !ok {
			return Plan{}, fmt.Errorf("public profile %q is not supported", profile)
		}
		artifacts, files, err = loadCuratedArtifacts(definition)
		if err != nil {
			return Plan{}, err
		}
		source = definition.source
		compatibleNodeRelease = definition.compatibleNodeRelease
	}

	mithril, err := buildMithrilPlan(profile, spec.Bootstrap, definition, artifacts)
	if err != nil {
		return Plan{}, err
	}

	networkMagic, err := parseNetworkMagic(profile, artifacts[networkartifacts.ShelleyGenesisKey])
	if err != nil {
		return Plan{}, err
	}
	requiresNetworkMagic, err := parseRequiresNetworkMagic(profile, artifacts[networkartifacts.ConfigurationKey])
	if err != nil {
		return Plan{}, err
	}

	input := fingerprintInput{
		SchemaVersion:        manifestSchemaVersion,
		Profile:              profile,
		NetworkMagic:         networkMagic,
		RequiresNetworkMagic: requiresNetworkMagic,
		Files:                make([]fingerprintInputFile, 0, len(files)),
	}
	for _, file := range files {
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
		SchemaVersion:         manifestSchemaVersion,
		Profile:               profile,
		NetworkMagic:          networkMagic,
		RequiresNetworkMagic:  requiresNetworkMagic,
		Files:                 connectionFiles(files),
		Source:                source,
		CompatibleNodeRelease: compatibleNodeRelease,
		Fingerprint:           fingerprint,
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
		Profile:              profile,
		NetworkMagic:         networkMagic,
		RequiresNetworkMagic: requiresNetworkMagic,
		Fingerprint:          fingerprint,
		Manifest:             manifest,
		Artifacts:            artifacts,
		Mithril:              mithril,
	}, nil
}

// SupportedCustomProfileKeys returns the source keys accepted from a custom
// ConfigMap or Secret profile bundle.
func SupportedCustomProfileKeys() []string {
	keys := make([]string, 0, len(requiredProfileFiles)+len(optionalProfileFiles))
	for _, file := range requiredProfileFiles {
		keys = append(keys, file.assetPath)
	}
	for _, file := range optionalProfileFiles {
		keys = append(keys, file.assetPath)
	}
	return keys
}

func normalizeProfileDir(profileDir string) (string, error) {
	if strings.TrimSpace(profileDir) == "" {
		profileDir = defaultProfileDir
	}
	profileDir = path.Clean(profileDir)
	if profileDir == "." || !strings.HasPrefix(profileDir, "/") {
		return "", fmt.Errorf("public profile mount dir must be an absolute path")
	}
	return profileDir, nil
}

func validateBootstrapProfile(profile string, bootstrap *BootstrapSpec) error {
	hasMithril := bootstrap != nil && bootstrap.Mithril != nil
	switch profile {
	case mainnetProfileName:
		if !hasMithril {
			return fmt.Errorf("public mainnet profile requires mithril bootstrap")
		}
	case previewProfileName, preprodProfileName, customProfileName:
		if bootstrap != nil {
			return fmt.Errorf("public bootstrap is supported only for mainnet")
		}
	}
	return nil
}

func buildMithrilPlan(profile string, bootstrap *BootstrapSpec, definition profileDefinition, artifacts map[string]string) (*MithrilPlan, error) {
	if profile != mainnetProfileName || bootstrap == nil || bootstrap.Mithril == nil {
		return nil, nil
	}
	if definition.mithril == nil {
		return nil, fmt.Errorf("public profile %q does not define mithril bootstrap data", profile)
	}

	image := strings.TrimSpace(bootstrap.Mithril.Image)
	if image == "" {
		image = defaultMithrilClientImage
	}
	snapshot := strings.TrimSpace(bootstrap.Mithril.Snapshot)
	if snapshot == "" {
		snapshot = defaultMithrilSnapshot
	}
	genesisKey := strings.TrimSpace(artifacts[definition.mithril.genesisVerificationKeyArtifact])
	if genesisKey == "" {
		return nil, fmt.Errorf("public profile %q mithril genesis verification key is missing", profile)
	}
	ancillaryKey := strings.TrimSpace(artifacts[definition.mithril.ancillaryVerificationKeyArtifact])
	if ancillaryKey == "" {
		return nil, fmt.Errorf("public profile %q mithril ancillary verification key is missing", profile)
	}

	bootstrap.Mithril.Image = image
	bootstrap.Mithril.Snapshot = snapshot

	return &MithrilPlan{
		Image:                    image,
		Snapshot:                 snapshot,
		AggregatorEndpoint:       definition.mithril.aggregatorEndpoint,
		GenesisVerificationKey:   genesisKey,
		AncillaryVerificationKey: ancillaryKey,
	}, nil
}

func loadCuratedArtifacts(definition profileDefinition) (map[string]string, []profileFile, error) {
	files := profileFiles(definition.optionalFiles)
	artifacts := make(map[string]string, len(files))
	for _, file := range files {
		raw, err := profileAssets.ReadFile(path.Join("profiles", definition.assetDir, file.assetPath))
		if err != nil {
			return nil, nil, fmt.Errorf("read %s profile asset %s: %w", definition.name, file.assetPath, err)
		}
		artifacts[file.artifactKey] = string(raw)
	}
	return artifacts, files, nil
}

func customArtifacts(bundle CustomBundle) (map[string]string, []profileFile, error) {
	if len(bundle.Files) == 0 {
		return nil, nil, fmt.Errorf("public custom profile bundle is empty")
	}

	supported := supportedProfileFilesBySourceKey()
	for key := range bundle.Files {
		if _, ok := supported[key]; !ok {
			return nil, nil, fmt.Errorf("public custom profile file %q is not supported", key)
		}
	}

	files := make([]profileFile, 0, len(requiredProfileFiles)+len(optionalProfileFiles))
	artifacts := make(map[string]string, len(requiredProfileFiles)+len(optionalProfileFiles))
	for _, file := range requiredProfileFiles {
		content, ok := bundle.Files[file.assetPath]
		if !ok {
			return nil, nil, fmt.Errorf("public custom profile file %q is required", file.assetPath)
		}
		if err := validateCustomFile(file.assetPath, content); err != nil {
			return nil, nil, err
		}
		files = append(files, file)
		artifacts[file.artifactKey] = content
	}
	for _, file := range optionalProfileFiles {
		content, ok := bundle.Files[file.assetPath]
		if !ok {
			continue
		}
		if err := validateCustomFile(file.assetPath, content); err != nil {
			return nil, nil, err
		}
		files = append(files, file)
		artifacts[file.artifactKey] = content
	}

	return artifacts, files, nil
}

func validateCustomFile(key string, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("public custom profile file %q must not be empty", key)
	}
	if !utf8.ValidString(content) {
		return fmt.Errorf("public custom profile file %q must be UTF-8 text", key)
	}
	return nil
}

func supportedProfileFilesBySourceKey() map[string]profileFile {
	files := make(map[string]profileFile, len(requiredProfileFiles)+len(optionalProfileFiles))
	for _, file := range requiredProfileFiles {
		files[file.assetPath] = file
	}
	for _, file := range optionalProfileFiles {
		files[file.assetPath] = file
	}
	return files
}

func profileFiles(optional []profileFile) []profileFile {
	files := make([]profileFile, 0, len(requiredProfileFiles)+len(optional))
	files = append(files, requiredProfileFiles...)
	files = append(files, optional...)
	return files
}

func parseNetworkMagic(profile string, shelleyGenesisJSON string) (int64, error) {
	var genesis shelleyGenesis
	if err := json.Unmarshal([]byte(shelleyGenesisJSON), &genesis); err != nil {
		return 0, fmt.Errorf("parse %s shelley genesis: %w", profile, err)
	}
	return genesis.NetworkMagic, nil
}

func parseRequiresNetworkMagic(profile string, configJSON string) (bool, error) {
	var config nodeConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return false, fmt.Errorf("parse %s node config: %w", profile, err)
	}
	switch config.RequiresNetworkMagic {
	case "RequiresMagic":
		return true, nil
	case "RequiresNoMagic":
		return false, nil
	default:
		return false, fmt.Errorf("%s RequiresNetworkMagic value %q is not supported", profile, config.RequiresNetworkMagic)
	}
}

func connectionFiles(files []profileFile) map[string]string {
	connectionFiles := make(map[string]string, len(files)+2)
	for _, file := range files {
		connectionFiles[file.connectionKey] = file.artifactKey
	}
	connectionFiles["publicProfile"] = networkartifacts.PublicProfileManifestKey
	connectionFiles["connection"] = networkartifacts.ConnectionKey
	return connectionFiles
}
