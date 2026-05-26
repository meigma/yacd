package dbsync

import (
	"encoding/json"
	"fmt"
)

// topology mirrors the upstream cardano-node topology wire format for a
// follower with a single local-root upstream.
type topology struct {
	LocalRoots         []localRoot `json:"localRoots"`
	PublicRoots        []any       `json:"publicRoots"`
	UseLedgerAfterSlot int         `json:"useLedgerAfterSlot"`
}

// localRoot is a single local-root group of access points.
type localRoot struct {
	AccessPoints []accessPoint `json:"accessPoints"`
	Advertise    bool          `json:"advertise"`
	Valency      int           `json:"valency"`
}

// accessPoint is a single upstream host:port tuple.
type accessPoint struct {
	Address string `json:"address"`
	Port    int32  `json:"port"`
}

// renderTopology marshals the upstream node endpoint into a single-root
// follower topology JSON file.
func renderTopology(nodeToNode NodeToNode) (string, error) {
	out, err := json.MarshalIndent(topology{
		LocalRoots: []localRoot{{
			AccessPoints: []accessPoint{{
				Address: nodeToNode.Host,
				Port:    nodeToNode.Port,
			}},
			Advertise: false,
			Valency:   1,
		}},
		PublicRoots:        []any{},
		UseLedgerAfterSlot: -1,
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal follower topology: %w", err)
	}
	return string(out) + "\n", nil
}
