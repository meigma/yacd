package cardanonetwork

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	cardanonetworkartifacts "github.com/meigma/yacd/internal/cardano/networkartifacts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// nodeSyncSourceOgmios is the wire value published in Status.Sync.Source
	// when the payload was derived from Ogmios health.
	nodeSyncSourceOgmios = "ogmios"
	// nodeSyncProbeTimeout bounds a single Ogmios health request.
	nodeSyncProbeTimeout = 3 * time.Second
	// nodeSyncLagThreshold is the maximum inferred tip lag that still reports
	// NodeSynchronized=True.
	nodeSyncLagThreshold = 10 * time.Minute
	// nodeSyncStalledAfter is the maximum age of the last tip update before a
	// lagging node is reported stalled.
	nodeSyncStalledAfter = 10 * time.Minute
	// nodeSyncProbeRequeueAfter keeps sync visibility fresh while Ogmios is
	// enabled.
	nodeSyncProbeRequeueAfter = time.Minute
	// nodeSyncNetworkSynchronizationReadyThreshold is the Ogmios synchronization
	// estimate that is close enough to tip to report synchronized.
	nodeSyncNetworkSynchronizationReadyThreshold = 0.99999
	// ogmiosConnectionStatusConnected is the Ogmios health connectionStatus
	// value for a connected node.
	ogmiosConnectionStatusConnected = "connected"
)

// cardanoNetworkSyncProber is the narrow port used by CardanoNetwork to read
// Ogmios health. Tests replace it so controller behavior can be exercised
// without depending on real cluster DNS or an Ogmios process.
type cardanoNetworkSyncProber interface {
	Probe(context.Context, string) (cardanoNetworkOgmiosHealth, error)
}

// cardanoNetworkSyncProberFunc adapts a function to [cardanoNetworkSyncProber]
// for tests.
type cardanoNetworkSyncProberFunc func(context.Context, string) (cardanoNetworkOgmiosHealth, error)

// Probe implements [cardanoNetworkSyncProber].
func (f cardanoNetworkSyncProberFunc) Probe(ctx context.Context, ogmiosURL string) (cardanoNetworkOgmiosHealth, error) {
	return f(ctx, ogmiosURL)
}

// defaultCardanoNetworkSyncProber calls Ogmios /health over HTTP. queryOgmios is
// intentionally test-only: it lets unit tests exercise condition projection
// without standing up an HTTP server for every case.
type defaultCardanoNetworkSyncProber struct {
	// httpClient is used for Ogmios requests. http.DefaultClient is used when
	// this field is nil.
	httpClient *http.Client
	// queryOgmios, when non-nil, replaces the live Ogmios HTTP request.
	queryOgmios func(context.Context, string) (cardanoNetworkOgmiosHealth, error)
}

// cardanoNetworkOgmiosHealth is the subset of Ogmios health used to publish
// CardanoNetwork node sync status.
type cardanoNetworkOgmiosHealth struct {
	// ConnectionStatus is the Ogmios health connectionStatus value.
	ConnectionStatus string
	// Tip is the last known tip reported by Ogmios health.
	Tip cardanoNetworkOgmiosTip
	// LastTipUpdate is the last known tip update timestamp.
	LastTipUpdate time.Time
	// NetworkSynchronization is Ogmios' optional 0..1 synchronization estimate.
	NetworkSynchronization *float64
}

// cardanoNetworkOgmiosTip is the last known tip fragment of an Ogmios health
// response.
type cardanoNetworkOgmiosTip struct {
	// Slot is the last known tip slot.
	Slot int64
	// BlockHeight is the optional tip block height.
	BlockHeight *int64
	// Hash is the optional tip hash.
	Hash string
}

// cardanoNetworkTiming is the Shelley genesis timing needed to infer wall-clock
// network slots.
type cardanoNetworkTiming struct {
	// SystemStart is the network start time.
	SystemStart time.Time
	// SlotLengthSeconds is the slot duration in seconds.
	SlotLengthSeconds float64
}

// cardanoNetworkSyncProber returns the configured sync prober.
func (r *CardanoNetworkReconciler) cardanoNetworkSyncProber() cardanoNetworkSyncProber {
	if r.syncProberOverride != nil {
		return r.syncProberOverride
	}

	return defaultCardanoNetworkSyncProber{httpClient: http.DefaultClient}
}

