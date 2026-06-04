package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/meigma/yacd/cli/internal/kube"
	walletstore "github.com/meigma/yacd/cli/internal/wallet"
	"github.com/spf13/cobra"
)

// newWalletExportCommand wires `yacd wallet export NET WALLET`. It reads the
// named managed wallet's Secret and writes its key material — <name>.skey,
// <name>.vkey, and <name>.addr — to a 0700 directory with 0600 files. The
// default location is .yacd/<namespace>/<network>/wallets/<name>/ (collapsed to
// .yacd/<network>/... when the namespace equals the network), under the same
// gitignored state root connect uses. The written paths are reported to stderr;
// key material is never printed to stdout.
func newWalletExportCommand(commandContext *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export NET WALLET",
		Short: "Export a managed wallet's keys and address to local files",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			outDir := strings.TrimSpace(commandContext.viper.GetString("out"))
			force := commandContext.viper.GetBool("force")

			walletCtx, err := commandContext.resolveWallet(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			name := strings.ToLower(strings.TrimSpace(args[1]))
			if name == "" {
				return fmt.Errorf("WALLET is required")
			}

			material, err := readWalletMaterial(cmd.Context(), walletCtx.kubeClient, walletCtx.namespace, walletstore.SecretName(walletCtx.name, name))
			if err != nil {
				return err
			}

			dir := outDir
			if dir == "" {
				dir = walletExportDir(walletCtx.namespace, walletCtx.name, name)
			}

			paths, err := writeWalletFiles(dir, name, material, force)
			if err != nil {
				return err
			}

			return printWalletExport(commandContext.err, paths)
		},
	}
	cmd.Flags().String("out", "", "Directory to write the wallet files into (default: .yacd/<namespace>/<network>/wallets/<name>)")
	cmd.Flags().Bool("force", false, "Overwrite existing wallet files")

	return cmd
}

// walletMaterial holds the raw Secret data the export verb writes to disk: the
// two key envelopes and the bech32 address.
type walletMaterial struct {
	signingKey      []byte
	verificationKey []byte
	address         []byte
}

// readWalletMaterial reads a wallet Secret and extracts the key envelopes and
// address. A missing Secret or a missing data key is surfaced as a typed error
// so a wrong wallet name reports clearly rather than writing partial files.
func readWalletMaterial(ctx context.Context, kubeClient kube.Client, namespace string, secretName string) (walletMaterial, error) {
	secret, err := kubeClient.GetSecret(ctx, namespace, secretName)
	if err != nil {
		return walletMaterial{}, err
	}

	signingKey, err := requireSecretData(secret.Data, walletstore.SigningKeyKey, namespace, secretName)
	if err != nil {
		return walletMaterial{}, err
	}
	verificationKey, err := requireSecretData(secret.Data, walletstore.VerificationKeyKey, namespace, secretName)
	if err != nil {
		return walletMaterial{}, err
	}
	address, err := requireSecretData(secret.Data, walletstore.AddressKey, namespace, secretName)
	if err != nil {
		return walletMaterial{}, err
	}

	return walletMaterial{signingKey: signingKey, verificationKey: verificationKey, address: address}, nil
}

// requireSecretData returns a non-empty Secret data value or a typed error.
func requireSecretData(data map[string][]byte, key string, namespace string, secretName string) ([]byte, error) {
	value, ok := data[key]
	if !ok || len(value) == 0 {
		return nil, fmt.Errorf("wallet secret %s/%s is missing %s", namespace, secretName, key)
	}

	return value, nil
}

// writeWalletFiles writes the three wallet files into dir with restrictive
// permissions and returns the written paths. The directory is created 0700 and
// each file 0600. Without --force an existing file is an error so an export
// never silently overwrites local keys.
func writeWalletFiles(dir string, name string, material walletMaterial, force bool) ([]string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}

	files := []struct {
		path string
		data []byte
	}{
		{path: filepath.Join(dir, name+".skey"), data: material.signingKey},
		{path: filepath.Join(dir, name+".vkey"), data: material.verificationKey},
		{path: filepath.Join(dir, name+".addr"), data: ensureTrailingNewline(material.address)},
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags |= os.O_EXCL
	}

	paths := make([]string, 0, len(files))
	for _, file := range files {
		f, err := os.OpenFile(file.path, flags, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return nil, fmt.Errorf("%s already exists; pass --force to overwrite", file.path)
			}
			return nil, fmt.Errorf("write %s: %w", file.path, err)
		}
		_, writeErr := f.Write(file.data)
		closeErr := f.Close()
		if writeErr != nil {
			return nil, fmt.Errorf("write %s: %w", file.path, writeErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("write %s: %w", file.path, closeErr)
		}
		paths = append(paths, file.path)
	}

	return paths, nil
}

// ensureTrailingNewline appends a newline to the address so the .addr file is a
// well-formed text line, matching the .skey/.vkey envelopes which already end
// in one.
func ensureTrailingNewline(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\n' {
		return data
	}

	return append(append([]byte{}, data...), '\n')
}

// walletExportDir returns the default export directory under the gitignored
// state root, collapsing the namespace segment when it equals the network for
// the common one-network-per-namespace case.
func walletExportDir(namespace string, network string, name string) string {
	if namespace == network {
		return filepath.Join(yacdStateDir, network, "wallets", name)
	}

	return filepath.Join(yacdStateDir, namespace, network, "wallets", name)
}

// printWalletExport reports the written paths to stderr (errw), keeping stdout
// free of any file content so the verb stays safe to redirect.
func printWalletExport(errw io.Writer, paths []string) error {
	w := &infoWriter{w: errw}
	w.println("Wrote wallet files:")
	for _, path := range paths {
		w.printf("  %s\n", path)
	}

	return w.err
}
