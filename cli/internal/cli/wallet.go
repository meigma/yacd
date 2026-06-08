package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/cli/internal/kube"
	walletstore "github.com/meigma/yacd/cli/internal/wallet"
	domainwallet "github.com/meigma/yacd/internal/cardano/wallet"
	"github.com/spf13/cobra"
)

// addChainOverrideFlags registers the funding endpoint overrides shared by the
// funding verbs (`wallet add --topup` and `wallet topup`). They are the
// resolver's highest-precedence rung. No token trust gate is needed: funding
// submits a locally-signed transaction (the signing key never leaves the CLI)
// and Kupo is read-only.
func addChainOverrideFlags(cmd *cobra.Command) {
	cmd.Flags().String("ogmios-url", "", "Ogmios URL to use for funding (default: the network's reachable endpoint)")
	cmd.Flags().String("kupo-url", "", "Kupo URL to use for funding (default: the network's reachable endpoint)")
}

// chainOverridesFromFlags reads the funding endpoint overrides directly off the
// command flags. Reading them from the flags rather than viper keeps an ambient
// YACD_OGMIOS_URL / YACD_KUPO_URL from shadowing the --ogmios-url / --kupo-url
// the user passed (or deliberately left unset).
func chainOverridesFromFlags(cmd *cobra.Command) (chainOverrides, error) {
	ogmiosURL, err := cmd.Flags().GetString("ogmios-url")
	if err != nil {
		return chainOverrides{}, err
	}
	kupoURL, err := cmd.Flags().GetString("kupo-url")
	if err != nil {
		return chainOverrides{}, err
	}
	return chainOverrides{
		OgmiosURL: strings.TrimSpace(ogmiosURL),
		KupoURL:   strings.TrimSpace(kupoURL),
	}, nil
}

// newWalletCommand wires the `yacd wallet` subtree: the developer-wallet verbs
// for a CardanoNetwork. Each verb takes the network NET as its first positional
// and resolves the same target as the other network verbs. The subtree owns
// managed wallet Secrets (name, keys, address) and funds them from the
// genesis-funded faucet wallet over self-managed Ogmios/Kupo port-forwards.
func newWalletCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wallet",
		Short: "Manage developer wallets for a YACD network",
		Args:  cobra.NoArgs,
	}

	cmd.AddCommand(newWalletListCommand(commandContext))
	cmd.AddCommand(newWalletAddCommand(commandContext))
	cmd.AddCommand(newWalletTopUpCommand(commandContext))
	cmd.AddCommand(newWalletExportCommand(commandContext))
	cmd.AddCommand(newWalletRemoveCommand(commandContext))

	return cmd
}

// walletContext is the resolved per-command runtime shared by the wallet verbs:
// the network identity, the kube client, the network object, and the wallet
// store bound to the network's namespace. It collapses the identical preamble
// each verb would otherwise repeat.
type walletContext struct {
	namespace  string
	name       string
	kubeClient kube.Client
	network    *yacdv1alpha1.CardanoNetwork
	store      *walletstore.Store
}

// resolveWallet runs the shared wallet-verb preamble: it resolves the network
// identity from NET, resolves and announces the kube target, fetches the
// network, and builds the wallet store. Verbs that only need the namespace and
// store (no live network) still get a fetched network, which doubles as an
// existence check so a typo names a clear not-found rather than an empty list.
func (commandContext *commandContext) resolveWallet(ctx context.Context, net string) (*walletContext, error) {
	runtimeConfig := commandContext.runtimeConfig
	name, namespace, err := resolveIdentity(net, runtimeConfig)
	if err != nil {
		return nil, err
	}

	kubeClient, target, err := commandContext.resolveKubeClient(runtimeConfig)
	if err != nil {
		return nil, err
	}
	if err := announceManagedTarget(commandContext.err, runtimeConfig, target); err != nil {
		return nil, err
	}

	network, err := kubeClient.GetCardanoNetwork(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	return &walletContext{
		namespace:  namespace,
		name:       name,
		kubeClient: kubeClient,
		network:    network,
		store:      walletstore.NewStore(kubeClient, namespace, name),
	}, nil
}

// newWalletListCommand wires `yacd wallet list NET`. It lists the managed
// wallets for the network — name and address, plus pubkey only in --json — as a
// table or machine-readable JSON. The reserved faucet wallet is excluded.
func newWalletListCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list NET",
		Short: "List managed wallets for a network",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			walletCtx, err := commandContext.resolveWallet(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			wallets, err := walletCtx.store.List(cmd.Context())
			if err != nil {
				return err
			}

			if commandContext.io.JSON() {
				return writeJSON(commandContext.out, newWalletListItems(wallets))
			}

			return printWalletList(commandContext.out, wallets)
		},
	}

	return cmd
}

