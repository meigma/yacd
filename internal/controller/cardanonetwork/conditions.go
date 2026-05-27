package cardanonetwork

import (
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// conditionType is the typed name of a CardanoNetwork status condition. The
// underlying string is what appears on the wire; the typed form keeps the
// package-internal vocabulary closed so callers cannot accidentally pass an
// arbitrary string to a condition builder.
type conditionType string

// conditionReason is the typed reason carried by a CardanoNetwork status
// condition. Same intent as [conditionType]: closed package-internal
// vocabulary, untyped on the wire.
type conditionReason string

// Condition types used on CardanoNetwork.Status.Conditions.
const (
	conditionTypeProgressing           conditionType = "Progressing"
	conditionTypeDegraded              conditionType = "Degraded"
	conditionTypeReady                 conditionType = "Ready"
	conditionTypeDBSyncAttachmentReady conditionType = "DBSyncAttachmentReady"
	conditionTypeNodeReady             conditionType = "NodeReady"
	conditionTypeOgmiosReady           conditionType = "OgmiosReady"
	conditionTypeKupoReady             conditionType = "KupoReady"
	conditionTypeFaucetReady           conditionType = "FaucetReady"
	conditionTypeArtifactsReady        conditionType = "ArtifactsReady"
)

// Shared condition reasons used across multiple condition types.
const (
	conditionReasonReconcileSucceeded           conditionReason = "ReconcileSucceeded"
	conditionReasonUnsupportedSpec              conditionReason = "UnsupportedSpec"
	conditionReasonUnsupportedNetworkChange     conditionReason = "UnsupportedNetworkChange"
	conditionReasonUnsupportedLocalnetChange    conditionReason = "UnsupportedLocalnetChange"
	conditionReasonMissingNetworkFingerprint    conditionReason = "MissingNetworkFingerprint"
	conditionReasonMissingLocalnetFingerprint   conditionReason = "MissingLocalnetFingerprint"
	conditionReasonUnsupportedStorageChange     conditionReason = "UnsupportedStorageChange"
	conditionReasonUnsupportedWorkloadChange    conditionReason = "UnsupportedWorkloadChange"
	conditionReasonResourceConflict             conditionReason = "ResourceConflict"
	conditionReasonPlacementConflict            conditionReason = "PlacementConflict"
	conditionReasonDeploymentProgressing        conditionReason = "DeploymentProgressing"
	conditionReasonReady                        conditionReason = "Ready"
	conditionReasonPrimaryWorkloadMissing       conditionReason = "PrimaryWorkloadMissing"
	conditionReasonDBSyncAttachmentNotRequested conditionReason = "DBSyncAttachmentNotRequested"
	conditionReasonDBSyncAttachmentPending      conditionReason = "DBSyncAttachmentPending"
)

// Per-component condition reasons. The Ready / Disabled pair appears for
// each optional sidecar (ogmios is technically required for chain access,
// but the API admits explicit opt-out).
const (
	conditionReasonNodeReady             conditionReason = "NodeReady"
	conditionReasonOgmiosReady           conditionReason = "OgmiosReady"
	conditionReasonOgmiosDisabled        conditionReason = "OgmiosDisabled"
	conditionReasonKupoReady             conditionReason = "KupoReady"
	conditionReasonKupoDisabled          conditionReason = "KupoDisabled"
	conditionReasonFaucetReady           conditionReason = "FaucetReady"
	conditionReasonFaucetDisabled        conditionReason = "FaucetDisabled"
	conditionReasonArtifactsReady        conditionReason = "ArtifactsReady"
	conditionReasonArtifactsPending      conditionReason = "ArtifactsPending"
	conditionReasonDBSyncAttachmentReady conditionReason = "DBSyncAttachmentReady"
)

// Condition messages with stable wording across reconciles. Messages stay
// untyped string — they have no enumerable domain.
const (
	conditionMessagePrimaryWorkloadApplied       = "Primary node, artifact publisher, and chain API resources are applied"
	conditionMessagePrimaryWorkloadUnsupported   = "Primary node workload is not supported for this CardanoNetwork spec"
	conditionMessageReady                        = "CardanoNetwork is usable through its published endpoints"
	conditionMessageDBSyncAttachmentReady        = "Attached db-sync sidecar is ready"
	conditionMessageDBSyncAttachmentNotReady     = "Attached db-sync sidecar is not ready"
	conditionMessageDBSyncAttachmentNotRequested = "No primary-sidecar db-sync attachment is requested"
	conditionMessageDBSyncAttachmentPending      = "Primary-sidecar db-sync attachment is requested but material is not attachable"
	conditionMessagePrimaryNodeReady             = "Primary node container is ready"
	conditionMessageOgmiosReady                  = "Ogmios sidecar is connected and available through its Service"
	conditionMessageOgmiosDisabled               = "Ogmios chain API is disabled"
	conditionMessageKupoReady                    = "Kupo sidecar is available through its Service"
	conditionMessageKupoDisabled                 = "Kupo chain index API is disabled"
	conditionMessageFaucetReady                  = "Faucet sidecar is available through its Service"
	conditionMessageFaucetDisabled               = "Faucet API is disabled"
	conditionMessageArtifactsReady               = "Network artifact ConfigMap is published and verified"
)

// primaryDeploymentConditionFunc is the constructor signature shared by the
// per-component readiness condition builders. primaryDeploymentContainerBlockedCondition
// uses it to project a single "container not ready" verdict back to whichever
// component condition the caller is computing.
type primaryDeploymentConditionFunc func(metav1.ConditionStatus, conditionReason, string) metav1.Condition

// readyCondition aggregates the per-component readiness conditions into a
// single Ready condition. Optional sidecar conditions only contribute when
// the corresponding sidecar is enabled, so disabling a sidecar does not
// hold Ready back.
func readyCondition(dbSyncAttachmentReady metav1.Condition, nodeReady metav1.Condition, ogmiosReady metav1.Condition, kupoReady metav1.Condition, faucetReady metav1.Condition, artifactsReady metav1.Condition, dbSyncAttached bool, kupoEnabled bool, faucetEnabled bool) metav1.Condition {
	dependencies := make([]metav1.Condition, 0, 6)
	if dbSyncAttached {
		dependencies = append(dependencies, dbSyncAttachmentReady)
	}
	dependencies = append(dependencies, nodeReady, ogmiosReady)
	if kupoEnabled {
		dependencies = append(dependencies, kupoReady)
	}
	if faucetEnabled {
		dependencies = append(dependencies, faucetReady)
	}
	dependencies = append(dependencies, artifactsReady)

	return ctrlstatus.AggregateReady(string(conditionTypeReady), string(conditionReasonReady), conditionMessageReady, dependencies...)
}

// dbSyncAttachmentReadyCondition constructs a DBSyncAttachmentReady condition.
func dbSyncAttachmentReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeDBSyncAttachmentReady), status, string(reason), message)
}

