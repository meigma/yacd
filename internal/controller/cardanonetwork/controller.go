// Package cardanonetwork contains the CardanoNetwork controller.
package cardanonetwork

import (
	"context"
	"errors"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// controllerName is the controller-runtime name used for logs, metrics,
	// and controller registration.
	controllerName = "cardanonetwork"

	primaryWorkloadReadinessRequeueAfter = 15 * time.Second
	resourceConflictRequeueAfter         = time.Minute
)

// CardanoNetworkReconciler reconciles CardanoNetwork resources.
type CardanoNetworkReconciler struct {
	// Client is the controller-runtime client used to read and write
	// CardanoNetwork resources and their owned children.
	client.Client

	// Reader is the uncached reader used for live runtime status checks.
	Reader client.Reader

	// Scheme is the runtime scheme used when setting controller references on
	// owned child resources.
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanonetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanonetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list

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
	if !network.DeletionTimestamp.IsZero() {
		log.V(1).Info("CardanoNetwork is deleting; skipping reconcile")
		return ctrl.Result{}, nil
	}

	resources, err := (primaryWorkloadBuilder{scheme: r.Scheme}).Build(network)
	if err != nil {
		var unsupportedSpec unsupportedSpecError
		if !errors.As(err, &unsupportedSpec) {
			return ctrl.Result{}, err
		}

		log.Info("CardanoNetwork primary workload is not supported yet", "error", err)
		if statusErr := r.patchStatusConditions(ctx, network,
			degradedCondition(metav1.ConditionTrue, conditionReasonUnsupportedSpec, err.Error()),
			progressingCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			condition(conditionTypeReady, metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			nodeReadyCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			ogmiosReadyCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			kupoReadyCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
		); statusErr != nil {
			return ctrl.Result{}, statusErr
		}

		return ctrl.Result{}, nil
	}

	localnetFingerprint := resources.Deployment.Spec.Template.Annotations[localnetFingerprintAnno]
	if err := validateAcceptedLocalnetFingerprint(network, localnetFingerprint); err != nil {
		return r.handlePrimaryWorkloadApplyError(ctx, network, err)
	}

	pvcResult, err := r.applyPrimaryPersistentVolumeClaim(ctx, resources.PersistentVolumeClaim)
	if err != nil {
		return r.handlePrimaryWorkloadApplyError(ctx, network, err)
	}

	deploymentResult, err := r.applyPrimaryDeployment(ctx, resources.Deployment)
	if err != nil {
		return r.handlePrimaryWorkloadApplyError(ctx, network, err)
	}

	serviceResult, err := r.applyPrimaryService(ctx, resources.Service)
	if err != nil {
		return r.handlePrimaryWorkloadApplyError(ctx, network, err)
	}

	var ogmiosServiceResult controllerutil.OperationResult
	if resources.OgmiosService != nil {
		ogmiosServiceResult, err = r.applyPrimaryService(ctx, resources.OgmiosService)
	} else {
		ogmiosServiceResult, err = r.deletePrimaryOgmiosService(ctx, network)
	}
	if err != nil {
		return r.handlePrimaryWorkloadApplyError(ctx, network, err)
	}

	var kupoServiceResult controllerutil.OperationResult
	if resources.KupoService != nil {
		kupoServiceResult, err = r.applyPrimaryService(ctx, resources.KupoService)
	} else {
		kupoServiceResult, err = r.deletePrimaryKupoService(ctx, network)
	}
	if err != nil {
		return r.handlePrimaryWorkloadApplyError(ctx, network, err)
	}

	ready, err := r.patchPrimaryWorkloadAppliedStatus(ctx, network, localnetFingerprint, resources.Service, resources.OgmiosService, resources.KupoService)
	if err != nil {
		return ctrl.Result{}, err
	}

	resultLog := log
	if pvcResult == controllerutil.OperationResultNone &&
		deploymentResult == controllerutil.OperationResultNone &&
		serviceResult == controllerutil.OperationResultNone &&
		ogmiosServiceResult == controllerutil.OperationResultNone &&
		kupoServiceResult == controllerutil.OperationResultNone {
		resultLog = log.V(1)
	}
	ogmiosServiceKey := "disabled"
	if resources.OgmiosService != nil {
		ogmiosServiceKey = client.ObjectKeyFromObject(resources.OgmiosService).String()
	}
	kupoServiceKey := "disabled"
	if resources.KupoService != nil {
		kupoServiceKey = client.ObjectKeyFromObject(resources.KupoService).String()
	}
	resultLog.Info("Applied CardanoNetwork primary workload",
		"persistentVolumeClaim", client.ObjectKeyFromObject(resources.PersistentVolumeClaim),
		"persistentVolumeClaimOperation", pvcResult,
		"deployment", client.ObjectKeyFromObject(resources.Deployment),
		"deploymentOperation", deploymentResult,
		"service", client.ObjectKeyFromObject(resources.Service),
		"serviceOperation", serviceResult,
		"ogmiosService", ogmiosServiceKey,
		"ogmiosServiceOperation", ogmiosServiceResult,
		"kupoService", kupoServiceKey,
		"kupoServiceOperation", kupoServiceResult,
		"localnetFingerprint", localnetFingerprint)

	if ready.Status != metav1.ConditionTrue && ready.Reason == conditionReasonDeploymentProgressing {
		return ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

func (r *CardanoNetworkReconciler) handlePrimaryWorkloadApplyError(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	err error,
) (ctrl.Result, error) {
	var unsupported unsupportedApplyError
	if !errors.As(err, &unsupported) {
		return ctrl.Result{}, err
	}

	if statusErr := r.patchStatusConditions(ctx, network,
		degradedCondition(metav1.ConditionTrue, unsupported.reason, unsupported.message),
		progressingCondition(metav1.ConditionFalse, unsupported.reason, unsupported.message),
		condition(conditionTypeReady, metav1.ConditionFalse, unsupported.reason, unsupported.message),
		nodeReadyCondition(metav1.ConditionFalse, unsupported.reason, unsupported.message),
		ogmiosReadyCondition(metav1.ConditionFalse, unsupported.reason, unsupported.message),
		kupoReadyCondition(metav1.ConditionFalse, unsupported.reason, unsupported.message),
	); statusErr != nil {
		return ctrl.Result{}, statusErr
	}
	if unsupported.reason == conditionReasonResourceConflict {
		return ctrl.Result{RequeueAfter: resourceConflictRequeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the CardanoNetwork controller with the manager.
func (r *CardanoNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	logf.Log.WithName("controllers").WithName(controllerName).
		Info("Starting CardanoNetwork controller scaffold")

	return ctrl.NewControllerManagedBy(mgr).
		For(&yacdv1alpha1.CardanoNetwork{}, ctrlbuilder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Service{}).
		Named(controllerName).
		Complete(r)
}
