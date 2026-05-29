package cardanodbsync

import (
	ctrlreadiness "github.com/meigma/yacd/internal/ctrlkit/readiness"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// conditionType is the typed name of a CardanoDBSync status condition. The
// underlying string is what appears on the wire; the typed form keeps the
// package-internal vocabulary closed so callers cannot accidentally pass an
// arbitrary string to a condition builder.
type conditionType string

// conditionReason is the typed reason carried by a CardanoDBSync status
// condition. Same intent as [conditionType]: closed package-internal
// vocabulary, untyped on the wire.
type conditionReason string

// Condition types used on CardanoDBSync.Status.Conditions.
const (
	conditionTypeProgressing          conditionType = "Progressing"
	conditionTypeDegraded             conditionType = "Degraded"
	conditionTypeReady                conditionType = "Ready"
	conditionTypeFollowerNodeReady    conditionType = "FollowerNodeReady"
	conditionTypeNodeSocketReady      conditionType = "NodeSocketReady"
	conditionTypeSidecarMaterialReady conditionType = "SidecarMaterialReady"
	conditionTypePostgresReady        conditionType = "PostgresReady"
	conditionTypeDBSyncReady          conditionType = "DBSyncReady"
	conditionTypeSynced               conditionType = "Synced"
)

// Shared condition reasons used across multiple condition types.
const (
	conditionReasonReconcileSucceeded                conditionReason = "ReconcileSucceeded"
	conditionReasonReady                             conditionReason = "Ready"
	conditionReasonUnsupportedSpec                   conditionReason = "UnsupportedSpec"
	conditionReasonUnsupportedStorageChange          conditionReason = "UnsupportedStorageChange"
	conditionReasonStorageExpansionRejected          conditionReason = "StorageExpansionRejected"
	conditionReasonUnsupportedWorkloadChange         conditionReason = "UnsupportedWorkloadChange"
	conditionReasonUnsupportedDatabaseIdentityChange conditionReason = "UnsupportedDatabaseIdentityChange"
	conditionReasonResourceConflict                  conditionReason = "ResourceConflict"
	conditionReasonWorkloadMissing                   conditionReason = "WorkloadMissing"
	conditionReasonDeploymentProgressing             conditionReason = "DeploymentProgressing"
	conditionReasonDedicatedFollowerPlacement        conditionReason = "DedicatedFollowerPlacement"
	conditionReasonPrimarySidecarPlacement           conditionReason = "PrimarySidecarPlacement"
	conditionReasonPlacementTransitionPending        conditionReason = "PlacementTransitionPending"
)

// Dependency-resolution condition reasons (raised before workloads apply).
const (
	conditionReasonUnsupportedDatabaseMode       conditionReason = "UnsupportedDatabaseMode"
	conditionReasonExternalDatabaseSecretMissing conditionReason = "ExternalDatabaseSecretMissing"
	conditionReasonExternalDatabaseSecretInvalid conditionReason = "ExternalDatabaseSecretInvalid"
	conditionReasonManagedDatabaseSecretMissing  conditionReason = "ManagedDatabaseSecretMissing"
	conditionReasonManagedDatabaseSecretInvalid  conditionReason = "ManagedDatabaseSecretInvalid"
	conditionReasonNetworkUnavailable            conditionReason = "NetworkUnavailable"
	conditionReasonNetworkStatusStale            conditionReason = "NetworkStatusStale"
	conditionReasonNetworkArtifactsPending       conditionReason = "NetworkArtifactsPending"
	conditionReasonNetworkArtifactsMismatch      conditionReason = "NetworkArtifactsMismatch"
	conditionReasonNodeToNodeEndpointMissing     conditionReason = "NodeToNodeEndpointMissing"
)

// Per-component readiness and runtime-probe condition reasons.
const (
	conditionReasonFollowerNodeReady     conditionReason = "FollowerNodeReady"
	conditionReasonNodeSocketReady       conditionReason = "NodeSocketReady"
	conditionReasonDBSyncReady           conditionReason = "DBSyncReady"
	conditionReasonPostgresReady         conditionReason = "PostgresReady"
	conditionReasonPostgresUnavailable   conditionReason = "PostgresUnavailable"
	conditionReasonPostgresSchemaPending conditionReason = "PostgresSchemaPending"
	conditionReasonNodeTipUnavailable    conditionReason = "NodeTipUnavailable"
	conditionReasonSyncLagging           conditionReason = "SyncLagging"
	conditionReasonSynced                conditionReason = "Synced"
	conditionReasonRuntimeProbesPending  conditionReason = "RuntimeProbesPending"
	conditionReasonSyncNotProbed         conditionReason = "SyncNotProbed"
)

// Condition messages with stable wording across reconciles. Messages stay
// untyped string — they have no enumerable domain.
const (
	conditionMessageWorkloadsApplied           = "CardanoDBSync workloads are applied"
	conditionMessageManagedPostgresApplied     = "Managed Postgres resources are applied"
	conditionMessageDependenciesConverging     = "CardanoDBSync dependencies are still converging"
	conditionMessageReady                      = "CardanoDBSync is ready"
	conditionMessageSidecarMaterialReady       = "Primary-sidecar material is ready"
	conditionMessageSidecarMaterialNotReady    = "Primary-sidecar material is not attachable"
	conditionMessageSidecarMaterialNotUsed     = "Primary-sidecar material is not used by dedicatedFollower placement"
	conditionMessageFollowerNodeReady          = "Follower node container is ready"
	conditionMessageFollowerNodeNotReady       = "Follower node container is not ready"
	conditionMessageNodeSocketNotUsed          = "Primary node socket is not used by dedicatedFollower placement"
	conditionMessageDBSyncContainerReady       = "db-sync container is ready"
	conditionMessageDBSyncContainerNotReady    = "db-sync container is not ready"
	conditionMessagePostgresReady              = "Postgres is reachable and db-sync progress query succeeded"
	conditionMessagePostgresReachable          = "Postgres is reachable"
	conditionMessageManagedPostgresReady       = "Managed Postgres container is ready"
	conditionMessageManagedPostgresNotReady    = "Managed Postgres container is not ready"
	conditionMessageManagedPostgresMissing     = "Managed Postgres Deployment is missing"
	conditionMessageManagedPostgresStale       = "Managed Postgres Deployment has not observed the latest generation"
	conditionMessageManagedPostgresUnavailable = "Managed Postgres Deployment is not available"
	conditionMessageDBSyncDeploymentMissing    = "CardanoDBSync Deployment is missing"
	conditionMessageDBSyncDeploymentStale      = "CardanoDBSync Deployment has not observed the latest generation"
	conditionMessageDBSyncDeploymentBusy       = "CardanoDBSync Deployment is not available"
	conditionMessageSyncProbedPending          = "db-sync progress will be probed after workloads are ready"
	conditionMessageNodeTipProbedPending       = "node tip will be probed after db-sync workloads are ready"
	conditionMessageSyncNotProbed              = "db-sync chain progress has not been probed yet"
	conditionMessageSynced                     = "db-sync is caught up to the node tip"
	conditionMessageSchemaPending              = "db-sync has not created the block table yet"
	conditionMessageNoBlocksIndexed            = "db-sync has not indexed any blocks yet"
)

// componentConditionFunc is the constructor signature shared by the
// per-component readiness condition builders. deploymentContainerCondition
// uses it to project a single "container not ready" verdict back to whichever
// component condition the caller is computing.
type componentConditionFunc func(metav1.ConditionStatus, conditionReason, string) metav1.Condition

// progressingCondition constructs a Progressing condition with the canonical
// type/reason/message shape.
func progressingCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeProgressing), status, string(reason), message)
}

