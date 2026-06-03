package cli

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/mocks"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// capturedTarget records the (kubeconfig, context) pair the installer factory
// was called with, so install-command tests can assert the explicit-or-ambient
// targeting behavior.
type capturedTarget struct {
	kubeconfig  string
	kubeContext string
	calls       int
}

// newInstallRoot builds a root command whose OperatorInstallerFactory returns
// the given mock installer and records the target it was called with. The
// cluster-state factory is wired to a *mocks.Store that fails the test if it is
// ever consulted: install must never read the managed-devnet record.
func newInstallRoot(t *testing.T, out, errOut *bytes.Buffer) (*mocks.Installer, *capturedTarget, func(args ...string) error) {
	t.Helper()

	installer := mocks.NewInstaller(t)
	target := &capturedTarget{}

	// A clusterstate store that panics on any call: install must not consult the
	// managed-devnet record. mockery's NewStore with no EXPECT() asserts that no
	// method is called at cleanup, which is exactly the guard we want.
	store := mocks.NewStore(t)

	root := NewRootCommand(Options{
		Out:                 out,
		Err:                 errOut,
		Viper:               viper.New(),
		ClusterStateFactory: func() (clusterstate.Store, error) { return store, nil },
		ClusterProvisionerFactory: func() (cluster.Provisioner, error) {
			return nil, fmt.Errorf("install must not provision a cluster")
		},
		OperatorInstallerFactory: func(kubeconfig, kubeContext string) (operator.Installer, error) {
			target.kubeconfig = kubeconfig
			target.kubeContext = kubeContext
			target.calls++
			return installer, nil
		},
	})

	run := func(args ...string) error {
		root.SetArgs(args)
		return root.ExecuteContext(context.Background())
	}

	return installer, target, run
}

// readyState is the cluster state EnsureOperator returns for a successful,
// already-ready install at the default operator version.
func readyState() operator.State {
	return operator.State{Installed: true, Ready: true, Version: "v0.1.1"}
}

func TestInstallHappyDefaultNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// Happy path: the apply returns already-ready, so no readiness poll is
	// needed. The spec carries the empty (default) namespace and the pinned
	// Default() values.
	installer.EXPECT().
		EnsureOperator(mock.Anything, operator.InstallSpec{Namespace: "", Values: operator.Default()}).
		Return(readyState(), nil)

	require.NoError(t, run("install"))

	out := stdout.String()
	assert.Contains(t, out, "v0.1.1")
	assert.Contains(t, out, operator.DefaultNamespace)
}

func TestInstallNamespaceFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	installer.EXPECT().
		EnsureOperator(mock.Anything, operator.InstallSpec{Namespace: "foo", Values: operator.Default()}).
		Return(readyState(), nil)

	require.NoError(t, run("install", "-n", "foo"))

	out := stdout.String()
	assert.Contains(t, out, "v0.1.1")
	assert.Contains(t, out, "foo")
}

func TestInstallExplicitTargetPassedToFactory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, target, run := newInstallRoot(t, &stdout, &stderr)

	installer.EXPECT().
		EnsureOperator(mock.Anything, mock.Anything).
		Return(readyState(), nil)

	// Unlike devnet, install accepts an explicit target and forwards it verbatim
	// to the installer factory (which lets ssa.New bind to that kubeconfig/context).
	require.NoError(t, run("install", "--kubeconfig", "/x", "--context", "y"))

	assert.Equal(t, 1, target.calls)
	assert.Equal(t, "/x", target.kubeconfig)
	assert.Equal(t, "y", target.kubeContext)
}

func TestInstallAmbientTargetWhenNoFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, target, run := newInstallRoot(t, &stdout, &stderr)

	installer.EXPECT().
		EnsureOperator(mock.Anything, mock.Anything).
		Return(readyState(), nil)

	// No explicit target: the factory is called with empty strings so ssa.New
	// defers to the ambient kubeconfig current-context. The clusterstate store
	// (wired in newInstallRoot) must never be consulted, which mockery asserts at
	// cleanup because no EXPECT was registered on it.
	require.NoError(t, run("install"))

	assert.Equal(t, 1, target.calls)
	assert.Equal(t, "", target.kubeconfig)
	assert.Equal(t, "", target.kubeContext)
}

