package cardanonetwork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	"github.com/meigma/yacd/internal/cardano/networkartifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestParseOgmiosHealthAcceptsTipFieldVariants(t *testing.T) {
	tests := []struct {
		name            string
		tip             string
		wantBlockHeight int64
		wantHash        string
	}{
		{
			name:            "height and hash",
			tip:             `"height": 42, "hash": "hash-42"`,
			wantBlockHeight: 42,
			wantHash:        "hash-42",
		},
		{
			name:            "blockHeight and id",
			tip:             `"blockHeight": 43, "id": "hash-43"`,
			wantBlockHeight: 43,
			wantHash:        "hash-43",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health, err := parseOgmiosHealth(json.RawMessage(fmt.Sprintf(`{
				"connectionStatus": "connected",
				"lastKnownTip": {"slot": 123, %s},
				"lastTipUpdate": "2026-05-31T12:00:00Z",
				"networkSynchronization": 0.987654
			}`, tt.tip)))

			require.NoError(t, err)
			assert.Equal(t, ogmiosConnectionStatusConnected, health.ConnectionStatus)
			require.NotNil(t, health.Tip)
			assert.Equal(t, int64(123), health.Tip.Slot)
			require.NotNil(t, health.Tip.BlockHeight)
			assert.Equal(t, tt.wantBlockHeight, *health.Tip.BlockHeight)
			assert.Equal(t, tt.wantHash, health.Tip.Hash)
			require.NotNil(t, health.NetworkSynchronization)
			assert.Equal(t, 0.987654, *health.NetworkSynchronization)
		})
	}
}

func TestParseOgmiosHealthAcceptsNullableTipFields(t *testing.T) {
	health, err := parseOgmiosHealth(json.RawMessage(`{
		"connectionStatus": "connected",
		"lastKnownTip": null,
		"lastTipUpdate": null,
		"networkSynchronization": null
	}`))

	require.NoError(t, err)
	assert.Equal(t, ogmiosConnectionStatusConnected, health.ConnectionStatus)
	assert.Nil(t, health.Tip)
	assert.Nil(t, health.LastTipUpdate)
	assert.Nil(t, health.NetworkSynchronization)
}

func TestDefaultCardanoNetworkSyncProberUsesOgmiosHealthEndpoint(t *testing.T) {
	tests := []struct {
		name                 string
		statusCode           int
		connectionStatus     string
		nullableProgress     bool
		wantErr              bool
		wantTip              bool
		wantNetworkSyncValue bool
	}{
		{name: "http 200", statusCode: http.StatusOK, connectionStatus: "connected", wantTip: true, wantNetworkSyncValue: true},
		{name: "http 202", statusCode: http.StatusAccepted, connectionStatus: "connected", wantTip: true, wantNetworkSyncValue: true},
		{name: "http 500 disconnected", statusCode: http.StatusInternalServerError, connectionStatus: "disconnected", nullableProgress: true},
		{name: "http 204", statusCode: http.StatusNoContent, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				w.WriteHeader(tt.statusCode)
				if ogmiosHealthStatusUsable(tt.statusCode) {
					if tt.nullableProgress {
						_, _ = w.Write(fmt.Appendf(nil, `{
								"connectionStatus": %q,
								"lastKnownTip": null,
								"lastTipUpdate": null,
								"networkSynchronization": null
							}`, tt.connectionStatus))
						return
					}
					_, _ = w.Write([]byte(`{
						"connectionStatus": "connected",
						"lastKnownTip": {"slot": 1, "height": 1, "hash": "hash"},
						"lastTipUpdate": "2026-05-31T12:00:00Z",
						"networkSynchronization": 1
					}`))
				}
			}))
			t.Cleanup(server.Close)

			health, err := (defaultCardanoNetworkSyncProber{httpClient: server.Client()}).Probe(context.Background(), wsURLFromHTTPURL(t, server.URL))

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "/health", path)
			assert.Equal(t, tt.connectionStatus, health.ConnectionStatus)
			if tt.wantTip {
				require.NotNil(t, health.Tip)
				assert.Equal(t, int64(1), health.Tip.Slot)
			} else {
				assert.Nil(t, health.Tip)
			}
			if tt.wantNetworkSyncValue {
				assert.NotNil(t, health.NetworkSynchronization)
			} else {
				assert.Nil(t, health.NetworkSynchronization)
			}
		})
	}
}

