package cardanodbsync

import (
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/dbsync"
)

// dbSyncDatabaseFromExternal renders the dbsync.Database planner input from
// an external Postgres spec. The PasswordSecretKey defaults to "password"
// when the spec does not set one.
func dbSyncDatabaseFromExternal(external *yacdv1alpha1.CardanoDBSyncExternalDatabaseSpec) dbsync.Database {
	if external == nil {
		return dbsync.Database{}
	}

	return dbsync.Database{
		Host:               external.Host,
		Port:               external.Port,
		Name:               external.Database,
		User:               external.User,
		PasswordSecretName: external.PasswordSecretRef.Name,
		PasswordSecretKey:  externalDatabasePasswordKey(external),
		SSLMode:            string(external.SSLMode),
	}
}

// dbSyncDatabaseFromManaged renders the dbsync.Database planner input for
// the managed Postgres workload, pointing at the in-cluster Postgres
// Service and the controller-owned auth Secret.
func dbSyncDatabaseFromManaged(dbSync *yacdv1alpha1.CardanoDBSync, authSecretName string) dbsync.Database {
	managed := dbSync.Spec.Database.Managed

	return dbsync.Database{
		Host:               fmt.Sprintf("%s.%s.svc.cluster.local", managedPostgresServiceName(dbSync), dbSync.Namespace),
		Port:               managedPostgresPort,
		Name:               managedPostgresDatabaseName(managed),
		User:               managedPostgresUser(managed),
		PasswordSecretName: authSecretName,
		PasswordSecretKey:  managedPostgresPasswordKey,
		SSLMode:            defaultManagedPostgresSSLMode,
	}
}

// externalDatabasePasswordKey returns the data key on the external Postgres
// password Secret, falling back to the package default when the spec does
// not set one.
func externalDatabasePasswordKey(database *yacdv1alpha1.CardanoDBSyncExternalDatabaseSpec) string {
	if database.PasswordSecretRef.Key != "" {
		return database.PasswordSecretRef.Key
	}

	return defaultExternalDatabasePasswordKey
}

// runtimeSettings derives the dbsync.Runtime planner input from the
// CardanoDBSync runtime config block. MetricsPort defaults to 8080; the
// Cache and EpochTable booleans are inverted because the planner stores
// the "disable" intent and the spec stores the "enable" intent.
func runtimeSettings(dbSync *yacdv1alpha1.CardanoDBSync) dbsync.Runtime {
	settings := dbsync.Runtime{MetricsPort: 8080}
	if dbSync.Spec.Config.Runtime == nil {
		return settings
	}

	settings.DisableCache = !dbSync.Spec.Config.Runtime.Cache
	settings.DisableEpochTable = !dbSync.Spec.Config.Runtime.EpochTable
	settings.ForceIndexes = dbSync.Spec.Config.Runtime.ForceIndexes
	if dbSync.Spec.Config.Runtime.MetricsPort != 0 {
		settings.MetricsPort = dbSync.Spec.Config.Runtime.MetricsPort
	}

	return settings
}

// storageSettings derives the dbsync.Storage planner input from the
// CardanoDBSync storage config block. Defaults come from defaults.go.
func storageSettings(dbSync *yacdv1alpha1.CardanoDBSync) dbsync.Storage {
	settings := dbsync.Storage{
		LedgerBackend: defaultLedgerBackend,
		NearTipEpoch:  defaultNearTipEpoch,
	}
	if dbSync.Spec.Config.LedgerBackend != "" {
		settings.LedgerBackend = string(dbSync.Spec.Config.LedgerBackend)
	}
	if dbSync.Spec.Config.Snapshot != nil {
		settings.NearTipEpoch = dbSync.Spec.Config.Snapshot.NearTipEpoch
	}
	settings.StateStorageSize = storageSizeFrom(dbSync.Spec.StateStorage, "")

	return settings
}