func TestInstallWaitsForReadiness(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// The apply returns not-ready (the SSA apply does not wait for the workload),
	// so --wait (the default) drives a readiness poll that converges to Ready.
	installer.EXPECT().
		EnsureOperator(mock.Anything, mock.Anything).
		Return(operator.State{Installed: true, Ready: false, Version: "v0.1.1"}, nil)
	installer.EXPECT().
		OperatorState(mock.Anything, operator.DefaultNamespace).
		Return(readyState(), nil)

	require.NoError(t, run("install"))

	out := stdout.String()
	assert.Contains(t, out, "ready")
	assert.Contains(t, out, "v0.1.1")
	assert.Contains(t, out, operator.DefaultNamespace)
}

func TestInstallWaitsForReadinessInExplicitNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// A non-default -n must flow through to the readiness poll: the command
	// resolves "foo" and the poll must target "foo", not the empty string or the
	// coincidental DefaultNamespace. The exact-match OperatorState("foo") locks
	// the resolved-namespace plumbing into the wait path so a regression that
	// polled the wrong namespace for a custom -n value is caught.
	installer.EXPECT().
		EnsureOperator(mock.Anything, operator.InstallSpec{Namespace: "foo", Values: operator.Default()}).
		Return(operator.State{Installed: true, Ready: false, Version: "v0.1.1"}, nil)
	installer.EXPECT().
		OperatorState(mock.Anything, "foo").
		Return(readyState(), nil)

	require.NoError(t, run("install", "-n", "foo"))

	out := stdout.String()
	assert.Contains(t, out, "ready")
	assert.Contains(t, out, "v0.1.1")
	assert.Contains(t, out, "foo")
	// The default namespace must never be polled when an explicit -n is given.
	installer.AssertNotCalled(t, "OperatorState", mock.Anything, operator.DefaultNamespace)
}

func TestInstallWaitTimeoutReturnsError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// The operator never becomes ready: the apply returns not-ready and the poll
	// keeps observing not-ready until the bounded --wait deadline expires. A tiny
	// timeout makes this deterministic via the context deadline (no wall-clock
	// polling), and the failure is mapped to the "did not become ready" message.
	installer.EXPECT().
		EnsureOperator(mock.Anything, mock.Anything).
		Return(operator.State{Installed: true, Ready: false, Version: "v0.1.1"}, nil)
	installer.EXPECT().
		OperatorState(mock.Anything, operator.DefaultNamespace).
		Return(operator.State{Installed: true, Ready: false, Version: "v0.1.1"}, nil).
		Maybe()

	err := run("install", "--timeout", "1ms")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not become ready")
}

func TestInstallRejectsNonPositiveTimeoutWhenWaiting(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// --wait defaults to true, so a non-positive --timeout is rejected before any
	// apply happens. EnsureOperator must never be called (the guard short-circuits
	// before any mutation).
	err := run("install", "--timeout", "0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--timeout must be greater than 0")
	installer.AssertNotCalled(t, "EnsureOperator", mock.Anything, mock.Anything)
}

func TestInstallNoWaitSkipsReadinessPoll(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// With --wait=false the apply result is reported as-is and OperatorState is
	// never polled (mockery fails the test if it is invoked, since no EXPECT was
	// registered for it).
	installer.EXPECT().
		EnsureOperator(mock.Anything, mock.Anything).
		Return(operator.State{Installed: true, Ready: false, Version: "v0.1.1"}, nil)

	require.NoError(t, run("install", "--wait=false"))

	out := stdout.String()
	assert.Contains(t, out, "not waiting")
	assert.Contains(t, out, "v0.1.1")
	installer.AssertNotCalled(t, "OperatorState", mock.Anything, mock.Anything)
}

