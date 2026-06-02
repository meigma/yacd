package ssa

import (
	"embed"
	"io/fs"
)

//go:embed manifests/operator.yaml
var manifestsFS embed.FS

// Manifests is the embedded, build-time-rendered operator chart that New
// applies by default. It is exposed as an fs.FS so the composition root can pass
// it to New and tests can substitute a synthetic filesystem.
var Manifests fs.FS = manifestsFS

// manifestPath is the path of the rendered manifest within Manifests.
const manifestPath = "manifests/operator.yaml"
