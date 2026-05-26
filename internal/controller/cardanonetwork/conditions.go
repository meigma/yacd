package cardanonetwork

import (
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition type strings used on CardanoNetwork.Status.Conditions.
const (
	conditionTypeProgressing    = "Progressing"
	conditionTypeDegraded       = "Degraded"
	conditionTypeReady          = "Ready"
	conditionTypeNodeReady      = "NodeReady"
	conditionTypeOgmiosReady    = "OgmiosReady"
	conditionTypeKupoReady      = "KupoReady"
	conditionTypeFaucetReady    = "FaucetReady"
	conditionTypeArtifactsReady = "ArtifactsReady"
)

// Shared condition reasons used across multiple condition types.
const (
	conditionReasonReconcileSucceeded         = "ReconcileSucceeded"
	conditionReasonUnsupportedSpec            = "UnsupportedSpec"
	conditionReasonUnsupportedLocalnetChange  = "UnsupportedLocalnetChange"
	conditionReasonMissingLocalnetFingerprint = "MissingLocalnetFingerprint"
	conditionReasonUnsupportedStorageChange   = "UnsupportedStorageChange"
	conditionReasonUnsupportedWorkloadChange  = "UnsupportedWorkloadChange"
	conditionReasonResourceConflict           = "ResourceConflict"
	conditionReasonDeploymentProgressing      = "DeploymentProgressing"
	conditionReasonReady                      = "Ready"
	conditionReasonPrimaryWorkloadMissing     = "PrimaryWorkloadMissing"
)

// Per-component condition reasons. The Ready / Disabled pair appears for
// each optional sidecar (ogmios is technically required for chain access,
// but the API admits explicit opt-out).
const (
	conditionReasonNodeReady        = "NodeReady"
	conditionReasonOgmiosReady      = "OgmiosReady"
	conditionReasonOgmiosDisabled   = "OgmiosDisabled"
	conditionReasonKupoReady        = "KupoReady"
	conditionReasonKupoDisabled     = "KupoDisabled"
	conditionReasonFaucetReady      = "FaucetReady"
	conditionReasonFaucetDisabled   = "FaucetDisabled"
	conditionReasonArtifactsReady   = "ArtifactsReady"
	conditionReasonArtifactsPending = "ArtifactsPending"
)

// Condition messages with stable wording across reconciles.
const (
	conditionMessagePrimaryWorkloadApplied     = "Primary node, artifact publisher, and chain API resources are applied"
	conditionMessagePrimaryWorkloadUnsupported = "Primary node workload is not supported for this CardanoNetwork spec"
	conditionMessageReady                      = "CardanoNetwork is usable through its published endpoints"
	conditionMessagePrimaryNodeReady           = "Primary node container is ready"
	conditionMessageOgmiosReady                = "Ogmios sidecar is connected and available through its Service"
	conditionMessageOgmiosDisabled             = "Ogmios chain API is disabled"
	conditionMessageKupoReady                  = "Kupo sidecar is available through its Service"
	conditionMessageKupoDisabled               = "Kupo chain index API is disabled"
	conditionMessageFaucetReady                = "Faucet sidecar is available through its Service"
	conditionMessageFaucetDisabled             = "Faucet API is disabled"
	conditionMessageArtifactsReady             = "Network artifact ConfigMap is published and verified"
)

// primaryDeploymentConditionFunc is the constructor signature shared by the
// per-component readiness condition builders. primaryDeploymentContainerBlockedCondition
// uses it to project a single "container not ready" verdict back to whichever
// component condition the caller is computing.
type primaryDeploymentConditionFunc func(metav1.ConditionStatus, string, string) metav1.Condition

// readyCondition aggregates the per-component readiness conditions into a
// single Ready condition. Optional sidecar conditions only contribute when
// the corresponding sidecar is enabled, so disabling a sidecar does not
// hold Ready back.
func readyCondition(nodeReady metav1.Condition, ogmiosReady metav1.Condition, kupoReady metav1.Condition, faucetReady metav1.Condition, artifactsReady metav1.Condition, kupoEnabled bool, faucetEnabled bool) metav1.Condition {
	dependencies := []metav1.Condition{nodeReady, ogmiosReady}
	if kupoEnabled {
		dependencies = append(dependencies, kupoReady)
	}
	if faucetEnabled {
		dependencies = append(dependencies, faucetReady)
	}
	dependencies = append(dependencies, artifactsReady)

	return ctrlstatus.AggregateReady(conditionTypeReady, conditionReasonReady, conditionMessageReady, dependencies...)
}

// degradedCondition constructs a Degraded condition with the canonical
// type/reason/message shape.
func degradedCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeDegraded, status, reason, message)
}

// nodeReadyCondition constructs a NodeReady condition.
func nodeReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeNodeReady, status, reason, message)
}

// ogmiosReadyCondition constructs an OgmiosReady condition.
func ogmiosReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeOgmiosReady, status, reason, message)
}

// kupoReadyCondition constructs a KupoReady condition.
func kupoReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeKupoReady, status, reason, message)
}

// faucetReadyCondition constructs a FaucetReady condition.
func faucetReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeFaucetReady, status, reason, message)
}

// artifactsReadyCondition constructs an ArtifactsReady condition.
func artifactsReadyCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeArtifactsReady, status, reason, message)
}

// progressingCondition constructs a Progressing condition with the canonical
// type/reason/message shape.
func progressingCondition(status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	return ctrlstatus.Condition(conditionTypeProgressing, status, reason, message)
}

// progressingForReadyCondition projects a Progressing condition out of the
// aggregate Ready condition. The reason allowlist ensures progress reasons
// (deployment-rolling, primary workload missing, artifacts pending) carry
// through to Progressing while permanent reasons stay on the Ready
// condition only.
func progressingForReadyCondition(ready metav1.Condition) metav1.Condition {
	return ctrlstatus.ProgressingForReady(
		conditionTypeProgressing,
		conditionReasonReady,
		conditionMessageReady,
		ready,
		conditionReasonDeploymentProgressing,
		conditionReasonPrimaryWorkloadMissing,
		conditionReasonArtifactsPending,
	)
}
