package sources

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/fxamacker/cbor/v2"
)

const (
	verificationKeyFile     = "utxo.vkey"
	signingKeyFile          = "utxo.skey"
	addressFile             = "utxo.addr"
	verificationKeyType     = "GenesisUTxOVerificationKey_ed25519"
	signingKeyType          = "GenesisUTxOSigningKey_ed25519"
	sourceDirectoryPathName = "."
	sourceNamePrefix        = "utxo"
	addressHRP              = "addr_test"
	addressPrefix           = addressHRP + "1"
	bech32Charset           = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
	maxSourceEntries        = 64
	maxKeyFileSize          = 4 * 1024
	maxAddressFileSize      = 512
	keyCBORBytesLength      = 32

	// CodeInvalidSourceName identifies a source name validation error.
	CodeInvalidSourceName = "invalid_source_name"
	// CodeSourceNotFound identifies a missing or unusable source.
	CodeSourceNotFound = "source_not_found"
	// CodeSourceIncomplete identifies a source missing required source files.
	CodeSourceIncomplete = "source_incomplete"
	// CodeSourceInvalidKey identifies malformed source file metadata.
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
	Address                    string `json:"address"`
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
	CBORHex     string `json:"cborHex"`
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
	if strings.ContainsFunc(name, unicode.IsControl) {
		return sourceError(CodeInvalidSourceName, "source name must not contain control characters")
	}
	if !strings.HasPrefix(name, sourceNamePrefix) || len(name) == len(sourceNamePrefix) {
		return sourceError(CodeInvalidSourceName, "source name must match utxo[1-9][0-9]*")
	}
	digits := name[len(sourceNamePrefix):]
	if digits[0] == '0' {
		return sourceError(CodeInvalidSourceName, "source name must match utxo[1-9][0-9]*")
	}
	for _, char := range digits {
		if char < '0' || char > '9' {
			return sourceError(CodeInvalidSourceName, "source name must match utxo[1-9][0-9]*")
		}
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

	entries, err := readDir(root, sourceDirectoryPathName, maxSourceEntries)
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

	if _, err := readDir(root, sourceDirectoryPathName, maxSourceEntries); err != nil {
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

func readDir(root *os.Root, name string, maxEntries int) ([]os.DirEntry, error) {
	dir, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = dir.Close()
	}()

	entries, err := dir.ReadDir(maxEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > maxEntries {
		return nil, fmt.Errorf("source directory has more than %d entries", maxEntries)
	}

	return entries, nil
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

	address, err := readAddress(root, filepath.Join(name, addressFile), name)
	if err != nil {
		return Source{}, err
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
		Address:                    address,
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
	contents, err := readRegularFile(root, path, maxKeyFileSize, sourceName, keyKind+" key")
	if err != nil {
		return keyMetadata{}, err
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
	if err := validateKeyCBOR(metadata.CBORHex, sourceName, keyKind); err != nil {
		return keyMetadata{}, err
	}

	return metadata, nil
}

func readAddress(root *os.Root, path string, sourceName string) (string, error) {
	contents, err := readRegularFile(root, path, maxAddressFileSize, sourceName, "address")
	if err != nil {
		return "", err
	}

	address := strings.TrimSpace(string(contents))
	if address == "" {
		return "", sourceError(CodeSourceInvalidKey, "address for source %q is empty", sourceName)
	}
	if strings.ContainsFunc(address, func(char rune) bool {
		return unicode.IsControl(char) || unicode.IsSpace(char)
	}) {
		return "", sourceError(CodeSourceInvalidKey, "address for source %q must not contain whitespace or control characters", sourceName)
	}
	if !strings.HasPrefix(address, addressPrefix) {
		return "", sourceError(CodeSourceInvalidKey, "address for source %q must start with %q", sourceName, addressPrefix)
	}
	if !validBech32(address) {
		return "", sourceError(CodeSourceInvalidKey, "address for source %q is not valid bech32", sourceName)
	}

	return address, nil
}

func readRegularFile(
	root *os.Root,
	path string,
	maxSize int64,
	sourceName string,
	fieldName string,
) ([]byte, error) {
	info, err := root.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, sourceError(
				CodeSourceIncomplete,
				"faucet source %q is missing %s",
				sourceName,
				fieldName,
			)
		}

		return nil, sourceError(
			CodeSourceReadFailed,
			"read %s for source %q: %v",
			fieldName,
			sourceName,
			err,
		)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, sourceError(
			CodeSourceInvalidKey,
			"%s for source %q must not be a symlink",
			fieldName,
			sourceName,
		)
	}
	if !info.Mode().IsRegular() {
		return nil, sourceError(
			CodeSourceInvalidKey,
			"%s for source %q must be a regular file",
			fieldName,
			sourceName,
		)
	}
	if info.Size() > maxSize {
		return nil, sourceError(
			CodeSourceInvalidKey,
			"%s for source %q is larger than %d bytes",
			fieldName,
			sourceName,
			maxSize,
		)
	}

	file, err := root.Open(path)
	if err != nil {
		return nil, sourceError(
			CodeSourceReadFailed,
			"read %s for source %q: %v",
			fieldName,
			sourceName,
			err,
		)
	}
	defer func() {
		_ = file.Close()
	}()

	contents, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, sourceError(
			CodeSourceReadFailed,
			"read %s for source %q: %v",
			fieldName,
			sourceName,
			err,
		)
	}
	if int64(len(contents)) > maxSize {
		return nil, sourceError(
			CodeSourceInvalidKey,
			"%s for source %q is larger than %d bytes",
			fieldName,
			sourceName,
			maxSize,
		)
	}

	return contents, nil
}

func validateKeyCBOR(cborHex string, sourceName string, keyKind string) error {
	cborHex = strings.TrimSpace(cborHex)
	if cborHex == "" {
		return sourceError(CodeSourceInvalidKey, "%s key for source %q is missing cborHex", keyKind, sourceName)
	}

	rawCBOR, err := hex.DecodeString(cborHex)
	if err != nil {
		return sourceError(CodeSourceInvalidKey, "decode %s key cborHex for source %q: %v", keyKind, sourceName, err)
	}

	var keyBytes []byte
	if err := cbor.Unmarshal(rawCBOR, &keyBytes); err != nil {
		return sourceError(CodeSourceInvalidKey, "parse %s key cborHex for source %q: %v", keyKind, sourceName, err)
	}
	if len(keyBytes) != keyCBORBytesLength {
		return sourceError(
			CodeSourceInvalidKey,
			"%s key cborHex for source %q decodes to %d bytes, want %d",
			keyKind,
			sourceName,
			len(keyBytes),
			keyCBORBytesLength,
		)
	}

	return nil
}

func validBech32(value string) bool {
	if value != strings.ToLower(value) {
		return false
	}

	separatorIndex := strings.LastIndexByte(value, '1')
	if separatorIndex != len(addressHRP) {
		return false
	}
	if value[:separatorIndex] != addressHRP {
		return false
	}

	data := value[separatorIndex+1:]
	if len(data) < 6 {
		return false
	}

	values := make([]byte, 0, len(addressHRP)*2+1+len(data))
	for _, char := range addressHRP {
		if char < 33 || char > 126 {
			return false
		}
		values = append(values, byte(char>>5))
	}
	values = append(values, 0)
	for _, char := range addressHRP {
		values = append(values, byte(char&31))
	}
	for _, char := range data {
		index := strings.IndexRune(bech32Charset, char)
		if index < 0 || index > 31 {
			return false
		}
		values = append(values, byte(index))
	}

	return bech32Polymod(values) == 1
}

func bech32Polymod(values []byte) uint32 {
	generators := [...]uint32{
		0x3b6a57b2,
		0x26508e6d,
		0x1ea119fa,
		0x3d4233dd,
		0x2a1462b3,
	}
	checksum := uint32(1)
	for _, value := range values {
		top := checksum >> 25
		checksum = (checksum&0x1ffffff)<<5 ^ uint32(value)
		for i, generator := range generators {
			if (top>>uint(i))&1 != 0 {
				checksum ^= generator
			}
		}
	}

	return checksum
}

func sourceError(code string, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}
