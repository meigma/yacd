package resources

import (
	"maps"

	ctrlmetadata "github.com/meigma/yacd/internal/ctrlkit/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AnnotationMerger reconciles annotations from desired onto current.
type AnnotationMerger func(current map[string]string, desired map[string]string) map[string]string

// PodSpecMutator reconciles the pod spec fields a controller owns.
type PodSpecMutator func(current *corev1.PodSpec, desired *corev1.PodSpec)

// MutateObjectMetadata overlays desired labels, reconciles annotations, and
// copies owner references for an owned child object.
func MutateObjectMetadata(current metav1.Object, desired metav1.Object, mergeAnnotations AnnotationMerger) {
	current.SetLabels(ctrlmetadata.OverlayStringMap(current.GetLabels(), desired.GetLabels()))
	current.SetAnnotations(mergeAnnotationsFor(mergeAnnotations, current.GetAnnotations(), desired.GetAnnotations()))
	current.SetOwnerReferences(desired.GetOwnerReferences())
}

// MutatePersistentVolumeClaim reconciles shared owned PVC fields and expands
// storage requests when the desired size is larger.
func MutatePersistentVolumeClaim(current *corev1.PersistentVolumeClaim, desired *corev1.PersistentVolumeClaim, mergeAnnotations AnnotationMerger) {
	MutateObjectMetadata(current, desired, mergeAnnotations)

	currentStorage := current.Spec.Resources.Requests[corev1.ResourceStorage]
	desiredStorage := desired.Spec.Resources.Requests[corev1.ResourceStorage]
	if current.Spec.Resources.Requests == nil {
		current.Spec.Resources.Requests = corev1.ResourceList{}
	}
	if currentStorage.Cmp(desiredStorage) < 0 {
		current.Spec.Resources.Requests[corev1.ResourceStorage] = desiredStorage
	}
}

// MutateDeployment reconciles shared owned Deployment fields and delegates pod
// spec ownership to the caller.
func MutateDeployment(current *appsv1.Deployment, desired *appsv1.Deployment, mergeAnnotations AnnotationMerger, mutatePodSpec PodSpecMutator) {
	MutateObjectMetadata(current, desired, mergeAnnotations)
	current.Spec.Paused = desired.Spec.Paused
	current.Spec.Replicas = desired.Spec.Replicas
	current.Spec.Strategy = desired.Spec.Strategy
	current.Spec.Template.Labels = ctrlmetadata.OverlayStringMap(current.Spec.Template.Labels, desired.Spec.Template.Labels)
	current.Spec.Template.Annotations = mergeAnnotationsFor(mergeAnnotations, current.Spec.Template.Annotations, desired.Spec.Template.Annotations)
	if mutatePodSpec != nil {
		mutatePodSpec(&current.Spec.Template.Spec, &desired.Spec.Template.Spec)
	}
}

// MutateService reconciles the owned fields common to controller-owned
// ClusterIP Services while preserving Kubernetes-assigned cluster IP fields.
func MutateService(current *corev1.Service, desired *corev1.Service, mergeAnnotations AnnotationMerger) {
	MutateObjectMetadata(current, desired, mergeAnnotations)
	current.Spec.Type = desired.Spec.Type
	current.Spec.Selector = maps.Clone(desired.Spec.Selector)
	current.Spec.Ports = desired.Spec.Ports
	current.Spec.ExternalName = desired.Spec.ExternalName
}

func mergeAnnotationsFor(mergeAnnotations AnnotationMerger, current map[string]string, desired map[string]string) map[string]string {
	if mergeAnnotations == nil {
		return ctrlmetadata.OverlayStringMap(current, desired)
	}

	return mergeAnnotations(current, desired)
}
