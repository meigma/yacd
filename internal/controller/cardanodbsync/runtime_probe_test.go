package cardanodbsync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	yacdv1alpha1 "github.com/meigma/yacd/api/v1alpha1"
	ctrlstatus "github.com/meigma/yacd/internal/ctrlkit/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestDefaultDBSyncRuntimeProberMapsNoRows(t *testing.T) {
	prober := defaultRuntimeProber{
		queryDB: func(context.Context, dbSyncRuntimeProbeTarget) (dbSyncDatabaseProgress, error) {
			return dbSyncDatabaseProgress{}, nil
		},
		queryOgmios: func(context.Context, string) (dbSyncNodeTip, error) {
			return dbSyncNodeTip{BlockHeight: ptr.To[int64](10)}, nil
		},
	}

	result, err := prober.Probe(context.Background(), dbSyncRuntimeProbeTarget{OgmiosURL: "ws://ogmios:1337"})

	require.NoError(t, err)
	assert.Equal(t, metav1.ConditionTrue, result.PostgresReady.Status)
	assert.Equal(t, string(conditionReasonPostgresReady), result.PostgresReady.Reason)
	assert.Equal(t, metav1.ConditionFalse, result.Synced.Status)
	assert.Equal(t, string(conditionReasonSyncLagging), result.Synced.Reason)
	require.NotNil(t, result.Sync)
	assert.Nil(t, result.Sync.DBBlockHeight)
	assert.Equal(t, int64(10), *result.Sync.NodeBlockHeight)
}

func TestDefaultDBSyncRuntimeProberMapsLatestIndexedBlock(t *testing.T) {
	prober := defaultRuntimeProber{
		queryDB: func(context.Context, dbSyncRuntimeProbeTarget) (dbSyncDatabaseProgress, error) {
			return dbSyncDatabaseProgress{
				DBBlockHeight: ptr.To[int64](99),
				DBSlotHeight:  ptr.To[int64](1200),
				Epoch:         ptr.To[int64](2),
			}, nil
		},
		queryOgmios: func(context.Context, string) (dbSyncNodeTip, error) {
			return dbSyncNodeTip{BlockHeight: ptr.To[int64](100)}, nil
		},
	}

	result, err := prober.Probe(context.Background(), dbSyncRuntimeProbeTarget{OgmiosURL: "ws://ogmios:1337"})

	require.NoError(t, err)
	assert.Equal(t, metav1.ConditionTrue, result.PostgresReady.Status)
	assert.Equal(t, metav1.ConditionTrue, result.Synced.Status)
	assert.Equal(t, string(conditionReasonSynced), result.Synced.Reason)
	require.NotNil(t, result.Sync)
	assert.Equal(t, int64(99), *result.Sync.DBBlockHeight)
	assert.Equal(t, int64(1200), *result.Sync.DBSlotHeight)
	assert.Equal(t, int64(2), *result.Sync.Epoch)
	assert.Equal(t, int64(100), *result.Sync.NodeBlockHeight)
	assert.Equal(t, int64(1), *result.Sync.LagBlocks)
}

func TestDefaultDBSyncRuntimeProberMapsMissingSchemaAsProgressing(t *testing.T) {
	prober := defaultRuntimeProber{
		queryDB: func(context.Context, dbSyncRuntimeProbeTarget) (dbSyncDatabaseProgress, error) {
			return dbSyncDatabaseProgress{}, errDBSyncSchemaPending
		},
		queryOgmios: func(context.Context, string) (dbSyncNodeTip, error) {
			return dbSyncNodeTip{BlockHeight: ptr.To[int64](7)}, nil
		},
	}

	result, err := prober.Probe(context.Background(), dbSyncRuntimeProbeTarget{OgmiosURL: "ws://ogmios:1337"})

	require.NoError(t, err)
	assert.Equal(t, metav1.ConditionTrue, result.PostgresReady.Status)
	assert.Equal(t, string(conditionReasonPostgresReady), result.PostgresReady.Reason)
	assert.Equal(t, metav1.ConditionFalse, result.Synced.Status)
	assert.Equal(t, string(conditionReasonPostgresSchemaPending), result.Synced.Reason)
	require.NotNil(t, result.Sync)
	assert.Equal(t, int64(7), *result.Sync.NodeBlockHeight)
	assert.Nil(t, result.Sync.DBBlockHeight)
}

