package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	examplev1alpha1 "github.com/meigma/template-k8s/api/v1alpha1"
	"github.com/meigma/template-k8s/internal/controller/telemetry"
)

const (
	nginxContainerName    = "nginx"
	nginxConfigKey        = "nginx.conf"
	nginxConfigVolumeName = "nginx-config"
	configHashAnnotation  = "example.meigma.io/config-hash"

	defaultNginxImage                    = "nginxinc/nginx-unprivileged:stable"
	defaultNginxPort                     = int32(8080)
	defaultTerminationGracePeriodSeconds = int64(corev1.DefaultTerminationGracePeriodSeconds)
	defaultVolumeMode                    = int32(0644)

	conditionAvailable          = "Available"
	reasonDeploymentReady       = "DeploymentReady"
	reasonDeploymentProgressing = "DeploymentProgressing"
	reasonDeploymentStale       = "DeploymentStatusStale"
)

// NginxDeploymentReconciler reconciles a NginxDeployment object.
type NginxDeploymentReconciler struct {
	// Client is the controller-runtime client used to read and write
	// NginxDeployment resources and their owned children.
	client.Client

	// Scheme is the runtime scheme used when setting controller references on
	// owned child resources.
	Scheme *runtime.Scheme

	// Telemetry receives child-apply and status-transition observations.
	// A nil value selects telemetry.NoopRecorder so callers can opt out.
	Telemetry telemetry.Recorder
}

// +kubebuilder:rbac:groups=example.meigma.io,resources=nginxdeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=example.meigma.io,resources=nginxdeployments/status,verbs=patch
// +kubebuilder:rbac:groups="",resources=configmaps;services,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
func (r *NginxDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx, "nginxdeployment", req.String())

	instance := &examplev1alpha1.NginxDeployment{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("NginxDeployment not found; ignoring deleted object")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	log = log.WithValues("generation", instance.Generation)
	ctx = logf.IntoContext(ctx, log)
	log.V(1).Info("Reconciling NginxDeployment")

	config := nginxConfig(instance)
	childApplies := make([]telemetry.ChildApply, 0, 3)

	configMapApply, err := r.reconcileConfigMap(ctx, instance, config)
	if err != nil {
		return ctrl.Result{}, err
	}
	childApplies = append(childApplies, telemetry.ChildApply{
		Resource:  telemetry.ChildResourceConfigMap,
		Operation: configMapApply,
	})

	deployment, deploymentApply, err := r.reconcileDeployment(ctx, instance, config)
	if err != nil {
		return ctrl.Result{}, err
	}
	childApplies = append(childApplies, telemetry.ChildApply{
		Resource:  telemetry.ChildResourceDeployment,
		Operation: deploymentApply,
	})

	serviceApply, err := r.reconcileService(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	childApplies = append(childApplies, telemetry.ChildApply{
		Resource:  telemetry.ChildResourceService,
		Operation: serviceApply,
	})

	r.telemetry().RecordChildApplies(instance, childApplies)
	logChildApplies(ctx, childApplies)

	if err := r.reconcileStatus(ctx, instance, deployment); err != nil {
		return ctrl.Result{}, err
	}

	log.V(1).Info("Finished reconciling NginxDeployment")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NginxDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&examplev1alpha1.NginxDeployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("nginxdeployment").
		Complete(r)
}

// reconcileConfigMap creates or patches the ConfigMap that holds the rendered
// nginx configuration for instance, returning the controllerutil operation
// result so callers can record telemetry and decide whether to log a change.
func (r *NginxDeploymentReconciler) reconcileConfigMap(
	ctx context.Context,
	instance *examplev1alpha1.NginxDeployment,
	config string,
) (controllerutil.OperationResult, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}

	result, err := controllerutil.CreateOrPatch(ctx, r.Client, configMap, func() error {
		configMap.Labels = labelsFor(instance)
		configMap.Data = map[string]string{
			nginxConfigKey: config,
		}
		return ctrl.SetControllerReference(instance, configMap, r.Scheme)
	})
	return result, err
}

