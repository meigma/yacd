package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
