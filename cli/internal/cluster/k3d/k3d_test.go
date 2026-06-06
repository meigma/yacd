package k3d

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubResolver returns a fixed binary path.
type stubResolver struct{ path string }

func (s stubResolver) Resolve(context.Context) (string, error) { return s.path, nil }

// recordedCall is one runner invocation.
type recordedCall struct {
	name string
	args []string
}

// scriptedResponse is what the fake runner returns for a subcommand.
type scriptedResponse struct {
	stdout []byte
	stderr []byte
	err    error
}

// fakeRunner records calls in order and returns a response chosen by the k3d
// subcommand (args[1]: list/create/delete).
type fakeRunner struct {
	responses map[string]scriptedResponse
	calls     []recordedCall
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, recordedCall{name: name, args: args})
	resp := f.responses[args[1]] // "list" | "create" | "delete"
	return resp.stdout, resp.stderr, resp.err
}

func (f *fakeRunner) subcommands() []string {
	subs := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		subs = append(subs, c.args[1])
	}
	return subs
}

func newProvisioner(runner *fakeRunner, prober prober) *Provisioner {
	return &Provisioner{resolver: stubResolver{path: "k3d"}, runner: runner, prober: prober}
}

func healthyProber(context.Context, string, string) error   { return nil }
func unhealthyProber(context.Context, string, string) error { return errors.New("unreachable") }

func proberThatPanics(context.Context, string, string) error {
	panic("prober must not be called when the cluster is not running")
}

const runningClusterJSON = `[{"name":"yacd","serversRunning":1,"serversCount":1,"agentsRunning":0,"agentsCount":0}]`

func devSpec() cluster.Spec {
	return cluster.Spec{Name: "yacd", K3sImage: cluster.K3sImage, Timeout: 2 * time.Minute, PortMappings: cluster.DefaultPortMappings}
}

func TestEnsureClusterCreatesWhenAbsent(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list":   {stdout: []byte("[]")},
		"create": {},
	}}
	prov := newProvisioner(runner, proberThatPanics)

	info, err := prov.EnsureCluster(context.Background(), devSpec())
	require.NoError(t, err)

	assert.Equal(t, []string{"list", "create"}, runner.subcommands())
	assert.Equal(t, "k3d-yacd", info.Context)
	assert.True(t, info.Running)

	createArgs := runner.calls[1].args
	assert.Contains(t, createArgs, "--image")
	assert.Contains(t, createArgs, cluster.K3sImage)
	assert.Contains(t, createArgs, "--wait")
	assert.Contains(t, createArgs, "--timeout")
	// Each port mapping is published as a "--port HOST:NODEPORT@loadbalancer"
	// pair, in order. Assert the exact set so a dropped, duplicated, or malformed
	// mapping fails rather than slipping past a presence-only check.
	assert.Equal(t, []string{"1337:30137@loadbalancer", "1442:30442@loadbalancer"}, portFlagValues(createArgs))
}

// portFlagValues returns the value following each "--port" flag in args, in
// order, so a test can assert the exact set of published port mappings.
func portFlagValues(args []string) []string {
	var values []string
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			values = append(values, args[i+1])
		}
	}
	return values
}

func TestEnsureClusterReportsHostPortConflict(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list": {stdout: []byte("[]")},
		"create": {err: errors.New(
			"exec k3d: exit status 1: Error response from daemon: " +
				"Ports are not available: exposing port TCP 0.0.0.0:1337: " +
				"listen tcp 0.0.0.0:1337: bind: address already in use")},
		"delete": {},
	}}
	prov := newProvisioner(runner, proberThatPanics)

	_, err := prov.EnsureCluster(context.Background(), devSpec())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in use")
	assert.Contains(t, err.Error(), "1337, 1442")
	// The partial cluster is still rolled back.
	assert.Contains(t, runner.subcommands(), "delete")
}

func TestEnsureClusterNoOpWhenHealthy(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list": {stdout: []byte(runningClusterJSON)},
	}}
	prov := newProvisioner(runner, healthyProber)

	info, err := prov.EnsureCluster(context.Background(), devSpec())
	require.NoError(t, err)

	assert.Equal(t, []string{"list"}, runner.subcommands(), "healthy cluster must not be recreated")
	assert.True(t, info.Running)
}

func TestEnsureClusterProbesHealthThroughSpecKubeconfig(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list": {stdout: []byte(runningClusterJSON)},
	}}
	var probedKubeconfig string
	prober := func(_ context.Context, kubeconfig, _ string) error {
		probedKubeconfig = kubeconfig
		return nil
	}
	prov := newProvisioner(runner, prober)

	spec := devSpec()
	spec.KubeconfigPath = "/tmp/saved-kubeconfig"
	_, err := prov.EnsureCluster(context.Background(), spec)
	require.NoError(t, err)

	// A running cluster whose context lives in the recorded kubeconfig must be
	// probed there, not through the ambient default, so it is not deleted when
	// the current kubeconfig no longer references it.
	assert.Equal(t, "/tmp/saved-kubeconfig", probedKubeconfig)
	assert.Equal(t, []string{"list"}, runner.subcommands(), "healthy cluster must not be recreated")
}

func TestEnsureClusterHealsWhenUnhealthy(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list":   {stdout: []byte(runningClusterJSON)},
		"delete": {},
		"create": {},
	}}
	prov := newProvisioner(runner, unhealthyProber)

	_, err := prov.EnsureCluster(context.Background(), devSpec())
	require.NoError(t, err)

	assert.Equal(t, []string{"list", "delete", "create"}, runner.subcommands())
}

