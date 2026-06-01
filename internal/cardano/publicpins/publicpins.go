// Package publicpins is the single source of truth for YACD's curated public
// Cardano network profiles: which files each profile is composed of, where each
// is fetched from, and (for the files YACD pins) the sha256 digest that
// authenticates it.
//
// Both the operator (internal/cardano/publicnet, to build the profile manifest
// and fingerprint) and the cardano-tools fetch verb (to download and verify the
// files at init time) import this package, so the controller's published
// manifest and the in-cluster fetch agree on exactly one definition. Keeping it
// controller-free and dependency-light — it imports only the networkartifacts
// key contract — lets both sides share it without an import cycle.
//
// Trust model. config.json carries a pinned digest and is the trust anchor:
// cardano-node verifies the Byron/Shelley/Alonzo/Conway genesis and the
// checkpoints file against the hashes recorded inside config.json at startup,
// so pinning config.json transitively anchors those files without a separate
// per-file pin. topology.json is pinned because it is not referenced by
// config.json, so a silent upstream change to peer selection should surface as
// a loud, reviewable fetch failure rather than be trusted blindly. The Mithril
// verification keys are pinned because they anchor snapshot verification and
// are likewise not referenced from config.json.
//
// The genesis and checkpoints files are downloaded but intentionally NOT pinned
// here: they are authenticated downstream by cardano-node against config.json's
// inline hashes, which is how node operators normally bootstrap from the
// operations book. peer-snapshot is unpinned and optional — it advances
// continuously with the chain (so any pin would be stale immediately) and only
// seeds peer discovery, not chain validity. Re-review the pinned digests below
// whenever upstream rotates a network configuration or CompatibleNodeRelease
// moves.
package publicpins

import "github.com/meigma/yacd/internal/cardano/networkartifacts"

const (
	// bookBase is the Cardano operations book environment root. config.json,
	// the genesis files, topology, checkpoints, and peer-snapshot come from
	// here, under the per-profile path bookBase+profile+"/".
	bookBase = "https://book.play.dev.cardano.org/environments/"
	// mithrilBase is the Mithril release-mainnet configuration root; the
	// mainnet Mithril verification keys come from here.
	mithrilBase = "https://raw.githubusercontent.com/input-output-hk/mithril/main/mithril-infra/configuration/release-mainnet/"

	// releaseMainnetMithrilAggregator is the Mithril aggregator endpoint used
	// to bootstrap a mainnet node from a verified snapshot.
	releaseMainnetMithrilAggregator = "https://aggregator.release-mainnet.api.mithril.network/aggregator"

	// CompatibleNodeRelease is the cardano-node release these pinned profiles
	// were reviewed against. Re-review the pinned digests when this moves.
	CompatibleNodeRelease = "11.0.1"

	previewName = "preview"
	preprodName = "preprod"
	mainnetName = "mainnet"
)

// SourceKind identifies which trusted root a profile file is downloaded from.
type SourceKind int

const (
	// SourceBook downloads from the operations book:
	// bookBase + profile + "/" + SourceName.
	SourceBook SourceKind = iota
	// SourceMithril downloads from the Mithril release config:
	// mithrilBase + SourceName.
	SourceMithril
)

// File describes one artifact file in a public profile.
type File struct {
	// ArtifactKey is the YACD networkartifacts key. It is the filename written
	// into the staging directory and the data key used in the artifact
	// manifest.
	ArtifactKey string
	// SourceName is the upstream filename used to build the download URL; it
	// can differ from ArtifactKey (config.json -> configuration.yaml,
	// topology.json -> primary-topology.json).
	SourceName string
	// ConnectionKey is the logical name this file takes in connection.json's
	// files map.
	ConnectionKey string
	// Source selects the download root.
	Source SourceKind
	// SHA256 is the hex-encoded (64-char) pinned digest the downloaded bytes
	// must match. Empty when Pinned is false.
	SHA256 string
	// Optional reports whether a missing file is tolerated for the profile.
	Optional bool
	// Pinned reports whether the file is verified against SHA256 at fetch time.
	// Unpinned files are either authenticated downstream by cardano-node
	// against config.json's inline hashes (genesis, checkpoints) or are
	// best-effort operational hints (peer-snapshot).
	Pinned bool
}