// newWalletAddCommand wires `yacd wallet add NET`. It generates a fresh wallet
// (name from --name or the wordlist), creates the owned Secret, and optionally
// funds it from the faucet when --topup is set. Without --topup it only
// generates and persists the wallet.
func newWalletAddCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add NET",
		Short: "Generate a new managed wallet, optionally funding it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicitName := strings.TrimSpace(commandContext.viper.GetString("name"))
			topup := strings.TrimSpace(commandContext.viper.GetString("topup"))
			await := commandContext.viper.GetBool("await")
			awaitTimeout := commandContext.viper.GetDuration("await-timeout")
			jsonOutput := commandContext.io.JSON()

			lovelace, topupRequested, err := parseOptionalLovelace(topup)
			if err != nil {
				return err
			}
			if await && !topupRequested {
				return fmt.Errorf("--await requires --topup")
			}
			if topupRequested {
				if err := requireAwaitTimeout(await, awaitTimeout); err != nil {
					return err
				}
			}

			walletCtx, err := commandContext.resolveWallet(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			name, err := walletCtx.chooseName(cmd.Context(), explicitName)
			if err != nil {
				return err
			}

			material, err := domainwallet.New()
			if err != nil {
				return fmt.Errorf("generate wallet: %w", err)
			}

			created, err := walletCtx.store.Create(cmd.Context(), name, material, walletCtx.network)
			if err != nil {
				return err
			}

			result := walletAddResult{Name: created.Name, Address: created.Address}
			if topupRequested {
				overrides, err := chainOverridesFromFlags(cmd)
				if err != nil {
					return err
				}
				funded, err := commandContext.fundWallet(cmd.Context(), walletCtx.kubeClient, walletCtx.store, walletCtx.network, walletCtx.namespace, walletCtx.name, fundRequest{
					destinationAddress: created.Address,
					lovelace:           lovelace,
					await:              await,
					awaitTimeout:       awaitTimeout,
					ogmiosURL:          overrides.OgmiosURL,
					kupoURL:            overrides.KupoURL,
				})
				if err != nil {
					return err
				}
				result.Funding = &funded
			}

			if jsonOutput {
				return writeJSON(commandContext.out, result)
			}

			return printWalletAdd(commandContext.out, result)
		},
	}
	cmd.Flags().String("name", "", "Wallet name (default: a generated adjective-noun name)")
	cmd.Flags().String("topup", "", "Fund the new wallet with this many lovelace from the faucet")
	cmd.Flags().Bool("await", false, "Wait for the funding transaction to confirm on-chain (requires --topup)")
	cmd.Flags().Duration("await-timeout", 2*time.Minute, "Maximum time to wait for --await confirmation")
	addChainOverrideFlags(cmd)

	return cmd
}

