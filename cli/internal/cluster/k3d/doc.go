// Package k3d implements the cluster.Provisioner port by shelling out to a
// pinned, checksum-verified k3d binary resolved through toolbin.
//
// It owns the EnsureCluster state machine (absent -> create, healthy -> no-op,
// unhealthy -> delete and recreate) and its partial-create rollback. Commands
// run through an injected exec.Runner so the state machine is unit-testable
// without Docker; cluster existence is read from "k3d cluster list -o json" and
// API-server health from an injectable probe (defaulted to a client-go /healthz
// call) that runs only when the cluster is running.
package k3d
