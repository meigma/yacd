package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
)

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

			kubeClient, err := commandContext.kubeClientFactory(kube.Config{
				Kubeconfig: runtimeConfig.Kubeconfig,
				Context:    runtimeConfig.KubeContext,
			})
			if err != nil {
				return err
			}

			namespace := runtimeConfig.Namespace
			if strings.TrimSpace(namespace) == "" {
				namespace = kubeClient.DefaultNamespace()
			}

			network, err := kubeClient.GetCardanoNetwork(cmd.Context(), namespace, args[0])
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

type infoOutput struct {
	Name               string            `json:"name"`
	Namespace          string            `json:"namespace"`
	ObservedGeneration int64             `json:"observedGeneration,omitempty"`
	Network            networkOutput     `json:"network"`
	Endpoints          endpointsOutput   `json:"endpoints"`
	Faucet             *faucetOutput     `json:"faucet,omitempty"`
	Conditions         []conditionOutput `json:"conditions"`
}

type networkOutput struct {
	Mode                string `json:"mode,omitempty"`
	LocalnetFingerprint string `json:"localnetFingerprint,omitempty"`
	NetworkMagic        *int64 `json:"networkMagic,omitempty"`
	Profile             string `json:"profile,omitempty"`
	Era                 string `json:"era,omitempty"`
}

type endpointsOutput struct {
	NodeToNode *endpointOutput `json:"nodeToNode,omitempty"`
	Ogmios     *endpointOutput `json:"ogmios,omitempty"`
	Kupo       *endpointOutput `json:"kupo,omitempty"`
	Faucet     *endpointOutput `json:"faucet,omitempty"`
}

type endpointOutput struct {
	ServiceName string `json:"serviceName,omitempty"`
	Port        int32  `json:"port,omitempty"`
	URL         string `json:"url,omitempty"`
}

type faucetOutput struct {
	AuthSecretName string `json:"authSecretName,omitempty"`
}

type conditionOutput struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

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

func printInfo(out io.Writer, info infoOutput) error {
	if err := printInfoHeader(out, info); err != nil {
		return err
	}
	if err := printNetworkInfo(out, info.Network); err != nil {
		return err
	}
	if err := printConditionsInfo(out, info.Conditions); err != nil {
		return err
	}
	if err := printEndpointsInfo(out, info.Endpoints); err != nil {
		return err
	}
	if err := printFaucetInfo(out, info.Faucet); err != nil {
		return err
	}

	return nil
}

func printInfoHeader(out io.Writer, info infoOutput) error {
	if _, err := fmt.Fprintf(out, "Name: %s\nNamespace: %s\n", info.Name, info.Namespace); err != nil {
		return fmt.Errorf("write info: %w", err)
	}
	if info.ObservedGeneration != 0 {
		if _, err := fmt.Fprintf(out, "Observed generation: %d\n", info.ObservedGeneration); err != nil {
			return fmt.Errorf("write info: %w", err)
		}
	}

	return nil
}

func printNetworkInfo(out io.Writer, network networkOutput) error {
	if network.Mode != "" || network.LocalnetFingerprint != "" || network.NetworkMagic != nil || network.Profile != "" || network.Era != "" {
		if _, err := fmt.Fprintln(out, "\nNetwork:"); err != nil {
			return fmt.Errorf("write info: %w", err)
		}
		if network.Mode != "" {
			if _, err := fmt.Fprintf(out, "  Mode: %s\n", network.Mode); err != nil {
				return fmt.Errorf("write info: %w", err)
			}
		}
		if network.LocalnetFingerprint != "" {
			if _, err := fmt.Fprintf(out, "  Localnet fingerprint: %s\n", network.LocalnetFingerprint); err != nil {
				return fmt.Errorf("write info: %w", err)
			}
		}
		if network.NetworkMagic != nil {
			if _, err := fmt.Fprintf(out, "  Network magic: %d\n", *network.NetworkMagic); err != nil {
				return fmt.Errorf("write info: %w", err)
			}
		}
		if network.Profile != "" {
			if _, err := fmt.Fprintf(out, "  Profile: %s\n", network.Profile); err != nil {
				return fmt.Errorf("write info: %w", err)
			}
		}
		if network.Era != "" {
			if _, err := fmt.Fprintf(out, "  Era: %s\n", network.Era); err != nil {
				return fmt.Errorf("write info: %w", err)
			}
		}
	}

	return nil
}

func printConditionsInfo(out io.Writer, conditions []conditionOutput) error {
	if _, err := fmt.Fprintln(out, "\nConditions:"); err != nil {
		return fmt.Errorf("write info: %w", err)
	}
	if len(conditions) == 0 {
		if _, err := fmt.Fprintln(out, "  None"); err != nil {
			return fmt.Errorf("write info: %w", err)
		}
	}
	for _, condition := range conditions {
		if _, err := fmt.Fprintf(out, "  %s: %s", condition.Type, condition.Status); err != nil {
			return fmt.Errorf("write info: %w", err)
		}
		if condition.Reason != "" {
			if _, err := fmt.Fprintf(out, " (%s)", condition.Reason); err != nil {
				return fmt.Errorf("write info: %w", err)
			}
		}
		if condition.Message != "" {
			if _, err := fmt.Fprintf(out, " - %s", condition.Message); err != nil {
				return fmt.Errorf("write info: %w", err)
			}
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return fmt.Errorf("write info: %w", err)
		}
	}

	return nil
}

func printEndpointsInfo(out io.Writer, endpoints endpointsOutput) error {
	if _, err := fmt.Fprintln(out, "\nEndpoints:"); err != nil {
		return fmt.Errorf("write info: %w", err)
	}
	if err := printEndpointInfo(out, "node-to-node", endpoints.NodeToNode); err != nil {
		return err
	}
	if err := printEndpointInfo(out, "ogmios", endpoints.Ogmios); err != nil {
		return err
	}
	if err := printEndpointInfo(out, "kupo", endpoints.Kupo); err != nil {
		return err
	}
	if err := printEndpointInfo(out, "faucet", endpoints.Faucet); err != nil {
		return err
	}

	return nil
}

func printFaucetInfo(out io.Writer, faucet *faucetOutput) error {
	if faucet == nil || faucet.AuthSecretName == "" {
		return nil
	}
	if _, err := fmt.Fprintln(out, "\nFaucet:"); err != nil {
		return fmt.Errorf("write info: %w", err)
	}
	if _, err := fmt.Fprintf(out, "  Auth Secret: %s\n", faucet.AuthSecretName); err != nil {
		return fmt.Errorf("write info: %w", err)
	}

	return nil
}

func printEndpointInfo(out io.Writer, name string, endpoint *endpointOutput) error {
	if endpoint == nil {
		if _, err := fmt.Fprintf(out, "  %s: unavailable\n", name); err != nil {
			return fmt.Errorf("write info: %w", err)
		}
		return nil
	}
	if _, err := fmt.Fprintf(out, "  %s: %s", name, endpoint.URL); err != nil {
		return fmt.Errorf("write info: %w", err)
	}
	if endpoint.ServiceName != "" {
		if _, err := fmt.Fprintf(out, " (service %s, port %d)", endpoint.ServiceName, endpoint.Port); err != nil {
			return fmt.Errorf("write info: %w", err)
		}
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return fmt.Errorf("write info: %w", err)
	}

	return nil
}