func TestDefaultDBSyncRuntimeProberMapsDBConnectionFailure(t *testing.T) {
	queriedOgmios := false
	prober := defaultRuntimeProber{
		queryDB: func(context.Context, dbSyncRuntimeProbeTarget) (dbSyncDatabaseProgress, error) {
			return dbSyncDatabaseProgress{}, errors.New("dial refused")
		},
		queryOgmios: func(context.Context, string) (dbSyncNodeTip, error) {
			queriedOgmios = true
			return dbSyncNodeTip{BlockHeight: ptr.To[int64](7)}, nil
		},
	}

	result, err := prober.Probe(context.Background(), dbSyncRuntimeProbeTarget{OgmiosURL: "ws://ogmios:1337"})

	require.NoError(t, err)
	assert.False(t, queriedOgmios)
	assert.Equal(t, metav1.ConditionFalse, result.PostgresReady.Status)
	assert.Equal(t, string(conditionReasonPostgresUnavailable), result.PostgresReady.Reason)
	assert.Equal(t, metav1.ConditionFalse, result.Synced.Status)
	assert.Equal(t, string(conditionReasonPostgresUnavailable), result.Synced.Reason)
	assert.Nil(t, result.Sync)
}

func TestDefaultDBSyncRuntimeProberProbePostgresDoesNotQueryOgmios(t *testing.T) {
	queriedOgmios := false
	prober := defaultRuntimeProber{
		queryDB: func(context.Context, dbSyncRuntimeProbeTarget) (dbSyncDatabaseProgress, error) {
			return dbSyncDatabaseProgress{DBBlockHeight: ptr.To[int64](12)}, nil
		},
		queryOgmios: func(context.Context, string) (dbSyncNodeTip, error) {
			queriedOgmios = true
			return dbSyncNodeTip{BlockHeight: ptr.To[int64](13)}, nil
		},
	}

	result, err := prober.ProbePostgres(context.Background(), dbSyncRuntimeProbeTarget{OgmiosURL: "ws://ogmios:1337"})

	require.NoError(t, err)
	assert.False(t, queriedOgmios)
	assert.Equal(t, metav1.ConditionTrue, result.PostgresReady.Status)
	assert.Equal(t, string(conditionReasonPostgresReady), result.PostgresReady.Reason)
	assert.Equal(t, metav1.ConditionFalse, result.Synced.Status)
	assert.Equal(t, string(conditionReasonRuntimeProbesPending), result.Synced.Reason)
	require.NotNil(t, result.Sync)
	assert.Equal(t, int64(12), *result.Sync.DBBlockHeight)
	assert.Nil(t, result.Sync.NodeBlockHeight)
}

func TestDefaultDBSyncRuntimeProberMapsOgmiosTipFailure(t *testing.T) {
	prober := defaultRuntimeProber{
		queryDB: func(context.Context, dbSyncRuntimeProbeTarget) (dbSyncDatabaseProgress, error) {
			return dbSyncDatabaseProgress{DBBlockHeight: ptr.To[int64](12)}, nil
		},
		queryOgmios: func(context.Context, string) (dbSyncNodeTip, error) {
			return dbSyncNodeTip{}, errors.New("not ready")
		},
	}

	result, err := prober.Probe(context.Background(), dbSyncRuntimeProbeTarget{OgmiosURL: "ws://ogmios:1337"})

	require.NoError(t, err)
	assert.Equal(t, metav1.ConditionTrue, result.PostgresReady.Status)
	assert.Equal(t, string(conditionReasonPostgresReady), result.PostgresReady.Reason)
	assert.Equal(t, metav1.ConditionFalse, result.Synced.Status)
	assert.Equal(t, string(conditionReasonNodeTipUnavailable), result.Synced.Reason)
	require.NotNil(t, result.Sync)
	assert.Equal(t, int64(12), *result.Sync.DBBlockHeight)
	assert.Nil(t, result.Sync.NodeBlockHeight)
}

