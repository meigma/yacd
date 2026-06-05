package cardanonetwork

import (
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// ogmiosSettings is the effective ogmios sidecar configuration after applying
// CardanoNetwork spec overrides on top of the package defaults.
type ogmiosSettings struct {
	// enabled is whether the ogmios sidecar should run.
	enabled bool
	// image is the resolved container image reference.
	image string
	// port is the resolved container/Service port.
	port int32
	// resources is an optional resource requirements override.
	resources *corev1.ResourceRequirements
}

// kupoSettings is the effective kupo sidecar configuration after applying
// CardanoNetwork spec overrides on top of the package defaults.
type kupoSettings struct {
	// enabled is whether the kupo sidecar should run.
	enabled bool
	// image is the resolved container image reference. validateKupoImage
	// rejects images other than defaultKupoImage.
	image string
	// port is the resolved container/Service port.
	port int32
	// resources is an optional resource requirements override.
	resources *corev1.ResourceRequirements
}

// resolveOgmiosSettings applies the CardanoNetwork spec on top of the package
// defaults and returns the effective ogmios configuration.
func resolveOgmiosSettings(network *yacdv1alpha1.CardanoNetwork) (ogmiosSettings, error) {
	settings := ogmiosSettings{
		enabled: true,
		image:   defaultOgmiosImage,
		port:    defaultOgmiosPort,
	}
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Ogmios == nil {
		return settings, nil
	}

	spec := network.Spec.ChainAPI.Ogmios
	if !spec.Enabled {
		settings.enabled = false
		return settings, nil
	}

	settings.image = strings.TrimSpace(spec.Image)
	if settings.image == "" {
		return ogmiosSettings{}, unsupportedSpec("ogmios image is required")
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return ogmiosSettings{}, unsupportedSpec("ogmios port must be between 1 and 65535")
	}
	settings.port = spec.Port
	if spec.Resources != nil {
		settings.resources = spec.Resources.DeepCopy()
	}

	return settings, nil
}

// resolveKupoSettings applies the CardanoNetwork spec on top of the package
// defaults and returns the effective kupo configuration based ONLY on the
// per-kupo spec.
//
// The second return value reports whether the user explicitly mentioned
// kupo in spec.ChainAPI.Kupo. applyDependentDefaults consumes that signal
// to decide whether the implicit "kupo follows ogmios" cascade applies.
//
// This function never reads ogmios state — cross-component defaults are
// resolved in a dedicated step so each per-component resolver stays a
// single-component decision.
func resolveKupoSettings(network *yacdv1alpha1.CardanoNetwork) (kupoSettings, bool, error) {
	settings := kupoSettings{
		enabled: true,
		image:   defaultKupoImage,
		port:    defaultKupoPort,
	}
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Kupo == nil {
		return settings, false, nil
	}

	spec := network.Spec.ChainAPI.Kupo
	if !spec.Enabled {
		settings.enabled = false
		return settings, true, nil
	}

	settings.image = strings.TrimSpace(spec.Image)
	if settings.image == "" {
		return kupoSettings{}, true, unsupportedSpec("kupo image is required")
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return kupoSettings{}, true, unsupportedSpec("kupo port must be between 1 and 65535")
	}
	settings.port = spec.Port
	if spec.Resources != nil {
		settings.resources = spec.Resources.DeepCopy()
	}

	return settings, true, nil
}

// applyDependentDefaults encodes cross-component defaults that depend on
// more than one resolved settings value. Per-component resolve* functions
// remain pure single-component decisions; this step runs after all of them.
//
// Product rule (kupo follows ogmios): when the user did not mention kupo in
// spec.ChainAPI.Kupo and ogmios is disabled, kupo defaults to disabled.
// kupo without ogmios is meaningless (kupo follows the chain through
// ogmios), and the user has not asked for it explicitly, so we keep kupo
// off by default in that combination. The "kupo cannot be enabled without
// ogmios" invariant is a separate hard error enforced by the builder.
func applyDependentDefaults(ogmios ogmiosSettings, kupo kupoSettings, kupoMentioned bool) kupoSettings {
	if !kupoMentioned && !ogmios.enabled {
		kupo.enabled = false
	}

	return kupo
}