// insertOptions resolves the dbsync.InsertOptions planner input from the
// CardanoDBSync insert config block. Preset selection picks a baseline
// (insertOptionsForPreset); each field override is applied on top.
func insertOptions(dbSync *yacdv1alpha1.CardanoDBSync) dbsync.InsertOptions {
	insert := dbSync.Spec.Config.Insert
	if insert == nil {
		return dbsync.InsertOptions{}
	}

	options := insertOptionsForPreset(insert.Preset)
	if insert.TxCBOR != nil {
		options.TxCBOR = enableDisable(*insert.TxCBOR)
	}
	if insert.TxOut != nil {
		if insert.TxOut.Mode != nil {
			options.TxOut.Mode = string(*insert.TxOut.Mode)
		}
		if insert.TxOut.ForceTxIn != nil {
			options.TxOut.ForceTxIn = *insert.TxOut.ForceTxIn
		}
		if insert.TxOut.UseAddressTable != nil {
			options.TxOut.UseAddressTable = *insert.TxOut.UseAddressTable
		}
	}
	if insert.Ledger != nil {
		options.Ledger = string(*insert.Ledger)
	}
	if insert.Shelley != nil {
		options.Shelley = featureSelection(options.Shelley, insert.Shelley.Enabled, insert.Shelley.StakeAddresses, nil, nil, nil)
	}
	if insert.MultiAsset != nil {
		options.MultiAsset = featureSelection(options.MultiAsset, insert.MultiAsset.Enabled, nil, insert.MultiAsset.Policies, nil, nil)
	}
	if insert.Metadata != nil {
		options.Metadata = featureSelection(options.Metadata, insert.Metadata.Enabled, nil, nil, insert.Metadata.Keys, nil)
	}
	if insert.Plutus != nil {
		options.Plutus = featureSelection(options.Plutus, insert.Plutus.Enabled, nil, nil, nil, insert.Plutus.ScriptHashes)
	}
	if insert.Governance != nil {
		options.Governance = enableDisable(*insert.Governance)
	}
	if insert.OffchainPoolData != nil {
		options.OffchainPoolData = enableDisable(*insert.OffchainPoolData)
	}
	if insert.OffchainVoteData != nil {
		options.OffchainVoteData = enableDisable(*insert.OffchainVoteData)
	}
	if insert.PoolStats != nil {
		options.PoolStats = enableDisable(*insert.PoolStats)
	}
	if insert.JSONType != nil {
		options.JSONType = string(*insert.JSONType)
	}
	if insert.RemoveJSONBFromSchema != nil {
		options.RemoveJSONBFromSchema = enableDisable(*insert.RemoveJSONBFromSchema)
	}

	return options
}

// insertOptionsForPreset returns the dbsync.InsertOptions baseline for the
// named preset. The default preset enables the full ledger + chain index;
// the OnlyUTxO, OnlyGovernance, and DisableAll presets narrow the feature
// surface for lighter sync workloads.
func insertOptionsForPreset(preset yacdv1alpha1.CardanoDBSyncInsertPreset) dbsync.InsertOptions {
	switch preset {
	case yacdv1alpha1.CardanoDBSyncInsertPresetOnlyUTxO:
		return dbsync.InsertOptions{
			TxCBOR:           "disable",
			TxOut:            dbsync.TxOutOption{Mode: "bootstrap"},
			Ledger:           "ignore",
			Shelley:          dbsync.FeatureSelection{Enabled: false},
			MultiAsset:       dbsync.FeatureSelection{Enabled: true},
			Metadata:         dbsync.FeatureSelection{Enabled: false},
			Plutus:           dbsync.FeatureSelection{Enabled: false},
			Governance:       "disable",
			OffchainPoolData: "disable",
			OffchainVoteData: "disable",
			PoolStats:        "disable",
			JSONType:         "text",
		}
	case yacdv1alpha1.CardanoDBSyncInsertPresetOnlyGovernance:
		return dbsync.InsertOptions{
			TxCBOR:           "disable",
			TxOut:            dbsync.TxOutOption{Mode: "disable"},
			Ledger:           "enable",
			Shelley:          dbsync.FeatureSelection{Enabled: false},
			MultiAsset:       dbsync.FeatureSelection{Enabled: false},
			Metadata:         dbsync.FeatureSelection{Enabled: false},
			Plutus:           dbsync.FeatureSelection{Enabled: false},
			Governance:       "enable",
			OffchainPoolData: "disable",
			OffchainVoteData: "disable",
			PoolStats:        "enable",
			JSONType:         "text",
		}
	case yacdv1alpha1.CardanoDBSyncInsertPresetDisableAll:
		return dbsync.InsertOptions{
			TxCBOR:           "disable",
			TxOut:            dbsync.TxOutOption{Mode: "disable"},
			Ledger:           "disable",
			Shelley:          dbsync.FeatureSelection{Enabled: false},
			MultiAsset:       dbsync.FeatureSelection{Enabled: false},
			Metadata:         dbsync.FeatureSelection{Enabled: false},
			Plutus:           dbsync.FeatureSelection{Enabled: false},
			Governance:       "disable",
			OffchainPoolData: "disable",
			OffchainVoteData: "disable",
			PoolStats:        "disable",
			JSONType:         "text",
		}
	default:
		return dbsync.InsertOptions{
			TxCBOR:           "disable",
			TxOut:            dbsync.TxOutOption{Mode: "enable"},
			Ledger:           "enable",
			Shelley:          dbsync.FeatureSelection{Enabled: true},
			MultiAsset:       dbsync.FeatureSelection{Enabled: true},
			Metadata:         dbsync.FeatureSelection{Enabled: true},
			Plutus:           dbsync.FeatureSelection{Enabled: true},
			Governance:       "enable",
			OffchainPoolData: "disable",
			OffchainVoteData: "disable",
			PoolStats:        "enable",
			JSONType:         "text",
		}
}
}

