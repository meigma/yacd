package k3d

import (
	"context"
	"fmt"
	"strings"

	"github.com/meigma/yacd/cli/internal/exec"
	"github.com/meigma/yacd/cli/internal/toolbin"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// prober reports whether a cluster's API server is reachable through the named
// kubeconfig context. An empty kubeconfig path uses the default loading rules.
type prober func(ctx context.Context, kubeconfig, kubeContext string) error

// Provisioner implements cluster.Provisioner over a resolved k3d binary.
type Provisioner struct {
	resolver toolbin.Resolver
	runner   exec.Runner
	prober   prober
}

// New constructs a Provisioner that resolves k3d through resolver and runs it
// through runner.
func New(resolver toolbin.Resolver, runner exec.Runner) *Provisioner {
	return &Provisioner{resolver: resolver, runner: runner, prober: defaultProbe}
}

// run executes the k3d binary and returns its stdout. The runner wraps any
// non-zero exit (with stderr) into the returned error.
func (p *Provisioner) run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	stdout, _, err := p.runner.Run(ctx, bin, args...)
	return stdout, err
}

// defaultProbe pings the API server's /healthz endpoint through the named
// context, honouring ctx for cancellation. A non-nil error means unreachable.
func defaultProbe(ctx context.Context, kubeconfig, kubeContext string) error {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if path := strings.TrimSpace(kubeconfig); path != "" {
		loadingRules.ExplicitPath = path
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: strings.TrimSpace(kubeContext)}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return fmt.Errorf("load kubeconfig for %s: %w", kubeContext, err)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("build client for %s: %w", kubeContext, err)
	}

	if _, err := clientset.Discovery().RESTClient().Get().AbsPath("/healthz").DoRaw(ctx); err != nil {
		return fmt.Errorf("probe api %s: %w", kubeContext, err)
	}

	return nil
}
