package artifactpublisher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	SchemaVersion = "yacd.meigma.io/cardano-network-artifacts/v1alpha1"

	AnnotationSchemaVersion       = "yacd.meigma.io/artifact-schema-version"
	AnnotationLocalnetFingerprint = "yacd.meigma.io/localnet-fingerprint"
	AnnotationDataHash            = "yacd.meigma.io/artifact-data-hash"

	envArtifactConfigMapName      = "YACD_ARTIFACT_CONFIGMAP_NAME"
	envArtifactConfigMapNamespace = "YACD_ARTIFACT_CONFIGMAP_NAMESPACE"
	envArtifactTokenPath          = "YACD_ARTIFACT_TOKEN_PATH"
	envArtifactCAPath             = "YACD_ARTIFACT_CA_PATH"
	envArtifactNamespacePath      = "YACD_ARTIFACT_NAMESPACE_PATH"
	envKubernetesAPIURL           = "YACD_KUBERNETES_API_URL"

	envLocalnetEnvDir       = "YACD_LOCALNET_ENV_DIR"
	envLocalnetManifestFile = "YACD_LOCALNET_PLAN_MANIFEST_FILE"

	envNetworkName          = "YACD_CARDANO_NETWORK_NAME"
	envNetworkNamespace     = "YACD_CARDANO_NETWORK_NAMESPACE"
	envNetworkMode          = "YACD_CARDANO_NETWORK_MODE"
	envNetworkEra           = "YACD_CARDANO_NETWORK_ERA"
	envNodeToNodeHost       = "YACD_CARDANO_NODE_TO_NODE_HOST"
	envNodeToNodePort       = "YACD_CARDANO_NODE_TO_NODE_PORT"
	envNodeToNodeURL        = "YACD_CARDANO_NODE_TO_NODE_URL"
	defaultServiceTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultServiceCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	defaultNamespacePath    = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

	contentTypeMergePatch = "application/merge-patch+json"
)

type artifactSource struct {
	key           string
	connectionKey string
	path          string
	optional      bool
}

var generatedArtifactSources = []artifactSource{
	{key: "configuration.yaml", connectionKey: "configuration", path: "configuration.yaml"},
	{key: "byron-genesis.json", connectionKey: "byronGenesis", path: "byron-genesis.json"},
	{key: "shelley-genesis.json", connectionKey: "shelleyGenesis", path: "shelley-genesis.json"},
	{key: "alonzo-genesis.json", connectionKey: "alonzoGenesis", path: "alonzo-genesis.json"},
	{key: "conway-genesis.json", connectionKey: "conwayGenesis", path: "conway-genesis.json"},
	{key: "dijkstra-genesis.json", connectionKey: "dijkstraGenesis", path: "dijkstra-genesis.json", optional: true},
	{key: "primary-topology.json", connectionKey: "primaryTopology", path: "node-data/node1/topology.json"},
}

type options struct {
	configMapName      string
	configMapNamespace string
	apiURL             string
	tokenPath          string
	caPath             string
	envDir             string
	manifestFile       string
	networkName        string
	networkNamespace   string
	networkMode        string
	networkEra         string
	nodeToNodeHost     string
	nodeToNodePort     int
	nodeToNodeURL      string
}

type localnetManifest struct {
	Inputs struct {
		NetworkMagic *int64 `json:"networkMagic"`
	} `json:"inputs"`
	Fingerprint struct {
		Algorithm string `json:"algorithm"`
		Value     string `json:"value"`
	} `json:"fingerprint"`
}

type connectionDocument struct {
	SchemaVersion     string             `json:"schemaVersion"`
	Network           connectionNetwork  `json:"network"`
	PrimaryNodeToNode connectionEndpoint `json:"primaryNodeToNode"`
	Files             map[string]string  `json:"files"`
}

type connectionNetwork struct {
	Name                string `json:"name"`
	Namespace           string `json:"namespace"`
	Mode                string `json:"mode"`
	NetworkMagic        int64  `json:"networkMagic"`
	Era                 string `json:"era"`
	LocalnetFingerprint string `json:"localnetFingerprint"`
}

type connectionEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	URL  string `json:"url"`
}

