package cardanonetwork

import (
	"context"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// acceptedNetworkIdentity is the accepted Cardano network identity read from
// owned runtime material. Status mirrors this value for humans and dependent
// controllers, but it is not part of the acceptance decision.
type acceptedNetworkIdentity struct {
	NetworkFingerprint  string
	LocalnetFingerprint string
}

func (i acceptedNetworkIdentity) empty() bool {
	return i.NetworkFingerprint == "" && i.LocalnetFingerprint == ""
}

// acceptedNetworkIdentity reads the accepted identity from owned children.
// The primary PVC is authoritative because it carries the durable node state.
// If the PVC is absent, the Deployment pod template is a fallback anchor that
// keeps already-applied networks from accepting unsafe spec drift.
func (r *CardanoNetworkReconciler) acceptedNetworkIdentity(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (acceptedNetworkIdentity, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryNodeStatePVCName(network)}, pvc); err != nil {
		if !apierrors.IsNotFound(err) {
			return acceptedNetworkIdentity{}, err
		}
		return r.acceptedNetworkIdentityFromDeployment(ctx, network)
	}
	if !metav1.IsControlledBy(pvc, network) {
		return acceptedNetworkIdentity{}, nil
	}

	return acceptedNetworkIdentityFromAnnotations(pvc.Annotations), nil
}

func (r *CardanoNetworkReconciler) acceptedNetworkIdentityFromDeployment(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
) (acceptedNetworkIdentity, error) {
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: network.Namespace, Name: primaryWorkloadName(network)}, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return acceptedNetworkIdentity{}, nil
		}
		return acceptedNetworkIdentity{}, err
	}
	if !metav1.IsControlledBy(deployment, network) {
		return acceptedNetworkIdentity{}, nil
	}

	return acceptedNetworkIdentityFromAnnotations(deployment.Spec.Template.Annotations), nil
}

func acceptedNetworkIdentityFromAnnotations(annotations map[string]string) acceptedNetworkIdentity {
	return acceptedNetworkIdentity{
		NetworkFingerprint:  annotations[networkFingerprintAnno],
		LocalnetFingerprint: annotations[localnetFingerprintAnno],
	}
}