func TestInstallDryRunPrintsPlanWithoutApplying(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// Dry run computes the plan via Plan and prints it; EnsureOperator must never
	// be called (no EXPECT registered, so mockery would fail if it were).
	installer.EXPECT().
		Plan(mock.Anything, operator.InstallSpec{Namespace: "", Values: operator.Default()}).
		Return(operator.Decision{
			Action:           operator.ActionInstall,
			InstalledVersion: "",
			TargetVersion:    "v0.1.1",
		}, nil)

	require.NoError(t, run("install", "--dry-run"))

	out := stdout.String()
	assert.Contains(t, out, "Plan")
	assert.Contains(t, out, "install")
	assert.Contains(t, out, "v0.1.1")
	assert.Contains(t, out, operator.DefaultNamespace)
	installer.AssertNotCalled(t, "EnsureOperator", mock.Anything, mock.Anything)
}

func TestInstallDryRunNamespaceFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// A non-default -n must reach Plan and appear in the plan output, parallel to
	// the non-dry-run TestInstallNamespaceFlag, so the resolved namespace is locked
	// into both the plan call and its rendered line.
	installer.EXPECT().
		Plan(mock.Anything, operator.InstallSpec{Namespace: "foo", Values: operator.Default()}).
		Return(operator.Decision{
			Action:           operator.ActionInstall,
			InstalledVersion: "",
			TargetVersion:    "v0.1.1",
		}, nil)

	require.NoError(t, run("install", "--dry-run", "-n", "foo"))

	out := stdout.String()
	assert.Contains(t, out, "Plan")
	assert.Contains(t, out, "foo")
	assert.Contains(t, out, "v0.1.1")
	installer.AssertNotCalled(t, "EnsureOperator", mock.Anything, mock.Anything)
}

func TestInstallDryRunWithNonPositiveTimeoutStillPlans(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// Dry-run never waits, so a non-positive --timeout must not gate the preview:
	// the plan is still computed and printed even though --wait defaults to true.
	installer.EXPECT().
		Plan(mock.Anything, operator.InstallSpec{Namespace: "", Values: operator.Default()}).
		Return(operator.Decision{
			Action:           operator.ActionInstall,
			InstalledVersion: "",
			TargetVersion:    "v0.1.1",
		}, nil)

	require.NoError(t, run("install", "--dry-run", "--timeout", "0"))

	out := stdout.String()
	assert.Contains(t, out, "Plan")
	assert.Contains(t, out, "v0.1.1")
	installer.AssertNotCalled(t, "EnsureOperator", mock.Anything, mock.Anything)
}

func TestInstallDryRunRefuseReturnsErrorWithGuidance(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// A would-refuse plan: Plan returns the typed ErrNewerOperator. The command
	// returns the typed error with actionable guidance attached so main surfaces
	// it once on stderr, matching the real-install refuse path, and exits nonzero.
	installer.EXPECT().
		Plan(mock.Anything, mock.Anything).
		Return(operator.Decision{Action: operator.ActionRefuse}, operator.ErrNewerOperator)

	err := run("install", "--dry-run")
	require.Error(t, err)
	assert.ErrorIs(t, err, operator.ErrNewerOperator)
	assert.Contains(t, err.Error(), "upgrade the CLI")
	assert.Contains(t, err.Error(), "refuse to install")
	// Nothing is printed to stdout for a refusal: the message belongs on stderr,
	// surfaced once by main, not duplicated across both streams.
	assert.Empty(t, stdout.String())
	installer.AssertNotCalled(t, "EnsureOperator", mock.Anything, mock.Anything)
}

func TestInstallRefuseReturnsActionableError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	installer, _, run := newInstallRoot(t, &stdout, &stderr)

	// A real install that the in-cluster policy refuses: EnsureOperator returns
	// ErrNewerOperator. The command surfaces an error whose message includes
	// actionable guidance and preserves the sentinel for errors.Is matching.
	installer.EXPECT().
		EnsureOperator(mock.Anything, mock.Anything).
		Return(operator.State{}, fmt.Errorf("%w: installed v0.2.0, this CLI embeds v0.1.1", operator.ErrNewerOperator))

	err := run("install")
	require.Error(t, err)
	assert.ErrorIs(t, err, operator.ErrNewerOperator)
	assert.Contains(t, err.Error(), "upgrade the CLI")
}