// featureSelection layers per-field overrides on top of a baseline
// dbsync.FeatureSelection. enabled, when non-nil, replaces the baseline's
// Enabled flag; the four lists are cloned so the planner cannot mutate the
// caller's slices.
func featureSelection(base dbsync.FeatureSelection, enabled *bool, stakeAddresses []string, policies []string, keys []int64, scriptHashes []string) dbsync.FeatureSelection {
	selection := dbsync.FeatureSelection{
		Enabled:        base.Enabled,
		StakeAddresses: slicesClone(stakeAddresses),
		Policies:       slicesClone(policies),
		Keys:           slicesClone(keys),
		ScriptHashes:   slicesClone(scriptHashes),
	}
	if enabled != nil {
		selection.Enabled = *enabled
	}

	return selection
}

// slicesClone returns a defensive copy of values, returning nil for a nil
// input so the planner's fingerprint algorithm distinguishes "unset" from
// "empty".
func slicesClone[T any](values []T) []T {
	if values == nil {
		return nil
	}
	copied := make([]T, len(values))
	copy(copied, values)

	return copied
}

// enableDisable converts a boolean override into the planner's
// "enable"/"disable" string vocabulary.
func enableDisable(enabled bool) string {
	if enabled {
		return "enable"
	}

	return "disable"
}

// storageClassNameFrom extracts the StorageClassName pointer from a storage
// spec, returning nil when the caller did not set one.
func storageClassNameFrom(storage *yacdv1alpha1.CardanoDBSyncStorageSpec) *string {
	if storage == nil {
		return nil
	}

	return storage.StorageClassName
}

// storageSizeFrom extracts the requested storage size from a storage spec
// as a Kubernetes resource.Quantity string. fallback is used when the spec
// does not set a size; the empty fallback leaves the requested size empty
// so the caller's parse call errors out (used by the dbsync state PVC
// which has no built-in fallback).
func storageSizeFrom(storage *yacdv1alpha1.CardanoDBSyncStorageSpec, fallback string) string {
	if storage == nil || storage.Size == nil {
		return fallback
	}

	return storage.Size.String()
}

// followerNodeImage resolves the follower-node container image. A spec
// override wins; otherwise the image is composed from the network's
// cardano-node version and the package's YACD packaging revision.
func (b dbSyncWorkloadBuilder) followerNodeImage(dbSync *yacdv1alpha1.CardanoDBSync, network *yacdv1alpha1.CardanoNetwork) string {
	if dbSync.Spec.FollowerNode != nil && dbSync.Spec.FollowerNode.Image != nil {
		return strings.TrimSpace(*dbSync.Spec.FollowerNode.Image)
	}

	return fmt.Sprintf("%s:%s-%s", defaultFollowerNodeImageRepository, strings.TrimSpace(network.Spec.Node.Version), defaultFollowerNodeImageRevision)
}

// managedPostgresImage resolves the managed Postgres container image,
// falling back to the package default when the spec does not set one.
func managedPostgresImage(managed *yacdv1alpha1.CardanoDBSyncManagedDatabaseSpec) string {
	if managed == nil || strings.TrimSpace(managed.Image) == "" {
		return defaultManagedPostgresImage
	}

	return strings.TrimSpace(managed.Image)
}

// managedPostgresDatabaseName resolves the managed Postgres database name,
// falling back to the package default when the spec does not set one.
func managedPostgresDatabaseName(managed *yacdv1alpha1.CardanoDBSyncManagedDatabaseSpec) string {
	if managed == nil || strings.TrimSpace(managed.Database) == "" {
		return defaultManagedPostgresDatabase
	}

	return strings.TrimSpace(managed.Database)
}

// managedPostgresUser resolves the managed Postgres superuser name,
// falling back to the package default when the spec does not set one.
func managedPostgresUser(managed *yacdv1alpha1.CardanoDBSyncManagedDatabaseSpec) string {
	if managed == nil || strings.TrimSpace(managed.User) == "" {
		return defaultManagedPostgresUser
	}

	return strings.TrimSpace(managed.User)
}