func TestCardanoNetworkSyncStatusComputesInferredSlotAndLag(t *testing.T) {
	tests := []struct {
		name            string
		genesis         string
		observedAt      time.Time
		tipSlot         int64
		wantInferred    int64
		wantLagSlots    int64
		wantLagSeconds  int64
		wantRoundedSync float64
	}{
		{
			name: "public style one second slots",
			genesis: `{
				"systemStart": "2026-05-31T00:00:00Z",
				"slotLength": 1
			}`,
			observedAt:      time.Date(2026, 5, 31, 0, 20, 0, 0, time.UTC),
			tipSlot:         1190,
			wantInferred:    1200,
			wantLagSlots:    10,
			wantLagSeconds:  10,
			wantRoundedSync: 0.98765,
		},
		{
			name: "local fractional slots",
			genesis: `{
				"systemStart": "2026-05-31T00:00:00Z",
				"slotLength": 0.2
			}`,
			observedAt:      time.Date(2026, 5, 31, 0, 0, 10, 0, time.UTC),
			tipSlot:         47,
			wantInferred:    50,
			wantLagSlots:    3,
			wantLagSeconds:  1,
			wantRoundedSync: 0.98765,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timing, err := parseShelleyGenesisTiming([]byte(tt.genesis))
			require.NoError(t, err)
			networkSynchronization := 0.987654
			status := cardanoNetworkSyncStatusFromHealth(cardanoNetworkOgmiosHealth{
				ConnectionStatus: ogmiosConnectionStatusConnected,
				Tip: &cardanoNetworkOgmiosTip{
					Slot: tt.tipSlot,
				},
				LastTipUpdate:          new(tt.observedAt),
				NetworkSynchronization: &networkSynchronization,
			}, timing, tt.observedAt)

			require.NotNil(t, status.InferredTipSlot)
			assert.Equal(t, tt.wantInferred, *status.InferredTipSlot)
			require.NotNil(t, status.LagSlots)
			assert.Equal(t, tt.wantLagSlots, *status.LagSlots)
			require.NotNil(t, status.LagSeconds)
			assert.Equal(t, tt.wantLagSeconds, *status.LagSeconds)
			require.NotNil(t, status.NetworkSynchronization)
			assert.Equal(t, tt.wantRoundedSync, *status.NetworkSynchronization)
		})
	}
}

