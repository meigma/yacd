package fetch

// Trusted source bases. config.json and the chain artifacts come from the
// Cardano operations book; the Mithril verification keys come from the Mithril
// release-mainnet configuration. These mirror the provenance recorded in
// internal/cardano/publicnet/profiles/*/SOURCE.md.
const (
	bookBase    = "https://book.play.dev.cardano.org/environments/"
	mithrilBase = "https://raw.githubusercontent.com/input-output-hk/mithril/main/mithril-infra/configuration/release-mainnet/"
)

// Pinned config.json digests, one per profile. config.json is the single trust
// anchor: it carries the Byron/Shelley/Alonzo/Conway and checkpoints file
// hashes inline, which cardano-node verifies at startup, so pinning config.json
// transitively covers every genesis file and the checkpoints file. These
// digests are reviewed against the embedded profile copies (and SOURCE.md) and
// must be re-reviewed against the live source whenever upstream rotates a
// network configuration.
const (
	previewConfigSHA256 = "bdfe303362cb443ba15754747be07893bfebee367d09fad41af5a626503df6d6"
	preprodConfigSHA256 = "6b2da527ab5ce7cfc6c02cf74a04f512fa845b870f9fdda25d26edcd5814e2c8"
	mainnetConfigSHA256 = "e3db8de7ec244b5fddc114e7249df9f4bda11e2193c367c2135a3a8612de2da7"
)

// Pinned Mithril verification key digests. These keys are not referenced from
// config.json, so they carry their own pins; they anchor Mithril snapshot
// verification and must never be fetched unverified.
const (
	mainnetMithrilGenesisSHA256   = "1dca8b11b21f72aedea1d102abbb2f783b64d61b6ba059cba0d2602bd5153e51"
	mainnetMithrilAncillarySHA256 = "187c8a48e59bca37216d18e3a3a45195116cf64244faa98ea51f77681f7786b4"
)

// pinnedFile describes one artifact to download for a profile.
type pinnedFile struct {
	// name is the file's name at the source and the filename written into the
	// output directory.
	name string
	// url is the exact download URL.
	url string
	// expectedSHA256 is the hex digest the downloaded bytes must match. Empty
	// means the file is not pinned here: genesis and checkpoints files are
	// verified downstream by cardano-node against the hashes inside the pinned
	// config.json, and topology/peer-snapshot are operational, non-critical
	// files.
	expectedSHA256 string
	// optional reports whether a download failure for this file is tolerated
	// (the file may legitimately not exist for a profile).
	optional bool
}

// bookFile builds a pinnedFile served from the operations book for profile.
func bookFile(profile, name, sha256 string, optional bool) pinnedFile {
	return pinnedFile{name: name, url: bookBase + profile + "/" + name, expectedSHA256: sha256, optional: optional}
}

// chainArtifacts returns the files common to every public profile: the pinned
// config.json plus the genesis and topology files verified downstream.
func chainArtifacts(profile, configSHA256 string) []pinnedFile {
	return []pinnedFile{
		bookFile(profile, "config.json", configSHA256, false),
		bookFile(profile, "byron-genesis.json", "", false),
		bookFile(profile, "shelley-genesis.json", "", false),
		bookFile(profile, "alonzo-genesis.json", "", false),
		bookFile(profile, "conway-genesis.json", "", false),
		bookFile(profile, "topology.json", "", false),
	}
}

// pinsFor returns the download manifest for a public profile and whether the
// profile is known.
func pinsFor(profile string) ([]pinnedFile, bool) {
	switch profile {
	case "preview":
		// preview config.json references CheckpointsFile + CheckpointsFileHash,
		// so checkpoints.json is required (unpinned here — cardano-node verifies
		// it against the hash inside the pinned config.json). peer-snapshot is a
		// best-effort bootstrap aid and stays optional.
		return append(chainArtifacts("preview", previewConfigSHA256),
			bookFile("preview", "checkpoints.json", "", false),
			bookFile("preview", "peer-snapshot.json", "", true),
		), true
	case "preprod":
		// preprod config.json does not reference a checkpoints file.
		return append(chainArtifacts("preprod", preprodConfigSHA256),
			bookFile("preprod", "peer-snapshot.json", "", true),
		), true
	case "mainnet":
		// mainnet config.json references CheckpointsFile + CheckpointsFileHash,
		// so checkpoints.json is required (verified downstream by that hash).
		return append(chainArtifacts("mainnet", mainnetConfigSHA256),
			bookFile("mainnet", "checkpoints.json", "", false),
			bookFile("mainnet", "peer-snapshot.json", "", true),
			pinnedFile{name: "mithril-genesis.vkey", url: mithrilBase + "genesis.vkey", expectedSHA256: mainnetMithrilGenesisSHA256},
			pinnedFile{name: "mithril-ancillary.vkey", url: mithrilBase + "ancillary.vkey", expectedSHA256: mainnetMithrilAncillarySHA256},
		), true
	default:
		return nil, false
	}
}

// knownProfiles lists the profiles fetch supports, for error messages.
//
//nolint:gochecknoglobals // immutable display list.
var knownProfiles = []string{"preview", "preprod", "mainnet"}
