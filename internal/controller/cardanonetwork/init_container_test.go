package cardanonetwork

import (
	"encoding/json"
	"testing"

	"github.com/meigma/yacd/internal/cardano/localnet"
	"github.com/meigma/yacd/internal/cardano/toolsimage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// TestLocalnetCreateEnvInitContainerBuildsFragment verifies the deterministic
// Kubernetes container fragment for the cardano-testnet create-env init step.
func TestLocalnetCreateEnvInitContainerBuildsFragment(t *testing.T) {
	plan := testLocalnetPlan(t)
	network := localCardanoNetwork("devnet")

	container, err := newTestPrimaryWorkloadBuilder(t).cardanoTestnetInitContainer(network, plan)
	require.NoError(t, err)

	assert.Equal(t, "cardano-testnet-create-env", container.Name)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.5", container.Image)
	assert.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)
	assert.Equal(t, []string{"/opt/yacd/bin/yacd-cardano-testnet-init"}, container.Command)
	assert.Equal(t, corev1.TerminationMessagePathDefault, container.TerminationMessagePath)
	assert.Equal(t, corev1.TerminationMessageFallbackToLogsOnError, container.TerminationMessagePolicy)

	assert.Equal(t, plan.CreateEnv.Args, container.Args)

	assert.Equal(t, []corev1.VolumeMount{
		{
			Name:      "localnet-state",
			MountPath: "/state",
		},
	}, container.VolumeMounts)

	env := envMap(container)
	assert.Equal(t, "/state/env", env["YACD_LOCALNET_ENV_DIR"])
	assert.Equal(t, "/state/env/configuration.yaml", env["YACD_LOCALNET_CONFIG_FILE"])
	assert.Equal(t, "/state/env/yacd-localnet-plan.json", env["YACD_LOCALNET_PLAN_MANIFEST_FILE"])
	assert.NotEmpty(t, env["YACD_LOCALNET_PLAN_MANIFEST"])

	assertRestrictedContainerSecurityContext(t, container.SecurityContext)
}

// TestCardanoTestnetImageHonorsInjectedOverride verifies the Reconciler-
// injected defaultCardanoTestnetImage replaces the legacy
// "<repo>:<toolVersion>-<revision>" reference on the create-env init
// container, the faucet source-address init container, and the default
// cardano-node container. This is the seam the local dev stack uses when
// the published cardano-testnet tag is behind publisher changes
// CardanoDBSync depends on.
func TestCardanoTestnetImageHonorsInjectedOverride(t *testing.T) {
	const override = "ghcr.io/meigma/yacd/cardano-testnet:tilt"

	plan := testLocalnetPlan(t)
	network := localCardanoNetwork("devnet")
	builder := newTestPrimaryWorkloadBuilder(t)
	builder.defaultCardanoTestnetImage = override

	initContainer, err := builder.cardanoTestnetInitContainer(network, plan)
	require.NoError(t, err)
	assert.Equal(t, override, initContainer.Image)

	addressInitContainer := builder.faucetSourceAddressInitContainer(plan)
	assert.Equal(t, override, addressInitContainer.Image)

	assert.Equal(t, override, builder.cardanoNodeImage(network))
}

// TestCardanoToolsImageHonorsInjectedOverride verifies the Reconciler-injected
// defaultCardanoToolsImage replaces the built-in toolsimage reference on both
// the served-artifact init container and the always-on serve sidecar, and that
// the built-in reference (the digest-pinned toolsimage default) is used with no
// override. This is the seam the local dev stack uses to substitute a freshly
// built cardano-tools image.
func TestCardanoToolsImageHonorsInjectedOverride(t *testing.T) {
	const override = "ghcr.io/meigma/yacd/cardano-tools:tilt"

	network := localCardanoNetwork("devnet")

	builder := newTestPrimaryWorkloadBuilder(t)
	defaultResources, err := builder.Build(network)
	require.NoError(t, err)
	defaultStage := requireContainerNamed(t, defaultResources.Deployment.Spec.Template.Spec.InitContainers, servedArtifactsInitContainerName)
	defaultServe := requireContainerNamed(t, defaultResources.Deployment.Spec.Template.Spec.Containers, serveContainerName)
	assert.Equal(t, toolsimage.Reference("", "11.0.1"), defaultStage.Image)
	assert.Equal(t, toolsimage.Reference("", "11.0.1"), defaultServe.Image)

	overrideBuilder := newTestPrimaryWorkloadBuilder(t)
	overrideBuilder.defaultCardanoToolsImage = override
	overrideResources, err := overrideBuilder.Build(network)
	require.NoError(t, err)
	overrideStage := requireContainerNamed(t, overrideResources.Deployment.Spec.Template.Spec.InitContainers, servedArtifactsInitContainerName)
	overrideServe := requireContainerNamed(t, overrideResources.Deployment.Spec.Template.Spec.Containers, serveContainerName)
	assert.Equal(t, override, overrideStage.Image)
	assert.Equal(t, override, overrideServe.Image)

	// The cardano-tools override must not bleed into the cardano-testnet
	// images, which are governed by their own override.
	nodeContainer := requireContainerNamed(t, overrideResources.Deployment.Spec.Template.Spec.Containers, cardanoNodeContainerName)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.5", nodeContainer.Image)
}

