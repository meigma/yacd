package fetch

import "github.com/meigma/yacd/internal/cardano/publicpins"

// pinnedFile is the fetch-local view of a curated profile file: the destination
// artifact key (the filename written into the output directory), the connection
// key it takes in connection.json's files map, the resolved download URL, the
// pinned sha256 digest (empty when the file is not pinned), and whether a
// missing download is tolerated.
type pinnedFile struct {
	dest           string
	connectionKey  string
	url            string
	expectedSHA256 string
	optional       bool
}

// profilePins bundles a profile's download manifest with the static identity
// (network magic, requiresNetworkMagic) the fetch verb records in
// connection.json. Both sides come from internal/cardano/publicpins so the
// operator's published artifact manifest and this fetch path share exactly one
// source of truth.
type profilePins struct {
	files                []pinnedFile
	networkMagic         int64
	requiresNetworkMagic bool
}

// pinsFor returns the download manifest plus static identity for a public
// profile and whether the profile is known. config.json and topology.json (and
// the mainnet Mithril keys) are pinned; the genesis and checkpoints files are
// downloaded unpinned and verified downstream by cardano-node against
// config.json's inline hashes.
func pinsFor(profile string) (profilePins, bool) {
	p, ok := publicpins.Lookup(profile)
	if !ok {
		return profilePins{}, false
	}
	files := make([]pinnedFile, 0, len(p.Files))
	for _, file := range p.Files {
		pin := ""
		if file.Pinned {
			pin = file.SHA256
		}
		files = append(files, pinnedFile{
			dest:           file.ArtifactKey,
			connectionKey:  file.ConnectionKey,
			url:            file.URL(profile),
			expectedSHA256: pin,
			optional:       file.Optional,
		})
	}
	return profilePins{
		files:                files,
		networkMagic:         p.NetworkMagic,
		requiresNetworkMagic: p.RequiresNetworkMagic,
	}, true
}

// knownProfiles lists the profiles fetch supports, for error messages.
//
//nolint:gochecknoglobals // immutable display list sourced from publicpins.
var knownProfiles = publicpins.Known()
