package cardanonetwork

import (
	"encoding/json"
	"testing"

	"github.com/meigma/yacd/internal/cardano/localnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// TestLocalnetCreateEnvInitContainerBuildsFragment verifies the deterministic
// Kubernetes container fragment for the cardano-testnet create-env init step.
func TestLocalnetCreateEnvInitContainerBuildsFragment(t *testing.T) {
	plan := testLocalnetPlan(t)

	container, err := newTestPrimaryWorkloadBuilder(t).cardanoTestnetInitContainer(plan)
	require.NoError(t, err)

	assert.Equal(t, "cardano-testnet-create-env", container.Name)
	assert.Equal(t, "ghcr.io/meigma/yacd/cardano-testnet:11.0.1-yacd.1", container.Image)
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

// TestLocalnetCreateEnvInitContainerManifestEnvRoundTrips verifies the
// idempotency manifest is carried as compact JSON in the container environment.
func TestLocalnetCreateEnvInitContainerManifestEnvRoundTrips(t *testing.T) {
	plan := testLocalnetPlan(t)

	container, err := newTestPrimaryWorkloadBuilder(t).cardanoTestnetInitContainer(plan)
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

	container, err := newTestPrimaryWorkloadBuilder(t).cardanoTestnetInitContainer(plan)
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

			_, err := newTestPrimaryWorkloadBuilder(t).cardanoTestnetInitContainer(plan)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
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
