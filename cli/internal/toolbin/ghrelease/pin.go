package ghrelease

import "github.com/meigma/yacd/cli/internal/toolbin"

// k3dVersion is the pinned k3d release. Bump it together with the SHA256 digests
// in DefaultK3dPin when upgrading k3d; the digests are the sha256 of each
// k3d-<os>-<arch> asset, taken from the release checksums.txt.
const k3dVersion = "v5.9.0"

// DefaultK3dPin is the built-in k3d pin the composition root passes to New. The
// SHA256 map is keyed by "<GOOS>/<GOARCH>"; AssetURL templates {os} and {arch}
// from runtime.GOOS/GOARCH (k3d's asset names use the Go os/arch spelling).
var DefaultK3dPin = toolbin.Pin{
	Version:  k3dVersion,
	AssetURL: "https://github.com/k3d-io/k3d/releases/download/" + k3dVersion + "/k3d-{os}-{arch}",
	SHA256: map[string]string{
		"linux/amd64":  "06d8f25bc3a971c4eb29e0ff08429b180402db0f4dec838c9eac427e296800a0",
		"linux/arm64":  "03cde5cf23e6e8e67de5a039ecf26e5b85aca82fba3e5d13dadf904cd218a250",
		"darwin/amd64": "b4aabc37534f95b9c764e7823f2df923f50d57600837aa60a06266cce47db732",
		"darwin/arm64": "fe106541d5d0a3f18debcd4d432a16f8c0ce3e6ddc06f8fbb6f696a122313e00",
	},
}