// primaryNodeSyncStatusConditions derives Status.Sync plus node sync conditions
// from the live Ogmios Service and verified artifact ConfigMap.
func (r *CardanoNetworkReconciler) primaryNodeSyncStatusConditions(
	ctx context.Context,
	network *yacdv1alpha1.CardanoNetwork,
	ogmiosService *corev1.Service,
	networkArtifactsConfigMap *corev1.ConfigMap,
	artifactsReady bool,
	artifactsMessage string,
) (*yacdv1alpha1.CardanoNetworkSyncStatus, metav1.Condition, metav1.Condition) {
	if ogmiosService == nil {
		return nodeSyncUnavailableConditions(conditionReasonOgmiosDisabled, conditionMessageOgmiosDisabled)
	}
	if !artifactsReady {
		if strings.TrimSpace(artifactsMessage) == "" {
			artifactsMessage = "Network artifact timing is unavailable until artifacts are verified"
		}
		return nodeSyncUnavailableConditions(conditionReasonNetworkTimingUnavailable, artifactsMessage)
	}

	timing, err := cardanoNetworkTimingFromArtifacts(networkArtifactsConfigMap)
	if err != nil {
		return nodeSyncUnavailableConditions(conditionReasonNetworkTimingUnavailable, fmt.Sprintf("Network timing is unavailable: %v", err))
	}

	ogmiosURL, err := cardanoNetworkOgmiosEndpointURL(network, ogmiosService)
	if err != nil {
		return nodeSyncUnavailableConditions(conditionReasonOgmiosHealthUnavailable, err.Error())
	}

	healthCtx, cancel := context.WithTimeout(ctx, nodeSyncProbeTimeout)
	health, err := r.cardanoNetworkSyncProber().Probe(healthCtx, ogmiosURL)
	cancel()
	if err != nil {
		return nodeSyncUnavailableConditions(conditionReasonOgmiosHealthUnavailable, fmt.Sprintf("Ogmios health is unavailable: %v", err))
	}

	observedAt := r.now()
	syncStatus := cardanoNetworkSyncStatusFromHealth(health, timing, observedAt)
	if !strings.EqualFold(health.ConnectionStatus, ogmiosConnectionStatusConnected) {
		message := fmt.Sprintf("Ogmios is %q, not connected", health.ConnectionStatus)
		return syncStatus,
			nodeSynchronizedCondition(metav1.ConditionFalse, conditionReasonOgmiosDisconnected, message),
			nodeProgressingCondition(metav1.ConditionFalse, conditionReasonOgmiosDisconnected, message)
	}

	nodeSynchronized, nodeProgressing := cardanoNetworkSyncConditions(syncStatus, health, observedAt)
	return syncStatus, nodeSynchronized, nodeProgressing
}

// nodeSyncUnavailableConditions returns failure conditions and no sync payload
// for probe prerequisites or probe failures.
func nodeSyncUnavailableConditions(reason conditionReason, message string) (*yacdv1alpha1.CardanoNetworkSyncStatus, metav1.Condition, metav1.Condition) {
	return nil,
		nodeSynchronizedCondition(metav1.ConditionFalse, reason, message),
		nodeProgressingCondition(metav1.ConditionFalse, reason, message)
}

