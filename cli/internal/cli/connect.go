package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/meigma/yacd/cli/internal/kube"
	"github.com/spf13/cobra"
)

const (
	// yacdStateDir is the gitignored runtime-state directory connect writes
	// under, mirroring the repo's .run/yacd-dev pattern.
	yacdStateDir = ".yacd"

	// connectReconnectInitialBackoff is the first delay before retrying a
	// failed re-establish; it doubles up to connectReconnectMaxBackoff.
	connectReconnectInitialBackoff = 1 * time.Second

	// connectReconnectMaxBackoff caps the reconnect backoff.
	connectReconnectMaxBackoff = 15 * time.Second
)

// newConnectCommand wires `yacd connect NAME`. It establishes supervised
// port-forwards to the network's chain-API endpoints, writes the loopback URLs
// to .yacd/<network>/endpoints.json, or .yacd/<namespace>/<network>/endpoints.json
// for namespace overrides, for other host processes to read, prints them, and
// holds the forwards open until interrupted. Run it in one terminal and your
// tools in another. If the forwards drop (pod restart, idle timeout) it
// re-establishes them — re-resolving the primary Pod — until the network is
// deleted or the user interrupts.
func newConnectCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect NAME",
		Short: "Forward a YACD network's endpoints and hold them open until interrupted",
		Long: `Establish supervised port-forwards to a network's chain-API endpoints, write
the loopback URLs to .yacd/<network>/endpoints.json (or
.yacd/<namespace>/<network>/endpoints.json when --namespace is set), and hold
them open until interrupted (Ctrl-C). Run it in one terminal and your tools in
another. Dropped forwards are re-established automatically. The endpoints file
never contains the faucet token, and its ports are only live while connect is
running.`,
		Args: cobra.ExactArgs(1),
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

			return runConnect(cmd.Context(), commandContext, kubeClient, namespace, name)
		},
	}

	return cmd
}

// runConnect is the connect supervision loop. The first connect attempt's
// failure is fatal so the user gets the clear "not ready"/"not found" message;
// after a successful connect, a dropped forward triggers an immediate
// re-establish, while a failed re-establish backs off and retries until the
// network is deleted (NotFound) or the context is cancelled.
//
// Drop detection is lazy: client-go surfaces a lost forward when traffic next
// flows over it, so an idle forward to a deleted Pod is noticed on the next use
// rather than the instant the Pod dies. That is harmless for an idle session —
// the forward is re-established (with a freshly resolved Pod and new local
// ports) the moment a tool reaches it and finds it broken.
func runConnect(ctx context.Context, commandContext *commandContext, kubeClient kube.Client, namespace string, name string) error {
	backoff := connectReconnectInitialBackoff
	connectedBefore := false

	for {
		connected, err := connectNetwork(ctx, kubeClient, namespace, name)
		if err != nil {
			if !connectedBefore || kube.IsNotFound(err) {
				return err
			}
			if _, werr := fmt.Fprintf(commandContext.err, "Reconnect to %s/%s failed: %v; retrying in %s...\n", namespace, name, err, backoff); werr != nil {
				return werr
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			backoff = nextBackoff(backoff)
			continue
		}

		connectedBefore = true
		backoff = connectReconnectInitialBackoff

		path, err := writeEndpointsFile(namespace, name, connected.endpoints)
		if err != nil {
			_ = connected.Close()
			return err
		}
		if err := printConnectStatus(commandContext.out, connected.endpoints, path); err != nil {
			_ = connected.Close()
			return err
		}

		select {
		case <-ctx.Done():
			_ = connected.Close()
			warnRemoveEndpointsFile(commandContext.err, path)
			// Best-effort status: a clean interrupt must not become a failure
			// exit code over a stderr write hiccup.
			_, _ = fmt.Fprintf(commandContext.err, "Disconnecting from %s/%s.\n", namespace, name)

			return nil
		case <-connected.Done():
			reason := connected.Err()
			_ = connected.Close()
			warnRemoveEndpointsFile(commandContext.err, path)
			if _, err := fmt.Fprintf(commandContext.err, "Forwards to %s/%s dropped (%v); re-establishing...\n", namespace, name, reason); err != nil {
				return err
			}
		}
	}
}

// nextBackoff doubles the reconnect backoff up to the cap.
func nextBackoff(current time.Duration) time.Duration {
	if next := current * 2; next < connectReconnectMaxBackoff {
		return next
	}

	return connectReconnectMaxBackoff
}

// writeEndpointsFile writes the token-free endpoints document with a 0700
// directory and 0600 file, then returns the path. The default namespace path
// stays .yacd/<network>/endpoints.json for compatibility; namespace overrides
// use .yacd/<namespace>/<network>/endpoints.json to avoid collisions.
func writeEndpointsFile(namespace string, name string, doc endpointsDocument) (string, error) {
	path := endpointsFilePath(namespace, name)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal endpoints document: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	return path, nil
}

func endpointsFilePath(namespace string, name string) string {
	if namespace == name {
		return filepath.Join(yacdStateDir, name, "endpoints.json")
	}

	return filepath.Join(yacdStateDir, namespace, name, "endpoints.json")
}

func warnRemoveEndpointsFile(errw io.Writer, path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_, _ = fmt.Fprintf(errw, "Warning: remove endpoint state %s: %v\n", path, err)
	}
}

// printConnectStatus prints the forwarded loopback endpoints and the file path
// through the sticky-error writer, so the per-line error handling stays compact.
func printConnectStatus(out io.Writer, doc endpointsDocument, path string) error {
	writer := &infoWriter{w: out}
	writer.printf("Forwarding %s (namespace %s):\n", doc.Network, doc.Namespace)
	if doc.OgmiosURL != "" {
		writer.printf("  %s=%s\n", envOgmiosURL, doc.OgmiosURL)
	}
	if doc.KupoURL != "" {
		writer.printf("  %s=%s\n", envKupoURL, doc.KupoURL)
	}
	if doc.FaucetURL != "" {
		writer.printf("  %s=%s\n", envFaucetURL, doc.FaucetURL)
	}
	writer.printf("Wrote %s — Ctrl-C to disconnect.\n", path)

	return writer.err
}
