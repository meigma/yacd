package artifactset

import (
	"encoding/json"
	"fmt"

	"github.com/meigma/yacd/internal/cardano/networkartifacts"
)

// publicMode is the connection.json network.mode value for a fetched public
// profile.
const publicMode = "public"

// PublicConnection is the subset of connection.json a fetched public profile
// can describe on its own. Unlike the localnet path, the fetch verb runs before
// any cluster identity exists, so it carries only the static, profile-intrinsic
// facts: the network magic, whether magic is required, the era, and the served
// file map. The owning CardanoNetwork name, namespace, and node-to-node
// endpoint are cluster-runtime facts the operator fills in elsewhere and are
// intentionally omitted here.
type PublicConnection struct {
	// Profile is the public network profile name (preview, preprod, mainnet).
	Profile string
	// NetworkMagic is the Cardano network magic for the profile; must be
	// non-zero.
	NetworkMagic int64
	// RequiresNetworkMagic mirrors the profile node config's
	// RequiresNetworkMagic value.
	RequiresNetworkMagic bool
	// Era is the Cardano era recorded in the connection metadata. It may be
	// empty when the fetch caller does not pin an era, in which case it is
	// omitted from the rendered document.
	Era string
	// Files maps each connection.json logical key (e.g. "configuration") to the
	// served artifact filename written into the output directory; must be
	// non-empty.
	Files map[string]string
}

// publicConnectionDocument is the on-wire shape of a public profile's
// connection.json. It shares the schema version and files contract with the
// localnet [connectionDocument] but omits the cluster-runtime fields (name,
// namespace, node-to-node endpoint, localnet fingerprint) the fetch verb cannot
// know, while carrying the public-mode profile and requiresNetworkMagic the
// controller's connection validator expects.
type publicConnectionDocument struct {
	SchemaVersion string                  `json:"schemaVersion"`
	Network       publicConnectionNetwork `json:"network"`
	Files         map[string]string       `json:"files"`
}

// publicConnectionNetwork is the network-identity sub-object of a public
// profile's connection.json.
type publicConnectionNetwork struct {
	Mode                 string `json:"mode"`
	Profile              string `json:"profile"`
	NetworkMagic         int64  `json:"networkMagic"`
	RequiresNetworkMagic bool   `json:"requiresNetworkMagic"`
	Era                  string `json:"era,omitempty"`
}

// RenderPublicConnection serializes the public connection.json document for a
// fetched profile. It reuses the shared schema version so producers and the
// controller's connection validator agree on one schema. The returned string is
// JSON with two-space indentation and a trailing newline, matching the localnet
// renderer's formatting.
//
// The network name, namespace, and node-to-node endpoint are intentionally
// absent: the fetch verb does not know them. The document records the profile
// under network.profile and sets network.requiresNetworkMagic, matching the
// controller's public-mode connection contract.
func RenderPublicConnection(conn PublicConnection) (string, error) {
	if conn.Profile == "" {
		return "", fmt.Errorf("public connection profile is required")
	}
	if conn.NetworkMagic == 0 {
		return "", fmt.Errorf("public connection network magic is required")
	}
	if len(conn.Files) == 0 {
		return "", fmt.Errorf("public connection files map is required")
	}

	doc := publicConnectionDocument{
		SchemaVersion: networkartifacts.SchemaVersion,
		Network: publicConnectionNetwork{
			Mode:                 publicMode,
			Profile:              conn.Profile,
			NetworkMagic:         conn.NetworkMagic,
			RequiresNetworkMagic: conn.RequiresNetworkMagic,
			Era:                  conn.Era,
		},
		Files: conn.Files,
	}

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal public connection document: %w", err)
	}
	return string(raw) + "\n", nil
}
