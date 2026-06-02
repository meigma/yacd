package ssa

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/meigma/yacd/cli/internal/operator"
	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// installNamespace is the namespace the operator is installed into. It matches
// the chart's render namespace; the chart's RBAC subjects are baked to it, so
// this phase rejects any other target.
const installNamespace = "yacd-system"

const (
	// establishTimeout bounds the wait for applied CRDs to become Established.
	establishTimeout = 60 * time.Second

	// pollInterval is the cadence of the Established poll.
	pollInterval = time.Second
)

// installer is the operator.Installer implementation. It applies the embedded,
// build-time-rendered chart to a cluster by server-side apply.
type installer struct {
	client    client.Client
	mapper    apimeta.RESTMapper
	manifests fs.FS
}

// New constructs an installer against the cluster selected by kubeconfig and
// context (empty values defer to the standard kubeconfig loading rules). The
// manifests filesystem holds the rendered chart; pass ssa.Manifests for the
// embedded default.
func New(kubeconfig, kubeContext string, manifests fs.FS) (operator.Installer, error) {
	restCfg, err := restConfig(kubeconfig, kubeContext)
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	c, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}

	return &installer{client: c, mapper: c.RESTMapper(), manifests: manifests}, nil
}

// restConfig loads a rest.Config from explicit kubeconfig path and context
// overrides, mirroring the kube adapter's loader.
func restConfig(kubeconfig, kubeContext string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if path := strings.TrimSpace(kubeconfig); path != "" {
		loadingRules.ExplicitPath = path
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: strings.TrimSpace(kubeContext)}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes config: %w", err)
	}

	return cfg, nil
}

// EnsureOperator reconciles the cluster to the embedded operator version.
func (i *installer) EnsureOperator(ctx context.Context, spec operator.InstallSpec) (operator.State, error) {
	if ns := strings.TrimSpace(spec.Namespace); ns != "" && ns != installNamespace {
		return operator.State{}, fmt.Errorf(
			"operator install namespace is pinned to %q in this version; %q is not supported",
			installNamespace, ns,
		)
	}

	objs, err := parseManifests(i.manifests, manifestPath)
	if err != nil {
		return operator.State{}, err
	}

	embedded, err := versionFromObjects(objs)
	if err != nil {
		return operator.State{}, err
	}

	state, err := i.OperatorState(ctx)
	if err != nil {
		return operator.State{}, err
	}

	if _, err := operator.Decide(embedded, state); err != nil {
		// Refuse: return the observed state alongside the actionable error.
		return state, err
	}

	if err := i.apply(ctx, objs); err != nil {
		return operator.State{}, err
	}

	return i.OperatorState(ctx)
}

// apply runs the idempotent install pipeline: ensure namespace, apply CRDs and
// wait Established, apply the workload, then prune.
func (i *installer) apply(ctx context.Context, objs []*unstructured.Unstructured) error {
	applied := make(map[objectKey]struct{}, len(objs)+1)

	nsKey, err := applyObject(ctx, i.client, i.mapper, namespaceObject(installNamespace), installNamespace)
	if err != nil {
		return err
	}
	applied[nsKey] = struct{}{}

	crds, workload := partitionCRDs(objs)
	for _, crd := range crds {
		key, err := applyObject(ctx, i.client, i.mapper, crd, installNamespace)
		if err != nil {
			return err
		}
		applied[key] = struct{}{}
	}
	if err := i.waitEstablished(ctx, crdNames(crds)); err != nil {
		return err
	}

	for _, obj := range workload {
		key, err := applyObject(ctx, i.client, i.mapper, obj, installNamespace)
		if err != nil {
			return err
		}
		applied[key] = struct{}{}
	}

	return prune(ctx, i.client, i.mapper, installNamespace, applied)
}

// waitEstablished polls each named CRD until it reports Established=True.
func (i *installer) waitEstablished(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}

	return wait.PollUntilContextTimeout(ctx, pollInterval, establishTimeout, true, func(ctx context.Context) (bool, error) {
		for _, name := range names {
			crd := &apiextensionsv1.CustomResourceDefinition{}
			if err := i.client.Get(ctx, client.ObjectKey{Name: name}, crd); err != nil {
				return false, fmt.Errorf("get crd %s: %w", name, err)
			}
			if !establishedTrue(crd) {
				return false, nil
			}
		}
		return true, nil
	})
}

// OperatorState reports the install state from the manager Deployment.
func (i *installer) OperatorState(ctx context.Context) (operator.State, error) {
	deployment, err := i.managerDeployment(ctx)
	if err != nil {
		return operator.State{}, err
	}
	if deployment == nil {
		return operator.State{Installed: false}, nil
	}

	return operator.State{
		Installed: true,
		Ready:     deploymentAvailable(deployment),
		Version:   deployment.Labels[versionLabel],
	}, nil
}

// managerDeployment returns the operator manager Deployment, or nil when none
// exists. More than one match is an error: the install is ambiguous and must
// not be reconciled blindly.
func (i *installer) managerDeployment(ctx context.Context) (*appsv1.Deployment, error) {
	list := &appsv1.DeploymentList{}
	err := i.client.List(ctx, list,
		client.InNamespace(installNamespace),
		client.MatchingLabels{managerNameLabel: managerNameValue, controlPlaneLabel: controlPlaneValue},
	)
	if err != nil {
		return nil, fmt.Errorf("list operator deployments: %w", err)
	}

	switch len(list.Items) {
	case 0:
		return nil, nil
	case 1:
		return &list.Items[0], nil
	default:
		return nil, fmt.Errorf("ambiguous operator install: %d manager Deployments in %s", len(list.Items), installNamespace)
	}
}
