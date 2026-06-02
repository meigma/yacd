// Package file implements the clusterstate.Store port on the local filesystem.
//
// It stores the managed-cluster record as JSON at <dir>/cluster.json (written
// atomically via a temp file and rename) and holds the managed-cluster lock as
// an OS file lock at <dir>/cluster.lock. The directory is 0700 and the record
// 0600, matching the CLI's other runtime-state files.
package file
