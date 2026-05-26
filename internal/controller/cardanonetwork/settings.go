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

// faucetSettings is the effective faucet sidecar configuration after applying
// CardanoNetwork spec overrides on top of the package defaults.
type faucetSettings struct {
	// enabled is whether the faucet sidecar should run. Requires both ogmios
	// and kupo to be enabled.
	enabled bool
	// image is the resolved container image reference. The repository must
	// match the resolved default faucet image repository.
	image string
	// port is the resolved container/Service port.
	port int32
	// defaultSource is the default UTXO source name used for top-ups
	// (must match the utxoN format).
	defaultSource string
	// minTopUpLovelace bounds the per-request top-up minimum.
	minTopUpLovelace int64
	// maxTopUpLovelace bounds the per-request top-up maximum.
	maxTopUpLovelace int64
	// resources is an optional resource requirements override.
	resources *corev1.ResourceRequirements
	// authSecretName is the per-CardanoNetwork faucet auth Secret name.
	authSecretName string
	// authSecretKey is the data key inside the auth Secret carrying the token.
	authSecretKey string
	// authTokenFilePath is the in-container mount path the faucet reads its
	// token from.
	authTokenFilePath string
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
// defaults and returns the effective kupo configuration.
//
// Product rule: kupo defaults to enabled when the CardanoNetwork spec does
// not mention kupo and ogmios is enabled (kupo without ogmios is meaningless
// because kupo follows the chain through ogmios). If ogmios is disabled and
// kupo is not explicitly enabled, kupo is disabled by default. The dependent
// "kupo requires ogmios" check is enforced by the builder after settings
// resolution, not here, so this function stays a single-component decision.
func resolveKupoSettings(network *yacdv1alpha1.CardanoNetwork, ogmios ogmiosSettings) (kupoSettings, error) {
	settings := kupoSettings{
		enabled: true,
		image:   defaultKupoImage,
		port:    defaultKupoPort,
	}
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Kupo == nil {
		// Cascade default: kupo follows ogmios when the user has not asked for
		// it explicitly.
		if !ogmios.enabled {
			settings.enabled = false
		}
		return settings, nil
	}

	spec := network.Spec.ChainAPI.Kupo
	if !spec.Enabled {
		settings.enabled = false
		return settings, nil
	}

	settings.image = strings.TrimSpace(spec.Image)
	if settings.image == "" {
		return kupoSettings{}, unsupportedSpec("kupo image is required")
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return kupoSettings{}, unsupportedSpec("kupo port must be between 1 and 65535")
	}
	settings.port = spec.Port
	if spec.Resources != nil {
		settings.resources = spec.Resources.DeepCopy()
	}

	return settings, nil
}

// resolveFaucetSettings applies the CardanoNetwork spec on top of the package
// defaults and returns the effective faucet configuration. The faucet
// requires both ogmios and kupo to be enabled.
func (b primaryWorkloadBuilder) resolveFaucetSettings(network *yacdv1alpha1.CardanoNetwork, ogmios ogmiosSettings, kupo kupoSettings) (faucetSettings, error) {
	settings := faucetSettings{
		enabled:           false,
		image:             b.resolvedDefaultFaucetImage(),
		port:              defaultFaucetPort,
		defaultSource:     defaultFaucetSource,
		minTopUpLovelace:  defaultFaucetMinLovelace,
		maxTopUpLovelace:  defaultFaucetMaxLovelace,
		authSecretName:    primaryFaucetAuthSecretName(network),
		authSecretKey:     faucetAuthTokenKey,
		authTokenFilePath: faucetAuthTokenPath,
	}
	if network.Spec.ChainAPI == nil || network.Spec.ChainAPI.Faucet == nil {
		return settings, nil
	}

	spec := network.Spec.ChainAPI.Faucet
	if !spec.Enabled {
		settings.enabled = false
		return settings, nil
	}
	settings.enabled = true
	if !ogmios.enabled {
		return faucetSettings{}, unsupportedSpec("faucet requires ogmios to be enabled")
	}
	if !kupo.enabled {
		return faucetSettings{}, unsupportedSpec("faucet requires kupo to be enabled")
	}

	if spec.Image != nil {
		settings.image = strings.TrimSpace(*spec.Image)
	}
	if strings.TrimSpace(settings.image) == "" {
		return faucetSettings{}, unsupportedSpec("faucet image is required")
	}
	// Repository pinning: even when an override is provided, it must live
	// under the same OCI repository as the default. This prevents accidental
	// adoption of a third-party image carrying faucet-shaped APIs.
	defaultImageRepo := imageRepository(b.resolvedDefaultFaucetImage())
	if imageRepository(settings.image) != defaultImageRepo {
		return faucetSettings{}, unsupportedSpec("faucet image repository must match the configured default faucet image repository %q", defaultImageRepo)
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return faucetSettings{}, unsupportedSpec("faucet port must be between 1 and 65535")
	}
	settings.port = spec.Port
	settings.defaultSource = strings.TrimSpace(spec.DefaultSource)
	if err := validateFaucetSourceName(settings.defaultSource); err != nil {
		return faucetSettings{}, err
	}
	if spec.MinTopUpLovelace < 1 {
		return faucetSettings{}, unsupportedSpec("faucet minTopUpLovelace must be greater than 0")
	}
	if spec.MaxTopUpLovelace < 1 {
		return faucetSettings{}, unsupportedSpec("faucet maxTopUpLovelace must be greater than 0")
	}
	if spec.MinTopUpLovelace > spec.MaxTopUpLovelace {
		return faucetSettings{}, unsupportedSpec("faucet minTopUpLovelace must not exceed maxTopUpLovelace")
	}
	settings.minTopUpLovelace = spec.MinTopUpLovelace
	settings.maxTopUpLovelace = spec.MaxTopUpLovelace
	if spec.Resources != nil {
		settings.resources = spec.Resources.DeepCopy()
	}

	return settings, nil
}

// resolvedDefaultFaucetImage returns the effective default faucet image. The
// Reconciler-injected DefaultFaucetImage is the legitimate primary injection
// point for the local dev stack's ko-built image; the package constant
// defaultFaucetImage is the final fallback used only when no injected value
// is configured.
//
// This controller-side fallback is a deliberate exception to the
// "defaults live in the planner" rule because the planner does not know
// about ko-injected images at all.
func (b primaryWorkloadBuilder) resolvedDefaultFaucetImage() string {
	if injected := strings.TrimSpace(b.defaultFaucetImage); injected != "" {
		return injected
	}

	return defaultFaucetImage
}
