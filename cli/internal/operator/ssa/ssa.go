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
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// installNamespace is the default namespace the operator is installed into when
// the caller leaves InstallSpec.Namespace empty. It is no longer a hard pin: any
// valid DNS-1123 namespace renders correctly because the chart's RBAC subjects
// follow the Helm release namespace. It aliases operator.DefaultNamespace so the
// default lives in exactly one place.
const installNamespace = operator.DefaultNamespace

const (
	// establishTimeout bounds the wait for applied CRDs to become Established.
	establishTimeout = 60 * time.Second

	// pollInterval is the cadence of the Established poll.
	pollInterval = time.Second
)

// installer is the operator.Installer implementation. It renders the embedded
// chart in-memory and applies it to a cluster by server-side apply.
type installer struct {
	client client.Client
	mapper apimeta.RESTMapper
	chart  fs.FS
}

// New constructs an installer against the cluster selected by kubeconfig and
// context (empty values defer to the standard kubeconfig loading rules). The
// chart filesystem, rooted at the operator Helm chart, holds what is rendered
// and applied; pass charts.OperatorChart for the embedded default.
func New(kubeconfig, kubeContext string, chart fs.FS) (operator.Installer, error) {
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

	return &installer{client: c, mapper: c.RESTMapper(), chart: chart}, nil
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

// EnsureOperator reconciles the cluster to the embedded operator version. It
// resolves the install namespace (empty defaults to installNamespace; any other
// value must be a valid DNS-1123 label), renders the embedded chart against it
// and the spec's typed values, then applies the rendered objects.
func (i *installer) EnsureOperator(ctx context.Context, spec operator.InstallSpec) (operator.State, error) {
	namespace, err := resolveNamespace(spec.Namespace)
	if err != nil {
		return operator.State{}, err
	}

	objs, err := render(i.chart, namespace, spec.Values.ToHelmValues())
	if err != nil {
		return operator.State{}, err
	}

	embedded, err := versionFromObjects(objs)
	if err != nil {
		return operator.State{}, err
	}

	state, err := i.operatorState(ctx, namespace)
	if err != nil {
		return operator.State{}, err
	}

	if _, err := operator.Decide(embedded, state); err != nil {
		// Refuse: return the observed state alongside the actionable error.
		return state, err
	}

	if err := i.apply(ctx, objs, namespace); err != nil {
		return operator.State{}, err
	}

	return i.operatorState(ctx, namespace)
}

// resolveNamespace defaults an empty namespace to installNamespace and validates
// any explicit value as a DNS-1123 label, the namespace naming rule.
func resolveNamespace(requested string) (string, error) {
	ns := strings.TrimSpace(requested)
	if ns == "" {
		return installNamespace, nil
	}
	if errs := validation.IsDNS1123Label(ns); len(errs) > 0 {
		return "", fmt.Errorf("invalid install namespace %q: %s", ns, strings.Join(errs, "; "))
	}
	return ns, nil
}

// apply runs the idempotent install pipeline against the resolved namespace:
// ensure the namespace, apply CRDs and wait Established, apply the workload, then
// prune. The same namespace drives the first Namespace object, the apply-time
// defaulting for namespaced objects, and the prune scope, so they agree with the
// rendered RBAC subjects.
func (i *installer) apply(ctx context.Context, objs []*unstructured.Unstructured, namespace string) error {
	applied := make(map[objectKey]struct{}, len(objs)+1)

	nsKey, err := applyObject(ctx, i.client, i.mapper, namespaceObject(namespace), namespace)
	if err != nil {
		return err
	}
	applied[nsKey] = struct{}{}

	crds, workload := partitionCRDs(objs)
	for _, crd := range crds {
		key, err := applyObject(ctx, i.client, i.mapper, crd, namespace)
		if err != nil {
			return err
		}
		applied[key] = struct{}{}
	}
	if err := i.waitEstablished(ctx, crdNames(crds)); err != nil {
		return err
	}

	for _, obj := range workload {
		key, err := applyObject(ctx, i.client, i.mapper, obj, namespace)
		if err != nil {
			return err
		}
		applied[key] = struct{}{}
	}

	return prune(ctx, i.client, i.mapper, namespace, applied)
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

// Plan reports the action the next EnsureOperator would take for spec, without
// mutating the cluster. It resolves the namespace, renders the embedded chart
// for its target version, reads the install state in that namespace, and runs
// the same Decide policy EnsureOperator uses. A would-refuse plan returns the
// typed Decide error alongside the Decision so the caller can surface it.
func (i *installer) Plan(ctx context.Context, spec operator.InstallSpec) (operator.Decision, error) {
	namespace, err := resolveNamespace(spec.Namespace)
	if err != nil {
		return operator.Decision{}, err
	}

	objs, err := render(i.chart, namespace, spec.Values.ToHelmValues())
	if err != nil {
		return operator.Decision{}, err
	}

	target, err := versionFromObjects(objs)
	if err != nil {
		return operator.Decision{}, err
	}

	state, err := i.operatorState(ctx, namespace)
	if err != nil {
		return operator.Decision{}, err
	}

	action, decideErr := operator.Decide(target, state)
	return operator.Decision{
		Action:           action,
		InstalledVersion: state.Version,
		TargetVersion:    target,
	}, decideErr
}

// OperatorState reports the install state from the manager Deployment in the
// given namespace. An empty namespace defaults to installNamespace.
// EnsureOperator reads state against the resolved install namespace directly
// via operatorState.
func (i *installer) OperatorState(ctx context.Context, namespace string) (operator.State, error) {
	ns := strings.TrimSpace(namespace)
	if ns == "" {
		ns = installNamespace
	}
	return i.operatorState(ctx, ns)
}

// operatorState reports the install state from the manager Deployment in the
// given namespace.
func (i *installer) operatorState(ctx context.Context, namespace string) (operator.State, error) {
	deployment, err := i.managerDeployment(ctx, namespace)
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

// managerDeployment returns the operator manager Deployment in the given
// namespace, or nil when none exists. More than one match is an error: the
// install is ambiguous and must not be reconciled blindly.
func (i *installer) managerDeployment(ctx context.Context, namespace string) (*appsv1.Deployment, error) {
	list := &appsv1.DeploymentList{}
	err := i.client.List(ctx, list,
		client.InNamespace(namespace),
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
		return nil, fmt.Errorf("ambiguous operator install: %d manager Deployments in %s", len(list.Items), namespace)
	}
}