// cardanoNetworkSyncConditions projects an Ogmios health sample and computed
// lag into NodeSynchronized and NodeProgressing conditions.
func cardanoNetworkSyncConditions(
	syncStatus *yacdv1alpha1.CardanoNetworkSyncStatus,
	health cardanoNetworkOgmiosHealth,
	observedAt time.Time,
) (metav1.Condition, metav1.Condition) {
	lagSynchronized := syncStatus.LagSeconds != nil && *syncStatus.LagSeconds <= int64(nodeSyncLagThreshold/time.Second)
	ogmiosSynchronized := health.NetworkSynchronization != nil && *health.NetworkSynchronization >= nodeSyncNetworkSynchronizationReadyThreshold
	if lagSynchronized || ogmiosSynchronized {
		return nodeSynchronizedCondition(metav1.ConditionTrue, conditionReasonNodeSynchronized, conditionMessageNodeSynchronized),
			nodeProgressingCondition(metav1.ConditionTrue, conditionReasonNodeSynchronized, conditionMessageNodeProgressing)
	}

	lagSlots := int64(0)
	if syncStatus.LagSlots != nil {
		lagSlots = *syncStatus.LagSlots
	}
	lagSeconds := int64(0)
	if syncStatus.LagSeconds != nil {
		lagSeconds = *syncStatus.LagSeconds
	}

	if observedAt.Sub(health.LastTipUpdate) > nodeSyncStalledAfter {
		stalledFor := observedAt.Sub(health.LastTipUpdate).Round(time.Second)
		message := fmt.Sprintf("Primary node is %d slots (%ds) behind the inferred network tip and has not advanced for %s", lagSlots, lagSeconds, stalledFor)
		return nodeSynchronizedCondition(metav1.ConditionFalse, conditionReasonNodeSyncStalled, message),
			nodeProgressingCondition(metav1.ConditionFalse, conditionReasonNodeSyncStalled, message)
	}

	message := fmt.Sprintf("Primary node is %d slots (%ds) behind the inferred network tip", lagSlots, lagSeconds)
	return nodeSynchronizedCondition(metav1.ConditionFalse, conditionReasonNodeCatchingUp, message),
		nodeProgressingCondition(metav1.ConditionTrue, conditionReasonNodeCatchingUp, conditionMessageNodeProgressing)
}

// cardanoNetworkSyncStatusFromHealth converts Ogmios health and network timing
// into the CardanoNetwork Status.Sync payload.
func cardanoNetworkSyncStatusFromHealth(
	health cardanoNetworkOgmiosHealth,
	timing cardanoNetworkTiming,
	observedAt time.Time,
) *yacdv1alpha1.CardanoNetworkSyncStatus {
	observed := metav1TimePtr(observedAt)
	lastTipUpdate := metav1TimePtr(health.LastTipUpdate)
	tipSlot := health.Tip.Slot
	inferredTipSlot := timing.inferredTipSlot(observedAt)
	lagSlots := max(inferredTipSlot-tipSlot, 0)
	lagSeconds := int64(math.Ceil(float64(lagSlots) * timing.SlotLengthSeconds))

	status := &yacdv1alpha1.CardanoNetworkSyncStatus{
		Source:           nodeSyncSourceOgmios,
		ConnectionStatus: health.ConnectionStatus,
		Tip: &yacdv1alpha1.CardanoNetworkTipStatus{
			Slot:        &tipSlot,
			BlockHeight: health.Tip.BlockHeight,
			Hash:        health.Tip.Hash,
		},
		LastTipUpdate:   lastTipUpdate,
		ObservedAt:      observed,
		InferredTipSlot: &inferredTipSlot,
		LagSlots:        &lagSlots,
		LagSeconds:      &lagSeconds,
	}
	if health.NetworkSynchronization != nil {
		networkSynchronization := roundFloat(*health.NetworkSynchronization, 5)
		status.NetworkSynchronization = &networkSynchronization
	}

	return status
}

// inferredTipSlot returns the slot implied by the elapsed wall-clock time since
// system start.
func (t cardanoNetworkTiming) inferredTipSlot(observedAt time.Time) int64 {
	elapsed := observedAt.Sub(t.SystemStart).Seconds()
	if elapsed <= 0 {
		return 0
	}

	return int64(math.Floor(elapsed / t.SlotLengthSeconds))
}

// cardanoNetworkTimingFromArtifacts parses Shelley genesis timing from a
// verified network artifact ConfigMap.
func cardanoNetworkTimingFromArtifacts(configMap *corev1.ConfigMap) (cardanoNetworkTiming, error) {
	if configMap == nil {
		return cardanoNetworkTiming{}, errors.New("artifact ConfigMap is missing")
	}
	raw := strings.TrimSpace(configMap.Data[cardanonetworkartifacts.ShelleyGenesisKey])
	if raw == "" {
		return cardanoNetworkTiming{}, fmt.Errorf("%s is missing", cardanonetworkartifacts.ShelleyGenesisKey)
	}

	return parseShelleyGenesisTiming([]byte(raw))
}