// degradedCondition constructs a Degraded condition with the canonical
// type/reason/message shape.
func degradedCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeDegraded), status, string(reason), message)
}

// nodeReadyCondition constructs a NodeReady condition.
func nodeReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeNodeReady), status, string(reason), message)
}

// ogmiosReadyCondition constructs an OgmiosReady condition.
func ogmiosReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeOgmiosReady), status, string(reason), message)
}

// kupoReadyCondition constructs a KupoReady condition.
func kupoReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeKupoReady), status, string(reason), message)
}

// faucetReadyCondition constructs a FaucetReady condition.
func faucetReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeFaucetReady), status, string(reason), message)
}

// artifactsReadyCondition constructs an ArtifactsReady condition.
func artifactsReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeArtifactsReady), status, string(reason), message)
}

// progressingCondition constructs a Progressing condition with the canonical
// type/reason/message shape.
func progressingCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeProgressing), status, string(reason), message)
}

// progressingForReadyCondition projects a Progressing condition out of the
// aggregate Ready condition. The reason allowlist ensures progress reasons
// (deployment-rolling, primary workload missing, artifacts pending) carry
// through to Progressing while permanent reasons stay on the Ready
// condition only.
func progressingForReadyCondition(ready metav1.Condition) metav1.Condition {
	return ctrlstatus.ProgressingForReady(
		string(conditionTypeProgressing),
		string(conditionReasonReady),
		conditionMessageReady,
		ready,
		string(conditionReasonDeploymentProgressing),
		string(conditionReasonPrimaryWorkloadMissing),
		string(conditionReasonArtifactsPending),
		string(conditionReasonDBSyncAttachmentPending),
	)
}
