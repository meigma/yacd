package sources

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	verificationKeyFile     = "utxo.vkey"
	signingKeyFile          = "utxo.skey"
	verificationKeyType     = "GenesisUTxOVerificationKey_ed25519"
	signingKeyType          = "GenesisUTxOSigningKey_ed25519"
	sourceDirectoryPathName = "."

	// CodeInvalidSourceName identifies a source name validation error.
	CodeInvalidSourceName = "invalid_source_name"
	// CodeSourceNotFound identifies a missing or unusable source.
	CodeSourceNotFound = "source_not_found"
	// CodeSourceIncomplete identifies a source missing required key files.
	CodeSourceIncomplete = "source_incomplete"
	// CodeSourceInvalidKey identifies malformed source key metadata.
	CodeSourceInvalidKey = "source_invalid_key"
	// CodeSourceReadFailed identifies a filesystem read failure.
	CodeSourceReadFailed = "source_read_failed"
)

// Store discovers faucet sources from a cardano-testnet utxo-keys directory.
type Store struct {
	rootDir     string
	defaultName string
}

// List describes the source listing API response.
type List struct {
	DefaultSource string   `json:"defaultSource"`
	Sources       []Source `json:"sources"`
}

// Source describes one usable faucet source without exposing signing material.
type Source struct {
	Name                       string `json:"name"`
	Default                    bool   `json:"default"`
	VerificationKeyType        string `json:"verificationKeyType"`
	SigningKeyType             string `json:"signingKeyType"`
	VerificationKeyDescription string `json:"verificationKeyDescription,omitempty"`
	SigningKeyDescription      string `json:"signingKeyDescription,omitempty"`
}

// Error is a structured source discovery error.
type Error struct {
	Code    string
	Message string
}

type keyMetadata struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// NewStore returns a source store rooted at rootDir.
func NewStore(rootDir string, defaultName string) Store {
	return Store{
		rootDir:     rootDir,
		defaultName: defaultName,
	}
}

// RootDir returns the configured utxo-keys directory.
func (s Store) RootDir() string {
	return s.rootDir
}

// DefaultName returns the configured default source name.
func (s Store) DefaultName() string {
	return s.defaultName
}

func (e *Error) Error() string {
	return e.Message
}

// IsCode reports whether err is a source Error with code.
func IsCode(err error, code string) bool {
	var sourceErr *Error
	ok := errors.As(err, &sourceErr)

	return ok && sourceErr.Code == code
}