func TestPrimaryNodeSyncStatusConditions(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	service := syncTestOgmiosService()
	artifacts := syncTestArtifactsConfigMap()
	caughtUpHealth := cardanoNetworkOgmiosHealth{
		ConnectionStatus: ogmiosConnectionStatusConnected,
		Tip: &cardanoNetworkOgmiosTip{
			Slot: 43195,
		},
		LastTipUpdate: new(now.Add(-time.Minute)),
	}
	syncedByOgmios := 0.99999

	tests := []struct {
		name                  string
		service               *corev1.Service
		artifacts             *corev1.ConfigMap
		artifactsReady        bool
		health                cardanoNetworkOgmiosHealth
		probeErr              error
		wantSync              bool
		wantSynchronized      conditionReason
		wantSynchronizedState metav1.ConditionStatus
		wantProgressing       conditionReason
		wantProgressingState  metav1.ConditionStatus
	}{
		{
			name:                  "synchronized by lag",
			service:               service,
			artifacts:             artifacts,
			artifactsReady:        true,
			health:                caughtUpHealth,
			wantSync:              true,
			wantSynchronized:      conditionReasonNodeSynchronized,
			wantSynchronizedState: metav1.ConditionTrue,
			wantProgressing:       conditionReasonNodeSynchronized,
			wantProgressingState:  metav1.ConditionTrue,
		},
		{
			name:           "synchronized by Ogmios synchronization estimate",
			service:        service,
			artifacts:      artifacts,
			artifactsReady: true,
			health: cardanoNetworkOgmiosHealth{
				ConnectionStatus:       ogmiosConnectionStatusConnected,
				Tip:                    &cardanoNetworkOgmiosTip{Slot: 100},
				LastTipUpdate:          new(now.Add(-11 * time.Minute)),
				NetworkSynchronization: &syncedByOgmios,
			},
			wantSync:              true,
			wantSynchronized:      conditionReasonNodeSynchronized,
			wantSynchronizedState: metav1.ConditionTrue,
			wantProgressing:       conditionReasonNodeSynchronized,
			wantProgressingState:  metav1.ConditionTrue,
		},
		{
			name:           "behind but advancing",
			service:        service,
			artifacts:      artifacts,
			artifactsReady: true,
			health: cardanoNetworkOgmiosHealth{
				ConnectionStatus: ogmiosConnectionStatusConnected,
				Tip:              &cardanoNetworkOgmiosTip{Slot: 42000},
				LastTipUpdate:    new(now.Add(-time.Minute)),
			},
			wantSync:              true,
			wantSynchronized:      conditionReasonNodeCatchingUp,
			wantSynchronizedState: metav1.ConditionFalse,
			wantProgressing:       conditionReasonNodeCatchingUp,
			wantProgressingState:  metav1.ConditionTrue,
		},
		{
			name:           "connected without tip yet",
			service:        service,
			artifacts:      artifacts,
			artifactsReady: true,
			health: cardanoNetworkOgmiosHealth{
				ConnectionStatus: ogmiosConnectionStatusConnected,
			},
			wantSync:              true,
			wantSynchronized:      conditionReasonNodeCatchingUp,
			wantSynchronizedState: metav1.ConditionFalse,
			wantProgressing:       conditionReasonNodeCatchingUp,
			wantProgressingState:  metav1.ConditionTrue,
		},
		{
			name:           "behind and stalled",
			service:        service,
			artifacts:      artifacts,
			artifactsReady: true,
			health: cardanoNetworkOgmiosHealth{
				ConnectionStatus: ogmiosConnectionStatusConnected,
				Tip:              &cardanoNetworkOgmiosTip{Slot: 42000},
				LastTipUpdate:    new(now.Add(-11 * time.Minute)),
			},
			wantSync:              true,
			wantSynchronized:      conditionReasonNodeSyncStalled,
			wantSynchronizedState: metav1.ConditionFalse,
			wantProgressing:       conditionReasonNodeSyncStalled,
			wantProgressingState:  metav1.ConditionFalse,
		},
		{
			name:           "disconnected",
			service:        service,
			artifacts:      artifacts,
			artifactsReady: true,
			health: cardanoNetworkOgmiosHealth{
				ConnectionStatus: "disconnected",
				Tip:              &cardanoNetworkOgmiosTip{Slot: 43195},
				LastTipUpdate:    new(now.Add(-time.Minute)),
			},
			wantSync:              true,
			wantSynchronized:      conditionReasonOgmiosDisconnected,
			wantSynchronizedState: metav1.ConditionFalse,
			wantProgressing:       conditionReasonOgmiosDisconnected,
			wantProgressingState:  metav1.ConditionFalse,
		},
		{
			name:                  "missing Ogmios",
			service:               nil,
			artifacts:             artifacts,
			artifactsReady:        true,
			wantSynchronized:      conditionReasonOgmiosDisabled,
			wantSynchronizedState: metav1.ConditionFalse,
			wantProgressing:       conditionReasonOgmiosDisabled,
			wantProgressingState:  metav1.ConditionFalse,
		},
		{
			name:                  "missing timing",
			service:               service,
			artifacts:             syncTestArtifactsConfigMapWithShelley("not json"),
			artifactsReady:        true,
			wantSynchronized:      conditionReasonNetworkTimingUnavailable,
			wantSynchronizedState: metav1.ConditionFalse,
			wantProgressing:       conditionReasonNetworkTimingUnavailable,
			wantProgressingState:  metav1.ConditionFalse,
		},
		{
			name:                  "malformed health",
			service:               service,
			artifacts:             artifacts,
			artifactsReady:        true,
			probeErr:              errors.New("bad health"),
			wantSynchronized:      conditionReasonOgmiosHealthUnavailable,
			wantSynchronizedState: metav1.ConditionFalse,
			wantProgressing:       conditionReasonOgmiosHealthUnavailable,
			wantProgressingState:  metav1.ConditionFalse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network := localCardanoNetwork("sync-condition")
			reconciler := newTestReconciler(t, network)
			reconciler.Now = func() time.Time { return now }
			probeCalled := false
			reconciler.syncProberOverride = cardanoNetworkSyncProberFunc(func(context.Context, string) (cardanoNetworkOgmiosHealth, error) {
				probeCalled = true
				return tt.health, tt.probeErr
			})

			syncStatus, synchronized, progressing := reconciler.primaryNodeSyncStatusConditions(context.Background(), network, tt.service, tt.artifacts, tt.artifactsReady, "artifacts pending")

			if tt.wantSync {
				require.NotNil(t, syncStatus)
			} else {
				assert.Nil(t, syncStatus)
			}
			if tt.service == nil || tt.artifacts == nil || tt.probeErr != nil || tt.artifacts.Data[networkartifacts.ShelleyGenesisKey] == "not json" {
				assert.Equal(t, tt.probeErr != nil, probeCalled)
			}
			assert.Equal(t, tt.wantSynchronizedState, synchronized.Status)
			assert.Equal(t, string(tt.wantSynchronized), synchronized.Reason)
			assert.Equal(t, tt.wantProgressingState, progressing.Status)
			assert.Equal(t, string(tt.wantProgressing), progressing.Reason)
		})
	}
}

