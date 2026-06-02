package cli

import (
	"fmt"
	"io"

	"github.com/meigma/yacd/cli/internal/clusterstate"
	"github.com/meigma/yacd/cli/internal/kube"
)

// ResolveTarget resolves the Kubernetes target a command operates against,
// applying a fixed precedence:
//
//  1. An explicit --kubeconfig/--context (or the YACD_KUBECONFIG/
//     YACD_KUBE_CONTEXT env vars, already folded into cfg) wins outright.
//  2. Otherwise, when a managed devnet cluster is tracked, its recorded context
//     is used.
//  3. Otherwise the ambient kubeconfig (its current-context) is used.
//
// The managed tier consults the cheap state record rather than probing the
// cluster, so read verbs stay fast and Docker-independent, and so automation
// that always passes an explicit target (CI, Chainsaw) resolves at tier 1 and
// never engages it. A machine that has never run `yacd devnet` has no record
// and resolves at tier 3, identical to the pre-devnet behavior.
func ResolveTarget(cfg RuntimeConfig, store clusterstate.Store) (kube.Config, error) {
	if cfg.Kubeconfig != "" || cfg.KubeContext != "" {
		return kube.Config{Kubeconfig: cfg.Kubeconfig, Context: cfg.KubeContext}, nil
	}

	record, found, err := store.Load()
	if err != nil {
		return kube.Config{}, fmt.Errorf("load managed cluster state: %w", err)
	}
	if found {
		return kube.Config{Kubeconfig: record.KubeconfigPath, Context: record.Context}, nil
	}

	return kube.Config{}, nil
}

// resolveKubeClient resolves the Kubernetes target through the shared
// precedence resolver and builds a client for it. It returns the resolved
// target alongside the client so mutating verbs can announce it.
func (commandContext *commandContext) resolveKubeClient(cfg RuntimeConfig) (kube.Client, kube.Config, error) {
	store, err := commandContext.clusterState()
	if err != nil {
		return nil, kube.Config{}, err
	}
	target, err := ResolveTarget(cfg, store)
	if err != nil {
		return nil, kube.Config{}, err
	}
	if isManagedTarget(cfg, target) {
		commandContext.managedEngaged = true
	}
	client, err := commandContext.kubeClientFactory(target)
	if err != nil {
		return nil, kube.Config{}, err
	}

	return client, target, nil
}

// isManagedTarget reports whether a resolved target came from the tracked
// managed devnet cluster: no explicit target was given (so the resolver could
// fall through to the record) and a context was resolved (so a record existed).
func isManagedTarget(cfg RuntimeConfig, target kube.Config) bool {
	return cfg.Kubeconfig == "" && cfg.KubeContext == "" && target.Context != ""
}

// announceManagedTarget writes a one-line note to w (stderr) when a command
// resolved to the tracked managed devnet cluster, so the user sees which
// cluster a mutating verb is about to change. It is intentionally silent for an
// explicit target (the user already named it) and for ambient resolution (no
// managed cluster), so scripted and CI usage is unaffected.
func announceManagedTarget(w io.Writer, cfg RuntimeConfig, target kube.Config) error {
	if !isManagedTarget(cfg, target) {
		return nil
	}
	if _, err := fmt.Fprintf(w, "Targeting managed devnet (context %q).\n", target.Context); err != nil {
		return fmt.Errorf("write resolved target: %w", err)
	}

	return nil
}
