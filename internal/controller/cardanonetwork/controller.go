// Package cardanonetwork contains the CardanoNetwork controller.
package cardanonetwork

import (
	"context"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// controllerName is the controller-runtime name used for logs, metrics,
	// and controller registration.
	controllerName = "cardanonetwork"
)

// CardanoNetworkReconciler reconciles CardanoNetwork resources.
type CardanoNetworkReconciler struct {
	// Client is the controller-runtime client used to read and write
	// CardanoNetwork resources and their owned children.
	client.Client

	// Scheme is the runtime scheme used when setting controller references on
	// owned child resources.
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanonetworks,verbs=get;list;watch

// Reconcile is the CardanoNetwork controller scaffold.
func (r *CardanoNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx, "cardanonetwork", req.String())

	network := &yacdv1alpha1.CardanoNetwork{}
	if err := r.Get(ctx, req.NamespacedName, network); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("CardanoNetwork not found; ignoring deleted object")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	statefulSet, err := (primaryWorkloadBuilder{scheme: r.Scheme}).Build(network)
	if err != nil {
		log.Info("CardanoNetwork primary workload is not supported yet", "error", err)
		return ctrl.Result{}, nil
	}

	log.V(1).Info("Built CardanoNetwork primary StatefulSet",
		"statefulset", client.ObjectKeyFromObject(statefulSet),
		"localnetFingerprint", statefulSet.Spec.Template.Annotations[localnetFingerprintAnno])

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the CardanoNetwork controller with the manager.
func (r *CardanoNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	logf.Log.WithName("controllers").WithName(controllerName).
		Info("Starting CardanoNetwork controller scaffold")

	return ctrl.NewControllerManagedBy(mgr).
		For(&yacdv1alpha1.CardanoNetwork{}).
		Named(controllerName).
		Complete(r)
}
