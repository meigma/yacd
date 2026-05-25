package readiness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeploymentAvailable(t *testing.T) {
	replicas := int32(2)
	tests := []struct {
		name       string
		deployment *appsv1.Deployment
		want       bool
	}{
		{
			name:       "nil deployment",
			deployment: nil,
			want:       false,
		},
		{
			name: "available",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{Replicas: &replicas},
				Status: appsv1.DeploymentStatus{
					UpdatedReplicas:   2,
					ReadyReplicas:     2,
					AvailableReplicas: 2,
					Conditions: []appsv1.DeploymentCondition{
						{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
					},
				},
			},
			want: true,
		},
		{
			name: "zero desired replicas are not available",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{Replicas: new(int32)},
			},
			want: false,
		},
		{
			name: "missing available condition",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{Replicas: &replicas},
				Status: appsv1.DeploymentStatus{
					UpdatedReplicas:   2,
					ReadyReplicas:     2,
					AvailableReplicas: 2,
				},
			},
			want: false,
		},
		{
			name: "not enough ready replicas",
			deployment: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{Replicas: &replicas},
				Status: appsv1.DeploymentStatus{
					UpdatedReplicas:   2,
					ReadyReplicas:     1,
					AvailableReplicas: 2,
					Conditions: []appsv1.DeploymentCondition{
						{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, DeploymentAvailable(tt.deployment))
		})
	}
}

func TestPodContainerReady(t *testing.T) {
	now := metav1.Now()
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "running ready container",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "node",
						Ready: true,
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				},
			}},
			want: true,
		},
		{
			name: "terminating pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "node",
							Ready: true,
							State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "container is ready but not running",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "node", Ready: true},
				},
			}},
			want: false,
		},
		{
			name: "wrong container",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "other",
						Ready: true,
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					},
				},
			}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, PodContainerReady(tt.pod, "node"))
		})
	}
}
