package cli

import (
	"fmt"
	"io"
)

// infoWriter is a sticky-error writer used by the info pretty-printer.
// Once any write fails, every subsequent call is a no-op and the error
// surfaces via err(). This collapses the per-Fprintf error ladder that
// would otherwise dominate the per-section helpers.
type infoWriter struct {
	w   io.Writer
	err error
}

func (i *infoWriter) printf(format string, args ...any) {
	if i.err != nil {
		return
	}
	if _, err := fmt.Fprintf(i.w, format, args...); err != nil {
		i.err = fmt.Errorf("write info: %w", err)
	}
}

func (i *infoWriter) println(s string) {
	if i.err != nil {
		return
	}
	if _, err := fmt.Fprintln(i.w, s); err != nil {
		i.err = fmt.Errorf("write info: %w", err)
	}
}

// printInfo renders an infoOutput to out in the human-readable text format
// used when --json is not set. The sections (header, network, conditions,
// endpoints, faucet) are written in a fixed order to keep output stable
// across releases.
func printInfo(out io.Writer, info infoOutput) error {
	w := &infoWriter{w: out}
	printInfoHeader(w, info)
	printNetworkInfo(w, info.Network)
	printConditionsInfo(w, info.Conditions)
	printEndpointsInfo(w, info.Endpoints)
	printFaucetInfo(w, info.Faucet)
	return w.err
}

// printInfoHeader writes the name/namespace block and the observed
// generation if the controller has reconciled at least once.
func printInfoHeader(w *infoWriter, info infoOutput) {
	w.printf("Name: %s\nNamespace: %s\n", info.Name, info.Namespace)
	if info.ObservedGeneration != 0 {
		w.printf("Observed generation: %d\n", info.ObservedGeneration)
	}
}

// printNetworkInfo writes the Network section if any field is populated.
// An empty network block is suppressed entirely to keep early-reconcile
// output uncluttered.
func printNetworkInfo(w *infoWriter, network networkOutput) {
	if network.Mode == "" && network.LocalnetFingerprint == "" && network.NetworkMagic == nil && network.Profile == "" && network.Era == "" {
		return
	}
	w.println("\nNetwork:")
	if network.Mode != "" {
		w.printf("  Mode: %s\n", network.Mode)
	}
	if network.LocalnetFingerprint != "" {
		w.printf("  Localnet fingerprint: %s\n", network.LocalnetFingerprint)
	}
	if network.NetworkMagic != nil {
		w.printf("  Network magic: %d\n", *network.NetworkMagic)
	}
	if network.Profile != "" {
		w.printf("  Profile: %s\n", network.Profile)
	}
	if network.Era != "" {
		w.printf("  Era: %s\n", network.Era)
	}
}

// printConditionsInfo writes the Conditions section, with one line per
// condition and a "None" placeholder when the controller has not yet
// published any.
func printConditionsInfo(w *infoWriter, conditions []conditionOutput) {
	w.println("\nConditions:")
	if len(conditions) == 0 {
		w.println("  None")
	}
	for _, condition := range conditions {
		w.printf("  %s: %s", condition.Type, condition.Status)
		if condition.Reason != "" {
			w.printf(" (%s)", condition.Reason)
		}
		if condition.Message != "" {
			w.printf(" - %s", condition.Message)
		}
		w.println("")
	}
}

// printEndpointsInfo writes the Endpoints section. Each endpoint is
// rendered in a fixed order so the output is stable; unpublished endpoints
// surface as "unavailable" rather than being omitted.
func printEndpointsInfo(w *infoWriter, endpoints endpointsOutput) {
	w.println("\nEndpoints:")
	printEndpointInfo(w, "node-to-node", endpoints.NodeToNode)
	printEndpointInfo(w, "ogmios", endpoints.Ogmios)
	printEndpointInfo(w, "kupo", endpoints.Kupo)
	printEndpointInfo(w, "faucet", endpoints.Faucet)
}

// printFaucetInfo writes the Faucet section when the controller has
// published the auth Secret name. It is suppressed entirely otherwise to
// keep non-faucet networks' output compact.
func printFaucetInfo(w *infoWriter, faucet *faucetOutput) {
	if faucet == nil || faucet.AuthSecretName == "" {
		return
	}
	w.println("\nFaucet:")
	w.printf("  Auth Secret: %s\n", faucet.AuthSecretName)
}

// printEndpointInfo writes a single endpoint entry. A nil endpoint renders
// as "unavailable" so the absence of one is visible in the output.
func printEndpointInfo(w *infoWriter, name string, endpoint *endpointOutput) {
	if endpoint == nil {
		w.printf("  %s: unavailable\n", name)
		return
	}
	w.printf("  %s: %s", name, endpoint.URL)
	if endpoint.ServiceName != "" {
		w.printf(" (service %s, port %d)", endpoint.ServiceName, endpoint.Port)
	}
	w.println("")
}
