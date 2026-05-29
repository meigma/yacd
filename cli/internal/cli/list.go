package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newListCommand wires the `yacd list` subcommand. It lists CardanoNetworks
// in the active namespace (or across all namespaces with -A) and projects
// each into name/namespace/mode/ready/endpoints, rendered as a table or, with
// --json, as machine-readable JSON.
func newListCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List YACD environments in the cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runtimeConfig, err := loadRuntimeConfig(commandContext.viper)
			if err != nil {
				return err
			}
			allNamespaces := commandContext.viper.GetBool("all-namespaces")
			jsonOutput := commandContext.viper.GetBool("json")

			kubeClient, err := commandContext.kubeClientFactory(kube.Config{
				Kubeconfig: runtimeConfig.Kubeconfig,
				Context:    runtimeConfig.KubeContext,
			})
			if err != nil {
				return err
			}

			namespace := ""
			if !allNamespaces {
				namespace = strings.TrimSpace(runtimeConfig.Namespace)
				if namespace == "" {
					namespace = kubeClient.DefaultNamespace()
				}
			}

			networks, err := kubeClient.ListCardanoNetworks(cmd.Context(), namespace)
			if err != nil {
				return err
			}

			items := make([]listItem, 0, len(networks))
			for i := range networks {
				items = append(items, newListItem(&networks[i]))
			}

			if jsonOutput {
				encoded, err := json.MarshalIndent(items, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal list JSON: %w", err)
				}
				if _, err := fmt.Fprintf(commandContext.out, "%s\n", encoded); err != nil {
					return fmt.Errorf("write list JSON: %w", err)
				}
				return nil
			}

			return printList(commandContext.out, items, namespace, allNamespaces)
		},
	}

	cmd.Flags().BoolP("all-namespaces", "A", false, "List CardanoNetworks across all namespaces")
	cmd.Flags().Bool("json", false, "Print machine-readable JSON")

	return cmd
}

// listItem is the JSON/table projection of a CardanoNetwork the list command
// emits. Field names are stable across releases.
type listItem struct {
	// Name is the CardanoNetwork name.
	Name string `json:"name"`

	// Namespace is the CardanoNetwork namespace.
	Namespace string `json:"namespace"`

	// Mode is the requested network mode (local or public).
	Mode string `json:"mode,omitempty"`

	// Ready reflects a fresh Ready condition observed as True.
	Ready bool `json:"ready"`

	// Endpoints holds the published chain-API endpoint URLs.
	Endpoints listEndpoints `json:"endpoints"`
}

// listEndpoints projects the published service endpoint URLs. Empty strings
// mean the endpoint has not been published yet.
type listEndpoints struct {
	// NodeToNode is the node-to-node TCP endpoint URL.
	NodeToNode string `json:"nodeToNode,omitempty"`

	// Ogmios is the Ogmios WebSocket endpoint URL.
	Ogmios string `json:"ogmios,omitempty"`

	// Kupo is the Kupo HTTP endpoint URL.
	Kupo string `json:"kupo,omitempty"`

	// Faucet is the faucet HTTP endpoint URL.
	Faucet string `json:"faucet,omitempty"`
}

// summary returns a compact comma-separated list of the published endpoint
// names for the table view, or "-" when none are published yet.
func (e listEndpoints) summary() string {
	var present []string
	if e.NodeToNode != "" {
		present = append(present, "node-to-node")
	}
	if e.Ogmios != "" {
		present = append(present, "ogmios")
	}
	if e.Kupo != "" {
		present = append(present, "kupo")
	}
	if e.Faucet != "" {
		present = append(present, "faucet")
	}
	if len(present) == 0 {
		return "-"
	}

	return strings.Join(present, ",")
}

// newListItem projects a CardanoNetwork into a listItem. Readiness comes from
// a fresh Ready condition so a stale status is reported as not ready.
func newListItem(network *yacdv1alpha1.CardanoNetwork) listItem {
	item := listItem{
		Name:      network.Name,
		Namespace: network.Namespace,
		Mode:      string(network.Spec.Mode),
	}
	if ready := kube.FreshCondition(network, kube.ConditionReady); ready != nil {
		item.Ready = ready.Status == metav1.ConditionTrue
	}
	if network.Status.Endpoints != nil {
		item.Endpoints = listEndpoints{
			NodeToNode: endpointURL(network.Status.Endpoints.NodeToNode),
			Ogmios:     endpointURL(network.Status.Endpoints.Ogmios),
			Kupo:       endpointURL(network.Status.Endpoints.Kupo),
			Faucet:     endpointURL(network.Status.Endpoints.Faucet),
		}
	}

	return item
}

// endpointURL returns the endpoint URL or an empty string when the endpoint
// has not been published.
func endpointURL(endpoint *yacdv1alpha1.ServiceEndpointStatus) string {
	if endpoint == nil {
		return ""
	}

	return endpoint.URL
}

// printList renders the projected items as an aligned table. An empty result
// is reported explicitly, with the search scope, so the user can tell "none"
// from a filtering error.
func printList(out io.Writer, items []listItem, namespace string, allNamespaces bool) error {
	if len(items) == 0 {
		message := "No CardanoNetworks found."
		if !allNamespaces {
			message = fmt.Sprintf("No CardanoNetworks found in namespace %q.", namespace)
		}
		if _, err := fmt.Fprintln(out, message); err != nil {
			return fmt.Errorf("write list: %w", err)
		}
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	rows := []string{"NAME\tNAMESPACE\tMODE\tREADY\tENDPOINTS"}
	for _, item := range items {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%t\t%s", item.Name, item.Namespace, item.Mode, item.Ready, item.Endpoints.summary()))
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, row); err != nil {
			return fmt.Errorf("write list: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush list: %w", err)
	}

	return nil
}