// parseShelleyGenesisTiming extracts systemStart and slotLength from
// shelley-genesis.json data.
func parseShelleyGenesisTiming(data []byte) (cardanoNetworkTiming, error) {
	var raw map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return cardanoNetworkTiming{}, err
	}

	systemStartRaw, ok := raw["systemStart"]
	if !ok {
		return cardanoNetworkTiming{}, errors.New("systemStart is missing")
	}
	systemStartValue, ok := jsonString(systemStartRaw)
	if !ok || strings.TrimSpace(systemStartValue) == "" {
		return cardanoNetworkTiming{}, errors.New("systemStart must be a string")
	}
	systemStart, err := time.Parse(time.RFC3339Nano, systemStartValue)
	if err != nil {
		return cardanoNetworkTiming{}, fmt.Errorf("systemStart is invalid: %w", err)
	}

	slotLengthRaw, ok := raw["slotLength"]
	if !ok {
		return cardanoNetworkTiming{}, errors.New("slotLength is missing")
	}
	slotLength, ok := jsonFloat64(slotLengthRaw)
	if !ok {
		return cardanoNetworkTiming{}, errors.New("slotLength must be a number")
	}
	if slotLength <= 0 {
		return cardanoNetworkTiming{}, errors.New("slotLength must be greater than zero")
	}

	return cardanoNetworkTiming{
		SystemStart:       systemStart,
		SlotLengthSeconds: slotLength,
	}, nil
}

// cardanoNetworkOgmiosEndpointURL renders the in-cluster Ogmios Service URL
// used by the sync prober.
func cardanoNetworkOgmiosEndpointURL(network *yacdv1alpha1.CardanoNetwork, service *corev1.Service) (string, error) {
	if network == nil {
		return "", errors.New("cardano network is missing")
	}
	if service == nil {
		return "", errors.New("ogmios service is missing")
	}
	if len(service.Spec.Ports) == 0 {
		return "", fmt.Errorf("ogmios service %s/%s has no ports", service.Namespace, service.Name)
	}

	return fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", ogmiosServiceURLType, service.Name, service.Namespace, service.Spec.Ports[0].Port), nil
}

// Probe fetches and parses Ogmios /health for a published Ogmios endpoint.
func (p defaultCardanoNetworkSyncProber) Probe(ctx context.Context, ogmiosURL string) (cardanoNetworkOgmiosHealth, error) {
	if p.queryOgmios != nil {
		return p.queryOgmios(ctx, ogmiosURL)
	}
	if strings.TrimSpace(ogmiosURL) == "" {
		return cardanoNetworkOgmiosHealth{}, errors.New("ogmios endpoint is not published")
	}

	httpURL, err := ogmiosHTTPURL(ogmiosURL)
	if err != nil {
		return cardanoNetworkOgmiosHealth{}, err
	}
	healthURL, err := ogmiosHealthURL(httpURL)
	if err != nil {
		return cardanoNetworkOgmiosHealth{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return cardanoNetworkOgmiosHealth{}, err
	}
	client := p.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return cardanoNetworkOgmiosHealth{}, err
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return cardanoNetworkOgmiosHealth{}, fmt.Errorf("ogmios returned HTTP %d", response.StatusCode)
	}

	var health json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		return cardanoNetworkOgmiosHealth{}, err
	}
	if len(health) == 0 {
		return cardanoNetworkOgmiosHealth{}, errors.New("ogmios health response is empty")
	}

	return parseOgmiosHealth(health)
}

// ogmiosHTTPURL converts an Ogmios websocket URL into the matching HTTP URL
// accepted by the /health endpoint.
func ogmiosHTTPURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "ws":
		parsed.Scheme = "http"
	case "wss":
		parsed.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported Ogmios URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", errors.New("ogmios URL host is empty")
	}

	return parsed.String(), nil
}

// ogmiosHealthURL appends /health to an Ogmios HTTP URL and strips query and
// fragment components.
func ogmiosHealthURL(httpURL string) (string, error) {
	parsed, err := url.Parse(httpURL)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/health"
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}

