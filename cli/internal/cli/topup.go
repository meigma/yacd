package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const faucetAuthTokenKey = "token"

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
	if degraded := kube.FreshCondition(network, "Degraded"); degraded != nil && degraded.Status == metav1.ConditionTrue {
		return fmt.Errorf("cardanonetwork %s/%s is degraded: %s: %s", namespace, name, degraded.Reason, degraded.Message)
	}
	for _, conditionType := range []string{"Ready", "FaucetReady"} {
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

func publishedFaucetURL(network *yacdv1alpha1.CardanoNetwork, namespace string, name string) (string, error) {
	if network.Status.Endpoints == nil || network.Status.Endpoints.Faucet == nil || strings.TrimSpace(network.Status.Endpoints.Faucet.URL) == "" {
		return "", fmt.Errorf("cardanonetwork %s/%s does not publish a faucet endpoint", namespace, name)
	}

	return strings.TrimSpace(network.Status.Endpoints.Faucet.URL), nil
}

func validateFaucetURLTrust(
	faucetURL string,
	statusFaucetURL string,
	namespace string,
	secretName string,
	custom bool,
	trustCustom bool,
	allowInsecure bool,
) error {
	parsed, err := parseHTTPURL(faucetURL)
	if err != nil {
		return err
	}
	if !custom || sameFaucetURL(faucetURL, statusFaucetURL) || isLoopbackHost(parsed.Hostname()) {
		return nil
	}

	secretRef := namespace + "/" + secretName
	if !trustCustom {
		return fmt.Errorf("refusing to send faucet auth Secret %s token to custom faucet URL host %q; pass --trust-faucet-url to allow this destination", secretRef, parsed.Host)
	}
	if parsed.Scheme == "http" && !allowInsecure {
		return fmt.Errorf("refusing to send faucet auth Secret %s token to insecure custom faucet URL host %q; pass --allow-insecure-faucet-url with --trust-faucet-url to allow HTTP", secretRef, parsed.Host)
	}

	return nil
}

func parseHTTPURL(raw string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid faucet URL %q: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("invalid faucet URL %q: scheme must be http or https", raw)
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return nil, fmt.Errorf("invalid faucet URL %q: host is required", raw)
	}

	return parsed, nil
}

func sameFaucetURL(left string, right string) bool {
	return strings.TrimRight(strings.TrimSpace(left), "/") == strings.TrimRight(strings.TrimSpace(right), "/")
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type topUpHTTPPayload struct {
	Address  string `json:"address"`
	Lovelace int64  `json:"lovelace"`
	Source   string `json:"source,omitempty"`
}

type topUpHTTPResult struct {
	TxID               string `json:"txId"`
	Source             string `json:"source"`
	SourceAddress      string `json:"sourceAddress"`
	DestinationAddress string `json:"destinationAddress"`
	Lovelace           int64  `json:"lovelace"`
}

type faucetErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func postTopUp(ctx context.Context, client httpDoer, faucetURL string, token string, payload topUpHTTPPayload) (topUpHTTPResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(token) == "" {
		return topUpHTTPResult{}, fmt.Errorf("faucet auth token is empty")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return topUpHTTPResult{}, fmt.Errorf("marshal top-up request: %w", err)
	}
	endpoint := strings.TrimRight(faucetURL, "/") + "/v1/topups"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return topUpHTTPResult{}, fmt.Errorf("build top-up request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return topUpHTTPResult{}, fmt.Errorf("submit top-up request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return topUpHTTPResult{}, decodeFaucetError(response)
	}

	var result topUpHTTPResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return topUpHTTPResult{}, fmt.Errorf("decode top-up response: %w", err)
	}
	if strings.TrimSpace(result.TxID) == "" {
		return topUpHTTPResult{}, fmt.Errorf("faucet returned an empty transaction id")
	}

	return result, nil
}

func decodeFaucetError(response *http.Response) error {
	var body faucetErrorResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 16*1024)).Decode(&body); err == nil {
		code := strings.TrimSpace(body.Error.Code)
		message := strings.TrimSpace(body.Error.Message)
		if code != "" && message != "" {
			return fmt.Errorf("faucet top-up failed: HTTP %d: %s: %s", response.StatusCode, code, message)
		}
	}

	return fmt.Errorf("faucet top-up failed: HTTP %d", response.StatusCode)
}