func TestCardanoNetworkReconcilerReconcilePublishesNodeSyncStatusWhenCaughtUp(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	network := localCardanoNetwork("node-sync-caught-up")
	reconciler := newTestReconciler(t, network)
	reconciler.Now = func() time.Time { return now }
	reconciler.syncProberOverride = cardanoNetworkSyncProberFunc(func(_ context.Context, ogmiosURL string) (cardanoNetworkOgmiosHealth, error) {
		assert.Contains(t, ogmiosURL, "ws://node-sync-caught-up-ogmios.default.svc.cluster.local")
		networkSynchronization := 0.999994
		return cardanoNetworkOgmiosHealth{
			ConnectionStatus: ogmiosConnectionStatusConnected,
			Tip: &cardanoNetworkOgmiosTip{
				Slot: 43195,
			},
			LastTipUpdate:          new(now.Add(-time.Minute)),
			NetworkSynchronization: &networkSynchronization,
		}, nil
	})

	readyNetworkForSyncTest(t, ctx, reconciler, network)
	result, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{RequeueAfter: nodeSyncProbeRequeueAfter}, result)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeSynchronized, metav1.ConditionTrue, conditionReasonNodeSynchronized)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeProgressing, metav1.ConditionTrue, conditionReasonNodeSynchronized)
	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Sync)
	assert.Equal(t, nodeSyncSourceOgmios, current.Status.Sync.Source)
	require.NotNil(t, current.Status.Sync.NetworkSynchronization)
	assert.Equal(t, 0.99999, *current.Status.Sync.NetworkSynchronization)
	require.NotNil(t, current.Status.Sync.LagSlots)
	assert.Equal(t, int64(5), *current.Status.Sync.LagSlots)
}

