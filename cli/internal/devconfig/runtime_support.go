package devconfig

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	defaultOgmiosImage       = "cardanosolutions/ogmios:v6.14.0"
	defaultOgmiosPort  int32 = 1337

	defaultKupoImage       = "cardanosolutions/kupo:v2.11.0"
	defaultKupoPort  int32 = 1442

	defaultFaucetPort int32 = 8080

	minimumMainnetNodeStorageSize = "300Gi"
)

var supportedOgmiosNodeVersions = map[string][]string{
	"v6.14": {"10.5.1", "10.5.3", "11.0.1"},
	"v6.13": {"10.1.2", "10.1.3", "10.1.4"},
	"v6.12": {"10.1.2", "10.1.3", "10.1.4"},
	"v6.11": {"10.1.2", "10.1.3", "10.1.4"},
	"v6.10": {"10.1.2", "10.1.3", "10.1.4"},
	"v6.9":  {"10.1.2", "10.1.3"},
	"v6.8":  {"9.1.1", "9.2.0"},
	"v6.7":  {"9.1.1", "9.2.0"},
}

type chainAPISettings struct {
	ogmios componentSettings
	kupo   componentSettings
	faucet componentSettings
}

type componentSettings struct {
	enabled bool
	image   string
	port    int32
}

func validateRuntimeSupport(network yacdv1alpha1.CardanoNetworkSpec) error {
	if err := validateSharedRuntimeSupport(network); err != nil {
		return err
	}
	switch network.Mode {
	case yacdv1alpha1.CardanoNetworkModeLocal:
		if err := validateLocalRuntimeSupport(network); err != nil {
			return err
		}
	case yacdv1alpha1.CardanoNetworkModePublic:
		if err := validatePublicRuntimeSupport(network); err != nil {
			return err
		}
	}

	settings, err := resolveChainAPIRuntimeSupport(network)
	if err != nil {
		return err
	}

	return validateRuntimePortConflicts(network.Node.Port, settings)
}

func validateSharedRuntimeSupport(network yacdv1alpha1.CardanoNetworkSpec) error {
	if network.Node.Image != nil && strings.TrimSpace(*network.Node.Image) == "" {
		return fmt.Errorf("spec.network.node.image must not be blank")
	}
	if network.Node.Port < 1 || network.Node.Port > 65535 {
		return fmt.Errorf("spec.network.node.port must be between 1 and 65535")
	}
	if network.Mode == yacdv1alpha1.CardanoNetworkModePublic &&
		network.Public != nil &&
		network.Public.Profile == yacdv1alpha1.PublicNetworkProfileMainnet &&
		network.Node.Storage != nil {
		minimum := resource.MustParse(minimumMainnetNodeStorageSize)
		if network.Node.Storage.Size.Cmp(minimum) < 0 {
			return fmt.Errorf("spec.network.node.storage.size must be at least %s for public mainnet", minimumMainnetNodeStorageSize)
		}
	}

	return nil
}

func validateLocalRuntimeSupport(network yacdv1alpha1.CardanoNetworkSpec) error {
	local := network.Local
	if local == nil {
		return nil
	}
	if local.Era != yacdv1alpha1.CardanoEraConway {
		return fmt.Errorf("spec.network.local.era %q is not supported; supported value: %q", local.Era, yacdv1alpha1.CardanoEraConway)
	}
	if local.Genesis != nil {
		return fmt.Errorf("spec.network.local.genesis is not supported by the current local runtime")
	}
	if local.Topology.Pools.Count != 1 {
		return fmt.Errorf("spec.network.local.topology.pools.count %d is not supported; supported value: 1", local.Topology.Pools.Count)
	}
	if local.Topology.Pools.Defaults != nil {
		return fmt.Errorf("spec.network.local.topology.pools.defaults is not supported by the current local runtime")
	}

	return nil
}

func validatePublicRuntimeSupport(network yacdv1alpha1.CardanoNetworkSpec) error {
	if publicKupoExplicitlyEnabled(network) {
		return fmt.Errorf("spec.network.chainAPI.kupo.enabled=true is not supported for public networks")
	}
	if publicFaucetExplicitlyEnabled(network) {
		return fmt.Errorf("spec.network.chainAPI.faucet.enabled=true is not supported for public networks")
	}

	return nil
}

