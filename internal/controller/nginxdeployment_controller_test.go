package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	examplev1alpha1 "github.com/meigma/template-k8s/api/v1alpha1"
	"github.com/meigma/template-k8s/internal/controller/telemetry"
)

const testNamespace = "default"

var _ = Describe("NginxDeployment Controller", func() {
	ctx := context.Background()
	var controllerReconciler *NginxDeploymentReconciler

	BeforeEach(func() {
		controllerReconciler = &NginxDeploymentReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	})

	It("creates owned nginx resources and reports initial availability", func() {
		spec := nginxSpec(2, "nginx:1.27", 8080, "events {}\nhttp { server { listen 8080; } }\n")
		resource := createNginxDeployment(ctx, "creates-children", spec)

		reconcileResource(ctx, controllerReconciler, resource)

		expectConfigMap(resource, spec.Config)
		expectDeployment(resource)
		expectService(resource, spec.Port)

		current := fetchNginxDeployment(ctx, objectKeyFor(resource))
		Expect(current.Status.ReadyReplicas).To(Equal(int32(0)))
		expectAvailableCondition(current, metav1.ConditionFalse, reasonDeploymentStale)
	})

	It("updates owned resources when the spec changes", func() {
		initialSpec := nginxSpec(1, "nginx:stable", 80, "events {}\nhttp { server { listen 80; } }\n")
		resource := createNginxDeployment(ctx, "updates-children", initialSpec)
		reconcileResource(ctx, controllerReconciler, resource)
		initialDeployment := fetchDeployment(ctx, objectKeyFor(resource))
		initialHash := initialDeployment.Spec.Template.Annotations[configHashAnnotation]

		updated := fetchNginxDeployment(ctx, objectKeyFor(resource))
		updated.Spec = nginxSpec(3, "nginx:1.28", 8081, "events {}\nhttp { server { listen 8081; } }\n")
		Expect(k8sClient.Update(ctx, updated)).To(Succeed())

		reconcileResource(ctx, controllerReconciler, updated)

		expectConfigMap(updated, updated.Spec.Config)
		deployment := expectDeployment(updated)
		Expect(deployment.Spec.Template.Annotations[configHashAnnotation]).NotTo(Equal(initialHash))
		expectService(updated, updated.Spec.Port)
	})

	It("records child resource apply metrics and events", func() {
		controllerReconciler, testTelemetry := newTelemetryReconciler()
		spec := nginxSpec(1, "nginx:stable", 80, "events {}\nhttp { server { listen 80; } }\n")
		resource := createNginxDeployment(ctx, "records-child-apply", spec)

		reconcileResource(ctx, controllerReconciler, resource)

		expectMetricValue(
			testTelemetry.registry,
			telemetry.MetricChildApplyTotal,
			map[string]string{"resource": "configmap", "operation": "created"},
		)
		expectMetricValue(
			testTelemetry.registry,
			telemetry.MetricChildApplyTotal,
			map[string]string{"resource": "deployment", "operation": "created"},
		)
		expectMetricValue(
			testTelemetry.registry,
			telemetry.MetricChildApplyTotal,
			map[string]string{"resource": "service", "operation": "created"},
		)
		expectEvent(testTelemetry.events, telemetry.EventReasonChildResourcesApplied)
	})

	It("records child resource corrections without no-op events", func() {
		controllerReconciler, testTelemetry := newTelemetryReconciler()
		initialSpec := nginxSpec(1, "nginx:stable", 80, "events {}\nhttp { server { listen 80; } }\n")
		resource := createNginxDeployment(ctx, "records-child-updates", initialSpec)
		reconcileResource(ctx, controllerReconciler, resource)
		expectEvent(testTelemetry.events, telemetry.EventReasonChildResourcesApplied)
		expectEvent(testTelemetry.events, reasonDeploymentStale)
		drainEvents(testTelemetry.events)

		updated := fetchNginxDeployment(ctx, objectKeyFor(resource))
		updated.Spec = nginxSpec(2, "nginx:1.28", 8081, "events {}\nhttp { server { listen 8081; } }\n")
		Expect(k8sClient.Update(ctx, updated)).To(Succeed())

		reconcileResource(ctx, controllerReconciler, updated)

		for _, resourceLabel := range []string{"configmap", "deployment", "service"} {
			expectMetricValue(
				testTelemetry.registry,
				telemetry.MetricChildApplyTotal,
				map[string]string{"resource": resourceLabel, "operation": "updated"},
			)
		}
		expectEvent(testTelemetry.events, telemetry.EventReasonChildResourcesApplied)
		drainEvents(testTelemetry.events)

		reconcileResource(ctx, controllerReconciler, fetchNginxDeployment(ctx, objectKeyFor(resource)))

		expectNoEvent(testTelemetry.events, telemetry.EventReasonChildResourcesApplied)
		for _, resourceLabel := range []string{"configmap", "deployment", "service"} {
			expectMetricValue(
				testTelemetry.registry,
				telemetry.MetricChildApplyTotal,
				map[string]string{"resource": resourceLabel, "operation": "updated"},
			)
		}
	})

	It("uses a default nginx config when the spec omits config", func() {
		spec := nginxSpec(1, "nginx:stable", 8082, "")
		resource := createNginxDeployment(ctx, "defaults-config", spec)

		reconcileResource(ctx, controllerReconciler, resource)

		expectedConfig := nginxConfig(resource)
		expectConfigMap(resource, expectedConfig)
		deployment := expectDeployment(resource)
		Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue(configHashAnnotation, configHash(expectedConfig)))
		expectService(resource, spec.Port)
	})

	It("marks the resource available when the owned deployment has enough ready replicas", func() {
		spec := nginxSpec(2, "nginx:stable", 80, "events {}\nhttp { server { listen 80; } }\n")
		resource := createNginxDeployment(ctx, "reports-readiness", spec)

		reconcileResource(ctx, controllerReconciler, resource)
		current := fetchNginxDeployment(ctx, objectKeyFor(resource))
		Expect(current.Status.ReadyReplicas).To(Equal(int32(0)))
		expectAvailableCondition(current, metav1.ConditionFalse, reasonDeploymentStale)

		deployment := fetchDeployment(ctx, objectKeyFor(resource))
		deployment.Status.ObservedGeneration = deployment.Generation
		deployment.Status.Replicas = nginxReplicas(resource)
		deployment.Status.ReadyReplicas = nginxReplicas(resource)
		deployment.Status.AvailableReplicas = nginxReplicas(resource)
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{
				Type:   appsv1.DeploymentAvailable,
				Status: corev1.ConditionTrue,
				Reason: "MinimumReplicasAvailable",
			},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		reconcileResource(ctx, controllerReconciler, resource)

		current = fetchNginxDeployment(ctx, objectKeyFor(resource))
		Expect(current.Status.ReadyReplicas).To(Equal(nginxReplicas(resource)))
		expectAvailableCondition(current, metav1.ConditionTrue, reasonDeploymentReady)
	})

	It("records status transition metrics and events after patching status", func() {
		controllerReconciler, testTelemetry := newTelemetryReconciler()
		spec := nginxSpec(2, "nginx:stable", 80, "events {}\nhttp { server { listen 80; } }\n")
		resource := createNginxDeployment(ctx, "records-status-events", spec)

		reconcileResource(ctx, controllerReconciler, resource)
		expectMetricValue(
			testTelemetry.registry,
			telemetry.MetricStatusTransitionTotal,
			map[string]string{"condition": conditionAvailable, "status": "false", "reason": reasonDeploymentStale},
		)
		expectEvent(testTelemetry.events, reasonDeploymentStale)
		drainEvents(testTelemetry.events)

		deployment := fetchDeployment(ctx, objectKeyFor(resource))
		deployment.Status.ObservedGeneration = deployment.Generation
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		reconcileResource(ctx, controllerReconciler, resource)
		expectMetricValue(
			testTelemetry.registry,
			telemetry.MetricStatusTransitionTotal,
			map[string]string{"condition": conditionAvailable, "status": "false", "reason": reasonDeploymentProgressing},
		)
		expectEvent(testTelemetry.events, reasonDeploymentProgressing)
		drainEvents(testTelemetry.events)

		deployment = fetchDeployment(ctx, objectKeyFor(resource))
		deployment.Status.ObservedGeneration = deployment.Generation
		deployment.Status.Replicas = nginxReplicas(resource)
		deployment.Status.ReadyReplicas = nginxReplicas(resource)
		deployment.Status.AvailableReplicas = nginxReplicas(resource)
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{
				Type:   appsv1.DeploymentAvailable,
				Status: corev1.ConditionTrue,
				Reason: "MinimumReplicasAvailable",
			},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		reconcileResource(ctx, controllerReconciler, resource)
		expectMetricValue(
			testTelemetry.registry,
			telemetry.MetricStatusTransitionTotal,
			map[string]string{"condition": conditionAvailable, "status": "true", "reason": reasonDeploymentReady},
		)
		expectEvent(testTelemetry.events, reasonDeploymentReady)
	})

	It("does not report available from stale deployment status after a spec update", func() {
		spec := nginxSpec(1, "nginx:stable", 80, "events {}\nhttp { server { listen 80; } }\n")
		resource := createNginxDeployment(ctx, "stale-status", spec)

		reconcileResource(ctx, controllerReconciler, resource)
		deployment := fetchDeployment(ctx, objectKeyFor(resource))
		deployment.Status.ObservedGeneration = deployment.Generation
		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.AvailableReplicas = 1
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{
				Type:   appsv1.DeploymentAvailable,
				Status: corev1.ConditionTrue,
				Reason: "MinimumReplicasAvailable",
			},
		}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
		reconcileResource(ctx, controllerReconciler, resource)

		current := fetchNginxDeployment(ctx, objectKeyFor(resource))
		expectAvailableCondition(current, metav1.ConditionTrue, reasonDeploymentReady)

		current.Spec = nginxSpec(1, "nginx:1.28", 80, "events {}\nhttp { server { listen 80; } }\n")
		Expect(k8sClient.Update(ctx, current)).To(Succeed())

		reconcileResource(ctx, controllerReconciler, current)

		current = fetchNginxDeployment(ctx, objectKeyFor(resource))
		expectAvailableCondition(current, metav1.ConditionFalse, reasonDeploymentStale)
		deployment = fetchDeployment(ctx, objectKeyFor(resource))
		Expect(deployment.Status.ObservedGeneration).To(BeNumerically("<", deployment.Generation))
	})

	It("preserves explicit scale-to-zero replicas", func() {
		spec := nginxSpec(0, "nginx:stable", 80, "events {}\nhttp { server { listen 80; } }\n")
		resource := createNginxDeployment(ctx, "scale-zero", spec)

		reconcileResource(ctx, controllerReconciler, resource)

		deployment := expectDeployment(resource)
		Expect(deployment.Spec.Replicas).NotTo(BeNil())
		Expect(*deployment.Spec.Replicas).To(Equal(int32(0)))
	})

	It("rejects names that cannot be reused for same-named child resources", func() {
		for _, name := range []string{
			"nginx-" + strings.Repeat("a", 64),
			"nginx.sample",
		} {
			resource := &examplev1alpha1.NginxDeployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
				},
				Spec: nginxSpec(1, "nginx:stable", 80, "events {}\nhttp { server { listen 80; } }\n"),
			}

			err := k8sClient.Create(ctx, resource)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		}
	})

	It("rejects oversized inline config before reconciling a ConfigMap", func() {
		resource := &examplev1alpha1.NginxDeployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "oversized-config",
				Namespace: testNamespace,
			},
			Spec: nginxSpec(1, "nginx:stable", 80, strings.Repeat("x", 65537)),
		}

		err := k8sClient.Create(ctx, resource)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err)).To(BeTrue())
	})

	It("reconciles through manager watches for parent and owned child events", func() {
		startControllerManager()

		spec := nginxSpec(1, "nginx:stable", 8080, "events {}\nhttp { server { listen 8080; } }\n")
		resource := createNginxDeployment(ctx, "manager-watches", spec)
		key := objectKeyFor(resource)

		Eventually(func() error {
			configMap := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, key, configMap); err != nil {
				return err
			}
			if got := configMap.Data[nginxConfigKey]; got != spec.Config {
				return fmt.Errorf("expected ConfigMap data to be reconciled to %q, got %q", spec.Config, got)
			}
			return nil
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		Eventually(func() error {
			return k8sClient.Get(ctx, key, &appsv1.Deployment{})
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		Eventually(func() error {
			return k8sClient.Get(ctx, key, &corev1.Service{})
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
		expectDeployment(resource)
		expectService(resource, spec.Port)

		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, key, configMap)).To(Succeed())
		configMap.Data[nginxConfigKey] = "drifted config"
		Expect(k8sClient.Update(ctx, configMap)).To(Succeed())

		Eventually(func() string {
			current := &corev1.ConfigMap{}
			if err := k8sClient.Get(ctx, key, current); err != nil {
				return err.Error()
			}
			return current.Data[nginxConfigKey]
		}, 10*time.Second, 100*time.Millisecond).Should(Equal(spec.Config))
	})
})

// startControllerManager boots a controller-runtime manager backed by the
// envtest API server, registers the NginxDeployment reconciler, and tears the
// manager down when the surrounding Ginkgo node completes. It is the seam
// that lets envtest exercise .For/.Owns watches end to end.
func startControllerManager() {
	testCtx, stop := context.WithCancel(context.Background())
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 k8sClient.Scheme(),
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		PprofBindAddress:       "0",
	})
	Expect(err).NotTo(HaveOccurred())
	Expect((&NginxDeploymentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)).To(Succeed())

	managerDone := make(chan error, 1)
	go func() {
		managerDone <- mgr.Start(testCtx)
	}()
	Consistently(managerDone, 250*time.Millisecond, 50*time.Millisecond).ShouldNot(Receive())
	DeferCleanup(func() {
		stop()
		Eventually(managerDone, 10*time.Second, 100*time.Millisecond).Should(Receive())
	})
}

