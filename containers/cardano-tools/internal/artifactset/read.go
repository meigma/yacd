package artifactset

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"unicode/utf8"
)

// ReadArtifacts walks [Sources] and reads each declared file from dir as UTF-8
// text. Optional sources missing on disk are skipped; any other read error is
// returned. The returned map is keyed by artifact data key.
func ReadArtifacts(dir string) (map[string]string, error) {
	sources := generatedSources
	artifactData := make(map[string]string, len(sources))
	for _, src := range sources {
		content, err := readTextFile(path.Join(dir, src.RelativePath))
		if errors.Is(err, os.ErrNotExist) && src.Optional {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read artifact %s: %w", src.RelativePath, err)
		}
		artifactData[src.Key] = content
	}
	return artifactData, nil
}

// ReadManifest reads the localnet plan manifest from manifestPath, validates it
// is the absolute envDir/yacd-localnet-plan.json, and parses out the network
// magic and fingerprint. The returned [Manifest] also carries the verbatim
// content for publication.
func ReadManifest(envDir, manifestPath string) (Manifest, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return Manifest{}, fmt.Errorf("manifest file path is required")
	}
	if !path.IsAbs(manifestPath) {
		return Manifest{}, fmt.Errorf("manifest file must be an absolute path")
	}
	expected := path.Join(envDir, "yacd-localnet-plan.json")
	if manifestPath != expected {
		return Manifest{}, fmt.Errorf("manifest file must be %s", expected)
	}

	raw, err := readTextFile(manifestPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("read localnet manifest: %w", err)
	}

	var parsed manifestJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return Manifest{}, fmt.Errorf("parse localnet manifest: %w", err)
	}
	if parsed.Inputs.NetworkMagic == nil {
		return Manifest{}, fmt.Errorf("localnet manifest inputs.networkMagic is required")
	}
	if strings.TrimSpace(parsed.Fingerprint.Value) == "" {
		return Manifest{}, fmt.Errorf("localnet manifest fingerprint.value is required")
	}

	return Manifest{
		NetworkMagic: *parsed.Inputs.NetworkMagic,
		Fingerprint:  parsed.Fingerprint.Value,
		Raw:          raw,
	}, nil
}

// readTextFile reads the file at filePath and returns its contents as a string.
// The file must be valid UTF-8.
func readTextFile(filePath string) (string, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(raw) {
		return "", fmt.Errorf("file %s is not valid UTF-8", filePath)
	}
	return string(raw), nil
}

// manifestJSON is the subset of yacd-localnet-plan.json [ReadManifest] parses
// to populate [Manifest].
type manifestJSON struct {
	Inputs struct {
		NetworkMagic *int64 `json:"networkMagic"`
	} `json:"inputs"`
	Fingerprint struct {
		Value string `json:"value"`
	} `json:"fingerprint"`
}