// TestLocalnetCreateEnvInitContainerManifestEnvRoundTrips verifies the
// idempotency manifest is carried as compact JSON in the container environment.
func TestLocalnetCreateEnvInitContainerManifestEnvRoundTrips(t *testing.T) {
	plan := testLocalnetPlan(t)

	container, err := newTestPrimaryWorkloadBuilder(t).cardanoTestnetInitContainer(localCardanoNetwork("devnet"), plan)
	require.NoError(t, err)

	raw := envMap(container)["YACD_LOCALNET_PLAN_MANIFEST"]
	assert.NotContains(t, raw, "\n")

	var got localnet.Manifest
	require.NoError(t, json.Unmarshal([]byte(raw), &got))
	assert.Equal(t, plan.Manifest, got)
}

// TestLocalnetCreateEnvInitContainerPreservesPlanArgs verifies the helper does
// not reinterpret the arguments produced by the pure localnet plan builder.
func TestLocalnetCreateEnvInitContainerPreservesPlanArgs(t *testing.T) {
	spec := localnet.DefaultSpec()
	spec.Tool.Binary = "/opt/cardano/bin/cardano-testnet"
	spec.Tool.Version = "11.0.1"
	plan, err := localnet.BuildPlan(spec)
	require.NoError(t, err)

	container, err := newTestPrimaryWorkloadBuilder(t).cardanoTestnetInitContainer(localCardanoNetwork("devnet"), plan)
	require.NoError(t, err)

	assert.Equal(t, []string{"/opt/yacd/bin/yacd-cardano-testnet-init"}, container.Command)
	assert.Equal(t, plan.CreateEnv.Args, container.Args)
}

// TestLocalnetCreateEnvInitContainerRejectsIncompletePlan verifies fields the
// Kubernetes fragment depends on fail before producing an invalid container.
func TestLocalnetCreateEnvInitContainerRejectsIncompletePlan(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*localnet.Plan)
		wantErr string
	}{
		{
			name: "missing tool version",
			mutate: func(plan *localnet.Plan) {
				plan.Spec.Tool.Version = ""
			},
			wantErr: "localnet tool version is required",
		},
		{
			name: "missing create env args",
			mutate: func(plan *localnet.Plan) {
				plan.CreateEnv.Args = nil
			},
			wantErr: "localnet create-env args are required",
		},
		{
			name: "missing state dir",
			mutate: func(plan *localnet.Plan) {
				plan.Layout.StateDir = ""
			},
			wantErr: "localnet state dir is required",
		},
		{
			name: "missing env dir",
			mutate: func(plan *localnet.Plan) {
				plan.Layout.EnvDir = ""
			},
			wantErr: "localnet env dir is required",
		},
		{
			name: "missing config file",
			mutate: func(plan *localnet.Plan) {
				plan.Layout.ConfigFile = ""
			},
			wantErr: "localnet config file is required",
		},
		{
			name: "missing manifest file",
			mutate: func(plan *localnet.Plan) {
				plan.Layout.ManifestFile = ""
			},
			wantErr: "localnet manifest file is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := testLocalnetPlan(t)
			tt.mutate(&plan)

			_, err := newTestPrimaryWorkloadBuilder(t).cardanoTestnetInitContainer(localCardanoNetwork("devnet"), plan)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestFaucetWalletGenesisFundingInitContainer verifies the genesis-funding init
// container invokes the cardano-tools fund-genesis verb with the faucet wallet
// address and funding amount, mounts the state volume, and uses the cardano-tools
// image and the hardened security context shared by the other tools init
// containers.
func TestFaucetWalletGenesisFundingInitContainer(t *testing.T) {
	plan := testLocalnetPlan(t)
	settings := faucetWalletSettings{enabled: true, fundingLovelace: defaultFaucetWalletFundingLovelace}

	b := newTestPrimaryWorkloadBuilder(t)
	container, err := b.faucetWalletGenesisFundingInitContainer(plan, settings, testFaucetWalletAddress)
	require.NoError(t, err)

	assert.Equal(t, faucetWalletGenesisInitContainerName, container.Name)
	assert.Equal(t, b.cardanoToolsImage(plan.Spec.Tool.Version), container.Image)
	assert.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)
	assert.Equal(t, []string{cardanoToolsCommand}, container.Command)
	assert.Empty(t, container.Env)

	assert.Equal(t, []string{
		"fund-genesis",
		"--env-dir", plan.Layout.EnvDir,
		"--address", testFaucetWalletAddress,
		"--lovelace", "1000000000000",
	}, container.Args)

	assert.Equal(t, []corev1.VolumeMount{
		{Name: localnetStateVolumeName, MountPath: "/state"},
	}, container.VolumeMounts)
	assertRestrictedContainerSecurityContext(t, container.SecurityContext)
}

// TestFaucetWalletGenesisFundingInitContainerRequiresAddress verifies the
// builder fails fast when the Reconciler did not thread an address in.
func TestFaucetWalletGenesisFundingInitContainerRequiresAddress(t *testing.T) {
	plan := testLocalnetPlan(t)
	_, err := newTestPrimaryWorkloadBuilder(t).faucetWalletGenesisFundingInitContainer(plan, faucetWalletSettings{enabled: true}, "  ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "faucet wallet address is required")
}

func testLocalnetPlan(t *testing.T) localnet.Plan {
	t.Helper()

	spec := localnet.DefaultSpec()
	spec.Tool.Version = "11.0.1"
	plan, err := localnet.BuildPlan(spec)
	require.NoError(t, err)

	return plan
}

func envMap(container corev1.Container) map[string]string {
	env := make(map[string]string, len(container.Env))
	for _, value := range container.Env {
		env[value.Name] = value.Value
	}

	return env
}