// observedTelemetry bundles the test doubles used to assert that the
// controller emits the expected metrics and Kubernetes Events.
type observedTelemetry struct {
	// registry is the Prometheus registry the metrics under test are
	// scraped from in assertions.
	registry *prometheus.Registry

	// events is the fake event recorder used to receive emitted Events.
	events *record.FakeRecorder

	// recorder is the production Recorder wired against registry and events
	// so the controller treats it as a real telemetry sink.
	recorder telemetry.Recorder
}

// newTelemetryReconciler builds a NginxDeploymentReconciler wired against a
// freshly created observedTelemetry so each spec gets an isolated registry
// and event recorder.
func newTelemetryReconciler() (*NginxDeploymentReconciler, observedTelemetry) {
	observed := newObservedTelemetry()
	return &NginxDeploymentReconciler{
		Client:    k8sClient,
		Scheme:    k8sClient.Scheme(),
		Telemetry: observed.recorder,
	}, observed
}

// newObservedTelemetry constructs the metric and event test doubles plus the
// production Recorder shim that ties them together.
func newObservedTelemetry() observedTelemetry {
	registry := prometheus.NewRegistry()
	metrics, err := telemetry.NewMetrics(registry)
	Expect(err).NotTo(HaveOccurred())

	events := record.NewFakeRecorder(32)
	return observedTelemetry{
		registry: registry,
		events:   events,
		recorder: telemetry.NewRecorder(metrics, events),
	}
}

