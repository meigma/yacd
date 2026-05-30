// Package artifactset assembles a CardanoNetwork artifact payload from a set
// of files on disk and the owning network identity.
//
// Build returns a [Set] holding the ConfigMap data map, the owned-key set used
// for pruning, and the metadata annotations a YACD controller verifies. The
// package is the single producer-side expression of the artifact contract: it
// imports the artifact data keys and schema version from
// internal/cardano/networkartifacts, the deterministic data-hash from
// internal/ctrlkit/artifacts, and the annotation keys from
// internal/controller/annotations, rather than re-declaring any of them. That
// shared import is what guarantees the hash and keys this tool writes match the
// ones the controller re-verifies.
//
// The package exports the artifact reading and assembly surface (ReadArtifacts,
// ReadManifest, Sources, Input, Set, Manifest, NetworkIdentity, Annotations,
// Build) plus IsSecretComponent, which the serve verb reuses to refuse
// requests for key material; everything else is unexported.
package artifactset