func resolveChainAPIRuntimeSupport(network yacdv1alpha1.CardanoNetworkSpec) (chainAPISettings, error) {
	ogmios, err := resolveOgmiosRuntimeSupport(network)
	if err != nil {
		return chainAPISettings{}, err
	}
	kupo, kupoMentioned, err := resolveKupoRuntimeSupport(network)
	if err != nil {
		return chainAPISettings{}, err
	}
	if !kupoMentioned && !ogmios.enabled {
		kupo.enabled = false
	}
	if network.Mode == yacdv1alpha1.CardanoNetworkModePublic && !kupoMentioned {
		kupo.enabled = false
	}
	if kupo.enabled && !ogmios.enabled {
		return chainAPISettings{}, fmt.Errorf("spec.network.chainAPI.kupo.enabled=true requires spec.network.chainAPI.ogmios.enabled=true")
	}
	if err := validateOgmiosRuntimeCompatibility(network.Node.Version, ogmios); err != nil {
		return chainAPISettings{}, err
	}
	if err := validateKupoRuntimeImage(kupo); err != nil {
		return chainAPISettings{}, err
	}
	faucet, err := resolveFaucetRuntimeSupport(network, ogmios, kupo)
	if err != nil {
		return chainAPISettings{}, err
	}

	return chainAPISettings{ogmios: ogmios, kupo: kupo, faucet: faucet}, nil
}

func resolveOgmiosRuntimeSupport(network yacdv1alpha1.CardanoNetworkSpec) (componentSettings, error) {
	settings := componentSettings{
		enabled: true,
		image:   defaultOgmiosImage,
		port:    defaultOgmiosPort,
	}
	if network.ChainAPI == nil || network.ChainAPI.Ogmios == nil {
		return settings, nil
	}
	spec := network.ChainAPI.Ogmios
	if !spec.Enabled {
		settings.enabled = false

		return settings, nil
	}
	settings.image = strings.TrimSpace(spec.Image)
	if settings.image == "" {
		return componentSettings{}, fmt.Errorf("spec.network.chainAPI.ogmios.image is required when ogmios is enabled")
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return componentSettings{}, fmt.Errorf("spec.network.chainAPI.ogmios.port must be between 1 and 65535")
	}
	settings.port = spec.Port

	return settings, nil
}

func resolveKupoRuntimeSupport(network yacdv1alpha1.CardanoNetworkSpec) (componentSettings, bool, error) {
	settings := componentSettings{
		enabled: true,
		image:   defaultKupoImage,
		port:    defaultKupoPort,
	}
	if network.ChainAPI == nil || network.ChainAPI.Kupo == nil {
		return settings, false, nil
	}
	spec := network.ChainAPI.Kupo
	if !spec.Enabled {
		settings.enabled = false

		return settings, true, nil
	}
	settings.image = strings.TrimSpace(spec.Image)
	if settings.image == "" {
		return componentSettings{}, true, fmt.Errorf("spec.network.chainAPI.kupo.image is required when kupo is enabled")
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return componentSettings{}, true, fmt.Errorf("spec.network.chainAPI.kupo.port must be between 1 and 65535")
	}
	settings.port = spec.Port

	return settings, true, nil
}

func resolveFaucetRuntimeSupport(network yacdv1alpha1.CardanoNetworkSpec, ogmios componentSettings, kupo componentSettings) (componentSettings, error) {
	settings := componentSettings{
		enabled: false,
		port:    defaultFaucetPort,
	}
	if network.ChainAPI == nil || network.ChainAPI.Faucet == nil {
		return settings, nil
	}
	spec := network.ChainAPI.Faucet
	if !spec.Enabled {
		return settings, nil
	}
	if !ogmios.enabled {
		return componentSettings{}, fmt.Errorf("spec.network.chainAPI.faucet.enabled=true requires spec.network.chainAPI.ogmios.enabled=true")
	}
	if !kupo.enabled {
		return componentSettings{}, fmt.Errorf("spec.network.chainAPI.faucet.enabled=true requires spec.network.chainAPI.kupo.enabled=true")
	}
	if spec.Image != nil && strings.TrimSpace(*spec.Image) == "" {
		return componentSettings{}, fmt.Errorf("spec.network.chainAPI.faucet.image must not be blank")
	}
	if spec.Port < 1 || spec.Port > 65535 {
		return componentSettings{}, fmt.Errorf("spec.network.chainAPI.faucet.port must be between 1 and 65535")
	}
	if err := validateFaucetSourceName(spec.DefaultSource); err != nil {
		return componentSettings{}, err
	}
	if spec.MinTopUpLovelace < 1 {
		return componentSettings{}, fmt.Errorf("spec.network.chainAPI.faucet.minTopUpLovelace must be greater than 0")
	}
	if spec.MaxTopUpLovelace < 1 {
		return componentSettings{}, fmt.Errorf("spec.network.chainAPI.faucet.maxTopUpLovelace must be greater than 0")
	}
	if spec.MinTopUpLovelace > spec.MaxTopUpLovelace {
		return componentSettings{}, fmt.Errorf("spec.network.chainAPI.faucet.minTopUpLovelace must not exceed maxTopUpLovelace")
	}
	settings.enabled = true
	settings.port = spec.Port

	return settings, nil
}