func TestEnsureClusterHealsWhenNotRunning(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list":   {stdout: []byte(`[{"name":"yacd","serversRunning":0,"serversCount":1}]`)},
		"delete": {},
		"create": {},
	}}
	// prober must not be consulted when the cluster is not running.
	prov := newProvisioner(runner, proberThatPanics)

	_, err := prov.EnsureCluster(context.Background(), devSpec())
	require.NoError(t, err)

	assert.Equal(t, []string{"list", "delete", "create"}, runner.subcommands())
}

func TestEnsureClusterRollsBackPartialCreate(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list":   {stdout: []byte("[]")},
		"create": {err: errors.New("boom")},
		"delete": {},
	}}
	prov := newProvisioner(runner, proberThatPanics)

	_, err := prov.EnsureCluster(context.Background(), devSpec())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create cluster")

	assert.Equal(t, []string{"list", "create", "delete"}, runner.subcommands(),
		"a failed create must be rolled back with a delete")
}

func TestDeleteClusterToleratesAbsent(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"delete": {stderr: []byte("No cluster found"), err: errors.New("exit status 1")},
	}}
	prov := newProvisioner(runner, healthyProber)

	require.NoError(t, prov.DeleteCluster(context.Background(), "yacd"))
}

func TestDeleteClusterReturnsRealError(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"delete": {stderr: []byte("Cannot connect to the Docker daemon"), err: errors.New("exit status 1")},
	}}
	prov := newProvisioner(runner, healthyProber)

	err := prov.DeleteCluster(context.Background(), "yacd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete cluster")
}

func TestStatusReportsExistenceAndHealth(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list": {stdout: []byte(runningClusterJSON)},
	}}
	prov := newProvisioner(runner, healthyProber)

	status, err := prov.Status(context.Background(), "yacd", "")
	require.NoError(t, err)
	assert.True(t, status.Exists)
	assert.True(t, status.Running)
	assert.True(t, status.Healthy)
	assert.Equal(t, "k3d-yacd", status.Context)
	assert.Equal(t, cluster.K3sImage, status.K3sImage)
}

func TestStatusReportsAbsentCluster(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list": {stdout: []byte(`[{"name":"other","serversRunning":1}]`)},
	}}
	prov := newProvisioner(runner, proberThatPanics)

	status, err := prov.Status(context.Background(), "yacd", "")
	require.NoError(t, err)
	assert.False(t, status.Exists)
}

func TestEnsureClusterNoOpReturnsRecordedKubeconfig(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list": {stdout: []byte(runningClusterJSON)},
	}}
	prov := newProvisioner(runner, healthyProber)

	spec := devSpec()
	spec.KubeconfigPath = "/home/dev/.kube/recorded"
	info, err := prov.EnsureCluster(context.Background(), spec)
	require.NoError(t, err)

	// The healthy no-op must report the recorded kubeconfig (where the context
	// lives), not the ambient default, so the saved state record stays correct
	// and a later run is not pointed at the wrong file.
	assert.Equal(t, "/home/dev/.kube/recorded", info.KubeconfigPath)
	assert.Equal(t, []string{"list"}, runner.subcommands())
}

func TestEnsureClusterNoOpFallsBackToDefaultKubeconfig(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list": {stdout: []byte(runningClusterJSON)},
	}}
	prov := newProvisioner(runner, healthyProber)

	// No recorded path (out-of-band cluster, no record): fall back to a non-empty
	// default rather than an empty path.
	info, err := prov.EnsureCluster(context.Background(), devSpec())
	require.NoError(t, err)
	assert.NotEmpty(t, info.KubeconfigPath)
}

func TestEnsureClusterAbortsOnConfigProbeError(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list": {stdout: []byte(runningClusterJSON)},
	}}
	// A kubeconfig/context load failure is not evidence of unhealth.
	prober := func(context.Context, string, string) error {
		return &probeConfigError{errors.New(`context "k3d-yacd" does not exist`)}
	}
	prov := newProvisioner(runner, prober)

	_, err := prov.EnsureCluster(context.Background(), devSpec())
	require.Error(t, err)
	assert.Equal(t, []string{"list"}, runner.subcommands(),
		"a kubeconfig-load probe error must not delete+recreate a healthy cluster")
}

func TestStatusProbesThroughGivenKubeconfig(t *testing.T) {
	runner := &fakeRunner{responses: map[string]scriptedResponse{
		"list": {stdout: []byte(runningClusterJSON)},
	}}
	var probed string
	prober := func(_ context.Context, kubeconfig, _ string) error {
		probed = kubeconfig
		return nil
	}
	prov := newProvisioner(runner, prober)

	_, err := prov.Status(context.Background(), "yacd", "/home/dev/.kube/recorded")
	require.NoError(t, err)
	assert.Equal(t, "/home/dev/.kube/recorded", probed)
}

func TestCreateReportsContextError(t *testing.T) {
	tests := []struct {
		name   string
		newCtx func() (context.Context, context.CancelFunc)
		expect string
	}{
		{
			name: "cancelled",
			newCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			expect: "cancelled",
		},
		{
			name: "timed out",
			newCtx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			expect: "timed out",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{responses: map[string]scriptedResponse{
				"list":   {stdout: []byte("[]")},
				"create": {err: errors.New("signal: killed")},
				"delete": {},
			}}
			prov := newProvisioner(runner, proberThatPanics)

			ctx, cancel := tt.newCtx()
			defer cancel()
			_, err := prov.EnsureCluster(ctx, devSpec())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expect)
			assert.NotContains(t, err.Error(), "signal: killed",
				"a bounded run should report the cause, not the raw killed-process error")
		})
	}
}
