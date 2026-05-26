// Package devconfig defines the developer-environment configuration envelope
// (yacd.yaml) and validates the document before downstream packages translate
// it into Kubernetes objects.
//
// The package performs envelope and explicit-field validation only. It does
// not synthesise CardanoNetwork specs, contact Kubernetes, or apply
// defaults; see the render package for manifest synthesis.
package devconfig
