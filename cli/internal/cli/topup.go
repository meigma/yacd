package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newTopUpCommand wires the `yacd topup NAME` subcommand. The command
// flow is: resolve the target faucet URL (preferring the cluster-published
// endpoint unless --faucet-url overrides it), gate token transmission
// through validateFaucetURLTrust, fetch the auth token from the published
// Secret, then POST to the faucet.
func newTopUpCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topup NAME",
		Short: "Submit a faucet top-up",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeConfig, err := loadRuntimeConfig(commandContext.viper)
			if err != nil {
				return err
			}

			destinationAddress := strings.TrimSpace(commandContext.viper.GetString("address"))
			lovelace := commandContext.viper.GetInt64("lovelace")
			source := strings.TrimSpace(commandContext.viper.GetString("source"))
			faucetURL := strings.TrimSpace(commandContext.viper.GetString("faucet-url"))
			customFaucetURL := faucetURL != ""
			trustFaucetURL := commandContext.viper.GetBool("trust-faucet-url")
			allowInsecureFaucetURL := commandContext.viper.GetBool("allow-insecure-faucet-url")
			jsonOutput := commandContext.viper.GetBool("json")
			if destinationAddress == "" {
				return fmt.Errorf("--address is required")
			}
			if lovelace <= 0 {
				return fmt.Errorf("--lovelace must be greater than 0")
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
			if err := requireFaucetReady(network, namespace, args[0]); err != nil {
				return err
			}
			statusFaucetURL, err := publishedFaucetURL(network, namespace, args[0])
			if err != nil {
				return err
			}
			if network.Status.Faucet == nil || strings.TrimSpace(network.Status.Faucet.AuthSecretName) == "" {
				return fmt.Errorf("cardanonetwork %s/%s does not publish a faucet auth Secret", namespace, args[0])
			}
			// Security-relevant default: when the user did not pass
			// --faucet-url, target the URL the cluster published. The
			// override path below is what triggers the trust gate.
			if faucetURL == "" {
				faucetURL = statusFaucetURL
			}
			if err := validateFaucetURLTrust(
				faucetURL,
				statusFaucetURL,
				namespace,
				network.Status.Faucet.AuthSecretName,
				customFaucetURL,
				trustFaucetURL,
				allowInsecureFaucetURL,
			); err != nil {
				return err
			}

			token, err := kubeClient.GetSecretValue(cmd.Context(), namespace, network.Status.Faucet.AuthSecretName, faucetAuthTokenKey)
			if err != nil {
				return err
			}

			result, err := postTopUp(cmd.Context(), commandContext.httpClient, faucetURL, strings.TrimSpace(token), topUpHTTPPayload{
				Address:  destinationAddress,
				Lovelace: lovelace,
				Source:   source,
			})
			if err != nil {
				return err
			}

			if jsonOutput {
				encoded, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal top-up JSON: %w", err)
				}
				if _, err := fmt.Fprintf(commandContext.out, "%s\n", encoded); err != nil {
					return fmt.Errorf("write top-up JSON: %w", err)
				}
				return nil
			}

			if _, err := fmt.Fprintf(commandContext.out, "Submitted top-up %s\nSource: %s\nLovelace: %d\nDestination: %s\n", result.TxID, result.Source, result.Lovelace, result.DestinationAddress); err != nil {
				return fmt.Errorf("write top-up result: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().String("address", "", "Destination Cardano testnet address")
	cmd.Flags().Int64("lovelace", 0, "Exact lovelace amount to send")
	cmd.Flags().String("source", "", "Faucet source name, for example utxo1")
	cmd.Flags().String("faucet-url", "", "Override the faucet URL from CardanoNetwork status")
	cmd.Flags().Bool("trust-faucet-url", false, "Allow sending the faucet auth token to a custom non-loopback URL")
	cmd.Flags().Bool("allow-insecure-faucet-url", false, "Allow trusted custom non-loopback HTTP faucet URLs")
	cmd.Flags().Bool("json", false, "Print machine-readable JSON")

	return cmd
}

// requireFaucetReady rejects a CardanoNetwork whose status cannot be
// trusted to publish a working faucet. It fails fast on stale status
// (observedGeneration < generation), on a Degraded condition, and on a
// missing or stale Ready / FaucetReady condition.
func requireFaucetReady(network *yacdv1alpha1.CardanoNetwork, namespace string, name string) error {
	if network.Status.ObservedGeneration != network.Generation {
		return fmt.Errorf(
			"cardanonetwork %s/%s status is stale: observedGeneration=%d generation=%d",
			namespace,
			name,
			network.Status.ObservedGeneration,
			network.Generation,
		)
	}
	if degraded := kube.FreshCondition(network, kube.ConditionDegraded); degraded != nil && degraded.Status == metav1.ConditionTrue {
		return fmt.Errorf("cardanonetwork %s/%s is degraded: %s: %s", namespace, name, degraded.Reason, degraded.Message)
	}
	for _, conditionType := range []kube.ConditionType{kube.ConditionReady, kube.ConditionFaucetReady} {
		condition := kube.FreshCondition(network, conditionType)
		if condition == nil {
			return fmt.Errorf("cardanonetwork %s/%s is not faucet-ready: %s condition is missing or stale", namespace, name, conditionType)
		}
		if condition.Status != metav1.ConditionTrue {
			return fmt.Errorf("cardanonetwork %s/%s is not faucet-ready", namespace, name)
		}
	}

	return nil
}

// publishedFaucetURL returns the faucet endpoint URL the CardanoNetwork
// controller published in status. It errors if status does not yet publish
// one, so callers cannot accidentally fall back to an empty target.
func publishedFaucetURL(network *yacdv1alpha1.CardanoNetwork, namespace string, name string) (string, error) {
	if network.Status.Endpoints == nil || network.Status.Endpoints.Faucet == nil || strings.TrimSpace(network.Status.Endpoints.Faucet.URL) == "" {
		return "", fmt.Errorf("cardanonetwork %s/%s does not publish a faucet endpoint", namespace, name)
	}

	return strings.TrimSpace(network.Status.Endpoints.Faucet.URL), nil
}