func TestCardanoNetworkReconcilerReconcileReportsNodeCatchingUp(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	network := localCardanoNetwork("node-sync-catching-up")
	reconciler := newTestReconciler(t, network)
	reconciler.Now = func() time.Time { return now }
	reconciler.syncProberOverride = cardanoNetworkSyncProberFunc(func(context.Context, string) (cardanoNetworkOgmiosHealth, error) {
		return cardanoNetworkOgmiosHealth{
			ConnectionStatus: ogmiosConnectionStatusConnected,
			Tip:              &cardanoNetworkOgmiosTip{Slot: 42000},
			LastTipUpdate:    new(now.Add(-time.Minute)),
		}, nil
	})

	readyNetworkForSyncTest(t, ctx, reconciler, network)
	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, network, conditionTypeReady, metav1.ConditionTrue, conditionReasonReady)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeSynchronized, metav1.ConditionFalse, conditionReasonNodeCatchingUp)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeProgressing, metav1.ConditionTrue, conditionReasonNodeCatchingUp)
	current := requireNetwork(t, ctx, reconciler, network)
	require.NotNil(t, current.Status.Sync)
	require.NotNil(t, current.Status.Sync.LagSlots)
	assert.Equal(t, int64(1200), *current.Status.Sync.LagSlots)
	require.NotNil(t, current.Status.Sync.LagSeconds)
	assert.Equal(t, int64(1200), *current.Status.Sync.LagSeconds)
}

func TestCardanoNetworkReconcilerReconcileReportsNodeSyncStalled(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	network := localCardanoNetwork("node-sync-stalled")
	reconciler := newTestReconciler(t, network)
	reconciler.Now = func() time.Time { return now }
	reconciler.syncProberOverride = cardanoNetworkSyncProberFunc(func(context.Context, string) (cardanoNetworkOgmiosHealth, error) {
		return cardanoNetworkOgmiosHealth{
			ConnectionStatus: ogmiosConnectionStatusConnected,
			Tip:              &cardanoNetworkOgmiosTip{Slot: 42000},
			LastTipUpdate:    new(now.Add(-11 * time.Minute)),
		}, nil
	})

	readyNetworkForSyncTest(t, ctx, reconciler, network)
	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeSynchronized, metav1.ConditionFalse, conditionReasonNodeSyncStalled)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeProgressing, metav1.ConditionFalse, conditionReasonNodeSyncStalled)
}

func TestCardanoNetworkReconcilerReconcileClearsNodeSyncStatusOnProbeFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	network := localCardanoNetwork("node-sync-probe-failure")
	network.Status.Sync = &yacdv1alpha1.CardanoNetworkSyncStatus{
		Source: nodeSyncSourceOgmios,
	}
	reconciler := newTestReconciler(t, network)
	reconciler.Now = func() time.Time { return now }
	reconciler.syncProberOverride = cardanoNetworkSyncProberFunc(func(context.Context, string) (cardanoNetworkOgmiosHealth, error) {
		return cardanoNetworkOgmiosHealth{}, errors.New("connection refused")
	})

	readyNetworkForSyncTest(t, ctx, reconciler, network)
	current := requireNetwork(t, ctx, reconciler, network)
	current.Status.Sync = &yacdv1alpha1.CardanoNetworkSyncStatus{Source: nodeSyncSourceOgmios}
	storeNetworkStatus(t, ctx, reconciler, current)
	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeSynchronized, metav1.ConditionFalse, conditionReasonOgmiosHealthUnavailable)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeProgressing, metav1.ConditionFalse, conditionReasonOgmiosHealthUnavailable)
	current = requireNetwork(t, ctx, reconciler, network)
	assert.Nil(t, current.Status.Sync)
}

