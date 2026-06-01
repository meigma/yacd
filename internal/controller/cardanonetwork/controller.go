package cardanonetwork

import (
	"context"
	"errors"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlbuilder "sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// controllerName is the controller-runtime name used for logs, metrics,
	// and controller registration.
	controllerName = "cardanonetwork"

	primaryWorkloadReadinessRequeueAfter = 15 * time.Second
	resourceConflictRequeueAfter         = time.Minute
	faucetSecretRepairRequeueAfter       = 10 * time.Minute
	disabledChildResourceLogValue        = "disabled"
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

	// DefaultFaucetImage is the image used for faucet sidecars when the
	// CardanoNetwork spec does not provide an override.
	DefaultFaucetImage string

	// DefaultCardanoTestnetImage overrides the cardano-testnet container
	// image used for the create-env init container, the faucet
	// source-address init container, and (when spec.node.image is unset)
	// the primary cardano-node container. Empty leaves the built-in
	// "<repo>:<toolVersion>-<revision>" formula in place.
	DefaultCardanoTestnetImage string

	// DefaultCardanoToolsImage overrides the cardano-tools container image
	// used for artifact staging (public profile fetch and artifact serve).
	// Empty leaves the built-in "<repo>:<toolVersion>-<revision>" formula
	// in place.
	DefaultCardanoToolsImage string

	// Now returns the current time. Tests override this to exercise
	// time-bounded recovery behavior deterministically.
	Now func() time.Time

	// syncProberOverride replaces the Ogmios health prober in tests.
	syncProberOverride cardanoNetworkSyncProber

	// timingProberOverride replaces the served-artifact timing prober in tests.
	timingProberOverride cardanoNetworkTimingProber
}

// +kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanonetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanonetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=yacd.meigma.io,resources=cardanodbsyncs,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list