type configMapPatch struct {
	Metadata configMapMetadataPatch `json:"metadata"`
	Data     map[string]string      `json:"data"`
}

type configMapMetadataPatch struct {
	Annotations map[string]string `json:"annotations"`
}

// Environ returns the current process environment as a map.
func Environ() map[string]string {
	env := make(map[string]string)
	for _, pair := range os.Environ() {
		name, value, ok := strings.Cut(pair, "=")
		if ok {
			env[name] = value
		}
	}
	return env
}

// Run publishes the generated localnet artifacts into one pre-created
// ConfigMap.
func Run(ctx context.Context, args []string, env map[string]string, stdout io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(args, " "))
	}

	opts, err := optionsFromEnv(env)
	if err != nil {
		return err
	}

	data, annotations, err := buildPatchData(opts)
	if err != nil {
		return err
	}

	if err := patchConfigMap(ctx, opts, configMapPatch{
		Metadata: configMapMetadataPatch{Annotations: annotations},
		Data:     data,
	}); err != nil {
		return err
	}

	if stdout != nil {
		fmt.Fprintf(stdout, "published Cardano network artifacts to ConfigMap %s/%s with data hash %s\n",
			opts.configMapNamespace, opts.configMapName, annotations[AnnotationDataHash])
	}

	return nil
}

func optionsFromEnv(env map[string]string) (options, error) {
	opts := options{
		configMapName:    strings.TrimSpace(env[envArtifactConfigMapName]),
		tokenPath:        envDefault(env, envArtifactTokenPath, defaultServiceTokenPath),
		caPath:           envDefault(env, envArtifactCAPath, defaultServiceCAPath),
		envDir:           strings.TrimSpace(env[envLocalnetEnvDir]),
		manifestFile:     strings.TrimSpace(env[envLocalnetManifestFile]),
		networkName:      strings.TrimSpace(env[envNetworkName]),
		networkMode:      strings.TrimSpace(env[envNetworkMode]),
		networkEra:       strings.TrimSpace(env[envNetworkEra]),
		nodeToNodeHost:   strings.TrimSpace(env[envNodeToNodeHost]),
		nodeToNodeURL:    strings.TrimSpace(env[envNodeToNodeURL]),
		networkNamespace: strings.TrimSpace(env[envNetworkNamespace]),
	}

	var err error
	opts.configMapNamespace = strings.TrimSpace(env[envArtifactConfigMapNamespace])
	if opts.configMapNamespace == "" {
		opts.configMapNamespace, err = readTrimmedFile(envDefault(env, envArtifactNamespacePath, defaultNamespacePath))
		if err != nil {
			return options{}, fmt.Errorf("resolve artifact ConfigMap namespace: %w", err)
		}
	}
	if opts.networkNamespace == "" {
		opts.networkNamespace = opts.configMapNamespace
	}

	opts.apiURL, err = kubernetesAPIURL(env)
	if err != nil {
		return options{}, err
	}

	port := strings.TrimSpace(env[envNodeToNodePort])
	if port != "" {
		parsedPort, parseErr := strconv.Atoi(port)
		if parseErr != nil || parsedPort < 1 || parsedPort > 65535 {
			return options{}, fmt.Errorf("%s must be a TCP port between 1 and 65535", envNodeToNodePort)
		}
		opts.nodeToNodePort = parsedPort
	}
	if opts.nodeToNodeURL == "" && opts.nodeToNodeHost != "" && opts.nodeToNodePort != 0 {
		opts.nodeToNodeURL = fmt.Sprintf("tcp://%s:%d", opts.nodeToNodeHost, opts.nodeToNodePort)
	}

	if err := opts.validate(); err != nil {
		return options{}, err
	}

	return opts, nil
}

