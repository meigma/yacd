package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/meigma/yacd/cli/internal/devconfig"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/meigma/yacd/cli/internal/render"
	"github.com/spf13/cobra"
)

func newDeployCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Render and deploy a YACD developer environment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeConfig, err := loadRuntimeConfig(commandContext.viper)
			if err != nil {
				return err
			}

			file := commandContext.viper.GetString("file")
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			timeout := commandContext.viper.GetDuration("timeout")
			dryRun := commandContext.viper.GetBool("dry-run")
			waitReady := commandContext.viper.GetBool("wait")

			environment, err := devconfig.LoadFile(file)
			if err != nil {
				return err
			}

			kubeConfig := kube.Config{
				Kubeconfig: runtimeConfig.Kubeconfig,
				Context:    runtimeConfig.KubeContext,
			}
			var kubeClient kube.Client
			fallbackNamespace := ""
			needsKubeDefaultNamespace := strings.TrimSpace(runtimeConfig.Namespace) == "" &&
				strings.TrimSpace(environment.Metadata.Namespace) == ""
			if !dryRun {
				kubeClient, err = commandContext.kubeClientFactory(kubeConfig)
				if err != nil {
					return err
				}
				if needsKubeDefaultNamespace {
					fallbackNamespace = kubeClient.DefaultNamespace()
				}
			} else if needsKubeDefaultNamespace {
				fallbackNamespace, err = commandContext.kubeNamespaceResolver(kubeConfig)
				if err != nil {
					return err
				}
			}

			network, err := render.CardanoNetwork(environment, render.Namespace(runtimeConfig.Namespace, environment.Metadata.Namespace, fallbackNamespace))
			if err != nil {
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

	cmd.Flags().StringP("file", "f", "", "Developer config file")
	cmd.Flags().Bool("dry-run", false, "Render the manifest without applying it")
	cmd.Flags().Bool("wait", false, "Wait for the CardanoNetwork to become ready")
	cmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait for readiness")

	return cmd
}
