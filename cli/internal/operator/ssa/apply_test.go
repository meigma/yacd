package ssa

import (
	"testing"

	"github.com/meigma/yacd/charts"
	"github.com/meigma/yacd/cli/internal/operator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func unstructuredObject(apiVersion, kind, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	obj.SetName(name)
	return obj
}

func TestPartitionCRDsSeparatesAndOrders(t *testing.T) {
	objs := []*unstructured.Unstructured{
		unstructuredObject("apps/v1", "Deployment", "manager"),
		unstructuredObject("apiextensions.k8s.io/v1", "CustomResourceDefinition", "networks"),
		unstructuredObject("rbac.authorization.k8s.io/v1", "ClusterRole", "role"),
		unstructuredObject("v1", "ServiceAccount", "sa"),
	}

	crds, rest := partitionCRDs(objs)

	require.Len(t, crds, 1)
	assert.Equal(t, "CustomResourceDefinition", crds[0].GetKind())

	gotOrder := make([]string, 0, len(rest))
	for _, obj := range rest {
		gotOrder = append(gotOrder, obj.GetKind())
	}
	assert.Equal(t, []string{"ServiceAccount", "ClusterRole", "Deployment"}, gotOrder,
		"identity and permissions apply before the workload")
}

func TestVersionFromEmbeddedChartMatchesChartAppVersion(t *testing.T) {
	objs, err := render(charts.OperatorChart, installNamespace, operator.Default().ToHelmValues())
	require.NoError(t, err)

	version, err := versionFromObjects(objs)
	require.NoError(t, err)

	// Tracks charts/yacd/Chart.yaml appVersion; bump alongside an operator
	// release re-sync. This is the tripwire that the embedded chart copy is in
	// sync with the pinned release.
	assert.Equal(t, "v0.1.1", version)
	assert.True(t, semver.IsValid(version), "embedded version must be valid semver")
}

func TestVersionFromObjectsRequiresManagerDeployment(t *testing.T) {
	objs := []*unstructured.Unstructured{
		unstructuredObject("v1", "ServiceAccount", "sa"),
	}

	_, err := versionFromObjects(objs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manager Deployment not found")
}

func TestDeploymentAvailable(t *testing.T) {
	available := func(generation, observed int64, replicas int32, condition corev1.ConditionStatus) *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Generation: generation},
			Status: appsv1.DeploymentStatus{
				ObservedGeneration: observed,
				AvailableReplicas:  replicas,
				Conditions: []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentAvailable, Status: condition},
				},
			},
		}
	}

	assert.True(t, deploymentAvailable(available(2, 2, 1, corev1.ConditionTrue)))
	assert.False(t, deploymentAvailable(available(2, 2, 1, corev1.ConditionFalse)), "condition not Available")
	assert.False(t, deploymentAvailable(available(3, 2, 1, corev1.ConditionTrue)), "status not yet observed")
	assert.False(t, deploymentAvailable(available(2, 2, 0, corev1.ConditionTrue)), "no available replicas")

	noCondition := &appsv1.Deployment{Status: appsv1.DeploymentStatus{AvailableReplicas: 1}}
	assert.False(t, deploymentAvailable(noCondition), "missing Available condition")
}
