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
	return cluster.Spec{Name: "yacd", K3sImage: cluster.K3sImage, Timeout: 2 * time.Minute}
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

	status, err := prov.Status(context.Background(), "yacd")
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

	status, err := prov.Status(context.Background(), "yacd")
	require.NoError(t, err)
	assert.False(t, status.Exists)
}
