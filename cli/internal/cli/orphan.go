package cli

import (
	"context"
	"fmt"

	"github.com/meigma/yacd/cli/internal/cluster"
	"github.com/spf13/cobra"
)

// orphanedStateMessage is printed when a stale managed-cluster record is cleared
// because the cluster it pointed at no longer exists.
const orphanedStateMessage = "Managed devnet cluster is gone; cleared stale state. " +
	"Re-run to use your default kube context, or `yacd devnet` to recreate it."

// withManagedReconcile wraps a verb's RunE so that, when the verb resolved to
// the tracked managed devnet cluster and then failed, it checks whether that
// cluster still exists and clears the stale state record if it is gone. The
// original error is always preserved; the cleanup only changes the next
// invocation, which then resolves to the ambient kube context.
//
// The check runs only on failure of a managed-targeted command, so the common
// path keeps the cheap record-based targeting with no cluster probe.
func (commandContext *commandContext) withManagedReconcile(cmd *cobra.Command) *cobra.Command {
	inner := cmd.RunE
	if inner == nil {
		return cmd
	}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		commandContext.managedEngaged = false
		err := inner(c, args)
		if err != nil && commandContext.managedEngaged {
			commandContext.clearOrphanedManagedState(c.Context())
		}
		return err
	}

	return cmd
}

// clearOrphanedManagedState clears the managed-cluster state record when the
// cluster it points at no longer exists. It is best-effort: any failure to
// resolve, probe, or clear is swallowed so it never masks the caller's original
// error, and it does not clear when the cluster is present (the failure was
// something else).
func (commandContext *commandContext) clearOrphanedManagedState(ctx context.Context) {
	provisioner, err := commandContext.clusterProvisioner()
	if err != nil {
		return
	}
	store, err := commandContext.clusterState()
	if err != nil {
		return
	}
	// Probe health through the recorded kubeconfig (the file the cluster's
	// context lives in), not the ambient default. Existence comes from the k3d
	// runtime regardless, so a missing record just probes the default.
	record, _, _ := store.Load()
	status, err := provisioner.Status(ctx, cluster.ManagedName, record.KubeconfigPath)
	if err != nil || status.Exists {
		return
	}

	commandContext.clearManagedStateRecord()
}

// clearManagedStateRecord clears the tracked managed-cluster record and prints
// the orphaned-state notice to stderr. It is best-effort and silent on failure.
func (commandContext *commandContext) clearManagedStateRecord() {
	store, err := commandContext.clusterState()
	if err != nil {
		return
	}
	if err := store.Clear(); err != nil {
		return
	}
	_, _ = fmt.Fprintln(commandContext.err, orphanedStateMessage) // ui-passthrough-ok
}