// degradedCondition constructs a Degraded condition with the canonical
// type/reason/message shape.
func degradedCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeDegraded), status, string(reason), message)
}

// readyCondition constructs a non-Ready Ready condition. Aggregate
// Ready=True is produced by [workloadsReadyCondition]; this constructor
// is only used for the non-Ready paths where the reconciler stamps a
// specific reason/message.
func readyCondition(reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeReady), metav1.ConditionFalse, string(reason), message)
}

// followerNodeReadyCondition constructs a FollowerNodeReady condition.
func followerNodeReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeFollowerNodeReady), status, string(reason), message)
}

// nodeSocketReadyCondition constructs a NodeSocketReady condition.
func nodeSocketReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeNodeSocketReady), status, string(reason), message)
}

// sidecarMaterialReadyCondition constructs a SidecarMaterialReady condition.
func sidecarMaterialReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeSidecarMaterialReady), status, string(reason), message)
}

// postgresReadyCondition constructs a PostgresReady condition.
func postgresReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypePostgresReady), status, string(reason), message)
}

// dbSyncReadyCondition constructs a DBSyncReady condition.
func dbSyncReadyCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeDBSyncReady), status, string(reason), message)
}

// syncedCondition constructs a Synced condition.
func syncedCondition(status metav1.ConditionStatus, reason conditionReason, message string) metav1.Condition {
	return ctrlstatus.Condition(string(conditionTypeSynced), status, string(reason), message)
}

