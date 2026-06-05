package operator

// defaultManagerRepository is the published operator manager image. The default
// install sets only the repository and leaves the tag and digest unset, so the
// chart renders repository:appVersion (its own Chart.yaml appVersion). Operator
// and CLI release together under one version, so that appVersion tag always
// resolves to the matching, already-published manager image. The supported way
// to change the operator version is to upgrade the CLI.
const defaultManagerRepository = "ghcr.io/meigma/yacd"

// Image identifies a container image by repository plus an optional tag or
// digest. When Digest is set it wins over Tag (matching the chart's
// repository@digest vs repository:tag rendering); leaving both empty lets the
// chart default the tag to its appVersion.
type Image struct {
	// Repository is the image repository, e.g. "ghcr.io/meigma/yacd".
	Repository string

	// Tag is the image tag. Ignored when Digest is set.
	Tag string

	// Digest is the image digest, e.g. "sha256:…". Takes precedence over Tag.
	Digest string
}

// toHelmValues renders the image into the chart's image value sub-tree, omitting
// empty fields so unset knobs fall through to the chart's own defaults.
func (i Image) toHelmValues() map[string]any {
	out := map[string]any{}
	if i.Repository != "" {
		out["repository"] = i.Repository
	}
	if i.Tag != "" {
		out["tag"] = i.Tag
	}
	if i.Digest != "" {
		out["digest"] = i.Digest
	}
	return out
}

// Values is the typed install contract for the operator chart. It mirrors the
// common chart knobs and converts to the Helm values tree the renderer feeds to
// chartutil.ToRenderValues. Zero-valued fields are omitted so they fall through
// to the chart's own values.yaml defaults; Extra is merged last and can override
// any field, including ones this struct does not model.
type Values struct {
	// Image is the operator manager image (chart key "image").
	Image Image

	// Replicas overrides the manager Deployment replica count (chart key
	// "replicaCount"). nil leaves the chart default.
	Replicas *int

	// LogFormat is the manager log format: "json" or "text" (chart key
	// "manager.logFormat"). Empty leaves the chart default.
	LogFormat string

	// LogLevel is the manager log level: "debug", "info", "warn", or "error"
	// (chart key "manager.logLevel"). Empty leaves the chart default.
	LogLevel string

	// LeaderElection toggles leader election (chart key
	// "leaderElection.enabled"). nil leaves the chart default (enabled).
	LeaderElection *bool

	// Extra is a full-chart escape hatch merged last over the typed fields, so
	// callers can set knobs this struct does not model (or override the ones it
	// does). Keys are chart value paths as nested maps.
	Extra map[string]any
}

// ToHelmValues converts the typed contract into the Helm values tree consumed by
// chartutil.ToRenderValues. Typed fields are applied first; Extra is deep-merged
// on top so it always wins.
func (v Values) ToHelmValues() map[string]any {
	out := map[string]any{}

	if img := v.Image.toHelmValues(); len(img) > 0 {
		out["image"] = img
	}
	if v.Replicas != nil {
		out["replicaCount"] = *v.Replicas
	}

	manager := map[string]any{}
	if v.LogFormat != "" {
		manager["logFormat"] = v.LogFormat
	}
	if v.LogLevel != "" {
		manager["logLevel"] = v.LogLevel
	}
	if len(manager) > 0 {
		out["manager"] = manager
	}

	if v.LeaderElection != nil {
		out["leaderElection"] = map[string]any{"enabled": *v.LeaderElection}
	}

	mergeValues(out, v.Extra)

	return out
}

// mergeValues deep-merges src into dst, with src winning on conflicts. Nested
// maps are merged recursively; any non-map value replaces the destination.
func mergeValues(dst, src map[string]any) {
	for key, srcVal := range src {
		if srcMap, ok := srcVal.(map[string]any); ok {
			if dstMap, ok := dst[key].(map[string]any); ok {
				mergeValues(dstMap, srcMap)
				continue
			}
		}
		dst[key] = srcVal
	}
}

// Default returns the baseline install: the manager image pinned to the chart's
// appVersion (repository:appVersion, the operator release this CLI ships with),
// leader election on, secure metrics, and json/info logging. It sets only the
// image repository; leaving the tag and digest empty lets the chart default the
// tag to its appVersion, so the rendered Deployment matches a plain
// `helm template` at the chart's default values.
func Default() Values {
	return Values{
		Image: Image{
			Repository: defaultManagerRepository,
		},
	}
}
