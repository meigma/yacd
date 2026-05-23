package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meigma/yacd/services/faucet/internal/server"
	"github.com/meigma/yacd/services/faucet/internal/topup"
	"github.com/spf13/viper"
)

const (
	testAddress   = "addr_test1vqy2n0vz5rlpykf6dcqn55xdcpey7mejyexlgj6370leayst4k6ta"
	testAuthToken = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestVersionFlagPrintsBuildMetadata(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := NewRootCommand(Options{
		Out: &stdout,
		Err: &stderr,
		Build: BuildInfo{
			Version: "0.1.0",
			Commit:  "abc1234",
			Date:    "2026-05-22T10:00:00Z",
		},
		ServerRunner: func(*server.Config) error {
			return errors.New("server should not run for --version")
		},
	})
	root.SetArgs([]string{"--version"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}
	if got, want := stdout.String(), "yacd-faucet 0.1.0 (abc1234) built 2026-05-22T10:00:00Z\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
}

func TestRootCommandUsesDefaults(t *testing.T) {
	t.Parallel()

	var captured *server.Config
	tokenFile := writeTokenFile(t, testAuthToken)
	root := NewRootCommand(Options{
		Viper: viper.New(),
		ServerRunner: func(config *server.Config) error {
			captured = config
			return nil
		},
	})
	root.SetArgs([]string{"--auth-token-file", tokenFile})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}
	if captured == nil {
		t.Fatal("server did not run")
	}
	if got, want := captured.ListenAddress, "127.0.0.1:8080"; got != want {
		t.Fatalf("listen address = %q, want %q", got, want)
	}
	if got, want := captured.Sources.RootDir(), "/state/env/utxo-keys"; got != want {
		t.Fatalf("utxo keys dir = %q, want %q", got, want)
	}
	if got, want := captured.Sources.DefaultName(), "utxo1"; got != want {
		t.Fatalf("default source = %q, want %q", got, want)
	}
	if got, want := captured.AuthToken, testAuthToken; got != want {
		t.Fatalf("auth token = %q, want %q", got, want)
	}
	_, err := captured.TopUps.Submit(context.Background(), topup.Request{
		DestinationAddress: testAddress,
		Lovelace:           topup.DefaultMaxLovelace + 1,
	})
	if err == nil {
		t.Fatal("top-up over default max succeeded, want error")
	}
	assertInvalidTopUpCode(t, err)
	_, err = captured.TopUps.Submit(context.Background(), topup.Request{
		DestinationAddress: testAddress,
		Lovelace:           topup.DefaultMinLovelace - 1,
	})
	if err == nil {
		t.Fatal("top-up below default min succeeded, want error")
	}
	assertInvalidTopUpCode(t, err)
}

func TestRootCommandReadsEnvironment(t *testing.T) {
	tokenFile := writeTokenFile(t, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	t.Setenv("YACD_FAUCET_LISTEN_ADDRESS", "127.0.0.1:9090")
	t.Setenv("YACD_FAUCET_UTXO_KEYS_DIR", "/custom/utxo-keys")
	t.Setenv("YACD_FAUCET_DEFAULT_SOURCE", "utxo2")
	t.Setenv("YACD_FAUCET_OGMIOS_URL", "ws://127.0.0.1:9999")
	t.Setenv("YACD_FAUCET_KUPO_URL", "http://127.0.0.1:9998")
	t.Setenv("YACD_FAUCET_AUTH_TOKEN_FILE", tokenFile)
	t.Setenv("YACD_FAUCET_MIN_TOPUP_LOVELACE", "50")
	t.Setenv("YACD_FAUCET_MAX_TOPUP_LOVELACE", "100")
	t.Setenv("YACD_FAUCET_CHAIN_REQUEST_TIMEOUT", "2s")
	t.Setenv("YACD_FAUCET_TX_TTL_SLOTS", "42")
	t.Setenv("YACD_FAUCET_LOG_LEVEL", "debug")
	t.Setenv("YACD_FAUCET_LOG_FORMAT", "json")

	var captured *server.Config
	root := NewRootCommand(Options{
		Viper: viper.New(),
		ServerRunner: func(config *server.Config) error {
			captured = config
			return nil
		},
	})
	root.SetArgs([]string{})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}
	if got, want := captured.ListenAddress, "127.0.0.1:9090"; got != want {
		t.Fatalf("listen address = %q, want %q", got, want)
	}
	if got, want := captured.Sources.RootDir(), "/custom/utxo-keys"; got != want {
		t.Fatalf("utxo keys dir = %q, want %q", got, want)
	}
	if got, want := captured.Sources.DefaultName(), "utxo2"; got != want {
		t.Fatalf("default source = %q, want %q", got, want)
	}
	if got, want := captured.AuthToken, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"; got != want {
		t.Fatalf("auth token = %q, want %q", got, want)
	}
	_, err := captured.TopUps.Submit(context.Background(), topup.Request{
		DestinationAddress: testAddress,
		Lovelace:           101,
	})
	if err == nil {
		t.Fatal("top-up over env max succeeded, want error")
	}
	assertInvalidTopUpCode(t, err)
	_, err = captured.TopUps.Submit(context.Background(), topup.Request{
		DestinationAddress: testAddress,
		Lovelace:           49,
	})
	if err == nil {
		t.Fatal("top-up below env min succeeded, want error")
	}
	assertInvalidTopUpCode(t, err)
}