// workloadsReadyCondition aggregates the per-component readiness conditions
// into a single Ready condition. The dependencies are: follower node ready,
// db-sync container ready, Postgres reachable, and synced within the lag
// threshold.
func workloadsReadyCondition(nodeAccessReady metav1.Condition, dbSyncReady metav1.Condition, postgresReady metav1.Condition, synced metav1.Condition) metav1.Condition {
	return ctrlstatus.AggregateReady(
		string(conditionTypeReady),
		string(conditionReasonReady),
		conditionMessageReady,
		nodeAccessReady,
		postgresReady,
		dbSyncReady,
		synced,
	)
}

// progressingForReadyCondition projects a Progressing condition out of the
// aggregate Ready condition. The reason allowlist ensures progress reasons
// (deployment-rolling, workload missing, runtime probes pending, transient
// Postgres / node-tip failures) carry through to Progressing while permanent
// reasons stay on the Ready condition only.
func progressingForReadyCondition(ready metav1.Condition) metav1.Condition {
	return ctrlstatus.ProgressingForReady(
		string(conditionTypeProgressing),
		string(conditionReasonReady),
		conditionMessageReady,
		ready,
		string(conditionReasonDeploymentProgressing),
		string(conditionReasonWorkloadMissing),
		string(conditionReasonRuntimeProbesPending),
		string(conditionReasonPostgresUnavailable),
		string(conditionReasonPostgresSchemaPending),
		string(conditionReasonNodeTipUnavailable),
		string(conditionReasonSyncLagging),
	)
}

// deploymentContainerCondition maps a Deployment-and-container readiness state
// to a component-specific Condition using the caller-supplied constructor.
// Each branch carries a stable message tuned to the named component.
func deploymentContainerCondition(
	readiness ctrlreadiness.DeploymentReadinessState,
	condition componentConditionFunc,
	readyReason conditionReason,
	readyMessage string,
	missingMessage string,
	staleMessage string,
	unavailableMessage string,
	containerNotReadyMessage string,
) metav1.Condition {
	switch readiness {
	case ctrlreadiness.DeploymentReady:
		return condition(metav1.ConditionTrue, readyReason, readyMessage)
	case ctrlreadiness.DeploymentMissing:
		return condition(metav1.ConditionFalse, conditionReasonWorkloadMissing, missingMessage)
	case ctrlreadiness.DeploymentStale:
		return condition(metav1.ConditionFalse, conditionReasonDeploymentProgressing, staleMessage)
	case ctrlreadiness.DeploymentUnavailable:
		return condition(metav1.ConditionFalse, conditionReasonDeploymentProgressing, unavailableMessage)
	default:
		return condition(metav1.ConditionFalse, conditionReasonDeploymentProgressing, containerNotReadyMessage)
	}
}
