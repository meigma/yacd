package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/SundaeSwap-finance/kugo"
	"github.com/SundaeSwap-finance/ogmigo/v6"
	"k8s.io/apimachinery/pkg/util/wait"
)

// topUpAwaitPollInterval is how often --await queries the chain index while
// waiting for the funding transaction to be included. Compressed localnet slots
// make confirmation fast, so a one-second cadence is responsive without
// hammering Kupo.
const topUpAwaitPollInterval = 1 * time.Second

// kupoConfirmer is the production UTxOConfirmer: it queries Kupo for the
// unspent outputs at an address through the vendored kugo client, rather than
// hand-rolling the HTTP/JSON contract.
type kupoConfirmer struct {
	client *kugo.Client
}

// newKupoConfirmer builds a UTxOConfirmer against the Kupo base URL. The
// kugo client's default logger is silenced so its per-poll debug lines do not
// pollute the CLI's output; the CLI surfaces its own confirmation status.
func newKupoConfirmer(kupoURL string) *kupoConfirmer {
	return &kupoConfirmer{client: kugo.New(kugo.WithEndpoint(kupoURL), kugo.WithLogger(ogmigo.NopLogger))}
}

// TransactionIDs returns the transaction IDs of the unspent outputs currently
// at address.
func (c *kupoConfirmer) TransactionIDs(ctx context.Context, address string) ([]string, error) {
	matches, err := c.client.Matches(ctx, kugo.Address(address), kugo.OnlyUnspent())
	if err != nil {
		return nil, fmt.Errorf("query kupo matches for %s: %w", address, err)
	}

	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.TransactionID)
	}

	return ids, nil
}

// awaitConfirmation polls the chain index until an unspent output created by
// txID appears at address, or the timeout elapses. Transient query errors do
// not abort the wait — they are remembered and surfaced only if the deadline is
// reached — so a brief Kupo hiccup does not fail an otherwise-confirmable
// top-up.
func awaitConfirmation(ctx context.Context, confirmer UTxOConfirmer, address string, txID string, timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("--await-timeout must be greater than 0")
	}

	var lastErr error
	err := wait.PollUntilContextTimeout(ctx, topUpAwaitPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		ids, queryErr := confirmer.TransactionIDs(ctx, address)
		if queryErr != nil {
			lastErr = queryErr

			return false, nil
		}
		for _, id := range ids {
			if strings.EqualFold(id, txID) {
				return true, nil
			}
		}

		return false, nil
	})
	if err != nil {
		if lastErr != nil {
			return fmt.Errorf("top-up %s to %s not confirmed within %s (last query error: %w)", txID, address, timeout, lastErr)
		}

		return fmt.Errorf("top-up %s to %s not confirmed within %s", txID, address, timeout)
	}

	return nil
}
