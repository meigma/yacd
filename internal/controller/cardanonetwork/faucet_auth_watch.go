package cardanonetwork

import (
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// faucetAuthSecretEventPredicate keeps the owned Secret watch scoped to
// CardanoNetwork faucet auth Secrets. Custom public profile Secret events use
// their own field-indexed handler in public_profile_source.go.
func faucetAuthSecretEventPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isPrimaryFaucetAuthSecretObject(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isPrimaryFaucetAuthSecretObject(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isPrimaryFaucetAuthSecretObject(e.ObjectOld) ||
				isPrimaryFaucetAuthSecretObject(e.ObjectNew)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return isPrimaryFaucetAuthSecretObject(e.Object)
		},
	}
}

func isPrimaryFaucetAuthSecretObject(object client.Object) bool {
	if object == nil {
		return false
	}
	if !strings.HasSuffix(object.GetName(), "-faucet-auth") {
		return false
	}
	labels := object.GetLabels()
	if labels[labelAppManagedBy] != "yacd" ||
		labels[labelAppName] != labelPrimaryNodeName ||
		labels[labelCardanoRole] != labelPrimaryRole {
		return false
	}

	controller := metav1.GetControllerOf(object)
	return controller != nil &&
		controller.APIVersion == yacdv1alpha1.GroupVersion.String() &&
		controller.Kind == "CardanoNetwork"
}
