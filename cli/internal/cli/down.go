package cli

import (
	"fmt"
	"time"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
)

// newDownCommand wires the `yacd down NAME` subcommand. It deletes the named
// CardanoNetwork and, unless --wait=false, blocks until the object and its
// garbage-collected children are gone. Deletion is idempotent: a network that
// is already absent is reported as success.
func newDownCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down NAME",
		Short: "Delete a YACD environment and wait for clean removal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeConfig, err := loadRuntimeConfig(commandContext.viper)
			if err != nil {
				return err
			}
			name, namespace, err := resolveIdentity(args[0], runtimeConfig)
			if err != nil {
				return err
			}
			timeout := commandContext.viper.GetDuration("timeout")
			waitGone := commandContext.viper.GetBool("wait")
			if waitGone && timeout <= 0 {
				return fmt.Errorf("--timeout must be greater than 0 when --wait is set")
			}

			kubeClient, err := commandContext.kubeClientFactory(kube.Config{
				Kubeconfig: runtimeConfig.Kubeconfig,
				Context:    runtimeConfig.KubeContext,
			})
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(commandContext.err, "Deleting CardanoNetwork %s/%s...\n", namespace, name); err != nil {
				return fmt.Errorf("write delete status: %w", err)
			}
			if err := kubeClient.DeleteCardanoNetwork(cmd.Context(), namespace, name); err != nil {
				return err
			}

			if waitGone {
				if err := kube.WaitGone(cmd.Context(), kubeClient, namespace, name, timeout); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(commandContext.err, "CardanoNetwork %s/%s is gone.\n", namespace, name); err != nil {
					return fmt.Errorf("write gone status: %w", err)
				}
			}

			return nil
		},
	}

	cmd.Flags().Bool("wait", true, "Wait for the CardanoNetwork and its resources to be removed")
	cmd.Flags().Duration("timeout", 5*time.Minute, "Maximum time to wait for removal")

	return cmd
}