// Reconcile applies the CardanoNetwork primary workload and publishes runtime status.
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

	dbSyncAttachment, err := r.primaryDBSyncAttachment(ctx, network)
	if err != nil {
		return ctrl.Result{}, err
	}

	acceptedIdentity, err := r.acceptedNetworkIdentity(ctx, network)
	if err != nil {
		return ctrl.Result{}, err
	}

	resources, err := (primaryWorkloadBuilder{
		scheme:                     r.Scheme,
		defaultFaucetImage:         r.DefaultFaucetImage,
		defaultCardanoTestnetImage: r.DefaultCardanoTestnetImage,
		defaultCardanoToolsImage:   r.DefaultCardanoToolsImage,
		acceptedIdentity:           acceptedIdentity,
		dbSyncAttachment:           dbSyncAttachment.Attachment,
	}).Build(network)
	if err != nil {
		var unsupportedSpec unsupportedSpecError
		if !errors.As(err, &unsupportedSpec) {
			return ctrl.Result{}, err
		}

		log.Info("CardanoNetwork primary workload is not supported yet", "error", err)
		if revokeErr := r.revokePrimaryFaucetExposure(ctx, network); revokeErr != nil {
			return ctrl.Result{}, revokeErr
		}
		dbSyncAttachmentCondition := dbSyncAttachment.statusCondition()
		if dbSyncAttachment.Attachment != nil {
			dbSyncAttachmentCondition = dbSyncAttachmentReadyCondition(
				metav1.ConditionFalse,
				conditionReasonUnsupportedSpec,
				conditionMessagePrimaryWorkloadUnsupported,
			)
		}
		if statusErr := r.patchStatusConditionsClearingFaucet(ctx, network,
			primaryNetworkPlan{},
			acceptedNetworkIdentity{},
			degradedCondition(metav1.ConditionTrue, conditionReasonUnsupportedSpec, err.Error()),
			progressingCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			ctrlstatus.Condition(string(conditionTypeReady), metav1.ConditionFalse, string(conditionReasonUnsupportedSpec), conditionMessagePrimaryWorkloadUnsupported),
			dbSyncAttachmentCondition,
			nodeReadyCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			nodeSynchronizedCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			nodeProgressingCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			ogmiosReadyCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			kupoReadyCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			faucetReadyCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
			artifactsReadyCondition(metav1.ConditionFalse, conditionReasonUnsupportedSpec, conditionMessagePrimaryWorkloadUnsupported),
		); statusErr != nil {
			return ctrl.Result{}, statusErr
		}

		return ctrl.Result{}, nil
	}

	if err := validateAcceptedNetworkFingerprint(acceptedIdentity, resources.NetworkPlan); err != nil {
		return r.handlePrimaryWorkloadApplyError(ctx, network, resources.NetworkPlan, acceptedIdentity, resources.DBSyncAttached, dbSyncAttachment.statusCondition(), err)
	}
	if err := r.validateAcceptedPrimaryPersistentVolumeClaim(ctx, resources.PersistentVolumeClaim, acceptedIdentity); err != nil {
		return r.handlePrimaryWorkloadApplyError(ctx, network, resources.NetworkPlan, acceptedIdentity, resources.DBSyncAttached, dbSyncAttachment.statusCondition(), err)
	}

	applyResults, err := r.applyPrimaryWorkloadResources(ctx, network, resources, acceptedIdentity)
	if err != nil {
		return r.handlePrimaryWorkloadApplyError(ctx, network, resources.NetworkPlan, acceptedIdentity, resources.DBSyncAttached, dbSyncAttachment.statusCondition(), err)
	}

	ready, err := r.patchPrimaryWorkloadAppliedStatus(ctx, network, resources.NetworkPlan, acceptedIdentity, resources.Service, resources.OgmiosService, resources.KupoService, resources.FaucetService, resources.ArtifactsService, resources.FaucetAuthSecret, resources.DBSyncAttached, dbSyncAttachment.statusCondition())
	if err != nil {
		return ctrl.Result{}, err
	}

	resultLog := log
	if applyResults.unchanged() {
		resultLog = log.V(1)
	}
	ogmiosServiceKey := disabledChildResourceLogValue
	if resources.OgmiosService != nil {
		ogmiosServiceKey = client.ObjectKeyFromObject(resources.OgmiosService).String()
	}
	kupoServiceKey := disabledChildResourceLogValue
	if resources.KupoService != nil {
		kupoServiceKey = client.ObjectKeyFromObject(resources.KupoService).String()
	}
	faucetServiceKey := disabledChildResourceLogValue
	if resources.FaucetService != nil {
		faucetServiceKey = client.ObjectKeyFromObject(resources.FaucetService).String()
	}
	artifactsServiceKey := disabledChildResourceLogValue
	if resources.ArtifactsService != nil {
		artifactsServiceKey = client.ObjectKeyFromObject(resources.ArtifactsService).String()
	}
	faucetAuthSecretKey := disabledChildResourceLogValue
	if resources.FaucetAuthSecret != nil {
		faucetAuthSecretKey = client.ObjectKeyFromObject(resources.FaucetAuthSecret).String()
	}
	resultLog.Info("Applied CardanoNetwork primary workload",
		"persistentVolumeClaim", client.ObjectKeyFromObject(resources.PersistentVolumeClaim),
		"persistentVolumeClaimOperation", applyResults.PersistentVolumeClaim,
		"deployment", client.ObjectKeyFromObject(resources.Deployment),
		"deploymentOperation", applyResults.Deployment,
		"service", client.ObjectKeyFromObject(resources.Service),
		"serviceOperation", applyResults.Service,
		"ogmiosService", ogmiosServiceKey,
		"ogmiosServiceOperation", applyResults.OgmiosService,
		"kupoService", kupoServiceKey,
		"kupoServiceOperation", applyResults.KupoService,
		"faucetService", faucetServiceKey,
		"faucetServiceOperation", applyResults.FaucetService,
		"artifactsService", artifactsServiceKey,
		"artifactsServiceOperation", applyResults.ArtifactsService,
		"faucetAuthSecret", faucetAuthSecretKey,
		"faucetAuthSecretOperation", applyResults.FaucetAuthSecret,
		"networkFingerprint", resources.NetworkPlan.Fingerprint)

	if result, requeue := primaryWorkloadRequeueResult(ready, resources.FaucetAuthSecret != nil, resources.OgmiosService != nil); requeue {
		return result, nil
	}

	return ctrl.Result{}, nil
}