func (o options) validate() error {
	required := map[string]string{
		envArtifactConfigMapName: o.configMapName,
		envLocalnetEnvDir:        o.envDir,
		envLocalnetManifestFile:  o.manifestFile,
		envNetworkName:           o.networkName,
		envNetworkMode:           o.networkMode,
		envNetworkEra:            o.networkEra,
		envNodeToNodeHost:        o.nodeToNodeHost,
		envNodeToNodeURL:         o.nodeToNodeURL,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if o.configMapNamespace == "" {
		return fmt.Errorf("%s or %s is required", envArtifactConfigMapNamespace, envArtifactNamespacePath)
	}
	if o.networkNamespace == "" {
		return fmt.Errorf("%s is required", envNetworkNamespace)
	}
	if o.nodeToNodePort == 0 {
		return fmt.Errorf("%s is required", envNodeToNodePort)
	}
	if o.tokenPath == "" {
		return fmt.Errorf("%s is required", envArtifactTokenPath)
	}
	if o.apiURL == "" {
		return fmt.Errorf("%s or Kubernetes service host/port environment is required", envKubernetesAPIURL)
	}
	return nil
}

func envDefault(env map[string]string, name, defaultValue string) string {
	value := strings.TrimSpace(env[name])
	if value == "" {
		return defaultValue
	}
	return value
}

func kubernetesAPIURL(env map[string]string) (string, error) {
	if apiURL := strings.TrimSpace(env[envKubernetesAPIURL]); apiURL != "" {
		return strings.TrimRight(apiURL, "/"), nil
	}

	host := strings.TrimSpace(env["KUBERNETES_SERVICE_HOST"])
	port := strings.TrimSpace(env["KUBERNETES_SERVICE_PORT_HTTPS"])
	if port == "" {
		port = strings.TrimSpace(env["KUBERNETES_SERVICE_PORT"])
	}
	if port == "" {
		port = "443"
	}
	if host == "" {
		return "", nil
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return "https://" + host + ":" + port, nil
}

func buildPatchData(opts options) (map[string]string, map[string]string, error) {
	data := make(map[string]string, len(generatedArtifactSources)+2)
	fileKeys := make(map[string]string, len(generatedArtifactSources)+2)

	for _, source := range generatedArtifactSources {
		if err := validatePublicArtifactSource(source); err != nil {
			return nil, nil, err
		}

		content, err := readArtifactTextFile(path.Join(opts.envDir, source.path))
		if errors.Is(err, os.ErrNotExist) && source.optional {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read artifact %s: %w", source.path, err)
		}
		data[source.key] = content
		fileKeys[source.connectionKey] = source.key
	}

	manifestContent, manifest, err := readLocalnetManifest(opts.envDir, opts.manifestFile)
	if err != nil {
		return nil, nil, err
	}
	data["yacd-localnet-plan.json"] = manifestContent
	fileKeys["localnetPlan"] = "yacd-localnet-plan.json"
	fileKeys["connection"] = "connection.json"

	connectionJSON, err := buildConnectionJSON(opts, manifest, fileKeys)
	if err != nil {
		return nil, nil, err
	}
	data["connection.json"] = connectionJSON

	hash := computeDataHash(data)
	annotations := map[string]string{
		AnnotationSchemaVersion:       SchemaVersion,
		AnnotationLocalnetFingerprint: manifest.Fingerprint.Value,
		AnnotationDataHash:            hash,
	}

	return data, annotations, nil
}

func validatePublicArtifactSource(source artifactSource) error {
	if strings.TrimSpace(source.key) == "" {
		return fmt.Errorf("artifact source has empty ConfigMap key")
	}
	if strings.TrimSpace(source.path) == "" {
		return fmt.Errorf("artifact source %s has empty path", source.key)
	}

	clean := path.Clean(source.path)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return fmt.Errorf("artifact source %s must stay within the localnet environment", source.path)
	}

	deniedComponents := map[string]struct{}{
		"byron-gen-command": {},
		"delegate-keys":     {},
		"drep-keys":         {},
		"faucet-keys":       {},
		"genesis-keys":      {},
		"keys":              {},
		"pools-keys":        {},
		"secrets":           {},
		"stake-delegators":  {},
		"utxo-keys":         {},
	}
	for _, part := range strings.Split(clean, "/") {
		if _, denied := deniedComponents[strings.ToLower(part)]; denied {
			return fmt.Errorf("artifact source %s is under secret/key material", source.path)
		}
	}

	deniedExtensions := map[string]struct{}{
		".cert":    {},
		".counter": {},
		".skey":    {},
		".vkey":    {},
	}
	if _, denied := deniedExtensions[strings.ToLower(path.Ext(clean))]; denied {
		return fmt.Errorf("artifact source %s is key material", source.path)
	}

	return nil
}

func readLocalnetManifest(envDir, manifestFile string) (string, localnetManifest, error) {
	if err := requirePathUnderEnv(envDir, manifestFile); err != nil {
		return "", localnetManifest{}, fmt.Errorf("validate localnet manifest path: %w", err)
	}

	content, err := readArtifactTextFile(manifestFile)
	if err != nil {
		return "", localnetManifest{}, fmt.Errorf("read localnet manifest: %w", err)
	}

	var manifest localnetManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return "", localnetManifest{}, fmt.Errorf("parse localnet manifest: %w", err)
	}
	if manifest.Inputs.NetworkMagic == nil {
		return "", localnetManifest{}, fmt.Errorf("localnet manifest inputs.networkMagic is required")
	}
	if strings.TrimSpace(manifest.Fingerprint.Value) == "" {
		return "", localnetManifest{}, fmt.Errorf("localnet manifest fingerprint.value is required")
	}

	return content, manifest, nil
}

