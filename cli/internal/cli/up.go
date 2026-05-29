package cli

import (
	"fmt"
	"io"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/devconfig"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/render"
	"github.com/spf13/cobra"
)

// newUpCommand wires the `yacd up NAME` subcommand. The command loads the
// developer environment file, renders it into a CardanoNetwork under the
// CLI-supplied identity (NAME plus the namespace, which defaults to NAME),
// and either prints the manifest (--dry-run) or auto-creates the namespace
// and server-side-applies the network through the kube.Client port. Unless
// --wait=false, it then polls until the network is Ready or the timeout
// elapses.
func newUpCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up NAME",
		Short: "Create or update a YACD environment and wait for readiness",
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

			file := commandContext.viper.GetString("file")
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			timeout := commandContext.viper.GetDuration("timeout")
			dryRun := commandContext.viper.GetBool("dry-run")
			allowMainnet := commandContext.viper.GetBool("allow-mainnet")
			waitReady := commandContext.viper.GetBool("wait")
			if waitReady && timeout <= 0 {
				return fmt.Errorf("--timeout must be greater than 0 when --wait is set")
			}

			environment, err := devconfig.LoadFile(file)
			if err != nil {
				return err
			}
			if err := rejectMainnetApplyWithoutAllow(environment.Spec.Network, allowMainnet, dryRun); err != nil {
				return err
			}

			network, err := render.CardanoNetwork(environment, name, namespace)
			if err != nil {
				return err
			}
			if err := warnMainnetDryRun(commandContext.err, network, allowMainnet, dryRun); err != nil {
				return err
			}

			if dryRun {
				manifest, err := render.Manifest(network)
				if err != nil {
					return err
				}
				if _, err := commandContext.out.Write(manifest); err != nil {
					return fmt.Errorf("write manifest: %w", err)
				}
				if len(manifest) == 0 || manifest[len(manifest)-1] != '\n' {
					if _, err := fmt.Fprintln(commandContext.out); err != nil {
						return fmt.Errorf("write manifest newline: %w", err)
					}
				}
				_, err = fmt.Fprintf(commandContext.err, "Dry run: rendered CardanoNetwork %s/%s; no resources applied.\n", network.Namespace, network.Name)
				return err
			}

			kubeClient, err := commandContext.kubeClientFactory(kube.Config{
				Kubeconfig: runtimeConfig.Kubeconfig,
				Context:    runtimeConfig.KubeContext,
			})
			if err != nil {
				return err
			}

			if err := kubeClient.EnsureNamespace(cmd.Context(), namespace); err != nil {
				return err
			}
			if err := kubeClient.ApplyCardanoNetwork(cmd.Context(), network); err != nil {
				return err
			}
			commandContext.logger.Debug("Applied CardanoNetwork", "namespace", network.Namespace, "name", network.Name)
			if _, err := fmt.Fprintf(commandContext.err, "Applied CardanoNetwork %s/%s.\n", network.Namespace, network.Name); err != nil {
				return fmt.Errorf("write apply status: %w", err)
			}

			if waitReady {
				if _, err := fmt.Fprintf(commandContext.err, "Waiting for CardanoNetwork %s/%s to become ready...\n", network.Namespace, network.Name); err != nil {
					return fmt.Errorf("write wait status: %w", err)
				}
				if _, err := kube.WaitReady(cmd.Context(), kubeClient, network.Namespace, network.Name, timeout); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(commandContext.err, "CardanoNetwork %s/%s is ready.\n", network.Namespace, network.Name); err != nil {
					return fmt.Errorf("write ready status: %w", err)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringP("file", "f", "", "Developer environment file")
	cmd.Flags().Bool("dry-run", false, "Render the manifest without applying it")
	cmd.Flags().Bool("allow-mainnet", false, "Allow applying a mainnet CardanoNetwork")
	cmd.Flags().Bool("wait", true, "Wait for the CardanoNetwork to become ready")
	cmd.Flags().Duration("timeout", 12*time.Minute, "Maximum time to wait for readiness")

	return cmd
}

func isMainnetNetwork(network *yacdv1alpha1.CardanoNetwork) bool {
	return network != nil &&
		network.Spec.Mode == yacdv1alpha1.CardanoNetworkModePublic &&
		network.Spec.Public != nil &&
		network.Spec.Public.Profile == yacdv1alpha1.PublicNetworkProfileMainnet
}

func isMainnetSpec(spec yacdv1alpha1.CardanoNetworkSpec) bool {
	return spec.Mode == yacdv1alpha1.CardanoNetworkModePublic &&
		spec.Public != nil &&
		spec.Public.Profile == yacdv1alpha1.PublicNetworkProfileMainnet
}

func rejectMainnetApplyWithoutAllow(spec yacdv1alpha1.CardanoNetworkSpec, allowMainnet bool, dryRun bool) error {
	if !isMainnetSpec(spec) || allowMainnet || dryRun {
		return nil
	}
	return fmt.Errorf("mainnet deployments require --allow-mainnet because they create large persistent volumes and bootstrap from Mithril")
}

func warnMainnetDryRun(w io.Writer, network *yacdv1alpha1.CardanoNetwork, allowMainnet bool, dryRun bool) error {
	if !dryRun || allowMainnet || !isMainnetNetwork(network) {
		return nil
	}
	if _, err := fmt.Fprintf(w, "Warning: rendering mainnet CardanoNetwork %s/%s without --allow-mainnet; dry run only, no resources applied.\n", network.Namespace, network.Name); err != nil {
		return fmt.Errorf("write mainnet dry-run warning: %w", err)
	}
	return nil
}