// expectMetricValue asserts that the named counter with the supplied label set
// has been incremented exactly once during the current spec.
func expectMetricValue(
	registry *prometheus.Registry,
	name string,
	labels map[string]string,
) {
	Expect(metricValue(registry, name, labels)).To(Equal(float64(1)))
}

// metricValue scans the registry for the named counter matching the supplied
// label set and returns its current value, or 0 when no matching sample is
// present.
func metricValue(registry *prometheus.Registry, name string, labels map[string]string) float64 {
	families, err := registry.Gather()
	Expect(err).NotTo(HaveOccurred())

	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			matches := len(metric.GetLabel()) == len(labels)
			for _, label := range metric.GetLabel() {
				if labels[label.GetName()] != label.GetValue() {
					matches = false
					break
				}
			}
			if matches && metric.GetCounter() != nil {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// expectEvent waits up to one second for an event whose reason matches the
// supplied string to arrive on the fake recorder.
func expectEvent(recorder *record.FakeRecorder, reason string) {
	Eventually(recorder.Events, time.Second, 50*time.Millisecond).Should(Receive(ContainSubstring(" " + reason + " ")))
}

// expectNoEvent asserts that no event with the supplied reason arrives during
// a short observation window.
func expectNoEvent(recorder *record.FakeRecorder, reason string) {
	Consistently(func() string {
		select {
		case event := <-recorder.Events:
			return event
		default:
			return ""
		}
	}, 250*time.Millisecond, 50*time.Millisecond).ShouldNot(ContainSubstring(" " + reason + " "))
}

// drainEvents removes all currently buffered events from the fake recorder so
// the next assertion only observes events emitted after the call.
func drainEvents(recorder *record.FakeRecorder) {
	for {
		select {
		case <-recorder.Events:
		default:
			return
		}
	}
}

// nginxSpec is a small builder that returns a fully populated
// NginxDeploymentSpec so individual specs can stay focused on the behaviour
// they assert.
func nginxSpec(replicas int32, image string, port int32, config string) examplev1alpha1.NginxDeploymentSpec {
	return examplev1alpha1.NginxDeploymentSpec{
		Replicas: &replicas,
		Image:    image,
		Port:     port,
		Config:   config,
	}
}

// createNginxDeployment creates an NginxDeployment in the test namespace and
// registers a cleanup so the resource and its owned children are deleted at
// the end of the surrounding Ginkgo node.
func createNginxDeployment(
	ctx context.Context,
	name string,
	spec examplev1alpha1.NginxDeploymentSpec,
) *examplev1alpha1.NginxDeployment {
	resource := &examplev1alpha1.NginxDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: spec,
	}
	Expect(k8sClient.Create(ctx, resource)).To(Succeed())
	DeferCleanup(cleanupNginxDeployment, ctx, types.NamespacedName{Name: name, Namespace: testNamespace})
	return resource
}

// cleanupNginxDeployment deletes the parent NginxDeployment together with the
// child Deployment, Service, and ConfigMap, ignoring NotFound errors so the
// helper is safe to run after a test already removed some resources.
func cleanupNginxDeployment(ctx context.Context, key types.NamespacedName) {
	objects := []client.Object{
		&examplev1alpha1.NginxDeployment{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}},
	}
	for _, object := range objects {
		err := k8sClient.Delete(ctx, object)
		Expect(client.IgnoreNotFound(err)).To(Succeed())
	}
}

