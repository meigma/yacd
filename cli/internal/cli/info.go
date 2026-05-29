package cli

import (
	"encoding/json"
	"fmt"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
)

// newInfoCommand wires the `yacd info NAME` subcommand. The command
// fetches the named CardanoNetwork, projects it into an infoOutput, and
// renders either JSON (when --json is set) or the human-readable text
// shape implemented in info_print.go.
func newInfoCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info NAME",
		Short: "Print CardanoNetwork status and connection information",
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

			kubeClient, err := commandContext.kubeClientFactory(kube.Config{
				Kubeconfig: runtimeConfig.Kubeconfig,
				Context:    runtimeConfig.KubeContext,
			})
			if err != nil {
				return err
			}

			network, err := kubeClient.GetCardanoNetwork(cmd.Context(), namespace, name)
			if err != nil {
				return err
			}

			info := newInfo(network)
			if commandContext.viper.GetBool("json") {
				encoded, err := json.MarshalIndent(info, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal info JSON: %w", err)
				}
				if _, err := fmt.Fprintf(commandContext.out, "%s\n", encoded); err != nil {
					return fmt.Errorf("write info JSON: %w", err)
				}
				return nil
			}

			return printInfo(commandContext.out, info)
		},
	}
	cmd.Flags().Bool("json", false, "Print machine-readable JSON")

	return cmd
}

// infoOutput is the JSON projection of CardanoNetwork status the info
// command emits. Field names are stable across releases; the Conditions
// slice is always present (possibly empty) so JSON consumers can iterate
// without a nil-check.
type infoOutput struct {
	Name               string            `json:"name"`
	Namespace          string            `json:"namespace"`
	ObservedGeneration int64             `json:"observedGeneration,omitempty"`
	Network            networkOutput     `json:"network"`
	Endpoints          endpointsOutput   `json:"endpoints"`
	Faucet             *faucetOutput     `json:"faucet,omitempty"`
	Conditions         []conditionOutput `json:"conditions"`
}

// networkOutput projects the CardanoNetwork.Status.Network sub-status.
type networkOutput struct {
	Mode                string `json:"mode,omitempty"`
	LocalnetFingerprint string `json:"localnetFingerprint,omitempty"`
	NetworkMagic        *int64 `json:"networkMagic,omitempty"`
	Profile             string `json:"profile,omitempty"`
	Era                 string `json:"era,omitempty"`
}

// endpointsOutput projects the published service endpoints. Each pointer
// is nil when the corresponding service has not yet been published.
type endpointsOutput struct {
	NodeToNode *endpointOutput `json:"nodeToNode,omitempty"`
	Ogmios     *endpointOutput `json:"ogmios,omitempty"`
	Kupo       *endpointOutput `json:"kupo,omitempty"`
	Faucet     *endpointOutput `json:"faucet,omitempty"`
}

// endpointOutput projects a single ServiceEndpointStatus.
type endpointOutput struct {
	ServiceName string `json:"serviceName,omitempty"`
	Port        int32  `json:"port,omitempty"`
	URL         string `json:"url,omitempty"`
}

// faucetOutput projects the optional faucet status sub-resource.
type faucetOutput struct {
	AuthSecretName string `json:"authSecretName,omitempty"`
}

// conditionOutput projects a single metav1.Condition with the timestamp
// formatted as RFC3339 for JSON stability.
type conditionOutput struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

// newInfo projects a CardanoNetwork into the JSON-shaped infoOutput. The
// projection drops nil sub-statuses rather than emitting empty objects, so
// JSON consumers can distinguish "not yet published" from "explicitly
// empty".
func newInfo(network *yacdv1alpha1.CardanoNetwork) infoOutput {
	info := infoOutput{
		Name:               network.Name,
		Namespace:          network.Namespace,
		ObservedGeneration: network.Status.ObservedGeneration,
	}

	if network.Status.Network != nil {
		info.Network.Mode = string(network.Status.Network.Mode)
		info.Network.LocalnetFingerprint = network.Status.Network.LocalnetFingerprint
		info.Network.NetworkMagic = network.Status.Network.NetworkMagic
		if network.Status.Network.Profile != nil {
			info.Network.Profile = string(*network.Status.Network.Profile)
		}
		if network.Status.Network.Era != nil {
			info.Network.Era = string(*network.Status.Network.Era)
		}
	}
	if network.Status.Endpoints != nil {
		info.Endpoints.NodeToNode = endpointInfo(network.Status.Endpoints.NodeToNode)
		info.Endpoints.Ogmios = endpointInfo(network.Status.Endpoints.Ogmios)
		info.Endpoints.Kupo = endpointInfo(network.Status.Endpoints.Kupo)
		info.Endpoints.Faucet = endpointInfo(network.Status.Endpoints.Faucet)
	}
	if network.Status.Faucet != nil {
		info.Faucet = &faucetOutput{
			AuthSecretName: network.Status.Faucet.AuthSecretName,
		}
	}

	info.Conditions = make([]conditionOutput, 0, len(network.Status.Conditions))
	for _, condition := range network.Status.Conditions {
		info.Conditions = append(info.Conditions, conditionOutput{
			Type:               condition.Type,
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			ObservedGeneration: condition.ObservedGeneration,
			LastTransitionTime: condition.LastTransitionTime.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	return info
}

// endpointInfo projects a single ServiceEndpointStatus or returns nil when
// the endpoint has not yet been published.
func endpointInfo(endpoint *yacdv1alpha1.ServiceEndpointStatus) *endpointOutput {
	if endpoint == nil {
		return nil
	}

	return &endpointOutput{
		ServiceName: endpoint.ServiceName,
		Port:        endpoint.Port,
		URL:         endpoint.URL,
	}
}