// reconcileDeployment creates or patches the owned nginx Deployment to match
// the desired spec, stamping a config hash annotation on the pod template so
// that pods restart whenever the rendered nginx configuration changes. It
// returns the freshly fetched Deployment alongside the operation result.
func (r *NginxDeploymentReconciler) reconcileDeployment(
	ctx context.Context,
	instance *examplev1alpha1.NginxDeployment,
	config string,
) (*appsv1.Deployment, controllerutil.OperationResult, error) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}

	result, err := controllerutil.CreateOrPatch(ctx, r.Client, deployment, func() error {
		replicas := nginxReplicas(instance)
		deployment.Labels = labelsFor(instance)
		deployment.Spec.Replicas = &replicas
		deployment.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: selectorLabelsFor(instance),
		}
		deployment.Spec.Template.Labels = labelsFor(instance)
		deployment.Spec.Template.Annotations = map[string]string{
			configHashAnnotation: configHash(config),
		}
		deployment.Spec.Template.Spec.SecurityContext = nginxPodSecurityContext()
		deployment.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyAlways
		deployment.Spec.Template.Spec.TerminationGracePeriodSeconds = new(defaultTerminationGracePeriodSeconds)
		deployment.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
		deployment.Spec.Template.Spec.SchedulerName = corev1.DefaultSchedulerName
		deployment.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:            nginxContainerName,
				Image:           nginxImage(instance),
				ImagePullPolicy: nginxImagePullPolicy(instance),
				SecurityContext: nginxContainerSecurityContext(),
				Ports: []corev1.ContainerPort{
					{
						Name:          "http",
						ContainerPort: nginxPort(instance),
						Protocol:      corev1.ProtocolTCP,
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      nginxConfigVolumeName,
						MountPath: "/etc/nginx/nginx.conf",
						SubPath:   nginxConfigKey,
						ReadOnly:  true,
					},
				},
				Resources:                nginxResourceRequirements(),
				TerminationMessagePath:   corev1.TerminationMessagePathDefault,
				TerminationMessagePolicy: corev1.TerminationMessageReadFile,
			},
		}
		deployment.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: nginxConfigVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						DefaultMode: new(defaultVolumeMode),
						LocalObjectReference: corev1.LocalObjectReference{
							Name: instance.Name,
						},
						Items: []corev1.KeyToPath{
							{
								Key:  nginxConfigKey,
								Path: nginxConfigKey,
							},
						},
					},
				},
			},
		}
		deployment.Spec.Template.Spec.EnableServiceLinks = new(true)
		return ctrl.SetControllerReference(instance, deployment, r.Scheme)
	})
	if err != nil {
		return nil, result, err
	}
	return deployment, result, r.Get(ctx, client.ObjectKeyFromObject(deployment), deployment)
}

// reconcileService creates or patches the same-named ClusterIP Service that
// fronts the nginx Deployment, returning the controllerutil operation result.
func (r *NginxDeploymentReconciler) reconcileService(
	ctx context.Context,
	instance *examplev1alpha1.NginxDeployment,
) (controllerutil.OperationResult, error) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}

	result, err := controllerutil.CreateOrPatch(ctx, r.Client, service, func() error {
		service.Labels = labelsFor(instance)
		service.Spec.Type = corev1.ServiceTypeClusterIP
		service.Spec.Selector = selectorLabelsFor(instance)
		service.Spec.Ports = []corev1.ServicePort{
			{
				Name:       "http",
				Port:       nginxPort(instance),
				TargetPort: intstr.FromString("http"),
				Protocol:   corev1.ProtocolTCP,
			},
		}
		return ctrl.SetControllerReference(instance, service, r.Scheme)
	})
	return result, err
}