// reconcileResource runs one Reconcile against the supplied resource and
// fails the surrounding spec if the reconciler returns an error.
func reconcileResource(
	ctx context.Context,
	controllerReconciler *NginxDeploymentReconciler,
	resource *examplev1alpha1.NginxDeployment,
) {
	_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: objectKeyFor(resource),
	})
	Expect(err).NotTo(HaveOccurred())
}

// expectConfigMap asserts that the same-named ConfigMap exists, is owned by
// the resource, and contains the expected nginx config payload.
func expectConfigMap(resource *examplev1alpha1.NginxDeployment, config string) {
	configMap := &corev1.ConfigMap{}
	Expect(k8sClient.Get(context.Background(), objectKeyFor(resource), configMap)).To(Succeed())
	expectManagedObject(configMap, resource)
	Expect(configMap.Data).To(HaveKeyWithValue(nginxConfigKey, config))
}

// expectDeployment asserts that the same-named Deployment exists, is owned by
// the resource, and matches the template fields the reconciler is expected to
// stamp (labels, selector, security context, container shape, volumes). It
// returns the fetched Deployment so callers can layer additional checks.
func expectDeployment(resource *examplev1alpha1.NginxDeployment) *appsv1.Deployment {
	deployment := fetchDeployment(context.Background(), objectKeyFor(resource))
	expectManagedObject(deployment, resource)
	Expect(deployment.Spec.Replicas).NotTo(BeNil())
	Expect(*deployment.Spec.Replicas).To(Equal(nginxReplicas(resource)))
	Expect(deployment.Spec.Selector.MatchLabels).To(Equal(selectorLabelsFor(resource)))
	Expect(deployment.Spec.Template.Labels).To(Equal(labelsFor(resource)))
	Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue(configHashAnnotation, configHash(nginxConfig(resource))))
	Expect(deployment.Spec.Template.Spec.SecurityContext).To(Equal(nginxPodSecurityContext()))
	Expect(deployment.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyAlways))
	Expect(deployment.Spec.Template.Spec.TerminationGracePeriodSeconds).To(Equal(new(defaultTerminationGracePeriodSeconds)))
	Expect(deployment.Spec.Template.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
	Expect(deployment.Spec.Template.Spec.SchedulerName).To(Equal(corev1.DefaultSchedulerName))
	Expect(deployment.Spec.Template.Spec.EnableServiceLinks).To(Equal(new(true)))

	Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
	container := deployment.Spec.Template.Spec.Containers[0]
	Expect(container.Name).To(Equal(nginxContainerName))
	Expect(container.Image).To(Equal(nginxImage(resource)))
	Expect(container.ImagePullPolicy).To(Equal(nginxImagePullPolicy(resource)))
	Expect(container.SecurityContext).To(Equal(nginxContainerSecurityContext()))
	Expect(container.Resources).To(Equal(nginxResourceRequirements()))
	Expect(container.TerminationMessagePath).To(Equal(corev1.TerminationMessagePathDefault))
	Expect(container.TerminationMessagePolicy).To(Equal(corev1.TerminationMessageReadFile))
	Expect(container.Ports).To(HaveLen(1))
	Expect(container.Ports[0].Name).To(Equal("http"))
	Expect(container.Ports[0].ContainerPort).To(Equal(nginxPort(resource)))
	Expect(container.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
	Expect(container.VolumeMounts).To(HaveLen(1))
	Expect(container.VolumeMounts[0].Name).To(Equal(nginxConfigVolumeName))
	Expect(container.VolumeMounts[0].MountPath).To(Equal("/etc/nginx/nginx.conf"))
	Expect(container.VolumeMounts[0].SubPath).To(Equal(nginxConfigKey))
	Expect(container.VolumeMounts[0].ReadOnly).To(BeTrue())

	Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(1))
	volume := deployment.Spec.Template.Spec.Volumes[0]
	Expect(volume.Name).To(Equal(nginxConfigVolumeName))
	Expect(volume.ConfigMap).NotTo(BeNil())
	Expect(volume.ConfigMap.DefaultMode).To(Equal(new(defaultVolumeMode)))
	Expect(volume.ConfigMap.Name).To(Equal(resource.Name))
	Expect(volume.ConfigMap.Items).To(ConsistOf(corev1.KeyToPath{
		Key:  nginxConfigKey,
		Path: nginxConfigKey,
	}))
	return deployment
}