// URL returns the absolute download URL for the file within the given profile.
func (f File) URL(profile string) string {
	if f.Source == SourceMithril {
		return mithrilBase + f.SourceName
	}
	return bookBase + profile + "/" + f.SourceName
}

// Mithril carries the mainnet Mithril bootstrap pins.
type Mithril struct {
	// AggregatorEndpoint is the Mithril aggregator a node bootstraps from.
	AggregatorEndpoint string
	// GenesisVerificationKeyArtifact and AncillaryVerificationKeyArtifact name
	// the artifact keys (in Files) that carry the Mithril verification keys.
	GenesisVerificationKeyArtifact   string
	AncillaryVerificationKeyArtifact string
}

// Profile is a curated public network profile definition.
type Profile struct {
	// Name is the profile identifier (preview, preprod, mainnet).
	Name string
	// SourceURL is the human-facing provenance URL recorded in the manifest.
	SourceURL string
	// NetworkMagic is the Cardano network magic for the profile. It is a static
	// per-profile fact recorded here (rather than parsed from shelley-genesis at
	// build time) so the operator can build the profile plan without holding the
	// genesis bytes, which it no longer embeds.
	NetworkMagic int64
	// RequiresNetworkMagic mirrors the RequiresNetworkMagic value in the
	// profile's node config (RequiresMagic vs RequiresNoMagic). Like
	// NetworkMagic it is recorded statically so the operator need not parse
	// config.json.
	RequiresNetworkMagic bool
	// Files lists the profile's files in deterministic order: the required
	// chain files first (config, byron, shelley, alonzo, conway, topology),
	// then the per-profile optional files. This order is stable so any
	// fingerprint computed over it stays stable.
	Files []File
	// Mithril is non-nil only for mainnet.
	Mithril *Mithril
}

// chainFiles returns the six required chain files common to every public
// profile, in deterministic order. config.json and topology.json are pinned;
// the four genesis files are downloaded but authenticated downstream by
// cardano-node against config.json's inline hashes, so they carry no pin here.
func chainFiles(configSHA, topologySHA string) []File {
	return []File{
		{ArtifactKey: networkartifacts.ConfigurationKey, SourceName: "config.json", ConnectionKey: "configuration", Source: SourceBook, SHA256: configSHA, Pinned: true},
		{ArtifactKey: networkartifacts.ByronGenesisKey, SourceName: "byron-genesis.json", ConnectionKey: "byronGenesis", Source: SourceBook},
		{ArtifactKey: networkartifacts.ShelleyGenesisKey, SourceName: "shelley-genesis.json", ConnectionKey: "shelleyGenesis", Source: SourceBook},
		{ArtifactKey: networkartifacts.AlonzoGenesisKey, SourceName: "alonzo-genesis.json", ConnectionKey: "alonzoGenesis", Source: SourceBook},
		{ArtifactKey: networkartifacts.ConwayGenesisKey, SourceName: "conway-genesis.json", ConnectionKey: "conwayGenesis", Source: SourceBook},
		{ArtifactKey: networkartifacts.PrimaryTopologyKey, SourceName: "topology.json", ConnectionKey: "primaryTopology", Source: SourceBook, SHA256: topologySHA, Pinned: true},
	}
}

// checkpointsFile returns the required checkpoints.json file. It is referenced
// by config.json (CheckpointsFile + CheckpointsFileHash) so cardano-node
// verifies it downstream; it is not pinned here.
func checkpointsFile() File {
	return File{ArtifactKey: networkartifacts.CheckpointsKey, SourceName: "checkpoints.json", ConnectionKey: "checkpoints", Source: SourceBook}
}