func TestDefaultDBSyncRuntimeProberMapsLagThreshold(t *testing.T) {
	tests := []struct {
		name       string
		dbBlock    int64
		nodeBlock  int64
		wantStatus metav1.ConditionStatus
		wantReason conditionReason
		wantLag    int64
	}{
		{
			name:       "within threshold",
			dbBlock:    10,
			nodeBlock:  11,
			wantStatus: metav1.ConditionTrue,
			wantReason: conditionReasonSynced,
			wantLag:    1,
		},
		{
			name:       "outside threshold",
			dbBlock:    10,
			nodeBlock:  12,
			wantStatus: metav1.ConditionFalse,
			wantReason: conditionReasonSyncLagging,
			wantLag:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prober := defaultRuntimeProber{
				queryDB: func(context.Context, dbSyncRuntimeProbeTarget) (dbSyncDatabaseProgress, error) {
					dbBlock := tt.dbBlock
					return dbSyncDatabaseProgress{DBBlockHeight: &dbBlock}, nil
				},
				queryOgmios: func(context.Context, string) (dbSyncNodeTip, error) {
					nodeBlock := tt.nodeBlock
					return dbSyncNodeTip{BlockHeight: &nodeBlock}, nil
				},
			}

			result, err := prober.Probe(context.Background(), dbSyncRuntimeProbeTarget{OgmiosURL: "ws://ogmios:1337"})

			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, result.Synced.Status)
			assert.Equal(t, string(tt.wantReason), result.Synced.Reason)
			require.NotNil(t, result.Sync)
			assert.Equal(t, tt.wantLag, *result.Sync.LagBlocks)
		})
	}
}

func TestDefaultDBSyncRuntimeProberQueriesOgmiosTip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/health", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"lastKnownTip":{"slot":101,"id":"abc","height":77},"connectionStatus":"connected"}`))
	}))
	t.Cleanup(server.Close)
	prober := defaultRuntimeProber{httpClient: server.Client()}

	tip, err := prober.nodeTip(context.Background(), server.URL)

	require.NoError(t, err)
	require.NotNil(t, tip.BlockHeight)
	assert.Equal(t, int64(77), *tip.BlockHeight)
}

func TestDefaultDBSyncRuntimeProberMapsOgmiosHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	prober := defaultRuntimeProber{httpClient: server.Client()}

	_, err := prober.nodeTip(context.Background(), server.URL)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 503")
}

func TestPostgresSchemaPendingMatchesStartupTableErrors(t *testing.T) {
	assert.True(t, postgresSchemaPending(&pgconn.PgError{Code: "42P01"}))
	assert.True(t, postgresSchemaPending(&pgconn.PgError{Code: "3F000"}))
	assert.True(t, postgresSchemaPending(&pgconn.PgError{Code: "42703"}))
	assert.False(t, postgresSchemaPending(&pgconn.PgError{Code: "28P01"}))
	assert.False(t, postgresSchemaPending(errors.New("dial refused")))
}

type fakeCardanoDBSyncRuntimeProber struct {
	result         dbSyncRuntimeProbeResult
	postgresResult *dbSyncRuntimeProbeResult
	err            error
	postgresErr    error
	calls          []dbSyncRuntimeProbeTarget
	postgresCalls  []dbSyncRuntimeProbeTarget
}

func (f *fakeCardanoDBSyncRuntimeProber) ProbePostgres(_ context.Context, target dbSyncRuntimeProbeTarget) (dbSyncRuntimeProbeResult, error) {
	f.postgresCalls = append(f.postgresCalls, target)
	if f.postgresResult != nil {
		return *f.postgresResult, f.postgresErr
	}
	return dbSyncRuntimeProbeResult{
		Sync:          f.result.Sync,
		PostgresReady: f.result.PostgresReady,
		Synced:        syncedCondition(metav1.ConditionFalse, conditionReasonRuntimeProbesPending, "db-sync progress will be probed after workloads are ready"),
	}, f.postgresErr
}

func (f *fakeCardanoDBSyncRuntimeProber) Probe(_ context.Context, target dbSyncRuntimeProbeTarget) (dbSyncRuntimeProbeResult, error) {
	f.calls = append(f.calls, target)
	return f.result, f.err
}

func syncedRuntimeProbeResult(dbBlock int64, nodeBlock int64) dbSyncRuntimeProbeResult {
	lag := nodeBlock - dbBlock
	dbSlot := dbBlock + 1000
	epoch := int64(1)
	return dbSyncRuntimeProbeResult{
		Sync: &yacdv1alpha1.CardanoDBSyncProgressStatus{
			DBBlockHeight:   &dbBlock,
			DBSlotHeight:    &dbSlot,
			Epoch:           &epoch,
			NodeBlockHeight: &nodeBlock,
			LagBlocks:       &lag,
		},
		PostgresReady: ctrlstatus.Condition(string(conditionTypePostgresReady), metav1.ConditionTrue, string(conditionReasonPostgresReady), "Postgres is reachable and db-sync progress query succeeded"),
		Synced:        ctrlstatus.Condition(string(conditionTypeSynced), metav1.ConditionTrue, string(conditionReasonSynced), "db-sync is caught up to the node tip"),
	}
}
