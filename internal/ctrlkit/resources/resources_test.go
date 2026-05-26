package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMutateObjectMetadata(t *testing.T) {
	current := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Labels:      map[string]string{"keep": "label"},
		Annotations: map[string]string{"owned": "old", "external": "keep"},
	}}
	desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Labels:          map[string]string{"desired": "label"},
		Annotations:     map[string]string{"owned": "new"},
		OwnerReferences: []metav1.OwnerReference{{Name: "owner"}},
	}}

	MutateObjectMetadata(current, desired, func(current map[string]string, desired map[string]string) map[string]string {
		return map[string]string{"owned": desired["owned"], "external": current["external"]}
	})

	assert.Equal(t, map[string]string{"keep": "label", "desired": "label"}, current.Labels)
	assert.Equal(t, map[string]string{"owned": "new", "external": "keep"}, current.Annotations)
	assert.Equal(t, []metav1.OwnerReference{{Name: "owner"}}, current.OwnerReferences)
}

func TestMutatePersistentVolumeClaimExpandsStorage(t *testing.T) {
	current := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Gi"),
			}},
		},
	}
	desired := &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("2Gi"),
			}},
		},
	}

	MutatePersistentVolumeClaim(current, desired, nil)

	storage := current.Spec.Resources.Requests[corev1.ResourceStorage]
	assert.Equal(t, "2Gi", storage.String())
}

func TestMutateDeployment(t *testing.T) {
	one := int32(1)
	two := int32(2)
	current := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Labels:          map[string]string{"keep": "label"},
			Annotations:     map[string]string{"owned": "old", "external": "keep"},
			OwnerReferences: []metav1.OwnerReference{{Name: "old-owner"}},
		},
		Spec: appsv1.DeploymentSpec{
			Paused:   false,
			Replicas: &one,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old"}},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"template-keep": "label"},
					Annotations: map[string]string{"owned": "old", "external": "keep"},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "old",
				},
			},
		},
	}
	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Labels:          map[string]string{"desired": "label"},
			Annotations:     map[string]string{"owned": "new"},
			OwnerReferences: []metav1.OwnerReference{{Name: "owner"}},
		},
		Spec: appsv1.DeploymentSpec{
			Paused:   true,
			Replicas: &two,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "new"}},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"template": "new"},
					Annotations: map[string]string{"owned": "new"},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "desired",
					Containers: []corev1.Container{
						{Name: "app", Image: "example/app:test"},
					},
				},
			},
		},
	}

	mergeOwnedOnly := func(current map[string]string, desired map[string]string) map[string]string {
		return map[string]string{
			"external": current["external"],
			"owned":    desired["owned"],
		}
	}
	MutateDeployment(current, desired, mergeOwnedOnly, func(current *corev1.PodSpec, desired *corev1.PodSpec) {
		current.ServiceAccountName = desired.ServiceAccountName
		current.Containers = desired.Containers
	})

	assert.Equal(t, map[string]string{"keep": "label", "desired": "label"}, current.Labels)
	assert.Equal(t, map[string]string{"external": "keep", "owned": "new"}, current.Annotations)
	assert.Equal(t, []metav1.OwnerReference{{Name: "owner"}}, current.OwnerReferences)
	assert.Equal(t, &metav1.LabelSelector{MatchLabels: map[string]string{"app": "old"}}, current.Spec.Selector)
	assert.True(t, current.Spec.Paused)
	assert.Equal(t, &two, current.Spec.Replicas)
	assert.Equal(t, appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}, current.Spec.Strategy)
	assert.Equal(t, map[string]string{"template-keep": "label", "template": "new"}, current.Spec.Template.Labels)
	assert.Equal(t, map[string]string{"external": "keep", "owned": "new"}, current.Spec.Template.Annotations)
	assert.Equal(t, "desired", current.Spec.Template.Spec.ServiceAccountName)
	assert.Equal(t, []corev1.Container{{Name: "app", Image: "example/app:test"}}, current.Spec.Template.Spec.Containers)
}

func TestMutateServicePreservesClusterIP(t *testing.T) {
	current := &corev1.Service{
		Spec: corev1.ServiceSpec{
			ClusterIP: "10.96.0.1",
			Selector:  map[string]string{"old": "true"},
		},
	}
	desired := &corev1.Service{
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": "test"},
			Ports:    []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	}

	MutateService(current, desired, nil)

	assert.Equal(t, "10.96.0.1", current.Spec.ClusterIP)
	assert.Equal(t, map[string]string{"app": "test"}, current.Spec.Selector)
	assert.Equal(t, []corev1.ServicePort{{Name: "http", Port: 80}}, current.Spec.Ports)
}
