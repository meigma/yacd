package cli

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/meigma/yacd/cli/internal/devconfig"
	"github.com/meigma/yacd/cli/internal/lifecycle"
	"github.com/spf13/cobra"
)

const (
	// devnetNetworkName is the name and namespace of the default network
	// `yacd devnet` applies.
	devnetNetworkName = "devnet"

	// defaultDevnetMagic is the network magic used in the exec hint when the
	// controller has not yet published one.
	defaultDevnetMagic int64 = 42
)

// newDevnetCommand wires the `yacd devnet` subtree. The parent verb brings the
// managed k3d cluster, the operator, and a default funded local network to a
// ready state; `devnet down` tears the cluster down and `devnet status` reports
// the unified state. All sequencing lives in lifecycle.Manager; this layer only
// builds the manager, streams progress to stderr, and prints the result.
func newDevnetCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devnet",
		Short: "Bring up a local Cardano devnet (cluster, operator, and a funded network)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rejectExplicitTarget(commandContext); err != nil {
				return err
			}
			timeout := commandContext.viper.GetDuration("timeout")
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be greater than 0")
			}

			manager, err := commandContext.newLifecycleManager()
			if err != nil {
				return err
			}

			environment, err := devconfig.Load(bytes.NewReader(defaultDevnetEnvYAML))
			if err != nil {
				return fmt.Errorf("load default devnet environment: %w", err)
			}

			bare := commandContext.viper.GetBool("bare")
			result, err := manager.Up(cmd.Context(), lifecycle.UpOptions{
				Bare:        bare,
				Env:         *environment,
				NetworkName: devnetNetworkName,
				Namespace:   devnetNetworkName,
				Timeout:     timeout,
			})
			if err != nil {
				return err
			}

			return printDevnetUp(commandContext.out, result, bare)
		},
	}
	cmd.Flags().Bool("bare", false, "Stop after installing the operator; apply no network")
	cmd.Flags().Duration("timeout", 12*time.Minute, "Maximum time to wait for the cluster, operator, and network")

	cmd.AddCommand(newDevnetDownCommand(commandContext))
	cmd.AddCommand(newDevnetStatusCommand(commandContext))

	return cmd
}

// newDevnetDownCommand wires `yacd devnet down`, which deletes the managed
// cluster and restores the prior kubectl context.
func newDevnetDownCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Delete the managed devnet cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rejectExplicitTarget(commandContext); err != nil {
				return err
			}
			timeout := commandContext.viper.GetDuration("timeout")
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be greater than 0")
			}

			manager, err := commandContext.newLifecycleManager()
			if err != nil {
				return err
			}
			if err := manager.Down(cmd.Context(), lifecycle.DownOptions{
				Timeout: timeout,
			}); err != nil {
				return err
			}

			_, err = fmt.Fprintln(commandContext.out, "devnet cluster removed.")
			return err
		},
	}
	cmd.Flags().Duration("timeout", 5*time.Minute, "Maximum time to wait for the cluster to be deleted")

	return cmd
}

// newDevnetStatusCommand wires `yacd devnet status`, the unified read-only view
// of the managed cluster, operator, and networks.
func newDevnetStatusCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the managed devnet cluster, operator, and network status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rejectExplicitTarget(commandContext); err != nil {
				return err
			}
			manager, err := commandContext.newLifecycleManager()
			if err != nil {
				return err
			}
			report, err := manager.Status(cmd.Context())
			if err != nil {
				return err
			}

			// status is the reconciliation point: a record for a cluster that no
			// longer exists is stale, so clear it and let the user know.
			if !report.Cluster.Exists && report.HasRecord {
				commandContext.clearManagedStateRecord()
			}

			return printDevnetStatus(commandContext.out, report)
		},
	}

	return cmd
}

