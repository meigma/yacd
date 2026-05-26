package dbsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// computeFingerprint hashes the full normalized Spec. Any change to a Spec
// JSON tag re-rolls the fingerprint, so tag stability matters across
// releases.
func computeFingerprint(spec Spec) (Fingerprint, error) {
	input, err := json.Marshal(spec)
	if err != nil {
		return Fingerprint{}, fmt.Errorf("marshal fingerprint input: %w", err)
	}
	return hashFingerprint(input), nil
}

// databaseIdentity is the curated subset of Spec inputs that require the
// underlying Postgres data directory to be recreated when they change. The
// hand-rolled struct makes the contract explicit and stable independent of
// future Spec additions.
//
// The wire shape of this struct (including nested Insert) is frozen forever:
// the controller stores the resulting hash on the owned PVC and rejects any
// drift as UnsupportedDatabaseIdentityChange. The legacy *Identity nested
// types below preserve that wire shape so the public domain types remain free
// to evolve their JSON tags.
type databaseIdentity struct {
	NetworkName          string                  `json:"networkName"`
	RequiresNetworkMagic bool                    `json:"requiresNetworkMagic"`
	NetworkArtifactHash  string                  `json:"networkArtifactHash,omitempty"`
	Image                string                  `json:"image"`
	Database             databaseIdentitySpec    `json:"database"`
	Storage              databaseIdentityStorage `json:"storage"`
	Insert               insertIdentity          `json:"insert"`
}

// databaseIdentitySpec is the database-connection identity subset.
type databaseIdentitySpec struct {
	Host string `json:"host"`
	Port int32  `json:"port"`
	Name string `json:"name"`
	User string `json:"user"`
}

// databaseIdentityStorage is the storage-identity subset.
type databaseIdentityStorage struct {
	LedgerBackend string `json:"ledgerBackend"`
}

// insertIdentity mirrors the original on-wire shape of InsertOptions for
// database identity hashing. Its JSON tags use Go field names and omit
// omitempty so the rendered bytes are stable across InsertOptions tag
// changes. Do not modify these tags.
type insertIdentity struct {
	TxCBOR                string                   `json:"TxCBOR"`
	TxOut                 txOutIdentity            `json:"TxOut"`
	Ledger                string                   `json:"Ledger"`
	Shelley               featureSelectionIdentity `json:"Shelley"`
	MultiAsset            featureSelectionIdentity `json:"MultiAsset"`
	Metadata              featureSelectionIdentity `json:"Metadata"`
	Plutus                featureSelectionIdentity `json:"Plutus"`
	Governance            string                   `json:"Governance"`
	OffchainPoolData      string                   `json:"OffchainPoolData"`
	OffchainVoteData      string                   `json:"OffchainVoteData"`
	PoolStats             string                   `json:"PoolStats"`
	JSONType              string                   `json:"JSONType"`
	RemoveJSONBFromSchema string                   `json:"RemoveJSONBFromSchema"`
}

// txOutIdentity mirrors the original on-wire shape of TxOutOption for
// database identity hashing.
type txOutIdentity struct {
	Mode            string `json:"Mode"`
	ForceTxIn       bool   `json:"ForceTxIn"`
	UseAddressTable bool   `json:"UseAddressTable"`
}

// featureSelectionIdentity mirrors the original on-wire shape of
// FeatureSelection for database identity hashing. Slice fields intentionally
// omit omitempty so nil and empty slices serialize as null, matching the
// pre-tag encoding.
type featureSelectionIdentity struct {
	Enabled        bool     `json:"Enabled"`
	StakeAddresses []string `json:"StakeAddresses"`
	Policies       []string `json:"Policies"`
	Keys           []int64  `json:"Keys"`
	ScriptHashes   []string `json:"ScriptHashes"`
}

// computeDatabaseIdentityFingerprint hashes the recreate-trigger subset of
// the normalized Spec.
func computeDatabaseIdentityFingerprint(spec Spec) (Fingerprint, error) {
	input, err := json.Marshal(databaseIdentity{
		NetworkName:          spec.NetworkName,
		RequiresNetworkMagic: spec.RequiresNetworkMagic,
		NetworkArtifactHash:  spec.NetworkArtifactHash,
		Image:                spec.Image,
		Database: databaseIdentitySpec{
			Host: spec.Database.Host,
			Port: spec.Database.Port,
			Name: spec.Database.Name,
			User: spec.Database.User,
		},
		Storage: databaseIdentityStorage{
			LedgerBackend: spec.Storage.LedgerBackend,
		},
		Insert: insertIdentityFor(spec.Insert),
	})
	if err != nil {
		return Fingerprint{}, fmt.Errorf("marshal database identity input: %w", err)
	}
	return hashFingerprint(input), nil
}

// insertIdentityFor projects InsertOptions into the frozen identity wire
// shape used by the database fingerprint.
func insertIdentityFor(o InsertOptions) insertIdentity {
	return insertIdentity{
		TxCBOR: o.TxCBOR,
		TxOut: txOutIdentity{
			Mode:            o.TxOut.Mode,
			ForceTxIn:       o.TxOut.ForceTxIn,
			UseAddressTable: o.TxOut.UseAddressTable,
		},
		Ledger:                o.Ledger,
		Shelley:               featureSelectionIdentityFor(o.Shelley),
		MultiAsset:            featureSelectionIdentityFor(o.MultiAsset),
		Metadata:              featureSelectionIdentityFor(o.Metadata),
		Plutus:                featureSelectionIdentityFor(o.Plutus),
		Governance:            o.Governance,
		OffchainPoolData:      o.OffchainPoolData,
		OffchainVoteData:      o.OffchainVoteData,
		PoolStats:             o.PoolStats,
		JSONType:              o.JSONType,
		RemoveJSONBFromSchema: o.RemoveJSONBFromSchema,
	}
}

// featureSelectionIdentityFor projects FeatureSelection into the frozen
// identity wire shape used by the database fingerprint. The two structs
// share a shape so a typed conversion is enough.
func featureSelectionIdentityFor(f FeatureSelection) featureSelectionIdentity {
	return featureSelectionIdentity(f)
}

// hashFingerprint returns a sha256 Fingerprint over input.
func hashFingerprint(input []byte) Fingerprint {
	sum := sha256.Sum256(input)
	return Fingerprint{
		Algorithm: "sha256",
		Value:     hex.EncodeToString(sum[:]),
	}
}