// ValidateName validates a request/config source name.
func ValidateName(name string) error {
	if name == "" {
		return sourceError(CodeInvalidSourceName, "source name is required")
	}
	if strings.TrimSpace(name) != name {
		return sourceError(CodeInvalidSourceName, "source name must not contain leading or trailing whitespace")
	}
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return sourceError(CodeInvalidSourceName, "source name must not contain traversal")
	}
	if strings.ContainsAny(name, `/\`) {
		return sourceError(CodeInvalidSourceName, "source name must not contain path separators")
	}
	if filepath.Base(name) != name {
		return sourceError(CodeInvalidSourceName, "source name must be a single path segment")
	}

	return nil
}

// List returns valid faucet sources sorted by name.
func (s Store) List() (List, error) {
	if err := ValidateName(s.defaultName); err != nil {
		return List{}, err
	}

	root, err := s.openRoot()
	if err != nil {
		return List{}, err
	}
	defer func() {
		_ = root.Close()
	}()

	entries, err := readDir(root, sourceDirectoryPathName)
	if err != nil {
		return List{}, sourceError(CodeSourceReadFailed, "read source directory %q: %v", s.rootDir, err)
	}

	result := List{
		DefaultSource: s.defaultName,
		Sources:       make([]Source, 0, len(entries)),
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := ValidateName(entry.Name()); err != nil {
			continue
		}

		source, err := s.readSource(root, entry.Name())
		if err != nil {
			if IsCode(err, CodeSourceNotFound) || IsCode(err, CodeSourceIncomplete) {
				continue
			}

			return List{}, err
		}
		result.Sources = append(result.Sources, source)
	}

	sort.Slice(result.Sources, func(i int, j int) bool {
		return result.Sources[i].Name < result.Sources[j].Name
	})

	return result, nil
}

// Get returns one valid faucet source by name.
func (s Store) Get(name string) (Source, error) {
	if err := ValidateName(name); err != nil {
		return Source{}, err
	}

	root, err := s.openRoot()
	if err != nil {
		return Source{}, err
	}
	defer func() {
		_ = root.Close()
	}()

	source, err := s.readSource(root, name)
	if err != nil {
		if IsCode(err, CodeSourceIncomplete) {
			return Source{}, sourceError(CodeSourceNotFound, "faucet source %q was not found", name)
		}

		return Source{}, err
	}

	return source, nil
}

// Ready returns nil when the source directory and default source are usable.
func (s Store) Ready() error {
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	defer func() {
		_ = root.Close()
	}()

	if _, err := readDir(root, sourceDirectoryPathName); err != nil {
		return sourceError(CodeSourceReadFailed, "read source directory %q: %v", s.rootDir, err)
	}
	if _, err := s.readSource(root, s.defaultName); err != nil {
		return err
	}

	return nil
}

func (s Store) openRoot() (*os.Root, error) {
	root, err := os.OpenRoot(s.rootDir)
	if err != nil {
		return nil, sourceError(CodeSourceReadFailed, "open source directory %q: %v", s.rootDir, err)
	}

	return root, nil
}

func readDir(root *os.Root, name string) ([]os.DirEntry, error) {
	dir, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = dir.Close()
	}()

	return dir.ReadDir(-1)
}

func (s Store) readSource(root *os.Root, name string) (Source, error) {
	info, err := root.Lstat(name)
	if err != nil {
		if os.IsNotExist(err) {
			return Source{}, sourceError(CodeSourceNotFound, "faucet source %q was not found", name)
		}

		return Source{}, sourceError(CodeSourceReadFailed, "stat source %q: %v", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Source{}, sourceError(CodeSourceNotFound, "faucet source %q was not found", name)
	}

	verificationKey, err := readKeyMetadata(
		root,
		filepath.Join(name, verificationKeyFile),
		name,
		"verification",
		verificationKeyType,
	)
	if err != nil {
		return Source{}, err
	}
	signingKey, err := readKeyMetadata(
		root,
		filepath.Join(name, signingKeyFile),
		name,
		"signing",
		signingKeyType,
	)
	if err != nil {
		return Source{}, err
	}

	return Source{
		Name:                       name,
		Default:                    name == s.defaultName,
		VerificationKeyType:        verificationKey.Type,
		SigningKeyType:             signingKey.Type,
		VerificationKeyDescription: verificationKey.Description,
		SigningKeyDescription:      signingKey.Description,
	}, nil
}

func readKeyMetadata(
	root *os.Root,
	path string,
	sourceName string,
	keyKind string,
	expectedType string,
) (keyMetadata, error) {
	info, err := root.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return keyMetadata{}, sourceError(
				CodeSourceIncomplete,
				"faucet source %q is missing %s key",
				sourceName,
				keyKind,
			)
		}

		return keyMetadata{}, sourceError(
			CodeSourceReadFailed,
			"read %s key for source %q: %v",
			keyKind,
			sourceName,
			err,
		)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return keyMetadata{}, sourceError(
			CodeSourceInvalidKey,
			"%s key for source %q must not be a symlink",
			keyKind,
			sourceName,
		)
	}
	if !info.Mode().IsRegular() {
		return keyMetadata{}, sourceError(
			CodeSourceInvalidKey,
			"%s key for source %q must be a regular file",
			keyKind,
			sourceName,
		)
	}

	contents, err := root.ReadFile(path)
	if err != nil {
		return keyMetadata{}, sourceError(
			CodeSourceReadFailed,
			"read %s key for source %q: %v",
			keyKind,
			sourceName,
			err,
		)
	}

	var metadata keyMetadata
	if err := json.Unmarshal(contents, &metadata); err != nil {
		return keyMetadata{}, sourceError(
			CodeSourceInvalidKey,
			"parse %s key for source %q: %v",
			keyKind,
			sourceName,
			err,
		)
	}
	if strings.TrimSpace(metadata.Type) == "" {
		return keyMetadata{}, sourceError(
			CodeSourceInvalidKey,
			"%s key for source %q is missing type",
			keyKind,
			sourceName,
		)
	}
	if metadata.Type != expectedType {
		return keyMetadata{}, sourceError(
			CodeSourceInvalidKey,
			"%s key for source %q has type %q, want %q",
			keyKind,
			sourceName,
			metadata.Type,
			expectedType,
		)
	}

	return metadata, nil
}

func sourceError(code string, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}