// peerSnapshotFile returns the unpinned, optional peer-snapshot.json file.
func peerSnapshotFile() File {
	return File{ArtifactKey: networkartifacts.PeerSnapshotKey, SourceName: "peer-snapshot.json", ConnectionKey: "peerSnapshot", Source: SourceBook, Optional: true}
}

//nolint:gochecknoglobals // immutable curated profile registry.
var profiles = map[string]Profile{
	previewName: {
		Name:                 previewName,
		SourceURL:            bookBase + "preview/",
		NetworkMagic:         2,
		RequiresNetworkMagic: true,
		// preview config.json references CheckpointsFile + CheckpointsFileHash,
		// so checkpoints.json is required (verified downstream by that hash).
		Files: append(
			chainFiles(
				"bdfe303362cb443ba15754747be07893bfebee367d09fad41af5a626503df6d6",
				"77db913f4b605cd874c1cf3cea160f9b4227b15fc07e4cce72d622bdda946de6",
			),
			checkpointsFile(),
			peerSnapshotFile(),
		),
	},
	preprodName: {
		Name:                 preprodName,
		SourceURL:            bookBase + "preprod/",
		NetworkMagic:         1,
		RequiresNetworkMagic: true,
		// preprod config.json does not reference a checkpoints file.
		Files: append(
			chainFiles(
				"6b2da527ab5ce7cfc6c02cf74a04f512fa845b870f9fdda25d26edcd5814e2c8",
				"bd18a5adaeaa926c0eeb5ae5cbc8f70c6f18e702b6cb079cfdee58d1206fc25c",
			),
			peerSnapshotFile(),
		),
	},
	mainnetName: {
		Name:                 mainnetName,
		SourceURL:            bookBase + "mainnet/",
		NetworkMagic:         764824073,
		RequiresNetworkMagic: false,
		// mainnet config.json references CheckpointsFile + CheckpointsFileHash,
		// so checkpoints.json is required (verified downstream by that hash).
		Files: append(
			chainFiles(
				"e3db8de7ec244b5fddc114e7249df9f4bda11e2193c367c2135a3a8612de2da7",
				"628fbf74cfe4e513c092d00b2937cdaf26c619ac2f7bf27aa6469505ad5f43c7",
			),
			checkpointsFile(),
			peerSnapshotFile(),
			File{ArtifactKey: networkartifacts.MithrilGenesisKey, SourceName: "genesis.vkey", ConnectionKey: "mithrilGenesisVerificationKey", Source: SourceMithril, SHA256: "1dca8b11b21f72aedea1d102abbb2f783b64d61b6ba059cba0d2602bd5153e51", Pinned: true},
			File{ArtifactKey: networkartifacts.MithrilAncillaryKey, SourceName: "ancillary.vkey", ConnectionKey: "mithrilAncillaryVerificationKey", Source: SourceMithril, SHA256: "187c8a48e59bca37216d18e3a3a45195116cf64244faa98ea51f77681f7786b4", Pinned: true},
		),
		Mithril: &Mithril{
			AggregatorEndpoint:               releaseMainnetMithrilAggregator,
			GenesisVerificationKeyArtifact:   networkartifacts.MithrilGenesisKey,
			AncillaryVerificationKeyArtifact: networkartifacts.MithrilAncillaryKey,
		},
	},
}

//nolint:gochecknoglobals // immutable display order for known profiles.
var knownProfiles = []string{previewName, preprodName, mainnetName}

// Lookup returns the curated profile definition and whether it is known.
func Lookup(profile string) (Profile, bool) {
	p, ok := profiles[profile]
	return p, ok
}

// Known returns the supported curated profile names in display order.
func Known() []string {
	out := make([]string, len(knownProfiles))
	copy(out, knownProfiles)
	return out
}

// MithrilAggregatorEndpoint returns the Mithril aggregator endpoint shared by
// mainnet bootstrap. Exposed so callers do not duplicate the constant.
func MithrilAggregatorEndpoint() string {
	return releaseMainnetMithrilAggregator
}