// expectService asserts that the same-named ClusterIP Service exists, is
// owned by the resource, and exposes the expected port using the http named
// target port.
func expectService(resource *examplev1alpha1.NginxDeployment, port int32) {
	service := &corev1.Service{}
	Expect(k8sClient.Get(context.Background(), objectKeyFor(resource), service)).To(Succeed())
	expectManagedObject(service, resource)
	Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
	Expect(service.Spec.Selector).To(Equal(selectorLabelsFor(resource)))
	Expect(service.Spec.Ports).To(HaveLen(1))
	Expect(service.Spec.Ports[0].Name).To(Equal("http"))
	Expect(service.Spec.Ports[0].Port).To(Equal(port))
	Expect(service.Spec.Ports[0].TargetPort).To(Equal(intstr.FromString("http")))
	Expect(service.Spec.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
}

// expectManagedObject asserts that object carries the labels stamped by the
// reconciler and an owner reference back to the supplied NginxDeployment
// with the controller flag set.
func expectManagedObject(object metav1.Object, owner *examplev1alpha1.NginxDeployment) {
	Expect(object.GetLabels()).To(Equal(labelsFor(owner)))
	for _, reference := range object.GetOwnerReferences() {
		if reference.APIVersion == examplev1alpha1.GroupVersion.String() &&
			reference.Kind == "NginxDeployment" &&
			reference.Name == owner.Name &&
			reference.UID == owner.UID &&
			reference.Controller != nil &&
			*reference.Controller {
			return
		}
	}
	Fail(fmt.Sprintf("expected %s to be owned by %s", object.GetName(), owner.Name))
}

// expectAvailableCondition asserts that the Available condition is present
// on the resource with the supplied status, reason, and an observedGeneration
// matching the parent's generation.
func expectAvailableCondition(
	resource *examplev1alpha1.NginxDeployment,
	status metav1.ConditionStatus,
	reason string,
) {
	condition := meta.FindStatusCondition(resource.Status.Conditions, conditionAvailable)
	Expect(condition).NotTo(BeNil())
	Expect(condition.Status).To(Equal(status))
	Expect(condition.Reason).To(Equal(reason))
	Expect(condition.ObservedGeneration).To(Equal(resource.Generation))
}

// fetchNginxDeployment reads the NginxDeployment at key and fails the spec if
// the Get returns an error.
func fetchNginxDeployment(
	ctx context.Context,
	key types.NamespacedName,
) *examplev1alpha1.NginxDeployment {
	resource := &examplev1alpha1.NginxDeployment{}
	Expect(k8sClient.Get(ctx, key, resource)).To(Succeed())
	return resource
}

// fetchDeployment reads the same-named Deployment for the supplied key and
// fails the spec if the Get returns an error.
func fetchDeployment(ctx context.Context, key types.NamespacedName) *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
	return deployment
}

// objectKeyFor returns the NamespacedName used to look up the resource and
// every same-named child object.
func objectKeyFor(instance *examplev1alpha1.NginxDeployment) types.NamespacedName {
	return types.NamespacedName{
		Name:      instance.Name,
		Namespace: instance.Namespace,
	}
}
