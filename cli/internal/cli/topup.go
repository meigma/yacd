package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newTopUpCommand wires the `yacd topup NAME LOVELACE` subcommand. The command
// flow is: resolve how to reach the faucet (a short-lived self-managed
// port-forward by default, or the --faucet-url / ambient YACD_FAUCET_URL
// override), gate token transmission through validateFaucetURLTrust, fetch the
// auth token from the published Secret, then POST to the faucet. Self-forwarding
// is what lets topup run on the host without a `yacd run` wrapper: the faucet
// URL the controller publishes is the in-cluster Service URL, which the host
// cannot reach directly.
func newTopUpCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topup NAME LOVELACE",
		Short: "Submit a faucet top-up",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			runtimeConfig, err := loadRuntimeConfig(commandContext.viper)
			if err != nil {
				return err
			}
			name, namespace, err := resolveIdentity(args[0], runtimeConfig)
			if err != nil {
				return err
			}
			lovelace, err := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
			if err != nil {
				return fmt.Errorf("invalid LOVELACE %q: must be a positive integer", args[1])
			}
			if lovelace <= 0 {
				return fmt.Errorf("LOVELACE must be greater than 0")
			}

			destinationAddress := strings.TrimSpace(commandContext.viper.GetString("address"))
			source := strings.TrimSpace(commandContext.viper.GetString("source"))
			// faucet-url falls back to the YACD_FAUCET_URL contract variable
			// through viper's AutomaticEnv, so an override is in effect both when
			// --faucet-url is passed and when topup runs under `yacd run` (which
			// sets YACD_FAUCET_URL). Either way we skip self-forwarding.
			overrideFaucetURL := strings.TrimSpace(commandContext.viper.GetString("faucet-url"))
			trustFaucetURL := commandContext.viper.GetBool("trust-faucet-url")
			allowInsecureFaucetURL := commandContext.viper.GetBool("allow-insecure-faucet-url")
			jsonOutput := commandContext.viper.GetBool("json")
			awaitConfirm := commandContext.viper.GetBool("await")
			awaitTimeout := commandContext.viper.GetDuration("await-timeout")
			// kupo-url falls back to YACD_KUPO_URL through AutomaticEnv. When we
			// self-forward below and no Kupo URL was supplied, we derive it from
			// the forwarded loopback Kupo so `topup --await` works standalone.
			kupoURL := strings.TrimSpace(commandContext.viper.GetString("kupo-url"))
			if destinationAddress == "" {
				return fmt.Errorf("--address is required")
			}
			if awaitConfirm {
				if awaitTimeout <= 0 {
					return fmt.Errorf("--await-timeout must be greater than 0")
				}
				// Validate an explicitly supplied Kupo URL before any cluster
				// contact. A URL derived from the self-forward below is
				// constructed safe and needs no re-validation.
				if kupoURL != "" {
					if err := validateKupoURL(kupoURL); err != nil {
						return err
					}
				}
			}

			kubeClient, target, err := commandContext.resolveKubeClient(runtimeConfig)
			if err != nil {
				return err
			}
			if err := announceManagedTarget(commandContext.err, runtimeConfig, target); err != nil {
				return err
			}

			network, err := kubeClient.GetCardanoNetwork(cmd.Context(), namespace, name)
			if err != nil {
				return err
			}
			if err := requireFaucetReady(network, namespace, name); err != nil {
				return err
			}
			statusFaucetURL, err := publishedFaucetURL(network, namespace, name)
			if err != nil {
				return err
			}
			if network.Status.Faucet == nil || strings.TrimSpace(network.Status.Faucet.AuthSecretName) == "" {
				return fmt.Errorf("cardanonetwork %s/%s does not publish a faucet auth Secret", namespace, name)
			}

			// Resolve how to reach the faucet without reading the token yet, so
			// the trust gate always runs before any Secret read.
			transport, err := resolveFaucetTransport(cmd.Context(), kubeClient, network, namespace, name, overrideFaucetURL, kupoURL, awaitConfirm)
			if err != nil {
				return err
			}
			if transport.session != nil {
				defer func() { _ = transport.session.Close() }()
			}
			kupoURL = transport.kupoURL

			if awaitConfirm && kupoURL == "" {
				// Only reachable on the override path: an explicit --faucet-url
				// suppresses the self-forward, so no Kupo endpoint was forwarded.
				return fmt.Errorf("--await requires a Kupo URL: pass --kupo-url, or drop --faucet-url so topup forwards Kupo for you")
			}

			if err := validateFaucetURLTrust(
				transport.faucetURL,
				statusFaucetURL,
				namespace,
				network.Status.Faucet.AuthSecretName,
				transport.custom,
				trustFaucetURL,
				allowInsecureFaucetURL,
			); err != nil {
				return err
			}

			token, err := kubeClient.GetSecretValue(cmd.Context(), namespace, network.Status.Faucet.AuthSecretName, faucetAuthTokenKey)
			if err != nil {
				return err
			}

			result, err := postTopUp(cmd.Context(), commandContext.httpClient, transport.faucetURL, strings.TrimSpace(token), topUpHTTPPayload{
				Address:  destinationAddress,
				Lovelace: lovelace,
				Source:   source,
			})
			if err != nil {
				return err
			}

			if awaitConfirm {
				// Await at the address we asked to fund (validated, non-empty),
				// not the faucet's echoed value, so an empty echo cannot widen
				// the Kupo query to all UTxOs.
				confirmer := commandContext.utxoConfirmerFactory(kupoURL)
				// One-time notice so the otherwise-silent poll does not look
				// hung. It goes to stderr (not stdout) to keep --json output
				// clean, and is best-effort: the funding transaction is already
				// submitted, so a stderr hiccup must not fail the command.
				_, _ = fmt.Fprintf(commandContext.err, "Waiting up to %s for %s to confirm on-chain...\n", awaitTimeout, result.TxID)
				if err := awaitConfirmation(cmd.Context(), confirmer, destinationAddress, result.TxID, awaitTimeout); err != nil {
					return err
				}
			}

			return printTopUpResult(commandContext.out, result, jsonOutput, awaitConfirm)
		},
	}

	cmd.Flags().String("address", "", "Destination Cardano testnet address")
	cmd.Flags().String("source", "", "Faucet source name, for example utxo1")
	cmd.Flags().String("faucet-url", "", "Override the faucet URL from CardanoNetwork status")
	cmd.Flags().Bool("trust-faucet-url", false, "Allow sending the faucet auth token to a custom non-loopback URL")
	cmd.Flags().Bool("allow-insecure-faucet-url", false, "Allow trusted custom non-loopback HTTP faucet URLs")
	cmd.Flags().Bool("json", false, "Print machine-readable JSON")
	cmd.Flags().Bool("await", false, "Wait for the funding transaction to be confirmed on-chain (requires Kupo)")
	cmd.Flags().Duration("await-timeout", 2*time.Minute, "Maximum time to wait for --await confirmation")
	cmd.Flags().String("kupo-url", "", "Kupo URL for --await (defaults to YACD_KUPO_URL)")

	return cmd
}

