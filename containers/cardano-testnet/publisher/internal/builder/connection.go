package builder

import (
	"encoding/json"
	"fmt"
)

// connectionDocument is the on-wire shape of connection.json.
type connectionDocument struct {
	SchemaVersion     string             `json:"schemaVersion"`
	Network           connectionNetwork  `json:"network"`
	PrimaryNodeToNode connectionEndpoint `json:"primaryNodeToNode"`
	Files             map[string]string  `json:"files"`
}

// connectionNetwork is the network-identity sub-object of
// connection.json.
type connectionNetwork struct {
	Name                string `json:"name"`
	Namespace           string `json:"namespace"`
	Mode                string `json:"mode"`
	NetworkMagic        int64  `json:"networkMagic"`
	Era                 string `json:"era"`
	LocalnetFingerprint string `json:"localnetFingerprint"`
}

// connectionEndpoint is the primary node-to-node endpoint sub-object
// of connection.json.
type connectionEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	URL  string `json:"url"`
}

// renderConnection serializes the connection.json document for input
// using fileKeys as the connection-key-to-ConfigMap-data-key mapping.
// The returned string is JSON with two-space indentation and a
// trailing newline.
func renderConnection(input Input, fileKeys map[string]string) (string, error) {
	doc := connectionDocument{
		SchemaVersion: SchemaVersion,
		Network: connectionNetwork{
			Name:                input.Network.Name,
			Namespace:           input.Network.Namespace,
			Mode:                input.Network.Mode,
			NetworkMagic:        input.Manifest.NetworkMagic,
			Era:                 input.Network.Era,
			LocalnetFingerprint: input.Manifest.Fingerprint,
		},
		PrimaryNodeToNode: connectionEndpoint{
			Host: input.Network.NodeToNodeHost,
			Port: input.Network.NodeToNodePort,
			URL:  input.Network.NodeToNodeURL,
		},
		Files: fileKeys,
	}

	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal connection document: %w", err)
	}
	return string(raw) + "\n", nil
}