// newLifecycleManager composes the lifecycle ports from the command-context
// factories. The Reporter streams progress to stderr; the context capture and
// restore seams default to the kube helpers.
func (commandContext *commandContext) newLifecycleManager() (*lifecycle.Manager, error) {
	provisioner, err := commandContext.clusterProvisioner()
	if err != nil {
		return nil, err
	}
	store, err := commandContext.clusterState()
	if err != nil {
		return nil, err
	}

	return &lifecycle.Manager{
		Provisioner:  provisioner,
		State:        store,
		NewInstaller: commandContext.operatorInstaller,
		NewNetworks:  commandContext.kubeClientFactory,
		K3dVersion:   commandContext.k3dVersion,
		Report:       &stepReporter{w: commandContext.err},
	}, nil
}

// stepReporter writes lifecycle progress to a writer (stderr). Top-level steps
// are prefixed with "==> "; sub-steps and completions are indented under them.
type stepReporter struct {
	w io.Writer
}

func (r *stepReporter) Step(format string, args ...any) {
	_, _ = fmt.Fprintf(r.w, "==> "+format+"\n", args...)
}

func (r *stepReporter) Substep(format string, args ...any) {
	_, _ = fmt.Fprintf(r.w, "    "+format+"\n", args...)
}

func (r *stepReporter) Done(format string, args ...any) {
	_, _ = fmt.Fprintf(r.w, "    "+format+"\n", args...)
}

// printDevnetUp renders a successful bring-up to out (stdout): the cluster and
// operator, then — unless bare — the network endpoints, the funded wallet
// address, and a copy-pasteable exec tip-query hint with the network magic
// interpolated.
func printDevnetUp(out io.Writer, result lifecycle.Result, bare bool) error {
	w := &infoWriter{w: out}
	w.println("\ndevnet is ready.")
	w.printf("  Cluster:  %s (context %s)\n", result.Cluster.Name, result.Cluster.Context)
	w.printf("  Operator: %s\n", result.Operator.Version)

	if bare || result.Network == nil {
		w.println("\nNo network applied (--bare). Apply one with: yacd up NAME -f FILE")
		return w.err
	}

	network := result.Network
	if network.Status.Endpoints != nil {
		if endpoint := network.Status.Endpoints.Ogmios; endpoint != nil {
			w.printf("  Ogmios:   %s\n", endpoint.URL)
		}
		if endpoint := network.Status.Endpoints.Kupo; endpoint != nil {
			w.printf("  Kupo:     %s\n", endpoint.URL)
		}
	}
	if result.WalletAddress != "" {
		w.printf("  Wallet:   %s\n", result.WalletAddress)
	}

	magic := defaultDevnetMagic
	if network.Status.Network != nil && network.Status.Network.NetworkMagic != nil {
		magic = *network.Status.Network.NetworkMagic
	}
	w.println("\nTry:")
	w.printf("  yacd exec %s -- cardano-cli query tip --testnet-magic %d\n", devnetNetworkName, magic)
	w.printf("  yacd devnet down\n")

	return w.err
}

// printDevnetStatus renders the unified status report to out (stdout). When no
// managed cluster exists it prints a one-line hint instead of an error.
func printDevnetStatus(out io.Writer, report lifecycle.Report) error {
	w := &infoWriter{w: out}
	if !report.Cluster.Exists {
		w.println("No managed devnet cluster. Run `yacd devnet` to create one.")
		return w.err
	}

	w.printf("Cluster:  %s", report.Cluster.Context)
	if report.Cluster.Running {
		w.printf(" (running)")
	}
	if report.Cluster.Healthy {
		w.printf(" (healthy)")
	}
	w.println("")

	if report.Operator.Installed {
		w.printf("Operator: %s", report.Operator.Version)
		if report.Operator.Ready {
			w.printf(" (ready)")
		}
		w.println("")
	} else {
		w.println("Operator: not installed")
	}

	w.println("\nNetworks:")
	if len(report.Networks) == 0 {
		w.println("  None")
	}
	for _, network := range report.Networks {
		w.printf("  %s/%s\n", network.Namespace, network.Name)
	}

	return w.err
}