// reconcileStatus patches the NginxDeployment status subresource with the
// observed ready replica count and the computed Available condition. It only
// issues a patch when the status actually changed and records a telemetry
// transition only when the Available condition's status or reason moved.
func (r *NginxDeploymentReconciler) reconcileStatus(
	ctx context.Context,
	instance *examplev1alpha1.NginxDeployment,
	deployment *appsv1.Deployment,
) error {
	original := instance.DeepCopy()
	originalCondition := meta.FindStatusCondition(original.Status.Conditions, conditionAvailable)
	instance.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	meta.SetStatusCondition(&instance.Status.Conditions, availableCondition(instance, deployment))
	currentCondition := meta.FindStatusCondition(instance.Status.Conditions, conditionAvailable)
	conditionChanged := conditionStateChanged(originalCondition, currentCondition)
	if equality.Semantic.DeepEqual(original.Status, instance.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, instance, client.MergeFrom(original)); err != nil {
		return err
	}
	if conditionChanged && currentCondition != nil {
		r.telemetry().RecordStatusTransition(instance, *currentCondition)
		logf.FromContext(ctx).Info(
			"Updated NginxDeployment status condition",
			"condition", currentCondition.Type,
			"status", currentCondition.Status,
			"reason", currentCondition.Reason,
			"readyReplicas", instance.Status.ReadyReplicas,
		)
	} else {
		logf.FromContext(ctx).V(1).Info("Updated NginxDeployment status", "readyReplicas", instance.Status.ReadyReplicas)
	}
	return nil
}

// logChildApplies emits an info-level log entry summarising which child
// resources were created or updated in this reconcile, or a V(1) entry when
// every child resource was already in the desired state.
func logChildApplies(ctx context.Context, applies []telemetry.ChildApply) {
	changes := make([]string, 0, len(applies))
	for _, apply := range applies {
		operation, ok := logOperation(apply.Operation)
		if !ok {
			continue
		}
		changes = append(changes, fmt.Sprintf("%s=%s", apply.Resource, operation))
	}

	log := logf.FromContext(ctx)
	if len(changes) == 0 {
		log.V(1).Info("Child resources already match desired state")
		return
	}
	log.Info("Applied child resources", "changes", strings.Join(changes, ","))
}

// logOperation maps a controllerutil OperationResult to a short string used in
// log output and reports whether the operation represents an actual change.
// "Unchanged" and "None" outcomes return false so callers can skip logging.
func logOperation(operation controllerutil.OperationResult) (string, bool) {
	switch operation {
	case controllerutil.OperationResultCreated:
		return string(controllerutil.OperationResultCreated), true
	case controllerutil.OperationResultUpdated,
		controllerutil.OperationResultUpdatedStatus,
		controllerutil.OperationResultUpdatedStatusOnly:
		return string(controllerutil.OperationResultUpdated), true
	default:
		return "", false
	}
}

// telemetry returns the configured Recorder, falling back to a no-op recorder
// when the controller was constructed without one so reconcile code never
// needs to nil-check the dependency.
func (r *NginxDeploymentReconciler) telemetry() telemetry.Recorder {
	if r.Telemetry != nil {
		return r.Telemetry
	}
	return telemetry.NoopRecorder()
}

// conditionStateChanged reports whether the current Available condition
// represents a transition relative to previous. A nil current condition is
// treated as no change so reconcile does not emit spurious transitions.
func conditionStateChanged(previous *metav1.Condition, current *metav1.Condition) bool {
	if current == nil {
		return false
	}
	if previous == nil {
		return true
	}
	return previous.Status != current.Status || previous.Reason != current.Reason
}

// availableCondition computes the Available condition for the NginxDeployment
// from the owned Deployment's observed state. It returns Unknown-like False
// values when the Deployment's status is stale relative to its generation so
// callers never trust pre-rollout readiness signals.
func availableCondition(
	instance *examplev1alpha1.NginxDeployment,
	deployment *appsv1.Deployment,
) metav1.Condition {
	desired := nginxReplicas(instance)
	ready := deployment.Status.ReadyReplicas
	if deployment.Status.ObservedGeneration < deployment.Generation {
		return metav1.Condition{
			Type:               conditionAvailable,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: instance.Generation,
			Reason:             reasonDeploymentStale,
			Message: fmt.Sprintf(
				"Deployment status has observed generation %d, waiting for generation %d",
				deployment.Status.ObservedGeneration,
				deployment.Generation,
			),
		}
	}
	if desired == 0 || (ready >= desired && deploymentAvailable(deployment)) {
		return metav1.Condition{
			Type:               conditionAvailable,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: instance.Generation,
			Reason:             reasonDeploymentReady,
			Message:            fmt.Sprintf("Deployment has %d/%d ready replicas", ready, desired),
		}
	}

	return metav1.Condition{
		Type:               conditionAvailable,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: instance.Generation,
		Reason:             reasonDeploymentProgressing,
		Message:            fmt.Sprintf("Deployment has %d/%d ready replicas", ready, desired),
	}
}