// newWalletTopUpCommand wires `yacd wallet topup NET WALLET LOVELACE`. It
// resolves the WALLET selector (managed name, pubkey hex, or raw bech32 address)
// to a destination address and funds it with LOVELACE from the faucet, or from
// --from when another managed wallet is the source.
func newWalletTopUpCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topup NET WALLET LOVELACE",
		Short: "Fund a wallet from the faucet (or another managed wallet)",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			lovelace, err := parseLovelace(args[2])
			if err != nil {
				return err
			}
			source := strings.TrimSpace(commandContext.viper.GetString("from"))
			await := commandContext.viper.GetBool("await")
			awaitTimeout := commandContext.viper.GetDuration("await-timeout")
			jsonOutput := commandContext.io.JSON()
			if err := requireAwaitTimeout(await, awaitTimeout); err != nil {
				return err
			}

			walletCtx, err := commandContext.resolveWallet(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			destinationAddress, err := walletCtx.store.Resolve(cmd.Context(), args[1])
			if err != nil {
				return err
			}

			overrides, err := chainOverridesFromFlags(cmd)
			if err != nil {
				return err
			}
			result, err := commandContext.fundWallet(cmd.Context(), walletCtx.kubeClient, walletCtx.store, walletCtx.network, walletCtx.namespace, walletCtx.name, fundRequest{
				destinationAddress: destinationAddress,
				lovelace:           lovelace,
				sourceName:         source,
				await:              await,
				awaitTimeout:       awaitTimeout,
				ogmiosURL:          overrides.OgmiosURL,
				kupoURL:            overrides.KupoURL,
			})
			if err != nil {
				return err
			}

			if jsonOutput {
				return writeJSON(commandContext.out, result)
			}

			return printWalletTopUp(commandContext.out, result)
		},
	}
	cmd.Flags().String("from", "", "Source wallet name to fund from (default: the faucet wallet)")
	cmd.Flags().Bool("await", false, "Wait for the funding transaction to confirm on-chain")
	cmd.Flags().Duration("await-timeout", 2*time.Minute, "Maximum time to wait for --await confirmation")
	addChainOverrideFlags(cmd)

	return cmd
}

// newWalletRemoveCommand wires `yacd wallet remove NET WALLET`. It deletes the
// named managed wallet's Secret. The reserved faucet wallet cannot be removed.
func newWalletRemoveCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove NET WALLET",
		Short: "Delete a managed wallet",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			walletCtx, err := commandContext.resolveWallet(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			name := strings.ToLower(strings.TrimSpace(args[1]))
			if name == "" {
				return fmt.Errorf("WALLET is required")
			}
			if name == walletstore.FaucetWalletName {
				return walletstore.ErrFaucetReserved
			}
			// Report a missing wallet clearly rather than a misleading "Removed"
			// (the underlying delete is idempotent on a not-found Secret).
			if _, err := walletCtx.kubeClient.GetSecret(cmd.Context(), walletCtx.namespace, walletstore.SecretName(walletCtx.name, name)); err != nil {
				if kube.IsNotFound(err) {
					return fmt.Errorf("wallet %q not found", name)
				}
				return err
			}
			if err := walletCtx.store.Delete(cmd.Context(), name); err != nil {
				return err
			}

			_, err = fmt.Fprintf(commandContext.out, "Removed wallet %q from %s/%s.\n", name, walletCtx.namespace, walletCtx.name)
			return err
		},
	}

	return cmd
}

// chooseName returns the wallet name to create: the explicit --name when given
// (lowercased and rejecting the reserved faucet name), or a generated name that
// is free among the network's existing managed wallets.
func (walletCtx *walletContext) chooseName(ctx context.Context, explicit string) (string, error) {
	if explicit != "" {
		name := strings.ToLower(explicit)
		if name == walletstore.FaucetWalletName {
			return "", walletstore.ErrFaucetReserved
		}
		return name, nil
	}

	wallets, err := walletCtx.store.List(ctx)
	if err != nil {
		return "", err
	}
	taken := make(map[string]struct{}, len(wallets))
	for _, wallet := range wallets {
		taken[wallet.Name] = struct{}{}
	}

	return walletstore.GenerateName(taken)
}

