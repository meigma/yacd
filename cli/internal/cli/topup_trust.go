package cli

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// validateFaucetURLTrust gates whether the CLI may send the faucet auth
// Bearer token to faucetURL.
//
// The function defends three attack vectors:
//
//  1. Token exfiltration to an attacker-supplied URL host. The caller may
//     pass --faucet-url pointing at any HTTP endpoint; sending the token
//     anywhere except the cluster-published URL or a loopback target would
//     leak it.
//  2. Accidental exposure via a non-loopback override. Even a legitimate
//     remote operator may not realise --faucet-url leaves the cluster boundary;
//     --trust-faucet-url is the explicit ack.
//  3. Plaintext eavesdropping. A trusted custom URL using http:// would
//     transmit the Bearer token in cleartext; --allow-insecure-faucet-url
//     is the second ack required to suppress that defence.
//
// The three checks below correspond to those three vectors in order.
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

	// Vector 1: only enforce the trust gates when the user actually
	// overrode the URL with a value that differs from what the cluster
	// published and is not a loopback target.
	if !custom || sameFaucetURL(faucetURL, statusFaucetURL) || isLoopbackHost(parsed.Hostname()) {
		return nil
	}

	secretRef := namespace + "/" + secretName

	// Vector 2: a custom non-loopback URL must be explicitly trusted.
	if !trustCustom {
		return fmt.Errorf("refusing to send faucet auth Secret %s token to custom faucet URL host %q; pass --trust-faucet-url to allow this destination", secretRef, parsed.Host)
	}

	// Vector 3: a trusted custom URL using plaintext HTTP additionally
	// needs --allow-insecure-faucet-url; the default is HTTPS-only.
	if parsed.Scheme == "http" && !allowInsecure {
		return fmt.Errorf("refusing to send faucet auth Secret %s token to insecure custom faucet URL host %q; pass --allow-insecure-faucet-url with --trust-faucet-url to allow HTTP", secretRef, parsed.Host)
	}

	return nil
}

// parseHTTPURL parses raw as an absolute http:// or https:// URL and
// returns a typed error suitable for inclusion in a user-facing message.
// A missing scheme, missing host, or unexpected scheme are all rejected
// here so the trust check downstream sees a well-formed URL.
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

// sameFaucetURL reports whether two faucet URLs refer to the same endpoint
// for the purpose of the trust gate, ignoring trailing slashes and
// surrounding whitespace. The comparison is intentionally cheap and
// syntactic; differences in case or default port appear as distinct URLs.
func sameFaucetURL(left string, right string) bool {
	return strings.TrimRight(strings.TrimSpace(left), "/") == strings.TrimRight(strings.TrimSpace(right), "/")
}

// isLoopbackHost reports whether host names a loopback target. Both
// "localhost" and any IP address whose net.IP.IsLoopback returns true
// qualify; everything else is non-loopback.
func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