func requirePathUnderEnv(envDir, filePath string) error {
	envDir = path.Clean(envDir)
	filePath = path.Clean(filePath)
	if envDir == "." || filePath == "." || !strings.HasPrefix(filePath, "/") {
		return fmt.Errorf("manifest file must be an absolute path")
	}
	if filePath != path.Join(envDir, "yacd-localnet-plan.json") {
		return fmt.Errorf("manifest file must be %s", path.Join(envDir, "yacd-localnet-plan.json"))
	}
	return nil
}

func readArtifactTextFile(filePath string) (string, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(raw) {
		return "", fmt.Errorf("file is not valid UTF-8")
	}
	return string(raw), nil
}

func buildConnectionJSON(opts options, manifest localnetManifest, fileKeys map[string]string) (string, error) {
	connection := connectionDocument{
		SchemaVersion: SchemaVersion,
		Network: connectionNetwork{
			Name:                opts.networkName,
			Namespace:           opts.networkNamespace,
			Mode:                opts.networkMode,
			NetworkMagic:        *manifest.Inputs.NetworkMagic,
			Era:                 opts.networkEra,
			LocalnetFingerprint: manifest.Fingerprint.Value,
		},
		PrimaryNodeToNode: connectionEndpoint{
			Host: opts.nodeToNodeHost,
			Port: opts.nodeToNodePort,
			URL:  opts.nodeToNodeURL,
		},
		Files: fileKeys,
	}

	raw, err := json.MarshalIndent(connection, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal connection.json: %w", err)
	}
	return string(raw) + "\n", nil
}

func computeDataHash(data map[string]string) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	digest := sha256.New()
	for _, key := range keys {
		value := data[key]
		fmt.Fprintf(digest, "%d:%s\n%d:", len(key), key, len(value))
		io.WriteString(digest, value)
		io.WriteString(digest, "\n")
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func patchConfigMap(ctx context.Context, opts options, patch configMapPatch) error {
	body, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshal ConfigMap patch: %w", err)
	}

	token, err := readTrimmedFile(opts.tokenPath)
	if err != nil {
		return fmt.Errorf("read service account token: %w", err)
	}
	if token == "" {
		return fmt.Errorf("service account token is empty")
	}

	endpoint := opts.apiURL +
		"/api/v1/namespaces/" + url.PathEscape(opts.configMapNamespace) +
		"/configmaps/" + url.PathEscape(opts.configMapName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build ConfigMap patch request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentTypeMergePatch)
	req.Header.Set("Accept", "application/json")

	client, err := httpClient(opts)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("patch ConfigMap %s/%s: %w", opts.configMapNamespace, opts.configMapName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("patch ConfigMap %s/%s returned %s: %s",
			opts.configMapNamespace, opts.configMapName, resp.Status, strings.TrimSpace(string(responseBody)))
	}

	return nil
}

func httpClient(opts options) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	parsedURL, err := url.Parse(opts.apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse Kubernetes API URL: %w", err)
	}

	if parsedURL.Scheme == "https" {
		caCert, err := os.ReadFile(opts.caPath)
		if err != nil {
			return nil, fmt.Errorf("read Kubernetes API CA bundle: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("Kubernetes API CA bundle does not contain PEM certificates")
		}
		transport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

func readTrimmedFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}