func validateOgmiosRuntimeCompatibility(nodeVersion string, settings componentSettings) error {
	if !settings.enabled {
		return nil
	}
	key, err := ogmiosCompatibilityKey(settings.image)
	if err != nil {
		return err
	}
	supported, ok := supportedOgmiosNodeVersions[key]
	if !ok {
		tag, _ := containerImageTag(settings.image)
		return fmt.Errorf("spec.network.chainAPI.ogmios.image tag %q is not supported", tag)
	}
	nodeVersion = strings.TrimSpace(nodeVersion)
	if slices.Contains(supported, nodeVersion) {
		return nil
	}

	return fmt.Errorf("spec.network.chainAPI.ogmios.image %s.* is not supported with spec.network.node.version %s; supported cardano-node versions: %s", key, nodeVersion, strings.Join(supported, ", "))
}

func validateKupoRuntimeImage(settings componentSettings) error {
	if !settings.enabled || settings.image == defaultKupoImage {
		return nil
	}

	return fmt.Errorf("spec.network.chainAPI.kupo.image %q is not supported; supported image: %s", settings.image, defaultKupoImage)
}

func validateRuntimePortConflicts(nodePort int32, settings chainAPISettings) error {
	seen := map[int32]string{
		nodePort: "spec.network.node.port",
	}
	for _, component := range []struct {
		name     string
		settings componentSettings
	}{
		{name: "ogmios", settings: settings.ogmios},
		{name: "kupo", settings: settings.kupo},
		{name: "faucet", settings: settings.faucet},
	} {
		if !component.settings.enabled {
			continue
		}
		if owner, ok := seen[component.settings.port]; ok {
			return fmt.Errorf("spec.network.chainAPI.%s.port %d conflicts with %s", component.name, component.settings.port, owner)
		}
		seen[component.settings.port] = "spec.network.chainAPI." + component.name + ".port"
	}

	return nil
}

func validateFaucetSourceName(sourceName string) error {
	sourceName = strings.TrimSpace(sourceName)
	if !strings.HasPrefix(sourceName, "utxo") || len(sourceName) < len("utxo1") {
		return fmt.Errorf("spec.network.chainAPI.faucet.defaultSource must use the utxoN source name format")
	}
	digits := sourceName[len("utxo"):]
	if digits[0] == '0' {
		return fmt.Errorf("spec.network.chainAPI.faucet.defaultSource must use the utxoN source name format")
	}
	for _, char := range digits {
		if char < '0' || char > '9' {
			return fmt.Errorf("spec.network.chainAPI.faucet.defaultSource must use the utxoN source name format")
		}
	}

	return nil
}

func publicKupoExplicitlyEnabled(network yacdv1alpha1.CardanoNetworkSpec) bool {
	return network.ChainAPI != nil && network.ChainAPI.Kupo != nil && network.ChainAPI.Kupo.Enabled
}

func publicFaucetExplicitlyEnabled(network yacdv1alpha1.CardanoNetworkSpec) bool {
	return network.ChainAPI != nil && network.ChainAPI.Faucet != nil && network.ChainAPI.Faucet.Enabled
}

func ogmiosCompatibilityKey(image string) (string, error) {
	tag, ok := containerImageTag(image)
	if !ok {
		return "", fmt.Errorf("spec.network.chainAPI.ogmios.image %q must include a supported release tag", image)
	}
	if !strings.HasPrefix(tag, "v") {
		return "", fmt.Errorf("spec.network.chainAPI.ogmios.image tag %q is not a supported release tag", tag)
	}

	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("spec.network.chainAPI.ogmios.image tag %q is not a supported release tag", tag)
	}
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return "", fmt.Errorf("spec.network.chainAPI.ogmios.image tag %q is not a supported release tag", tag)
		}
	}

	return "v" + parts[0] + "." + parts[1], nil
}

func containerImageTag(image string) (string, bool) {
	withoutDigest, _, _ := strings.Cut(strings.TrimSpace(image), "@")
	lastSlash := strings.LastIndex(withoutDigest, "/")
	lastColon := strings.LastIndex(withoutDigest, ":")
	if lastColon <= lastSlash {
		return "", false
	}

	return withoutDigest[lastColon+1:], true
}