func TestRootCommandAllowsRemoteListenWhenExplicit(t *testing.T) {
	t.Parallel()

	var captured *server.Config
	tokenFile := writeTokenFile(t, testAuthToken)
	root := NewRootCommand(Options{
		Viper: viper.New(),
		ServerRunner: func(config *server.Config) error {
			captured = config
			return nil
		},
	})
	root.SetArgs([]string{"--listen-address", "0.0.0.0:8080", "--allow-remote-listen", "--auth-token-file", tokenFile})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned an error: %v", err)
	}
	if captured == nil {
		t.Fatal("server did not run")
	}
	if got, want := captured.ListenAddress, "0.0.0.0:8080"; got != want {
		t.Fatalf("listen address = %q, want %q", got, want)
	}
}

func TestRootCommandRejectsInvalidTopUpConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "remote listen without opt-in",
			args: []string{"--listen-address", "0.0.0.0:8080"},
			want: "not loopback",
		},
		{
			name: "missing Ogmios URL",
			args: []string{"--ogmios-url", ""},
			want: "--ogmios-url is required",
		},
		{
			name: "missing Kupo URL",
			args: []string{"--kupo-url", ""},
			want: "--kupo-url is required",
		},
		{
			name: "invalid max top-up",
			args: []string{"--max-topup-lovelace", "0"},
			want: "--max-topup-lovelace must be positive",
		},
		{
			name: "invalid min top-up",
			args: []string{"--min-topup-lovelace", "0"},
			want: "--min-topup-lovelace must be positive",
		},
		{
			name: "min exceeds max",
			args: []string{"--min-topup-lovelace", "101", "--max-topup-lovelace", "100"},
			want: "--min-topup-lovelace must be less than or equal to --max-topup-lovelace",
		},
		{
			name: "invalid chain timeout",
			args: []string{"--chain-request-timeout", "0s"},
			want: "--chain-request-timeout must be positive",
		},
		{
			name: "invalid tx ttl",
			args: []string{"--tx-ttl-slots", "0"},
			want: "--tx-ttl-slots must be positive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := NewRootCommand(Options{
				Viper: viper.New(),
				ServerRunner: func(*server.Config) error {
					return errors.New("server should not run with invalid top-up config")
				},
			})
			root.SetArgs(tt.args)

			err := root.ExecuteContext(context.Background())
			if err == nil {
				t.Fatal("ExecuteContext succeeded, want config error")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) {
				t.Fatalf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRootCommandRejectsInvalidAuthTokenFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path func(t *testing.T) string
		want string
	}{
		{
			name: "missing",
			path: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "missing-token")
			},
			want: "read --auth-token-file",
		},
		{
			name: "directory",
			path: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			want: "read --auth-token-file",
		},
		{
			name: "empty",
			path: func(t *testing.T) string {
				t.Helper()
				return writeTokenFile(t, "")
			},
			want: "at least 32 characters",
		},
		{
			name: "short",
			path: func(t *testing.T) string {
				t.Helper()
				return writeTokenFile(t, "short")
			},
			want: "at least 32 characters",
		},
		{
			name: "embedded whitespace",
			path: func(t *testing.T) string {
				t.Helper()
				return writeTokenFile(t, "aaaaaaaaaaaaaaaa aaaaaaaaaaaaaaa")
			},
			want: "must not contain whitespace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := NewRootCommand(Options{
				Viper: viper.New(),
				ServerRunner: func(*server.Config) error {
					return errors.New("server should not run with invalid auth token")
				},
			})
			root.SetArgs([]string{"--auth-token-file", tt.path(t)})

			err := root.ExecuteContext(context.Background())
			if err == nil {
				t.Fatal("ExecuteContext succeeded, want token error")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) {
				t.Fatalf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRootCommandRejectsInvalidLogLevel(t *testing.T) {
	t.Parallel()

	root := NewRootCommand(Options{
		Viper: viper.New(),
		ServerRunner: func(*server.Config) error {
			return errors.New("server should not run with invalid log level")
		},
	})
	root.SetArgs([]string{"--log-level", "trace"})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("ExecuteContext succeeded, want log level error")
	}
	if got := err.Error(); !strings.Contains(got, `unsupported log level "trace"`) {
		t.Fatalf("error = %q, want unsupported log level", got)
	}
}

func TestRootCommandRejectsInvalidLogFormat(t *testing.T) {
	t.Parallel()

	root := NewRootCommand(Options{
		Viper: viper.New(),
		ServerRunner: func(*server.Config) error {
			return errors.New("server should not run with invalid log format")
		},
	})
	root.SetArgs([]string{"--log-format", "pretty"})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("ExecuteContext succeeded, want log format error")
	}
	if got := err.Error(); !strings.Contains(got, `unsupported log format "pretty"`) {
		t.Fatalf("error = %q, want unsupported log format", got)
	}
}

func TestRootCommandRejectsUnexpectedArgs(t *testing.T) {
	t.Parallel()

	root := NewRootCommand(Options{
		Viper: viper.New(),
		ServerRunner: func(*server.Config) error {
			return errors.New("server should not run with unexpected args")
		},
	})
	root.SetArgs([]string{"unexpected"})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("ExecuteContext succeeded, want argument error")
	}
	if got := err.Error(); !strings.Contains(got, `unknown command "unexpected"`) {
		t.Fatalf("error = %q, want unexpected arg message", got)
	}
}

func writeTokenFile(t *testing.T, token string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	return path
}

func assertInvalidTopUpCode(t *testing.T, err error) {
	t.Helper()

	var topupErr *topup.Error
	if !errors.As(err, &topupErr) {
		t.Fatalf("error = %v, want top-up error", err)
	}
	if topupErr.Code != topup.CodeInvalidRequest {
		t.Fatalf("top-up error code = %q, want %q", topupErr.Code, topup.CodeInvalidRequest)
	}
}