func (r *CardanoNetworkReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func primaryWorkloadRequeueResult(
	ready metav1.Condition,
	hasFaucetAuthSecret bool,
	ogmiosEnabled bool,
) (ctrl.Result, bool) {
	if ready.Status != metav1.ConditionTrue &&
		(ready.Reason == string(conditionReasonDeploymentProgressing) ||
			ready.Reason == string(conditionReasonDBSyncAttachmentPending)) {
		return ctrl.Result{RequeueAfter: primaryWorkloadReadinessRequeueAfter}, true
	}
	if ogmiosEnabled {
		return ctrl.Result{RequeueAfter: nodeSyncProbeRequeueAfter}, true
	}
	if hasFaucetAuthSecret {
		return ctrl.Result{RequeueAfter: faucetSecretRepairRequeueAfter}, true
	}

	return ctrl.Result{}, false
}

// primaryWorkloadApplyResults captures the per-resource OperationResult
// returned by each apply* call so the reconciler can decide whether the
// run produced cluster mutations (and therefore whether to log at info or
// debug).
type primaryWorkloadApplyResults struct {
	PersistentVolumeClaim  controllerutil.OperationResult
	Deployment             controllerutil.OperationResult
	Service                controllerutil.OperationResult
	OgmiosService          controllerutil.OperationResult
	KupoService            controllerutil.OperationResult
	FaucetService          controllerutil.OperationResult
	ArtifactsService       controllerutil.OperationResult
	FaucetAuthSecret       controllerutil.OperationResult
	FaucetAuthSecretObject *corev1.Secret
}

// unchanged reports whether every owned child was already in the desired
// state. Used to demote the reconcile log line to debug level when nothing
// actually changed.
func (r primaryWorkloadApplyResults) unchanged() bool {
	return r.PersistentVolumeClaim == controllerutil.OperationResultNone &&
		r.Deployment == controllerutil.OperationResultNone &&
		r.Service == controllerutil.OperationResultNone &&
		r.OgmiosService == controllerutil.OperationResultNone &&
		r.KupoService == controllerutil.OperationResultNone &&
		r.FaucetService == controllerutil.OperationResultNone &&
		r.ArtifactsService == controllerutil.OperationResultNone &&
		r.FaucetAuthSecret == controllerutil.OperationResultNone
}

// applyPrimaryWorkloadResources applies the primary workload bundle in
// dependency order: the PVC and faucet auth Secret are created before the
// Deployment so its volumes can mount; the Deployment itself rolls last;
// finally the optional Services are reconciled or deleted to match the spec.
func (r *CardanoNetworkReconciler) applyPrimaryWorkloadResources(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	resources *primaryWorkloadResources,
	acceptedIdentity acceptedNetworkIdentity,
) (primaryWorkloadApplyResults, error) {
	var results primaryWorkloadApplyResults
	var err error

	results.PersistentVolumeClaim, err = r.applyPrimaryPersistentVolumeClaim(ctx, resources.PersistentVolumeClaim, acceptedIdentity)
	if err != nil {
		return results, err
	}

	if resources.FaucetAuthSecret != nil {
		results.FaucetAuthSecret, results.FaucetAuthSecretObject, err = r.applyPrimaryFaucetAuthSecret(ctx, resources.FaucetAuthSecret)
		if err != nil {
			return results, err
		}
	}

	if results.FaucetAuthSecretObject != nil {
		setDeploymentFaucetAuthTokenHash(resources.Deployment, results.FaucetAuthSecretObject)
	}
	results.Deployment, err = r.applyPrimaryDeployment(ctx, resources.Deployment)
	if err != nil {
		return results, err
	}

	results.Service, err = r.applyPrimaryService(ctx, resources.Service)
	if err != nil {
		return results, err
	}

	results.OgmiosService, err = r.applyOrDeletePrimaryChainAPIService(ctx, network, resources.OgmiosService, r.deletePrimaryOgmiosService)
	if err != nil {
		return results, err
	}

	results.KupoService, err = r.applyOrDeletePrimaryChainAPIService(ctx, network, resources.KupoService, r.deletePrimaryKupoService)
	if err != nil {
		return results, err
	}

	results.FaucetService, err = r.applyOrDeletePrimaryChainAPIService(ctx, network, resources.FaucetService, r.deletePrimaryFaucetService)
	if err != nil {
		return results, err
	}

	results.ArtifactsService, err = r.applyOrDeletePrimaryChainAPIService(ctx, network, resources.ArtifactsService, r.deletePrimaryArtifactsService)
	if err != nil {
		return results, err
	}

	if resources.FaucetAuthSecret == nil {
		results.FaucetAuthSecret, err = r.deletePrimaryFaucetAuthSecret(ctx, network)
	}

	return results, err
}

// applyOrDeletePrimaryChainAPIService applies the desired chain API Service
// when the corresponding sidecar is enabled, or deletes the live Service
// (using the caller's per-sidecar delete helper) when the sidecar is
// disabled. This keeps the optional-Service flip-flop in one shape.
func (r *CardanoNetworkReconciler) applyOrDeletePrimaryChainAPIService(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	service *corev1.Service,
	deleteFn func(context.Context, *yacdv1alpha1.CardanoNetwork) (controllerutil.OperationResult, error),
) (controllerutil.OperationResult, error) {
	if service != nil {
		return r.applyPrimaryService(ctx, service)
	}

	return deleteFn(ctx, network)
}

// handlePrimaryWorkloadApplyError funnels typed status condition errors
// from any apply step into a Degraded status patch and faucet revocation.
// Untyped errors are returned unchanged so the controller-runtime loop
// reschedules with its default backoff.
func (r *CardanoNetworkReconciler) handlePrimaryWorkloadApplyError(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	networkPlan primaryNetworkPlan,
	acceptedIdentity acceptedNetworkIdentity,
	dbSyncAttached bool,
	dbSyncAttachmentCondition metav1.Condition,
	err error,
) (ctrl.Result, error) {
	var conditionErr statusConditionError
	if !errors.As(err, &conditionErr) {
		return ctrl.Result{}, err
	}

	if revokeErr := r.revokePrimaryFaucetExposure(ctx, network); revokeErr != nil {
		return ctrl.Result{}, revokeErr
	}
	// conditionErr.Reason is untyped (it crosses the ctrlstatus boundary as a
	// plain string); cast to conditionReason once and reuse for the condition
	// builders below.
	reason := conditionReason(conditionErr.Reason)
	dbSyncAttachment := dbSyncAttachmentReadyCondition(
		metav1.ConditionFalse,
		conditionReasonDBSyncAttachmentNotRequested,
		conditionMessageDBSyncAttachmentNotRequested,
	)
	if dbSyncAttached {
		dbSyncAttachment = dbSyncAttachmentReadyCondition(metav1.ConditionFalse, reason, conditionErr.Message)
	} else if dbSyncAttachmentCondition.Type != "" {
		dbSyncAttachment = dbSyncAttachmentCondition
	}
	if statusErr := r.patchStatusConditionsClearingFaucet(ctx, network,
		networkPlan,
		acceptedIdentity,
		degradedCondition(metav1.ConditionTrue, reason, conditionErr.Message),
		progressingCondition(metav1.ConditionFalse, reason, conditionErr.Message),
		ctrlstatus.Condition(string(conditionTypeReady), metav1.ConditionFalse, conditionErr.Reason, conditionErr.Message),
		dbSyncAttachment,
		nodeReadyCondition(metav1.ConditionFalse, reason, conditionErr.Message),
		nodeSynchronizedCondition(metav1.ConditionFalse, reason, conditionErr.Message),
		nodeProgressingCondition(metav1.ConditionFalse, reason, conditionErr.Message),
		ogmiosReadyCondition(metav1.ConditionFalse, reason, conditionErr.Message),
		kupoReadyCondition(metav1.ConditionFalse, reason, conditionErr.Message),
		faucetReadyCondition(metav1.ConditionFalse, reason, conditionErr.Message),
		artifactsReadyCondition(metav1.ConditionFalse, reason, conditionErr.Message),
	); statusErr != nil {
		return ctrl.Result{}, statusErr
	}
	if reason == conditionReasonResourceConflict {
		return ctrl.Result{RequeueAfter: resourceConflictRequeueAfter}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the CardanoNetwork controller with the manager.
func (r *CardanoNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	logf.Log.WithName("controllers").WithName(controllerName).
		Info("Starting CardanoNetwork controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&yacdv1alpha1.CardanoNetwork{}, ctrlbuilder.WithPredicates(cardanoNetworkEventPredicate())).
		Watches(&yacdv1alpha1.CardanoDBSync{}, r.dbSyncPlacementEventHandler()).
		Owns(&corev1.Secret{}, ctrlbuilder.WithPredicates(faucetAuthSecretEventPredicate())).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Service{}).
		Named(controllerName).
		Complete(r)
}

func cardanoNetworkEventPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNetwork, oldOK := e.ObjectOld.(*yacdv1alpha1.CardanoNetwork)
			newNetwork, newOK := e.ObjectNew.(*yacdv1alpha1.CardanoNetwork)
			if !oldOK || !newOK {
				return true
			}
			if oldNetwork.Generation != newNetwork.Generation {
				return true
			}

			return networkIdentityStatusFingerprintsChanged(oldNetwork, newNetwork)
		},
	}
}

func networkIdentityStatusFingerprintsChanged(oldNetwork *yacdv1alpha1.CardanoNetwork, newNetwork *yacdv1alpha1.CardanoNetwork) bool {
	oldIdentity := oldNetwork.Status.Network
	newIdentity := newNetwork.Status.Network
	if oldIdentity == nil || newIdentity == nil {
		return oldIdentity != newIdentity
	}

	return oldIdentity.NetworkFingerprint != newIdentity.NetworkFingerprint ||
		oldIdentity.LocalnetFingerprint != newIdentity.LocalnetFingerprint
}
