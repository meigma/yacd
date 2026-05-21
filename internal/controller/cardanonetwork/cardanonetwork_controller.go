// Package cardanonetwork contains the CardanoNetwork controller.
package cardanonetwork

import (
	"context"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
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
	logf.FromContext(ctx, "cardanonetwork", req.String()).
		Info("CardanoNetwork controller scaffold is active")

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