// walletListItem is the JSON/table projection of a managed wallet the list verb
// emits. Field names are stable across releases.
type walletListItem struct {
	// Name is the well-known wallet name.
	Name string `json:"name"`

	// Address is the wallet's bech32 testnet address.
	Address string `json:"address"`

	// Source describes how the wallet is funded.
	Source string `json:"source"`
}

// walletAddResult is the result of `wallet add`: the chosen name and address,
// plus the funding outcome when --topup ran.
type walletAddResult struct {
	// Name is the created wallet's name.
	Name string `json:"name"`

	// Address is the created wallet's bech32 testnet address.
	Address string `json:"address"`

	// Funding is the funding outcome, present only when --topup was requested.
	Funding *fundResult `json:"funding,omitempty"`
}

// newWalletListItems projects managed wallets into their JSON shape.
func newWalletListItems(wallets []walletstore.ManagedWallet) []walletListItem {
	items := make([]walletListItem, 0, len(wallets))
	for _, wallet := range wallets {
		items = append(items, walletListItem{Name: wallet.Name, Address: wallet.Address, Source: wallet.Source})
	}

	return items
}

// parseLovelace parses a required positive lovelace amount.
func parseLovelace(raw string) (int64, error) {
	lovelace, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid LOVELACE %q: must be a positive integer", raw)
	}
	if lovelace <= 0 {
		return 0, fmt.Errorf("LOVELACE must be greater than 0")
	}

	return lovelace, nil
}

// parseOptionalLovelace parses the optional --topup amount. An empty value means
// no top-up was requested.
func parseOptionalLovelace(raw string) (int64, bool, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, false, nil
	}
	lovelace, err := parseLovelace(raw)
	if err != nil {
		return 0, false, err
	}

	return lovelace, true, nil
}

// requireAwaitTimeout validates the --await-timeout when --await is set.
func requireAwaitTimeout(await bool, timeout time.Duration) error {
	if await && timeout <= 0 {
		return fmt.Errorf("--await-timeout must be greater than 0")
	}

	return nil
}

// writeJSON marshals value as indented JSON with a trailing newline to out.
func writeJSON(out io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	if _, err := fmt.Fprintf(out, "%s\n", encoded); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}

	return nil
}

// printWalletList renders the managed wallets as an aligned table, or an
// explicit "no wallets" line so an empty result is unambiguous.
func printWalletList(out io.Writer, wallets []walletstore.ManagedWallet) error {
	if len(wallets) == 0 {
		if _, err := fmt.Fprintln(out, "No managed wallets."); err != nil {
			return fmt.Errorf("write wallet list: %w", err)
		}
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	rows := []string{"NAME\tADDRESS\tSOURCE"}
	for _, wallet := range wallets {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s", wallet.Name, wallet.Address, wallet.Source))
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, row); err != nil {
			return fmt.Errorf("write wallet list: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush wallet list: %w", err)
	}

	return nil
}

// printWalletAdd renders the created wallet and, when present, its funding
// outcome in the human-readable shape.
func printWalletAdd(out io.Writer, result walletAddResult) error {
	w := &infoWriter{w: out}
	w.printf("Created wallet %q\n", result.Name)
	w.printf("Address: %s\n", result.Address)
	if result.Funding != nil {
		writeFunding(w, *result.Funding)
	}

	return w.err
}

// printWalletTopUp renders a funding outcome in the human-readable shape.
func printWalletTopUp(out io.Writer, result fundResult) error {
	w := &infoWriter{w: out}
	writeFunding(w, result)

	return w.err
}

// writeFunding writes the shared funding-outcome block through the sticky-error
// writer.
func writeFunding(w *infoWriter, result fundResult) {
	w.printf("Funded %s with %d lovelace from %s\n", result.DestinationAddress, result.Lovelace, result.Source)
	w.printf("Transaction: %s\n", result.TxID)
	if result.Confirmed {
		w.printf("Confirmed on-chain.\n")
	}
}