// deploymentAvailable reports whether the Deployment carries a True
// DeploymentAvailable condition, which is the canonical Kubernetes signal that
// minimum replicas are running.
func deploymentAvailable(deployment *appsv1.Deployment) bool {
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// labelsFor returns the labels stamped on every owned child resource. It is
// currently identical to selectorLabelsFor and is kept as a separate seam in
// case workload labels diverge from selector labels in the future.
func labelsFor(instance *examplev1alpha1.NginxDeployment) map[string]string {
	return selectorLabelsFor(instance)
}

// selectorLabelsFor returns the label set used both for the Deployment's pod
// selector and for the Service's pod selector so they always agree.
func selectorLabelsFor(instance *examplev1alpha1.NginxDeployment) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "nginx",
		"app.kubernetes.io/instance":   instance.Name,
		"app.kubernetes.io/managed-by": "template-k8s",
	}
}

// nginxConfig returns the nginx configuration the controller should project
// into the ConfigMap. It uses Spec.Config when supplied or falls back to a
// minimal hello-world configuration listening on the resolved port.
func nginxConfig(instance *examplev1alpha1.NginxDeployment) string {
	if instance.Spec.Config != "" {
		return instance.Spec.Config
	}
	return fmt.Sprintf(`pid /tmp/nginx.pid;
events {}
http {
  client_body_temp_path /tmp/client_temp;
  proxy_temp_path /tmp/proxy_temp;
  fastcgi_temp_path /tmp/fastcgi_temp;
  uwsgi_temp_path /tmp/uwsgi_temp;
  scgi_temp_path /tmp/scgi_temp;

  server {
    listen %d;
    location / {
      return 200 "hello from template-k8s\n";
    }
  }
}
`, nginxPort(instance))
}

// nginxImage resolves the container image to run, defaulting to the package
// default when Spec.Image is unset.
func nginxImage(instance *examplev1alpha1.NginxDeployment) string {
	if instance.Spec.Image != "" {
		return instance.Spec.Image
	}
	return defaultNginxImage
}

// nginxImagePullPolicy chooses an ImagePullPolicy that matches Kubernetes
// defaults: Always for floating "latest" tags and untagged images and
// IfNotPresent for pinned tags and digests.
func nginxImagePullPolicy(instance *examplev1alpha1.NginxDeployment) corev1.PullPolicy {
	image := nginxImage(instance)
	if strings.Contains(image, "@") {
		return corev1.PullIfNotPresent
	}

	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon <= lastSlash || image[lastColon+1:] == "latest" {
		return corev1.PullAlways
	}
	return corev1.PullIfNotPresent
}

// nginxReplicas resolves the desired replica count, preserving an explicit
// zero value and defaulting to one when the spec leaves replicas unset.
func nginxReplicas(instance *examplev1alpha1.NginxDeployment) int32 {
	if instance.Spec.Replicas != nil {
		return *instance.Spec.Replicas
	}
	return 1
}

// nginxPort resolves the container port nginx listens on, defaulting to
// defaultNginxPort when Spec.Port is unset.
func nginxPort(instance *examplev1alpha1.NginxDeployment) int32 {
	if instance.Spec.Port > 0 {
		return instance.Spec.Port
	}
	return defaultNginxPort
}

// nginxPodSecurityContext returns a Restricted-compatible PodSecurityContext
// suitable for the unprivileged nginx image used by this template.
func nginxPodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsNonRoot: new(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// nginxContainerSecurityContext returns a Restricted-compatible
// SecurityContext that drops all Linux capabilities and forbids privilege
// escalation.
func nginxContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: new(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

// nginxResourceRequirements returns the modest CPU and memory requests the
// template stamps on the nginx container. There are intentionally no limits
// so this stays a sane starting point rather than a production policy.
func nginxResourceRequirements() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10m"),
			corev1.ResourceMemory: resource.MustParse("32Mi"),
		},
	}
}

// configHash returns the hex-encoded SHA-256 of the supplied nginx config.
// The hash is stamped on the pod template as an annotation so changes to the
// projected ConfigMap data force a rollout.
func configHash(config string) string {
	sum := sha256.Sum256([]byte(config))
	return hex.EncodeToString(sum[:])
}