// parseOgmiosHealth parses the health fields the controller needs while
// accepting minor Ogmios field-name variants.
func parseOgmiosHealth(data json.RawMessage) (cardanoNetworkOgmiosHealth, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		return cardanoNetworkOgmiosHealth{}, err
	}
	raw, ok := result.(map[string]any)
	if !ok {
		return cardanoNetworkOgmiosHealth{}, errors.New("ogmios health response must be an object")
	}

	connectionStatus, ok := jsonString(raw["connectionStatus"])
	if !ok || strings.TrimSpace(connectionStatus) == "" {
		return cardanoNetworkOgmiosHealth{}, errors.New("ogmios health response missing connectionStatus")
	}

	lastTipUpdateValue, ok := jsonString(raw["lastTipUpdate"])
	if !ok || strings.TrimSpace(lastTipUpdateValue) == "" {
		return cardanoNetworkOgmiosHealth{}, errors.New("ogmios health response missing lastTipUpdate")
	}
	lastTipUpdate, err := time.Parse(time.RFC3339Nano, lastTipUpdateValue)
	if err != nil {
		return cardanoNetworkOgmiosHealth{}, fmt.Errorf("ogmios lastTipUpdate is invalid: %w", err)
	}

	tipRaw, ok := raw["lastKnownTip"].(map[string]any)
	if !ok {
		return cardanoNetworkOgmiosHealth{}, errors.New("ogmios health response missing lastKnownTip")
	}
	slot, ok := jsonInt64(tipRaw["slot"])
	if !ok {
		return cardanoNetworkOgmiosHealth{}, errors.New("ogmios lastKnownTip missing slot")
	}
	var blockHeight *int64
	if height, ok := firstJSONInt64(tipRaw, "height", "blockHeight"); ok {
		blockHeight = &height
	}
	hash := ""
	if value, ok := firstJSONString(tipRaw, "hash", "id"); ok {
		hash = value
	}

	var networkSynchronization *float64
	if rawNetworkSynchronization, ok := raw["networkSynchronization"]; ok {
		value, ok := jsonFloat64(rawNetworkSynchronization)
		if !ok {
			return cardanoNetworkOgmiosHealth{}, errors.New("ogmios networkSynchronization must be a number")
		}
		if value < 0 || value > 1 {
			return cardanoNetworkOgmiosHealth{}, errors.New("ogmios networkSynchronization must be between 0 and 1")
		}
		networkSynchronization = &value
	}

	return cardanoNetworkOgmiosHealth{
		ConnectionStatus: connectionStatus,
		Tip: cardanoNetworkOgmiosTip{
			Slot:        slot,
			BlockHeight: blockHeight,
			Hash:        hash,
		},
		LastTipUpdate:          lastTipUpdate,
		NetworkSynchronization: networkSynchronization,
	}, nil
}

// firstJSONInt64 returns the first named int64 field found in values.
func firstJSONInt64(values map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		if value, ok := jsonInt64(values[key]); ok {
			return value, true
		}
	}

	return 0, false
}

// firstJSONString returns the first named string field found in values.
func firstJSONString(values map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := jsonString(values[key]); ok {
			return value, true
		}
	}

	return "", false
}

// jsonString converts a decoded JSON value to a string.
func jsonString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	default:
		return "", false
	}
}

// jsonInt64 converts a decoded JSON value to an int64 without accepting
// fractional numbers.
func jsonInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Int64()
		return result, err == nil
	case float64:
		result := int64(typed)
		return result, typed == float64(result)
	default:
		return 0, false
	}
}

// jsonFloat64 converts a decoded JSON value to a float64.
func jsonFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Float64()
		return result, err == nil
	case float64:
		return typed, true
	case string:
		result, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return result, err == nil
	default:
		return 0, false
	}
}

// roundFloat rounds value to the requested decimal places.
func roundFloat(value float64, places int) float64 {
	scale := math.Pow10(places)
	return math.Round(value*scale) / scale
}

// metav1TimePtr converts a Go time into a Kubernetes timestamp pointer.
func metav1TimePtr(value time.Time) *metav1.Time {
	result := metav1.NewTime(value.UTC())
	return &result
}