func TestCardanoNetworkReconcilerReconcileReportsMissingNetworkTiming(t *testing.T) {
	ctx := context.Background()
	network := localCardanoNetwork("node-sync-missing-timing")
	network.Status.Sync = &yacdv1alpha1.CardanoNetworkSyncStatus{
		Source: nodeSyncSourceOgmios,
	}
	reconciler := newTestReconciler(t, network)
	reconciler.syncProberOverride = cardanoNetworkSyncProberFunc(func(context.Context, string) (cardanoNetworkOgmiosHealth, error) {
		require.FailNow(t, "sync probe should not run without valid timing")
		return cardanoNetworkOgmiosHealth{}, nil
	})

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	configMap := publishNetworkArtifacts(t, ctx, reconciler, network)
	configMap.Data[networkartifacts.ShelleyGenesisKey] = "not json"
	require.NoError(t, reconciler.Update(ctx, configMap))
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName, ogmiosContainerName, kupoContainerName)
	current := requireNetwork(t, ctx, reconciler, network)
	current.Status.Sync = &yacdv1alpha1.CardanoNetworkSyncStatus{Source: nodeSyncSourceOgmios}
	storeNetworkStatus(t, ctx, reconciler, current)

	_, err = reconciler.Reconcile(ctx, reconcileRequestFor(network))

	require.NoError(t, err)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeSynchronized, metav1.ConditionFalse, conditionReasonNetworkTimingUnavailable)
	assertCondition(t, ctx, reconciler, network, conditionTypeNodeProgressing, metav1.ConditionFalse, conditionReasonNetworkTimingUnavailable)
	current = requireNetwork(t, ctx, reconciler, network)
	assert.Nil(t, current.Status.Sync)
}

func readyNetworkForSyncTest(
	t *testing.T,
	ctx context.Context,
	reconciler *CardanoNetworkReconciler,
	network *yacdv1alpha1.CardanoNetwork,
) {
	t.Helper()

	_, err := reconciler.Reconcile(ctx, reconcileRequestFor(network))
	require.NoError(t, err)
	publishNetworkArtifacts(t, ctx, reconciler, network)
	deployment := requirePrimaryDeployment(t, ctx, reconciler, network)
	markPrimaryDeploymentAvailable(t, ctx, reconciler, deployment)
	markPrimaryPodContainersReady(t, ctx, reconciler, network, cardanoNodeContainerName, ogmiosContainerName, kupoContainerName)
}

func syncTestOgmiosService() *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sync-condition-ogmios",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Port: defaultOgmiosPort,
			}},
		},
	}
}

func syncTestArtifactsConfigMap() *corev1.ConfigMap {
	return syncTestArtifactsConfigMapWithShelley(`{
		"systemStart": "2026-05-31T00:00:00Z",
		"slotLength": 1
	}`)
}

func syncTestArtifactsConfigMapWithShelley(shelleyGenesis string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sync-condition-network-artifacts",
			Namespace: "default",
		},
		Data: map[string]string{
			networkartifacts.ShelleyGenesisKey: shelleyGenesis,
		},
	}
}

func wsURLFromHTTPURL(t *testing.T, rawURL string) string {
	t.Helper()

	require.Contains(t, rawURL, "http://")
	return "ws://" + rawURL[len("http://"):]
}

func syncedNodeSyncProber() cardanoNetworkSyncProber {
	networkSynchronization := 1.0
	return cardanoNetworkSyncProberFunc(func(context.Context, string) (cardanoNetworkOgmiosHealth, error) {
		return cardanoNetworkOgmiosHealth{
			ConnectionStatus:       ogmiosConnectionStatusConnected,
			Tip:                    &cardanoNetworkOgmiosTip{Slot: 0},
			LastTipUpdate:          new(time.Now()),
			NetworkSynchronization: &networkSynchronization,
		}, nil
	})
}
