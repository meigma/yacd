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
	nodeSyncSourceOgmios                         = "ogmios"
	nodeSyncProbeTimeout                         = 3 * time.Second
	nodeSyncLagThreshold                         = 10 * time.Minute
	nodeSyncStalledAfter                         = 10 * time.Minute
	nodeSyncProbeRequeueAfter                    = time.Minute
	nodeSyncNetworkSynchronizationReadyThreshold = 0.99999
	ogmiosConnectionStatusConnected              = "connected"
)

// cardanoNetworkSyncProber is the narrow port used by CardanoNetwork to read
// Ogmios health. Tests replace it so controller behavior can be exercised
// without depending on real cluster DNS or an Ogmios process.
type cardanoNetworkSyncProber interface {
	Probe(context.Context, string) (cardanoNetworkOgmiosHealth, error)
}

type cardanoNetworkSyncProberFunc func(context.Context, string) (cardanoNetworkOgmiosHealth, error)

func (f cardanoNetworkSyncProberFunc) Probe(ctx context.Context, ogmiosURL string) (cardanoNetworkOgmiosHealth, error) {
	return f(ctx, ogmiosURL)
}

// defaultCardanoNetworkSyncProber calls Ogmios /health over HTTP. queryOgmios is
// intentionally test-only: it lets unit tests exercise condition projection
// without standing up an HTTP server for every case.
type defaultCardanoNetworkSyncProber struct {
	httpClient  *http.Client
	queryOgmios func(context.Context, string) (cardanoNetworkOgmiosHealth, error)
}

type cardanoNetworkOgmiosHealth struct {
	ConnectionStatus       string
	Tip                    cardanoNetworkOgmiosTip
	LastTipUpdate          time.Time
	NetworkSynchronization *float64
}

type cardanoNetworkOgmiosTip struct {
	Slot        int64
	BlockHeight *int64
	Hash        string
}

type cardanoNetworkTiming struct {
	SystemStart       time.Time
	SlotLengthSeconds float64
}

func (r *CardanoNetworkReconciler) cardanoNetworkSyncProber() cardanoNetworkSyncProber {
	if r.syncProberOverride != nil {
		return r.syncProberOverride
	}

	return defaultCardanoNetworkSyncProber{httpClient: http.DefaultClient}
}

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

func nodeSyncUnavailableConditions(reason conditionReason, message string) (*yacdv1alpha1.CardanoNetworkSyncStatus, metav1.Condition, metav1.Condition) {
	return nil,
		nodeSynchronizedCondition(metav1.ConditionFalse, reason, message),
		nodeProgressingCondition(metav1.ConditionFalse, reason, message)
}

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

func (t cardanoNetworkTiming) inferredTipSlot(observedAt time.Time) int64 {
	elapsed := observedAt.Sub(t.SystemStart).Seconds()
	if elapsed <= 0 {
		return 0
	}

	return int64(math.Floor(elapsed / t.SlotLengthSeconds))
}

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

func firstJSONInt64(values map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		if value, ok := jsonInt64(values[key]); ok {
			return value, true
		}
	}

	return 0, false
}

func firstJSONString(values map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := jsonString(values[key]); ok {
			return value, true
		}
	}

	return "", false
}

func jsonString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	default:
		return "", false
	}
}

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

func roundFloat(value float64, places int) float64 {
	scale := math.Pow10(places)
	return math.Round(value*scale) / scale
}

func metav1TimePtr(value time.Time) *metav1.Time {
	result := metav1.NewTime(value.UTC())
	return &result
}