// topupTransport is the resolved way topup reaches the faucet: the faucet URL to
// POST to, whether it is a user override (and therefore subject to the trust
// gate), the Kupo URL to use for --await, and an optional live port-forward
// session the caller must Close.
type topupTransport struct {
	faucetURL string
	custom    bool
	kupoURL   string
	session   kube.ForwardSession
}

// resolveFaucetTransport decides how topup reaches the faucet without reading
// any Secret, so the caller can run the trust gate before fetching the token. An
// override URL — explicit --faucet-url or ambient YACD_FAUCET_URL — is used as-is
// and marked custom. Otherwise topup opens a short-lived port-forward (which also
// covers Kupo for --await) and returns the loopback URLs plus the live session;
// the loopback faucet URL is trust-gate-exempt.
func resolveFaucetTransport(ctx context.Context, kubeClient kube.Client, network *yacdv1alpha1.CardanoNetwork, namespace string, name string, overrideURL string, kupoURL string, awaitConfirm bool) (topupTransport, error) {
	if overrideURL != "" {
		return topupTransport{faucetURL: overrideURL, custom: true, kupoURL: kupoURL}, nil
	}

	session, endpoints, err := forwardEndpoints(ctx, kubeClient, network, namespace, name)
	if err != nil {
		return topupTransport{}, err
	}
	if strings.TrimSpace(endpoints.FaucetURL) == "" {
		_ = session.Close()
		return topupTransport{}, fmt.Errorf("cardanonetwork %s/%s does not publish a faucet endpoint to forward", namespace, name)
	}
	if awaitConfirm && kupoURL == "" {
		kupoURL = endpoints.KupoURL
	}

	return topupTransport{faucetURL: endpoints.FaucetURL, kupoURL: kupoURL, session: session}, nil
}

// printTopUpResult renders the faucet response as JSON (--json) or as a short
// human-readable block, noting on-chain confirmation when --await ran.
func printTopUpResult(out io.Writer, result topUpHTTPResult, jsonOutput bool, awaitConfirm bool) error {
	if jsonOutput {
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal top-up JSON: %w", err)
		}
		if _, err := fmt.Fprintf(out, "%s\n", encoded); err != nil {
			return fmt.Errorf("write top-up JSON: %w", err)
		}
		return nil
	}

	if _, err := fmt.Fprintf(out, "Submitted top-up %s\nSource: %s\nLovelace: %d\nDestination: %s\n", result.TxID, result.Source, result.Lovelace, result.DestinationAddress); err != nil {
		return fmt.Errorf("write top-up result: %w", err)
	}
	if awaitConfirm {
		if _, err := fmt.Fprintf(out, "Confirmed on-chain.\n"); err != nil {
			return fmt.Errorf("write top-up confirmation: %w", err)
		}
	}

	return nil
}

// requireFaucetReady rejects a CardanoNetwork whose status cannot be
// trusted to publish a working faucet. It fails fast on stale status
// (observedGeneration < generation), on a Degraded condition, and on a
// missing or stale Ready / FaucetReady condition.
func requireFaucetReady(network *yacdv1alpha1.CardanoNetwork, namespace string, name string) error {
	if err := requireFreshStatus(network, namespace, name); err != nil {
		return err
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
