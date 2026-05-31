package fetch

import "github.com/meigma/yacd/internal/cardano/publicpins"

// pinnedFile is the fetch-local view of a curated profile file: the destination
// artifact key (the filename written into the output directory), the resolved
// download URL, the pinned sha256 digest (empty when the file is not pinned),
// and whether a missing download is tolerated.
type pinnedFile struct {
	dest           string
	url            string
	expectedSHA256 string
	optional       bool
}

// pinsFor returns the download manifest for a public profile and whether the
// profile is known. The profile definitions, source URLs, and pinned digests
// are owned by internal/cardano/publicpins so the operator's published artifact
// manifest and this fetch path share exactly one source of truth. config.json
// and topology.json (and the mainnet Mithril keys) are pinned; the genesis and
// checkpoints files are downloaded unpinned and verified downstream by
// cardano-node against config.json's inline hashes.
func pinsFor(profile string) ([]pinnedFile, bool) {
	p, ok := publicpins.Lookup(profile)
	if !ok {
		return nil, false
	}
	files := make([]pinnedFile, 0, len(p.Files))
	for _, file := range p.Files {
		pin := ""
		if file.Pinned {
			pin = file.SHA256
		}
		files = append(files, pinnedFile{
			dest:           file.ArtifactKey,
			url:            file.URL(profile),
			expectedSHA256: pin,
			optional:       file.Optional,
		})
	}
	return files, true
}

// knownProfiles lists the profiles fetch supports, for error messages.
//
//nolint:gochecknoglobals // immutable display list sourced from publicpins.
var knownProfiles = publicpins.Known()
